//nolint:ireturn // Returning interface is intentional for DI
package collect

import (
	"context"
	"database/sql"
	"math/big"
	"strings"
	"sync"
	"time"

	"github/chapool/go-wallet/internal/models"
	"github/chapool/go-wallet/internal/wallet/chain"
	"github/chapool/go-wallet/internal/wallet/hotwallet"
	"github/chapool/go-wallet/internal/wallet/scan"
	"github/chapool/go-wallet/internal/wallet/signer"

	"github.com/aarondl/null/v8"
	"github.com/aarondl/sqlboiler/v4/boil"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

const (
	collectGasLimitNative              uint64 = 21000
	defaultEIP1559Multiplier           int64  = 2
	defaultERC20CollectGasLimit        uint64 = 120000
	minCollectAmountWeiValue                  = 5_000_000_000_000_000 // 0.005 native token
	minBalanceWithGasBuffValue                = 100_000_000_000_000   // 0.0001 native token
	receiptPollIntervalSeconds                = 3
	receiptWaitTimeoutMinutes                 = 2
	defaultMinERC20CollectAmountString        = "1"                // 默认收集 1 个 token（可按 token 配置覆盖）
	nativeTopUpBufferWeiValue                 = 50_000_000_000_000 // 0.00005 native token
	abiPaddedAddressLength                    = 32
)

var (
	minCollectAmountWei   = big.NewInt(minCollectAmountWeiValue)
	receiptPollInterval   = receiptPollIntervalSeconds * time.Second
	receiptWaitTimeout    = receiptWaitTimeoutMinutes * time.Minute
	minBalanceWithGasBuff = big.NewInt(minBalanceWithGasBuffValue)
	nativeTopUpBufferWei  = big.NewInt(nativeTopUpBufferWeiValue)
	erc20TransferMethodID = common.FromHex("a9059cbb")
)

// Service defines the contract for automatic and manual fund collection.
type Service interface {
	// StartAutoCollect launches a background task that periodically scans all chains.
	StartAutoCollect(ctx context.Context, interval time.Duration)
	// CollectForChain runs a collection cycle for a specific chain.
	CollectForChain(ctx context.Context, chainID int) error
	// CollectWallet triggers collection for a specific wallet by ID.
	CollectWallet(ctx context.Context, walletID string) error
}

type service struct {
	db               *sql.DB
	chainService     chain.Service
	scanService      scan.Service
	hotWalletService hotwallet.Service
	signerService    signer.Service
	collecting       sync.Map
}

// NewService creates a new collect service.
//
//nolint:ireturn // Returning interface is intentional for DI
func NewService(
	db *sql.DB,
	chainService chain.Service,
	scanService scan.Service,
	hotWalletService hotwallet.Service,
	signerService signer.Service,
) Service {
	return &service{
		db:               db,
		chainService:     chainService,
		scanService:      scanService,
		hotWalletService: hotWalletService,
		signerService:    signerService,
	}
}

// StartAutoCollect schedules automatic collection using the provided interval.
func (s *service) StartAutoCollect(ctx context.Context, interval time.Duration) {
	log.Info().
		Dur("interval", interval).
		Msg("Starting auto collect scheduler")

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		s.runAutoCollect(ctx) // Run immediately on start

		for {
			select {
			case <-ctx.Done():
				log.Info().Msg("Auto collect scheduler stopped")
				return
			case <-ticker.C:
				s.runAutoCollect(ctx)
			}
		}
	}()
}

