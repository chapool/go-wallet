//nolint:ireturn
package withdraw

import (
	"context"
	"database/sql"
	"encoding/hex"
	"math/big"
	"strings"

	"github/chapool/go-wallet/internal/models"
	"github/chapool/go-wallet/internal/wallet/balance"
	"github/chapool/go-wallet/internal/wallet/hotwallet"
	"github/chapool/go-wallet/internal/wallet/scan"
	"github/chapool/go-wallet/internal/wallet/signer"

	"github.com/aarondl/null/v8"
	"github.com/aarondl/sqlboiler/v4/boil"
	"github.com/aarondl/sqlboiler/v4/queries/qm"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

// Service 提现服务接口
type Service interface {
	// RequestWithdraw 发起提现请求
	RequestWithdraw(ctx context.Context, userID string, req *Request) (*models.Withdraw, error)

	// ProcessWithdraw 处理提现（签名并广播）
	ProcessWithdraw(ctx context.Context, withdrawID string) error

	// ApproveWithdraw 管理员批准提现请求
	ApproveWithdraw(ctx context.Context, withdrawID string) (*models.Withdraw, error)

	// RejectWithdraw 管理员拒绝提现请求
	RejectWithdraw(ctx context.Context, withdrawID string, reason string) (*models.Withdraw, error)

	// UpdateWithdrawStatus 根据交易确认数更新提现状态
	UpdateWithdrawStatus(ctx context.Context, chainID int, latestBlockNumber int64) error
}

type service struct {
	db               *sql.DB
	balanceService   balance.Service
	hotWalletService hotwallet.Service
	scanService      scan.Service
	signerService    signer.Service
}

const (
	defaultERC20GasLimit      = 100000
	defaultETHGasLimit        = 21000
	defaultDecimalsBase       = 10
	defaultFloatPrec          = 256
	eip1559FeeMultiplier      = 2
	paddedAddressLength       = 32
	defaultConfirmationBlocks = 12 // 默认确认区块数
)

// NewService 创建提现服务
//
//nolint:ireturn // 返回接口类型是预期的设计
func NewService(
	db *sql.DB,
	balanceService balance.Service,
	hotWalletService hotwallet.Service,
	scanService scan.Service,
	signerService signer.Service,
) Service {
	return &service{
		db:               db,
		balanceService:   balanceService,
		hotWalletService: hotWalletService,
		scanService:      scanService,
		signerService:    signerService,
	}
}

// RequestWithdraw 发起提现请求
func (s *service) RequestWithdraw(ctx context.Context, userID string, req *Request) (*models.Withdraw, error) {
	// 1. 验证参数
	if req.Amount.Sign() <= 0 {
		return nil, errors.New("invalid amount")
	}

	// 2. 获取代币信息
	token, err := models.Tokens(models.TokenWhere.ID.EQ(req.TokenID)).One(ctx, s.db)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("token not found")
		}
		return nil, errors.Wrap(err, "failed to get token")
	}

	// 3. 检查余额
	availableBalance, err := s.balanceService.GetAvailableBalance(ctx, userID, token.ChainID, token.ID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to check balance")
	}

	if availableBalance.Cmp(req.Amount) < 0 {
		return nil, errors.New("insufficient balance")
	}

	// 4. 开启事务，创建提现记录和扣减余额（冻结）
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin transaction")
	}
	defer func() { _ = tx.Rollback() }()

	// 创建提现记录
	withdraw := &models.Withdraw{
		UserID:    userID,
		ToAddress: req.ToAddress,
		TokenID:   req.TokenID,
		Amount:    req.Amount.Text('f', -1), // 保持精度
		Fee:       "0",                      // 暂时未计算手续费，后续 update
		ChainID:   token.ChainID,
		ChainType: token.ChainType,
		Status:    models.WithdrawStatusUserWithdrawRequest, // 初始状态：等待管理员审核
	}

	if err := withdraw.Insert(ctx, tx, boil.Infer()); err != nil {
		return nil, errors.Wrap(err, "failed to insert withdraw record")
	}

	// 创建 Credits 记录（冻结资金）
	// 金额为负数
	negAmount := new(big.Float).Neg(req.Amount)
	credit := &models.Credit{
		UserID:        userID,
		Address:       req.ToAddress, // 使用提现地址作为关联地址
		TokenID:       token.ID,
		TokenSymbol:   token.TokenSymbol,
		Amount:        negAmount.Text('f', -1),
		CreditType:    "withdraw",
		BusinessType:  "blockchain",
		ReferenceID:   withdraw.ID,
		ReferenceType: "withdraw",
		ChainID:       null.IntFrom(token.ChainID),
		ChainType:     null.StringFrom(token.ChainType),
		Status:        "frozen", // 冻结状态，等待处理
	}

	// 获取用户在该链的地址填充 credits.address
	// 这里简化处理：查询用户在该链的第一个地址
	userWallet, err := models.Wallets(
		models.WalletWhere.UserID.EQ(userID),
		models.WalletWhere.ChainID.EQ(token.ChainID),
	).One(ctx, tx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find user wallet for credit record")
	}
	credit.Address = userWallet.Address

	if err := credit.Insert(ctx, tx, boil.Infer()); err != nil {
		return nil, errors.Wrap(err, "failed to insert credit record")
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit transaction")
	}

	log.Info().
		Str("withdraw_id", withdraw.ID).
		Str("user_id", userID).
		Str("amount", withdraw.Amount).
		Msg("Withdraw request created")

	return withdraw, nil
}

