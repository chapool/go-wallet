//nolint:ireturn // Returning interface aids DI
package rebalance

import (
	"context"
	"database/sql"
	"math/big"
	"sort"
	"strings"
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
	rebalanceGasLimitNative        uint64 = 21000
	rebalanceEIPMultiplier         int64  = 2
	weiPerEtherValue                      = 1_000_000_000_000_000_000
	minHotWalletsForRebalance             = 2
	rebalanceMinBalanceETH                = 3
	rebalanceMaxBalanceETH                = 8
	rebalanceGasBufferWeiValue            = 200_000_000_000_000 // 0.0002 ETH
	rebalanceReceiptTimeoutMinutes        = 2
	rebalancePollIntervalSeconds          = 3
	abiPaddedAddressLength                = 32
)

var (
	weiPerEther             = big.NewInt(weiPerEtherValue)
	rebalanceMinBalanceWei  = new(big.Int).Mul(big.NewInt(rebalanceMinBalanceETH), weiPerEther) // 3 ETH
	rebalanceMaxBalanceWei  = new(big.Int).Mul(big.NewInt(rebalanceMaxBalanceETH), weiPerEther) // 8 ETH
	rebalanceGasBufferWei   = big.NewInt(rebalanceGasBufferWeiValue)
	rebalanceReceiptTimeout = rebalanceReceiptTimeoutMinutes * time.Minute
	rebalancePollInterval   = rebalancePollIntervalSeconds * time.Second
)

// Service defines the rebalance operations contract.
type Service interface {
	StartAutoRebalance(ctx context.Context, interval time.Duration)
	RebalanceForChain(ctx context.Context, chainID int) error
	Rebalance(ctx context.Context, req *Request) error
}

type service struct {
	db               *sql.DB
	chainService     chain.Service
	scanService      scan.Service
	hotWalletService hotwallet.Service
	signerService    signer.Service
}

// NewService creates a new rebalance service.
//
//nolint:ireturn // Returning interface aids DI
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

// StartAutoRebalance schedules automatic balance checks for all chains.
func (s *service) StartAutoRebalance(ctx context.Context, interval time.Duration) {
	log.Info().
		Dur("interval", interval).
		Msg("Starting auto rebalance scheduler")

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		s.runAutoRebalance(ctx)

		for {
			select {
			case <-ctx.Done():
				log.Info().Msg("Auto rebalance scheduler stopped")
				return
			case <-ticker.C:
				s.runAutoRebalance(ctx)
			}
		}
	}()
}