// CollectForChain runs a collection cycle for all user wallets on a given chain.
func (s *service) CollectForChain(ctx context.Context, chainID int) error {
	wallets, err := models.Wallets(
		models.WalletWhere.ChainID.EQ(chainID),
		models.WalletWhere.WalletType.EQ("user"),
	).All(ctx, s.db)
	if err != nil {
		return errors.Wrap(err, "failed to load user wallets for chain")
	}

	if len(wallets) == 0 {
		return nil
	}

	tokens, err := s.getActiveTokensForChain(ctx, chainID)
	if err != nil {
		return errors.Wrap(err, "failed to load active tokens for chain")
	}

	hotWallet, err := s.hotWalletService.GetHotWallet(ctx, chainID)
	if err != nil {
		return errors.Wrap(err, "failed to load target hot wallet")
	}

	for _, wallet := range wallets {
		select {
		case <-ctx.Done():
			return errors.Wrap(ctx.Err(), "context canceled during collection")
		default:
		}

		if err := s.collectWalletERC20(ctx, wallet, hotWallet, tokens); err != nil {
			log.Error().
				Err(err).
				Str("wallet_id", wallet.ID).
				Str("address", wallet.Address).
				Int("chain_id", chainID).
				Msg("CollectService: wallet ERC20 collection failed")
		}

		if err := s.collectWalletNative(ctx, wallet, hotWallet); err != nil {
			log.Error().
				Err(err).
				Str("wallet_id", wallet.ID).
				Str("address", wallet.Address).
				Int("chain_id", chainID).
				Msg("CollectService: wallet collection failed")
		}
	}

	return nil
}

// CollectWallet triggers collection for a specific wallet ID.
func (s *service) CollectWallet(ctx context.Context, walletID string) error {
	wallet, err := models.Wallets(models.WalletWhere.ID.EQ(walletID)).One(ctx, s.db)
	if err != nil {
		return errors.Wrap(err, "failed to load wallet")
	}

	if wallet.WalletType != "user" {
		return errors.New("only user wallets support collection")
	}

	hotWallet, err := s.hotWalletService.GetHotWallet(ctx, wallet.ChainID)
	if err != nil {
		return errors.Wrap(err, "failed to load target hot wallet")
	}

	return s.collectWalletNative(ctx, wallet, hotWallet)
}

func (s *service) runAutoCollect(ctx context.Context) {
	chains, err := s.chainService.GetActiveChains(ctx)
	if err != nil {
		log.Error().Err(err).Msg("CollectService: failed to load active chains")
		return
	}

	for _, ch := range chains {
		if err := s.CollectForChain(ctx, ch.ChainID); err != nil {
			log.Error().
				Int("chain_id", ch.ChainID).
				Err(err).
				Msg("CollectService: collect cycle failed")
		}
	}
}