// ProcessWithdraw 处理提现
func (s *service) ProcessWithdraw(ctx context.Context, withdrawID string) error {
	// 1. 获取并锁定提现记录 (FOR UPDATE)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to begin transaction")
	}
	defer func() { _ = tx.Rollback() }()

	withdraw, err := models.Withdraws(
		models.WithdrawWhere.ID.EQ(withdrawID),
		qm.For("UPDATE"),
	).One(ctx, tx)
	if err != nil {
		return errors.Wrap(err, "failed to get withdraw record")
	}

	if withdraw.Status != models.WithdrawStatusUserWithdrawRequest {
		// 只有 user_withdraw_request 状态的提现可以处理（等待审核的提现）
		return errors.Errorf("withdraw status is %s, expected %s", withdraw.Status, models.WithdrawStatusUserWithdrawRequest)
	}

	// 2. 获取热钱包
	hotWallet, err := s.hotWalletService.GetHotWallet(ctx, withdraw.ChainID)
	if err != nil {
		return errors.Wrap(err, "failed to get hot wallet")
	}

	// 3. 获取 RPC 客户端
	client, err := s.scanService.GetClient(ctx, withdraw.ChainID)
	if err != nil {
		return errors.Wrap(err, "failed to get RPC client")
	}

	// 4. 获取代币信息
	token, err := models.Tokens(models.TokenWhere.ID.EQ(withdraw.TokenID)).One(ctx, tx)
	if err != nil {
		return errors.Wrap(err, "failed to get token info")
	}

	// 5. 转换 Amount 到 Wei (BigInt)
	amountFloat, _, err := big.ParseFloat(withdraw.Amount, defaultDecimalsBase, defaultFloatPrec, big.ToNearestEven)
	if err != nil {
		return errors.Wrap(err, "failed to parse amount")
	}
	decimalsFloat := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(defaultDecimalsBase), big.NewInt(int64(token.Decimals)), nil))
	amountWeiFloat := new(big.Float).Mul(amountFloat, decimalsFloat)
	amountWei := new(big.Int)
	amountWeiFloat.Int(amountWei) // 转换为 Int

	// 6. 获取 gas 价格（用于余额检查和交易构建）
	tipCap, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to suggest gas tip cap")
	}
	latestBlock, err := client.GetBlockByNumber(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to get latest block")
	}
	baseFee := latestBlock.BaseFee()
	if baseFee == nil {
		return errors.New("chain does not support EIP-1559 (baseFee is nil)")
	}
	maxFee := new(big.Int).Add(new(big.Int).Mul(baseFee, big.NewInt(eip1559FeeMultiplier)), tipCap)

	// 7. 检查热钱包余额
	hotWalletAddr := common.HexToAddress(hotWallet.Address)
	if err := s.checkHotWalletBalance(ctx, client, token, hotWalletAddr, amountWei, maxFee); err != nil {
		return err
	}

	// 8. 获取 Nonce (原子递增)
	nonce, err := s.hotWalletService.GetNextNonce(ctx, hotWallet.Address, withdraw.ChainID)
	if err != nil {
		return errors.Wrap(err, "failed to get nonce")
	}

	// 9. 构建交易签名请求
	// tipCap, baseFee, maxFee 已经在余额检查时计算过了，直接使用

	// amountWei 已经在余额检查时计算过了，这里直接使用

	// 构建 SignRequest
	signReq := &signer.SignEVMRequest{
		ChainID:              int64(withdraw.ChainID),
		To:                   withdraw.ToAddress,
		Value:                amountWei.String(),
		GasLimit:             defaultETHGasLimit, // ETH 转账默认，ERC20 需要 EstimateGas
		MaxFeePerGas:         maxFee.String(),
		MaxPriorityFeePerGas: tipCap.String(),
		//nolint:gosec // Nonce is guaranteed to be positive and fit in uint64
		Nonce:          uint64(nonce),
		FromAddress:    hotWallet.Address,
		DerivationPath: hotWallet.DerivationPath,
	}

	if !token.IsNative {
		// ERC20 转账
		// 需要构建 ERC20 Transfer data: transfer(to, amount)
		// 并将 To 改为 Token Address
		if !token.TokenAddress.Valid {
			return errors.New("token address is invalid for non-native token")
		}

		// ABI Encode: transfer(address,uint256)
		// methodID: a9059cbb
		// param1: to (padded to 32 bytes)
		// param2: amount (padded to 32 bytes)
		methodID, _ := hex.DecodeString("a9059cbb")
		paddedAddress := common.HexToAddress(withdraw.ToAddress)
		paddedAmount := common.BigToHash(amountWei)

		var data []byte
		data = append(data, methodID...)
		data = append(data, common.LeftPadBytes(paddedAddress.Bytes(), paddedAddressLength)...)
		data = append(data, paddedAmount.Bytes()...)

		signReq.To = token.TokenAddress.String
		signReq.Value = "0" // ETH value 为 0
		signReq.Data = data

		// 估算 Gas
		// msg := ethereum.CallMsg{...}
		// gas, err := client.EstimateGas(ctx, msg)
		// signReq.GasLimit = gas
		signReq.GasLimit = defaultERC20GasLimit // 简单默认值，生产环境必须估算
	}

	// 5. 签名
	signResp, err := s.signerService.SignEVMTransaction(ctx, signReq)
	if err != nil {
		return errors.Wrap(err, "failed to sign transaction")
	}

	// 6. 广播
	// 需要将 raw transaction bytes 解码为 Transaction 对象
	txObj := new(types.Transaction)
	if err := txObj.UnmarshalBinary(signResp.RawTransaction); err != nil {
		return errors.Wrap(err, "failed to unmarshal signed transaction")
	}

	if err := client.SendTransaction(ctx, txObj); err != nil {
		return errors.Wrap(err, "failed to broadcast transaction")
	}

	// 7. 更新状态为 pending（交易已发送，等待确认）
	// 后续由区块扫描器根据确认数更新为 processing → confirmed
	withdraw.Status = models.WithdrawStatusPending
	withdraw.TXHash = null.StringFrom(signResp.TxHash)
	withdraw.FromAddress = null.StringFrom(hotWallet.Address)
	withdraw.Nonce = null.IntFrom(nonce)

	if _, err := withdraw.Update(ctx, tx, boil.Infer()); err != nil {
		return errors.Wrap(err, "failed to update withdraw status")
	}

	// 提交事务
	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit transaction")
	}

	log.Info().
		Str("withdraw_id", withdrawID).
		Str("tx_hash", signResp.TxHash).
		Msg("Withdraw processed and broadcasted")

	return nil
}