// RebalanceForChain checks hot wallets on a specific chain.
func (s *service) RebalanceForChain(ctx context.Context, chainID int) error {
	wallets, err := models.Wallets(
		models.WalletWhere.ChainID.EQ(chainID),
		models.WalletWhere.WalletType.EQ("hot"),
	).All(ctx, s.db)
	if err != nil {
		return errors.Wrap(err, "failed to load hot wallets")
	}

	if len(wallets) < minHotWalletsForRebalance {
		return nil
	}

	client, err := s.scanService.GetClient(ctx, chainID)
	if err != nil {
		return errors.Wrap(err, "failed to get RPC client")
	}

	type walletBalance struct {
		wallet  *models.Wallet
		balance *big.Int
	}

	donors := make([]walletBalance, 0)
	receivers := make([]walletBalance, 0)

	for _, wallet := range wallets {
		addr := common.HexToAddress(strings.ToLower(wallet.Address))
		balance, err := client.BalanceAt(ctx, addr)
		if err != nil {
			log.Error().
				Err(err).
				Str("address", wallet.Address).
				Int("chain_id", chainID).
				Msg("RebalanceService: failed to fetch hot wallet balance")
			continue
		}

		entry := walletBalance{wallet: wallet, balance: balance}
		switch {
		case balance.Cmp(rebalanceMaxBalanceWei) > 0:
			donors = append(donors, entry)
		case balance.Cmp(rebalanceMinBalanceWei) < 0:
			receivers = append(receivers, entry)
		}
	}

	if len(donors) == 0 || len(receivers) == 0 {
		return nil
	}

	sort.Slice(donors, func(i, j int) bool {
		return donors[i].balance.Cmp(donors[j].balance) > 0
	})
	sort.Slice(receivers, func(i, j int) bool {
		return receivers[i].balance.Cmp(receivers[j].balance) < 0
	})

	for i := range receivers {
		needed := new(big.Int).Sub(rebalanceMinBalanceWei, receivers[i].balance)
		if needed.Sign() <= 0 {
			continue
		}

		for donorIdx := range donors {
			available := new(big.Int).Sub(donors[donorIdx].balance, rebalanceMaxBalanceWei)
			available.Sub(available, rebalanceGasBufferWei)
			if available.Sign() <= 0 {
				continue
			}

			transfer := big.NewInt(0).Set(available)
			if transfer.Cmp(needed) > 0 {
				transfer = needed
			}

			if transfer.Sign() <= 0 {
				continue
			}

			if err := s.transferBetweenHotWallets(ctx, donors[donorIdx].wallet, receivers[i].wallet, transfer); err != nil {
				log.Error().
					Err(err).
					Str("from", donors[donorIdx].wallet.Address).
					Str("to", receivers[i].wallet.Address).
					Int("chain_id", chainID).
					Msg("RebalanceService: transfer failed")
				continue
			}

			donors[donorIdx].balance.Sub(donors[donorIdx].balance, transfer)
			receivers[i].balance.Add(receivers[i].balance, transfer)
			needed.Sub(needed, transfer)

			if donors[donorIdx].balance.Cmp(rebalanceMaxBalanceWei) <= 0 {
				continue
			}

			if needed.Sign() <= 0 {
				break
			}
		}
	}

	return nil
}

// Rebalance manually moves funds between two hot wallets.
func (s *service) Rebalance(ctx context.Context, req *Request) error {
	if req == nil || req.Amount == nil || req.Amount.Sign() <= 0 {
		return errors.New("invalid rebalance request")
	}

	fromWallet, err := models.Wallets(
		models.WalletWhere.Address.EQ(strings.ToLower(req.FromAddress)),
		models.WalletWhere.WalletType.EQ("hot"),
		models.WalletWhere.ChainID.EQ(req.ChainID),
	).One(ctx, s.db)
	if err != nil {
		return errors.Wrap(err, "failed to load source hot wallet")
	}

	toWallet, err := models.Wallets(
		models.WalletWhere.Address.EQ(strings.ToLower(req.ToAddress)),
		models.WalletWhere.WalletType.EQ("hot"),
		models.WalletWhere.ChainID.EQ(req.ChainID),
	).One(ctx, s.db)
	if err != nil {
		return errors.Wrap(err, "failed to load destination hot wallet")
	}

	return s.transferBetweenHotWallets(ctx, fromWallet, toWallet, req.Amount)
}

func (s *service) runAutoRebalance(ctx context.Context) {
	chains, err := s.chainService.GetActiveChains(ctx)
	if err != nil {
		log.Error().Err(err).Msg("RebalanceService: failed to load chains")
		return
	}

	for _, ch := range chains {
		if err := s.RebalanceForChain(ctx, ch.ChainID); err != nil {
			log.Error().
				Err(err).
				Int("chain_id", ch.ChainID).
				Msg("RebalanceService: chain rebalance failed")
		}
	}
}