func (s *service) collectWalletNative(ctx context.Context, wallet *models.Wallet, hotWallet *models.Wallet) error {
	if wallet == nil || hotWallet == nil {
		return errors.New("wallet or hot wallet is nil")
	}

	lockKey := wallet.ID
	if _, loaded := s.collecting.LoadOrStore(lockKey, struct{}{}); loaded {
		log.Debug().
			Str("wallet_id", wallet.ID).
			Msg("CollectService: wallet is already collecting, skipping")
		return nil
	}
	defer s.collecting.Delete(lockKey)

	client, err := s.scanService.GetClient(ctx, wallet.ChainID)
	if err != nil {
		return errors.Wrap(err, "failed to get RPC client")
	}

	fromAddr := common.HexToAddress(strings.ToLower(wallet.Address))
	toAddr := common.HexToAddress(strings.ToLower(hotWallet.Address))

	balanceWei, err := client.BalanceAt(ctx, fromAddr)
	if err != nil {
		return errors.Wrap(err, "failed to query wallet balance")
	}

	if balanceWei.Cmp(minCollectAmountWei) < 0 {
		log.Debug().
			Str("wallet_id", wallet.ID).
			Str("address", wallet.Address).
			Str("balance_wei", balanceWei.String()).
			Msg("CollectService: balance below minimum threshold, skip")
		return nil
	}

	tipCap, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to suggest gas tip cap")
	}

	latestBlock, err := client.GetBlockByNumber(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to fetch latest block header")
	}

	baseFee := latestBlock.BaseFee()
	if baseFee == nil {
		baseFee = big.NewInt(0)
	}

	maxFee := new(big.Int).Add(
		new(big.Int).Mul(baseFee, big.NewInt(defaultEIP1559Multiplier)),
		tipCap,
	)

	gasLimit := collectGasLimitNative
	gasFee := new(big.Int).Mul(maxFee, big.NewInt(int64(gasLimit)))

	if balanceWei.Cmp(gasFee) <= 0 {
		log.Debug().
			Str("wallet_id", wallet.ID).
			Str("address", wallet.Address).
			Str("balance_wei", balanceWei.String()).
			Str("gas_fee_wei", gasFee.String()).
			Msg("CollectService: insufficient balance to cover gas")
		return nil
	}

	transferAmount := new(big.Int).Sub(balanceWei, gasFee)
	if transferAmount.Cmp(minCollectAmountWei) < 0 || transferAmount.Cmp(minBalanceWithGasBuff) <= 0 {
		log.Debug().
			Str("wallet_id", wallet.ID).
			Str("address", wallet.Address).
			Str("transfer_amount", transferAmount.String()).
			Msg("CollectService: net transfer below minimum threshold")
		return nil
	}

	nonce, err := client.PendingNonceAt(ctx, fromAddr)
	if err != nil {
		return errors.Wrap(err, "failed to fetch pending nonce")
	}

	signReq := &signer.SignEVMRequest{
		ChainID:              int64(wallet.ChainID),
		To:                   toAddr.Hex(),
		Value:                transferAmount.String(),
		GasLimit:             gasLimit,
		MaxFeePerGas:         maxFee.String(),
		MaxPriorityFeePerGas: tipCap.String(),
		Nonce:                nonce,
		FromAddress:          fromAddr.Hex(),
		DerivationPath:       wallet.DerivationPath,
	}

	signResp, err := s.signerService.SignEVMTransaction(ctx, signReq)
	if err != nil {
		return errors.Wrap(err, "failed to sign collect transaction")
	}

	txObj := new(types.Transaction)
	if err := txObj.UnmarshalBinary(signResp.RawTransaction); err != nil {
		return errors.Wrap(err, "failed to decode signed transaction")
	}

	if err := client.SendTransaction(ctx, txObj); err != nil {
		return errors.Wrap(err, "failed to broadcast collect transaction")
	}

	receipt, err := s.waitForReceipt(ctx, client, txObj.Hash())
	if err != nil {
		return errors.Wrap(err, "failed while waiting for collect receipt")
	}

	status := models.TransactionStatusConfirmed
	if receipt.Status != types.ReceiptStatusSuccessful {
		status = models.TransactionStatusFailed
	}

	if err := s.insertCollectTransaction(ctx, wallet, hotWallet, transferAmount, txObj, receipt, status, null.String{}); err != nil {
		return errors.Wrap(err, "failed to insert collect transaction record")
	}

	log.Info().
		Str("wallet_id", wallet.ID).
		Str("user_id", wallet.UserID).
		Str("from", fromAddr.Hex()).
		Str("to", toAddr.Hex()).
		Str("tx_hash", txObj.Hash().Hex()).
		Str("amount_wei", transferAmount.String()).
		Msg("CollectService: collected native funds to hot wallet")

	return nil
}