// ApproveWithdraw 管理员批准提现请求
func (s *service) ApproveWithdraw(ctx context.Context, withdrawID string) (*models.Withdraw, error) {
	// 1. 获取并锁定提现记录
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin transaction")
	}
	defer func() { _ = tx.Rollback() }()

	withdraw, err := models.Withdraws(
		models.WithdrawWhere.ID.EQ(withdrawID),
		qm.For("UPDATE"),
	).One(ctx, tx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("withdraw not found")
		}
		return nil, errors.Wrap(err, "failed to get withdraw record")
	}

	// 2. 检查状态
	if withdraw.Status != models.WithdrawStatusUserWithdrawRequest {
		return nil, errors.Errorf("withdraw status is %s, expected %s", withdraw.Status, models.WithdrawStatusUserWithdrawRequest)
	}

	// 3. 提交事务（状态检查完成）
	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit transaction")
	}

	// 4. 处理提现（签名并广播）
	if err := s.ProcessWithdraw(ctx, withdrawID); err != nil {
		// 如果处理失败，更新状态为 failed 并记录错误信息
		s.updateWithdrawStatusOnError(ctx, withdrawID, err)
		return nil, errors.Wrap(err, "failed to process withdraw after approval")
	}

	// 5. 重新获取提现记录（获取更新后的状态）
	withdraw, err = models.Withdraws(models.WithdrawWhere.ID.EQ(withdrawID)).One(ctx, s.db)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get updated withdraw record")
	}

	return withdraw, nil
}