func (s *service) transferBetweenHotWallets(
	ctx context.Context,
	fromWallet *models.Wallet,
	toWallet *models.Wallet,
	amountWei *big.Int,
) error {
	client, err := s.scanService.GetClient(ctx, fromWallet.ChainID)
	if err != nil {
		return errors.Wrap(err, "failed to get RPC client")
	}

	fromAddr := common.HexToAddress(strings.ToLower(fromWallet.Address))
	toAddr := common.HexToAddress(strings.ToLower(toWallet.Address))

	balance, err := client.BalanceAt(ctx, fromAddr)
	if err != nil {
		return errors.Wrap(err, "failed to fetch source balance")
	}

	if new(big.Int).Add(amountWei, rebalanceGasBufferWei).Cmp(balance) > 0 {
		return errors.New("insufficient balance on source hot wallet")
	}

	tipCap, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to suggest gas tip cap")
	}

	latestBlock, err := client.GetBlockByNumber(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to fetch latest block")
	}

	baseFee := latestBlock.BaseFee()
	if baseFee == nil {
		baseFee = big.NewInt(0)
	}

	maxFee := new(big.Int).Add(
		new(big.Int).Mul(baseFee, big.NewInt(rebalanceEIPMultiplier)),
		tipCap,
	)

	gasFee := new(big.Int).Mul(maxFee, big.NewInt(int64(rebalanceGasLimitNative)))
	if new(big.Int).Add(amountWei, gasFee).Cmp(balance) > 0 {
		return errors.New("insufficient funds after gas estimation")
	}

	nonce, err := s.hotWalletService.GetNextNonce(ctx, strings.ToLower(fromWallet.Address), fromWallet.ChainID)
	if err != nil {
		return errors.Wrap(err, "failed to reserve nonce")
	}

	signReq := &signer.SignEVMRequest{
		ChainID:              int64(fromWallet.ChainID),
		To:                   toAddr.Hex(),
		Value:                amountWei.String(),
		GasLimit:             rebalanceGasLimitNative,
		MaxFeePerGas:         maxFee.String(),
		MaxPriorityFeePerGas: tipCap.String(),
		//nolint:gosec // Nonce is guaranteed to be positive and fit in uint64
		Nonce:          uint64(nonce),
		FromAddress:    fromAddr.Hex(),
		DerivationPath: fromWallet.DerivationPath,
	}

	signResp, err := s.signerService.SignEVMTransaction(ctx, signReq)
	if err != nil {
		return errors.Wrap(err, "failed to sign rebalance transaction")
	}

	txObj := new(types.Transaction)
	if err := txObj.UnmarshalBinary(signResp.RawTransaction); err != nil {
		return errors.Wrap(err, "failed to decode signed rebalance transaction")
	}

	if err := client.SendTransaction(ctx, txObj); err != nil {
		return errors.Wrap(err, "failed to broadcast rebalance transaction")
	}

	receipt, err := s.waitForReceipt(ctx, client, txObj.Hash())
	if err != nil {
		return errors.Wrap(err, "failed while waiting for rebalance receipt")
	}

	status := models.TransactionStatusConfirmed
	if receipt.Status != types.ReceiptStatusSuccessful {
		status = models.TransactionStatusFailed
	}

	transaction := &models.Transaction{
		ChainID:           fromWallet.ChainID,
		BlockHash:         receipt.BlockHash.Hex(),
		BlockNo:           receipt.BlockNumber.Int64(),
		TXHash:            strings.ToLower(txObj.Hash().Hex()),
		FromAddr:          strings.ToLower(fromWallet.Address),
		ToAddr:            strings.ToLower(toWallet.Address),
		TokenAddr:         null.String{}, // Native token
		Amount:            amountWei.String(),
		Type:              models.TransactionTypeRebalance,
		Status:            status,
		ConfirmationCount: null.IntFrom(0),
	}

	if err := transaction.Insert(ctx, s.db, boil.Infer()); err != nil {
		return errors.Wrap(err, "failed to insert rebalance transaction record")
	}

	log.Info().
		Str("from", fromAddr.Hex()).
		Str("to", toAddr.Hex()).
		Str("tx_hash", txObj.Hash().Hex()).
		Str("amount_wei", amountWei.String()).
		Int("chain_id", fromWallet.ChainID).
		Msg("RebalanceService: rebalance transaction broadcasted")

	return nil
}

func (s *service) waitForReceipt(ctx context.Context, client *scan.RPCClient, txHash common.Hash) (*types.Receipt, error) {
	localCtx, cancel := context.WithTimeout(ctx, rebalanceReceiptTimeout)
	defer cancel()

	ticker := time.NewTicker(rebalancePollInterval)
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