func (s *service) collectWalletERC20(
	ctx context.Context,
	wallet *models.Wallet,
	hotWallet *models.Wallet,
	tokens []*models.Token,
) error {
	if len(tokens) == 0 {
		return nil
	}

	fromAddr := common.HexToAddress(strings.ToLower(wallet.Address))
	toAddr := common.HexToAddress(strings.ToLower(hotWallet.Address))

	client, err := s.scanService.GetClient(ctx, wallet.ChainID)
	if err != nil {
		return errors.Wrap(err, "failed to get RPC client")
	}

	nativeBalance, err := client.BalanceAt(ctx, fromAddr)
	if err != nil {
		return errors.Wrap(err, "failed to fetch native balance for ERC20 collect")
	}

	tipCap, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to suggest gas tip cap for ERC20 collect")
	}

	latestBlock, err := client.GetBlockByNumber(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to fetch latest block header for ERC20 collect")
	}

	baseFee := latestBlock.BaseFee()
	if baseFee == nil {
		baseFee = big.NewInt(0)
	}

	maxFee := new(big.Int).Add(
		new(big.Int).Mul(baseFee, big.NewInt(defaultEIP1559Multiplier)),
		tipCap,
	)

	gasFee := new(big.Int).Mul(maxFee, big.NewInt(int64(defaultERC20CollectGasLimit)))

	for _, token := range tokens {
		if token == nil || token.IsNative || !token.TokenAddress.Valid || token.TokenAddress.String == "" {
			continue
		}

		tokenAddressStr := strings.ToLower(token.TokenAddress.String)
		tokenAddr := common.HexToAddress(tokenAddressStr)

		tokenBalance, err := client.TokenBalance(ctx, tokenAddr, fromAddr)
		if err != nil {
			log.Warn().
				Err(err).
				Str("wallet_id", wallet.ID).
				Str("token_address", tokenAddressStr).
				Msg("CollectService: failed to query ERC20 balance")
			continue
		}

		if tokenBalance.Sign() <= 0 {
			continue
		}

		minCollectAmount := getTokenMinCollectAmount(token)
		if tokenBalance.Cmp(minCollectAmount) < 0 {
			continue
		}

		if nativeBalance.Cmp(gasFee) <= 0 {
			requiredBalance := new(big.Int).Add(gasFee, minBalanceWithGasBuff)
			updatedBalance, err := s.ensureNativeGas(ctx, wallet, hotWallet, client, nativeBalance, requiredBalance)
			if err != nil {
				log.Warn().
					Err(err).
					Str("wallet_id", wallet.ID).
					Str("token_address", tokenAddressStr).
					Msg("CollectService: failed to top up native balance for ERC20 gas fee")
				continue
			}
			nativeBalance = updatedBalance
		}

		data := make([]byte, 0, len(erc20TransferMethodID)+abiPaddedAddressLength*2)
		data = append(data, erc20TransferMethodID...)
		data = append(data, common.LeftPadBytes(toAddr.Bytes(), abiPaddedAddressLength)...)
		data = append(data, common.LeftPadBytes(tokenBalance.Bytes(), abiPaddedAddressLength)...)

		nonce, err := client.PendingNonceAt(ctx, fromAddr)
		if err != nil {
			log.Warn().
				Err(err).
				Str("wallet_id", wallet.ID).
				Str("token_address", tokenAddressStr).
				Msg("CollectService: failed to fetch nonce for ERC20 collect")
			continue
		}

		signReq := &signer.SignEVMRequest{
			ChainID:              int64(wallet.ChainID),
			To:                   tokenAddr.Hex(),
			Value:                "0",
			GasLimit:             defaultERC20CollectGasLimit,
			MaxFeePerGas:         maxFee.String(),
			MaxPriorityFeePerGas: tipCap.String(),
			Nonce:                nonce,
			Data:                 data,
			FromAddress:          fromAddr.Hex(),
			DerivationPath:       wallet.DerivationPath,
		}

		signResp, err := s.signerService.SignEVMTransaction(ctx, signReq)
		if err != nil {
			log.Warn().
				Err(err).
				Str("wallet_id", wallet.ID).
				Str("token_address", tokenAddressStr).
				Msg("CollectService: failed to sign ERC20 collect transaction")
			continue
		}

		txObj := new(types.Transaction)
		if err := txObj.UnmarshalBinary(signResp.RawTransaction); err != nil {
			log.Warn().
				Err(err).
				Str("wallet_id", wallet.ID).
				Str("token_address", tokenAddressStr).
				Msg("CollectService: failed to decode ERC20 collect transaction")
			continue
		}

		if err := client.SendTransaction(ctx, txObj); err != nil {
			log.Warn().
				Err(err).
				Str("wallet_id", wallet.ID).
				Str("token_address", tokenAddressStr).
				Msg("CollectService: failed to broadcast ERC20 collect transaction")
			continue
		}

		receipt, err := s.waitForReceipt(ctx, client, txObj.Hash())
		if err != nil {
			log.Warn().
				Err(err).
				Str("wallet_id", wallet.ID).
				Str("token_address", tokenAddressStr).
				Msg("CollectService: failed while waiting for ERC20 collect receipt")
			continue
		}

		status := models.TransactionStatusConfirmed
		if receipt.Status != types.ReceiptStatusSuccessful {
			status = models.TransactionStatusFailed
		}

		if err := s.insertCollectTransaction(
			ctx,
			wallet,
			hotWallet,
			tokenBalance,
			txObj,
			receipt,
			status,
			null.StringFrom(tokenAddressStr),
		); err != nil {
			log.Error().
				Err(err).
				Str("wallet_id", wallet.ID).
				Str("token_address", tokenAddressStr).
				Msg("CollectService: failed to insert ERC20 collect transaction")
			continue
		}

		nativeBalance.Sub(nativeBalance, gasFee)
		log.Info().
			Str("wallet_id", wallet.ID).
			Str("token_address", tokenAddressStr).
			Str("amount", tokenBalance.String()).
			Str("tx_hash", txObj.Hash().Hex()).
			Msg("CollectService: collected ERC20 funds to hot wallet")
	}

	return nil
}