// RejectWithdraw 管理员拒绝提现请求
func (s *service) RejectWithdraw(ctx context.Context, withdrawID string, reason string) (*models.Withdraw, error) {
	// 1. 获取并锁定提现记录
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin transaction")
	}
	defer func() { _ = tx.Rollback() }()

	withdraw, err := models.Withdraws(
		models.WithdrawWhere.ID.EQ(withdrawID),
		qm.For("UPDATE"),
	).One(ctx, tx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("withdraw not found")
		}
		return nil, errors.Wrap(err, "failed to get withdraw record")
	}

	// 2. 检查是否有 frozen 的 credits（允许拒绝任何有 frozen credits 的提现，无论状态）
	credits, err := models.Credits(
		models.CreditWhere.ReferenceID.EQ(withdrawID),
		models.CreditWhere.ReferenceType.EQ("withdraw"),
		models.CreditWhere.Status.EQ("frozen"),
	).All(ctx, tx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get frozen credits")
	}

	if len(credits) == 0 {
		// 没有 frozen credits，说明已经处理过了，不允许拒绝
		return nil, errors.Errorf("withdraw has no frozen credits to reject (status: %s)", withdraw.Status)
	}

	// 3. 检查状态（只允许拒绝 user_withdraw_request 或 failed 状态的提现）
	allowedStatuses := []string{
		models.WithdrawStatusUserWithdrawRequest,
		models.WithdrawStatusFailed,
	}
	statusAllowed := false
	for _, allowedStatus := range allowedStatuses {
		if withdraw.Status == allowedStatus {
			statusAllowed = true
			break
		}
	}

	if !statusAllowed {
		return nil, errors.Errorf("withdraw status is %s, can only reject %s or %s status", withdraw.Status, models.WithdrawStatusUserWithdrawRequest, models.WithdrawStatusFailed)
	}

	// 4. 记录之前的状态（用于日志）
	previousStatus := withdraw.Status

	// 5. 更新状态为失败
	withdraw.Status = models.WithdrawStatusFailed
	// 如果提供了拒绝原因，使用提供的原因；否则使用默认的拒绝原因
	if reason != "" {
		withdraw.ErrorMessage = null.StringFrom(reason)
	} else {
		// 如果没有提供原因，使用默认的拒绝原因（覆盖之前的错误信息）
		withdraw.ErrorMessage = null.StringFrom("Rejected by admin")
	}

	if _, err := withdraw.Update(ctx, tx, boil.Infer()); err != nil {
		return nil, errors.Wrap(err, "failed to update withdraw status")
	}

	// 6. 解冻 credits（将 frozen 状态的 credits 更新为 failed）
	for _, credit := range credits {
		credit.Status = "failed"
		if _, err := credit.Update(ctx, tx, boil.Infer()); err != nil {
			return nil, errors.Wrap(err, "failed to update credit status")
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit transaction")
	}

	log.Info().
		Str("withdraw_id", withdrawID).
		Str("reason", reason).
		Str("previous_status", previousStatus).
		Msg("Withdraw rejected by admin")

	return withdraw, nil
}

