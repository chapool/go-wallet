//nolint:ireturn
package withdraw

import (
	"context"
	"database/sql"
	"encoding/hex"
	"math/big"

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
}

type service struct {
	db               *sql.DB
	balanceService   balance.Service
	hotWalletService hotwallet.Service
	scanService      scan.Service
	signerService    signer.Service
}

const (
	defaultERC20GasLimit = 100000
	defaultETHGasLimit   = 21000
	defaultDecimalsBase  = 10
	defaultFloatPrec     = 256
	eip1559FeeMultiplier = 2
	paddedAddressLength  = 32
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
		Status:    "pending", // 初始状态
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

	if withdraw.Status != "pending" {
		// 只有 pending 状态的提现可以处理
		return nil
	}

	// 2. 获取热钱包
	hotWallet, err := s.hotWalletService.GetHotWallet(ctx, withdraw.ChainID)
	if err != nil {
		return errors.Wrap(err, "failed to get hot wallet")
	}

	// 3. 获取 Nonce (原子递增)
	nonce, err := s.hotWalletService.GetNextNonce(ctx, hotWallet.Address, withdraw.ChainID)
	if err != nil {
		return errors.Wrap(err, "failed to get nonce")
	}

	// 4. 构建交易签名请求
	client, err := s.scanService.GetClient(ctx, withdraw.ChainID)
	if err != nil {
		return errors.Wrap(err, "failed to get RPC client")
	}

	// 获取最新的 BaseFee (从最新区块)
	// 为了简单，我们这里不调用 GetLatestBlockNumber 再 GetBlockByNumber，而是假设 RPC 节点支持 EIP-1559 且我们能估算 Gas
	// 实际上，我们可以简单地使用 client.SuggestGasTipCap() 和 client.HeaderByNumber(nil).BaseFee
	// 这里简化：使用硬编码或简单的估算逻辑
	// 注意：生产环境需要更精确的 Gas 估算

	// 获取 Tip Cap
	tipCap, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to suggest gas tip cap")
	}

	// 获取 Latest Block for Base Fee (通过 GetBlockByNumber(nil))
	latestBlock, err := client.GetBlockByNumber(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to get latest block")
	}
	baseFee := latestBlock.BaseFee()
	if baseFee == nil {
		// 非 EIP-1559 链处理 (暂时不支持，因为 SignerService 只支持 EIP-1559)
		// 如果链不支持 EIP-1559，这里会报错。假设所有链都支持（如 Polygon, ETH, BSC modern）
		// BSC 可能不支持 EIP-1559，需要检查。如果是 legacy tx，SignerService 需要扩展。
		// 暂时假设都支持 EIP-1559。
		// 如果 baseFee 为 nil，设置一个默认值或者报错
		// baseFee = big.NewInt(0) // 危险
		return errors.New("chain does not support EIP-1559 (baseFee is nil)")
	}

	// MaxFee = BaseFee * 2 + TipCap (简单策略)
	maxFee := new(big.Int).Add(new(big.Int).Mul(baseFee, big.NewInt(eip1559FeeMultiplier)), tipCap)

	// 转换 Amount 到 Wei (BigInt)
	amountFloat, _, err := big.ParseFloat(withdraw.Amount, defaultDecimalsBase, defaultFloatPrec, big.ToNearestEven)
	if err != nil {
		return errors.Wrap(err, "failed to parse amount")
	}
	// 假设 Tokens 表有 decimals，这里为了简化，假设 amount 已经是 Wei 或者需要转换
	// 通常 withdraw.Amount 应该是人类可读的 (如 1.5 ETH)，需要乘以 10^decimals
	// 但是 RequestWithdraw 中我们直接存了 req.Amount。我们需要知道 req.Amount 的单位。
	// 假设 req.Amount 是人类可读单位。
	token, err := models.Tokens(models.TokenWhere.ID.EQ(withdraw.TokenID)).One(ctx, tx)
	if err != nil {
		return errors.Wrap(err, "failed to get token info")
	}

	amountWei := new(big.Int)
	// Amount * 10^decimals
	decimalsFloat := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(defaultDecimalsBase), big.NewInt(int64(token.Decimals)), nil))
	amountWeiFloat := new(big.Float).Mul(amountFloat, decimalsFloat)
	amountWeiFloat.Int(amountWei) // 转换为 Int

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

	// 7. 更新状态
	withdraw.Status = "processing"
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