func (s *service) ensureNativeGas(
	ctx context.Context,
	wallet *models.Wallet,
	hotWallet *models.Wallet,
	client *scan.RPCClient,
	currentBalance *big.Int,
	requiredBalance *big.Int,
) (*big.Int, error) {
	if currentBalance.Cmp(requiredBalance) >= 0 {
		return currentBalance, nil
	}

	shortfall := new(big.Int).Sub(requiredBalance, currentBalance)
	shortfall.Add(shortfall, nativeTopUpBufferWei)

	fromAddr := common.HexToAddress(strings.ToLower(hotWallet.Address))
	toAddr := common.HexToAddress(strings.ToLower(wallet.Address))

	hotBalance, err := client.BalanceAt(ctx, fromAddr)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query hot wallet balance for top-up")
	}

	tipCap, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to suggest gas tip cap for top-up")
	}

	latestBlock, err := client.GetBlockByNumber(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch latest block header for top-up")
	}

	baseFee := latestBlock.BaseFee()
	if baseFee == nil {
		baseFee = big.NewInt(0)
	}

	maxFee := new(big.Int).Add(
		new(big.Int).Mul(baseFee, big.NewInt(defaultEIP1559Multiplier)),
		tipCap,
	)

	gasFee := new(big.Int).Mul(maxFee, big.NewInt(int64(collectGasLimitNative)))
	totalCost := new(big.Int).Add(shortfall, gasFee)

	if hotBalance.Cmp(totalCost) <= 0 {
		return nil, errors.New("hot wallet does not have enough native balance for top-up")
	}

	nonce, err := client.PendingNonceAt(ctx, fromAddr)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch hot wallet nonce for top-up")
	}

	signReq := &signer.SignEVMRequest{
		ChainID:              int64(wallet.ChainID),
		To:                   toAddr.Hex(),
		Value:                shortfall.String(),
		GasLimit:             collectGasLimitNative,
		MaxFeePerGas:         maxFee.String(),
		MaxPriorityFeePerGas: tipCap.String(),
		Nonce:                nonce,
		FromAddress:          fromAddr.Hex(),
		DerivationPath:       hotWallet.DerivationPath,
	}

	signResp, err := s.signerService.SignEVMTransaction(ctx, signReq)
	if err != nil {
		return nil, errors.Wrap(err, "failed to sign native top-up transaction")
	}

	txObj := new(types.Transaction)
	if err := txObj.UnmarshalBinary(signResp.RawTransaction); err != nil {
		return nil, errors.Wrap(err, "failed to decode native top-up transaction")
	}

	if err := client.SendTransaction(ctx, txObj); err != nil {
		return nil, errors.Wrap(err, "failed to broadcast native top-up transaction")
	}

	receipt, err := s.waitForReceipt(ctx, client, txObj.Hash())
	if err != nil {
		return nil, errors.Wrap(err, "failed while waiting for native top-up receipt")
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		return nil, errors.New("native top-up transaction failed")
	}

	log.Info().
		Str("wallet_id", wallet.ID).
		Str("hot_wallet_id", hotWallet.ID).
		Str("tx_hash", txObj.Hash().Hex()).
		Str("topup_amount", shortfall.String()).
		Msg("CollectService: topped up native gas for ERC20 collect")

	return new(big.Int).Add(currentBalance, shortfall), nil
}