// UpdateWithdrawStatus 根据交易确认数更新提现状态
// 状态流转：pending → processing → confirmed
func (s *service) UpdateWithdrawStatus(ctx context.Context, chainID int, latestBlockNumber int64) error {
	// 获取链配置
	chain, err := models.Chains(
		models.ChainWhere.ChainID.EQ(chainID),
	).One(ctx, s.db)

	if err != nil {
		return errors.Wrapf(err, "failed to get chain config for chain_id=%d", chainID)
	}

	// 获取确认区块数（用于判断 confirmed 状态）
	confirmationBlocks := int64(defaultConfirmationBlocks)
	if chain.ConfirmationBlocks.Valid {
		confirmationBlocks = int64(chain.ConfirmationBlocks.Int)
	}

	// 查询所有待更新的提现记录（pending 或 processing 状态，且有 tx_hash）
	withdraws, err := models.Withdraws(
		models.WithdrawWhere.ChainID.EQ(chainID),
		models.WithdrawWhere.Status.IN([]string{
			models.WithdrawStatusPending,
			models.WithdrawStatusProcessing,
		}),
		models.WithdrawWhere.TXHash.IsNotNull(),
	).All(ctx, s.db)

	if err != nil {
		return errors.Wrap(err, "failed to query withdraws")
	}

	if len(withdraws) == 0 {
		return nil
	}

	log.Info().
		Int("chain_id", chainID).
		Int64("latest_block", latestBlockNumber).
		Int64("confirmation_blocks", confirmationBlocks).
		Int("pending_withdraw_count", len(withdraws)).
		Msg("Updating withdraw confirmation status")

	updatedCount := 0
	for _, withdraw := range withdraws {
		if !withdraw.TXHash.Valid {
			continue
		}

		txHash := withdraw.TXHash.String

		// 通过 tx_hash 查询 transactions 表获取确认数
		tx, err := s.getOrCreateTransactionRecord(ctx, chainID, txHash, withdraw, latestBlockNumber)
		if err != nil {
			log.Debug().
				Str("withdraw_id", withdraw.ID).
				Str("tx_hash", txHash).
				Err(err).
				Msg("Failed to get or create transaction record, skipping")
			continue
		}

		// 计算确认数
		confirmationCount := latestBlockNumber - tx.BlockNo

		// 确保确认数不为负数（防止区块重组等情况）
		if confirmationCount < 0 {
			log.Warn().
				Str("withdraw_id", withdraw.ID).
				Str("tx_hash", txHash).
				Int64("block_no", tx.BlockNo).
				Int64("latest_block", latestBlockNumber).
				Int64("confirmation_count", confirmationCount).
				Msg("Negative confirmation count detected, skipping status update")
			continue
		}

		// 根据确认数确定新状态
		var newStatus string
		switch {
		case confirmationCount >= confirmationBlocks:
			// 达到确认数，状态为 confirmed
			newStatus = models.WithdrawStatusConfirmed
		case confirmationCount > 0:
			// 有确认但未达到最终确认数，状态为 processing
			newStatus = models.WithdrawStatusProcessing
		default:
			// 确认数为 0，保持 pending
			newStatus = models.WithdrawStatusPending
		}

		// 如果状态需要更新
		if withdraw.Status != newStatus {
			oldStatus := withdraw.Status
			withdraw.Status = newStatus

			if _, err := withdraw.Update(ctx, s.db, boil.Whitelist(
				models.WithdrawColumns.Status,
				models.WithdrawColumns.UpdatedAt,
			)); err != nil {
				log.Error().
					Str("withdraw_id", withdraw.ID).
					Str("tx_hash", txHash).
					Err(err).
					Msg("Failed to update withdraw status")
				continue
			}

			updatedCount++

			log.Info().
				Int("chain_id", chainID).
				Str("withdraw_id", withdraw.ID).
				Str("tx_hash", txHash).
				Str("old_status", oldStatus).
				Str("new_status", newStatus).
				Int64("confirmation_count", confirmationCount).
				Int64("block_no", tx.BlockNo).
				Int64("latest_block", latestBlockNumber).
				Msg("Withdraw status updated")
		}
	}

	if updatedCount > 0 {
		log.Info().
			Int("chain_id", chainID).
			Int("updated_count", updatedCount).
			Msg("Withdraw statuses updated")
	}

	return nil
}

// getOrCreateTransactionRecord 获取或创建交易记录
// 如果 transactions 表中没有记录，尝试通过 RPC 查询并创建
func (s *service) getOrCreateTransactionRecord(ctx context.Context, chainID int, txHash string, withdraw *models.Withdraw, latestBlockNumber int64) (*models.Transaction, error) {
	// 先尝试从数据库查询
	tx, err := models.Transactions(
		models.TransactionWhere.TXHash.EQ(txHash),
	).One(ctx, s.db)

	if err == nil {
		return tx, nil
	}

	// 如果查询失败且不是 ErrNoRows，返回错误
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, errors.Wrap(err, "failed to query transaction")
	}

	// 交易还未被扫描到，尝试通过 RPC 查询交易状态
	// 如果交易已确认，创建 transactions 记录
	if err := s.createTransactionRecordIfConfirmed(ctx, chainID, txHash, withdraw, latestBlockNumber); err != nil {
		return nil, errors.Wrap(err, "failed to create transaction record via RPC")
	}

	// 重新查询 transactions 记录
	tx, err = models.Transactions(
		models.TransactionWhere.TXHash.EQ(txHash),
	).One(ctx, s.db)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query transaction after creating record")
	}

	return tx, nil
}

// createTransactionRecordIfConfirmed 如果交易已确认，创建 transactions 记录
func (s *service) createTransactionRecordIfConfirmed(ctx context.Context, chainID int, txHash string, withdraw *models.Withdraw, latestBlockNumber int64) error {
	// 获取 RPC 客户端
	client, err := s.scanService.GetClient(ctx, chainID)
	if err != nil {
		return errors.Wrap(err, "failed to get RPC client")
	}

	// 查询交易回执
	txHashHash := common.HexToHash(txHash)
	receipt, err := client.GetTransactionReceipt(ctx, txHashHash)
	if err != nil {
		// 交易可能还在 pending 状态，返回错误但不记录为错误
		return errors.Wrap(err, "transaction receipt not found (may still be pending)")
	}

	// 检查交易是否成功
	if receipt.Status != types.ReceiptStatusSuccessful {
		// 交易失败，创建失败的 transactions 记录
		transaction := &models.Transaction{
			ChainID:           chainID,
			BlockHash:         receipt.BlockHash.Hex(),
			BlockNo:           receipt.BlockNumber.Int64(),
			TXHash:            strings.ToLower(txHash),
			FromAddr:          strings.ToLower(withdraw.FromAddress.String),
			ToAddr:            strings.ToLower(withdraw.ToAddress),
			TokenAddr:         null.String{}, // 需要根据 token 判断
			Amount:            withdraw.Amount,
			Type:              models.TransactionTypeWithdraw,
			Status:            models.TransactionStatusFailed,
			ConfirmationCount: null.IntFrom(int(latestBlockNumber - receipt.BlockNumber.Int64())),
		}

		// 获取 token 信息以确定 token_addr
		token, err := models.Tokens(models.TokenWhere.ID.EQ(withdraw.TokenID)).One(ctx, s.db)
		if err == nil && !token.IsNative && token.TokenAddress.Valid {
			transaction.TokenAddr = null.StringFrom(strings.ToLower(token.TokenAddress.String))
		}

		if err := transaction.Insert(ctx, s.db, boil.Infer()); err != nil {
			return errors.Wrap(err, "failed to create failed transaction record")
		}

		log.Info().
			Str("withdraw_id", withdraw.ID).
			Str("tx_hash", txHash).
			Int64("block_no", receipt.BlockNumber.Int64()).
			Msg("Created failed transaction record for withdraw")
		return nil
	}

	// 交易成功，创建 transactions 记录
	transaction := &models.Transaction{
		ChainID:           chainID,
		BlockHash:         receipt.BlockHash.Hex(),
		BlockNo:           receipt.BlockNumber.Int64(),
		TXHash:            strings.ToLower(txHash),
		FromAddr:          strings.ToLower(withdraw.FromAddress.String),
		ToAddr:            strings.ToLower(withdraw.ToAddress),
		TokenAddr:         null.String{}, // 需要根据 token 判断
		Amount:            withdraw.Amount,
		Type:              models.TransactionTypeWithdraw,
		Status:            models.TransactionStatusConfirmed,
		ConfirmationCount: null.IntFrom(int(latestBlockNumber - receipt.BlockNumber.Int64())),
	}

	// 获取 token 信息以确定 token_addr
	token, err := models.Tokens(models.TokenWhere.ID.EQ(withdraw.TokenID)).One(ctx, s.db)
	if err == nil && !token.IsNative && token.TokenAddress.Valid {
		transaction.TokenAddr = null.StringFrom(strings.ToLower(token.TokenAddress.String))
	}

	if err := transaction.Insert(ctx, s.db, boil.Infer()); err != nil {
		return errors.Wrap(err, "failed to create transaction record")
	}

	log.Info().
		Str("withdraw_id", withdraw.ID).
		Str("tx_hash", txHash).
		Int64("block_no", receipt.BlockNumber.Int64()).
		Int64("confirmation_count", latestBlockNumber-receipt.BlockNumber.Int64()).
		Msg("Created transaction record for withdraw")

	return nil
}