func (s *service) insertCollectTransaction(
	ctx context.Context,
	fromWallet *models.Wallet,
	toWallet *models.Wallet,
	amountWei *big.Int,
	tx *types.Transaction,
	receipt *types.Receipt,
	status string,
	tokenAddress null.String,
) error {
	transaction := &models.Transaction{
		ChainID:           fromWallet.ChainID,
		BlockHash:         receipt.BlockHash.Hex(),
		BlockNo:           receipt.BlockNumber.Int64(),
		TXHash:            strings.ToLower(tx.Hash().Hex()),
		FromAddr:          strings.ToLower(fromWallet.Address),
		ToAddr:            strings.ToLower(toWallet.Address),
		TokenAddr:         tokenAddress,
		Amount:            amountWei.String(),
		Type:              models.TransactionTypeCollect,
		Status:            status,
		ConfirmationCount: null.IntFrom(0),
	}

	if err := transaction.Insert(ctx, s.db, boil.Infer()); err != nil {
		return errors.Wrap(err, "failed to insert collect transaction")
	}

	return nil
}

func (s *service) waitForReceipt(ctx context.Context, client *scan.RPCClient, txHash common.Hash) (*types.Receipt, error) {
	localCtx, cancel := context.WithTimeout(ctx, receiptWaitTimeout)
	defer cancel()

	ticker := time.NewTicker(receiptPollInterval)
	defer ticker.Stop()

	for {
		receipt, err := client.GetTransactionReceipt(localCtx, txHash)
		if err == nil {
			return receipt, nil
		}

		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, err
		}

		if !errors.Is(err, ethereum.NotFound) {
			return nil, err
		}

		select {
		case <-localCtx.Done():
			return nil, errors.Wrap(localCtx.Err(), "context canceled while waiting for receipt")
		case <-ticker.C:
			continue
		}
	}
}

func (s *service) getActiveTokensForChain(ctx context.Context, chainID int) ([]*models.Token, error) {
	tokens, err := models.Tokens(
		models.TokenWhere.ChainID.EQ(chainID),
		models.TokenWhere.IsActive.EQ(true),
	).All(ctx, s.db)
	if err != nil {
		return nil, err
	}
	return tokens, nil
}

func getTokenMinCollectAmount(token *models.Token) *big.Int {
	if token != nil && token.MinWithdrawAmount.Valid && token.MinWithdrawAmount.String != "" {
		if amountWei, err := convertAmountToWei(token.MinWithdrawAmount.String, token.Decimals); err == nil && amountWei.Sign() > 0 {
			return amountWei
		}
	}

	amountWei, err := convertAmountToWei(defaultMinERC20CollectAmountString, token.Decimals)
	if err != nil {
		return big.NewInt(1)
	}

	return amountWei
}

func convertAmountToWei(amountStr string, decimals int) (*big.Int, error) {
	const (
		defaultDecimalBase  = 10
		defaultFloatBits    = 256
		defaultRoundingMode = big.ToNearestEven
		defaultScaleBase    = 10
	)

	amountFloat, _, err := big.ParseFloat(amountStr, defaultDecimalBase, defaultFloatBits, defaultRoundingMode)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse token amount")
	}

	if decimals <= 0 {
		result := new(big.Int)
		amountFloat.Int(result)
		return result, nil
	}

	scale := new(big.Int).Exp(big.NewInt(defaultScaleBase), big.NewInt(int64(decimals)), nil)
	scaleFloat := new(big.Float).SetInt(scale)
	amountFloat.Mul(amountFloat, scaleFloat)

	result := new(big.Int)
	amountFloat.Int(result)
	return result, nil
}