// checkHotWalletBalance 检查热钱包余额是否充足
func (s *service) checkHotWalletBalance(
	ctx context.Context,
	client *scan.RPCClient,
	token *models.Token,
	hotWalletAddr common.Address,
	amountWei *big.Int,
	maxFee *big.Int,
) error {
	if token.IsNative {
		return s.checkNativeTokenBalance(ctx, client, hotWalletAddr, amountWei, maxFee)
	}
	return s.checkERC20TokenBalance(ctx, client, token, hotWalletAddr, amountWei, maxFee)
}

// checkNativeTokenBalance 检查 native token 余额
func (s *service) checkNativeTokenBalance(
	ctx context.Context,
	client *scan.RPCClient,
	hotWalletAddr common.Address,
	amountWei *big.Int,
	maxFee *big.Int,
) error {
	balance, err := client.BalanceAt(ctx, hotWalletAddr)
	if err != nil {
		return errors.Wrap(err, "failed to get hot wallet native token balance")
	}

	// 估算 gas 费用
	gasLimit := big.NewInt(defaultETHGasLimit)
	estimatedGasCost := new(big.Int).Mul(gasLimit, maxFee)

	requiredBalance := new(big.Int).Add(amountWei, estimatedGasCost)
	if balance.Cmp(requiredBalance) < 0 {
		return errors.Errorf("insufficient balance in hot wallet: have %s, need %s (amount: %s + gas: %s)",
			balance.String(), requiredBalance.String(), amountWei.String(), estimatedGasCost.String())
	}

	return nil
}

// checkERC20TokenBalance 检查 ERC20 token 余额和 native token 余额（用于 gas）
func (s *service) checkERC20TokenBalance(
	ctx context.Context,
	client *scan.RPCClient,
	token *models.Token,
	hotWalletAddr common.Address,
	amountWei *big.Int,
	maxFee *big.Int,
) error {
	if !token.TokenAddress.Valid {
		return errors.New("token address is invalid for non-native token")
	}

	// 检查 ERC20 token 余额
	tokenAddr := common.HexToAddress(token.TokenAddress.String)
	tokenBalance, err := client.TokenBalance(ctx, tokenAddr, hotWalletAddr)
	if err != nil {
		return errors.Wrap(err, "failed to get hot wallet ERC20 token balance")
	}

	if tokenBalance.Cmp(amountWei) < 0 {
		return errors.Errorf("insufficient ERC20 token balance in hot wallet: have %s, need %s",
			tokenBalance.String(), amountWei.String())
	}

	// 检查 native token 余额（用于 gas）
	balance, err := client.BalanceAt(ctx, hotWalletAddr)
	if err != nil {
		return errors.Wrap(err, "failed to get hot wallet native token balance for gas")
	}

	// 估算 gas 费用
	gasLimit := big.NewInt(defaultERC20GasLimit)
	estimatedGasCost := new(big.Int).Mul(gasLimit, maxFee)

	if balance.Cmp(estimatedGasCost) < 0 {
		return errors.Errorf("insufficient native token balance in hot wallet for gas: have %s, need %s",
			balance.String(), estimatedGasCost.String())
	}

	return nil
}

// updateWithdrawStatusOnError 当提现处理失败时更新状态为 failed
func (s *service) updateWithdrawStatusOnError(ctx context.Context, withdrawID string, processErr error) {
	updateTx, updateErr := s.db.BeginTx(ctx, nil)
	if updateErr != nil {
		log.Error().Err(updateErr).Str("withdraw_id", withdrawID).Msg("Failed to begin transaction for error status update")
		return
	}
	defer func() { _ = updateTx.Rollback() }()

	withdrawRecord, getErr := models.Withdraws(
		models.WithdrawWhere.ID.EQ(withdrawID),
		qm.For("UPDATE"),
	).One(ctx, updateTx)
	if getErr != nil {
		log.Error().Err(getErr).Str("withdraw_id", withdrawID).Msg("Failed to get withdraw record for error status update")
		return
	}

	withdrawRecord.Status = models.WithdrawStatusFailed
	withdrawRecord.ErrorMessage = null.StringFrom(processErr.Error())
	if _, updateErr := withdrawRecord.Update(ctx, updateTx, boil.Infer()); updateErr != nil {
		log.Error().Err(updateErr).Str("withdraw_id", withdrawID).Msg("Failed to update withdraw status to failed")
		return
	}

	if commitErr := updateTx.Commit(); commitErr != nil {
		log.Error().Err(commitErr).Str("withdraw_id", withdrawID).Msg("Failed to commit transaction for error status update")
		return
	}

	log.Info().
		Str("withdraw_id", withdrawID).
		Str("error", processErr.Error()).
		Msg("Updated withdraw status to failed after processing error")
}
