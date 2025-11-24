package deposit

import (
	"context"
	"database/sql"
	"math/big"
	"strings"

	"github/chapool/go-wallet/internal/models"

	"github.com/aarondl/null/v8"
	"github.com/aarondl/sqlboiler/v4/boil"
	"github.com/aarondl/sqlboiler/v4/queries/qm"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

const (
	nativeTokenDecimals = 18 // 原生代币通常是 18 位小数
)

// service 实现 Service 接口
type service struct {
	db        *sql.DB
	processor *transactionStatusProcessor
}

// NewService 创建充值服务
//
//nolint:ireturn
func NewService(db *sql.DB) Service {
	return &service{
		db:        db,
		processor: newTransactionStatusProcessor(db),
	}
}

// ProcessDeposit 处理充值交易（创建 Credits 记录）
func (s *service) ProcessDeposit(ctx context.Context, transaction *models.Transaction) error {
	// 只处理已终结的充值交易
	if transaction.Status != "finalized" {
		return nil
	}

	// 检查是否已创建 Credits 记录
	exists, err := s.creditExists(ctx, transaction.ID)
	if err != nil {
		return errors.Wrap(err, "failed to check credit existence")
	}

	if exists {
		// 已创建，跳过
		return nil
	}

	// 创建 Credits 记录
	_, err = s.CreateCredit(ctx, transaction)
	if err != nil {
		return errors.Wrap(err, "failed to create credit")
	}

	return nil
}

// UpdateConfirmationStatus 更新交易确认状态
func (s *service) UpdateConfirmationStatus(ctx context.Context, chainID int, latestBlockNumber int64) error {
	return s.processor.updateTransactionStatus(ctx, chainID, big.NewInt(latestBlockNumber))
}

// ProcessFinalizedDeposits 处理已终结但尚未生成 Credits 的充值
func (s *service) ProcessFinalizedDeposits(ctx context.Context, chainID int) error {
	transactions, err := models.Transactions(
		models.TransactionWhere.ChainID.EQ(chainID),
		models.TransactionWhere.Type.EQ(models.TransactionTypeDeposit),
		models.TransactionWhere.Status.EQ(models.TransactionStatusFinalized),
		qm.Where(`
			NOT EXISTS (
				SELECT 1 FROM credits 
				WHERE credits.reference_id = transactions.id::text 
				AND credits.reference_type = 'blockchain_tx'
			)
		`),
		qm.OrderBy(models.TransactionColumns.BlockNo+" ASC"),
	).All(ctx, s.db)

	if err != nil {
		return errors.Wrap(err, "failed to query finalized deposits")
	}

	log.Info().
		Int("chain_id", chainID).
		Int("count", len(transactions)).
		Msg("Found finalized deposits to process")

	for _, tx := range transactions {
		log.Debug().
			Str("tx_hash", tx.TXHash).
			Str("tx_id", tx.ID).
			Int64("block_no", tx.BlockNo).
			Str("status", tx.Status).
			Msg("Processing finalized deposit")

		if err := s.ProcessDeposit(ctx, tx); err != nil {
			log.Err(err).
				Str("tx_hash", tx.TXHash).
				Str("tx_id", tx.ID).
				Int("chain_id", chainID).
				Msg("Failed to process finalized deposit")
			// 继续处理其他交易，不中断
			continue
		}

		log.Info().
			Str("tx_hash", tx.TXHash).
			Str("tx_id", tx.ID).
			Int("chain_id", chainID).
			Msg("Successfully processed finalized deposit")
	}

	return nil
}

// CreateCredit 创建 Credits 记录
func (s *service) CreateCredit(ctx context.Context, transaction *models.Transaction) (*models.Credit, error) {
	log.Debug().
		Str("tx_hash", transaction.TXHash).
		Str("tx_id", transaction.ID).
		Str("to_addr", transaction.ToAddr).
		Int("chain_id", transaction.ChainID).
		Msg("Creating credit for transaction")

	// 获取钱包信息（通过地址查找）
	wallet, err := models.Wallets(
		models.WalletWhere.ChainID.EQ(transaction.ChainID),
		models.WalletWhere.Address.EQ(strings.ToLower(transaction.ToAddr)),
	).One(ctx, s.db)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.Errorf("wallet not found for address %s on chain_id %d", transaction.ToAddr, transaction.ChainID)
		}
		return nil, errors.Wrapf(err, "failed to get wallet for address %s", transaction.ToAddr)
	}

	log.Debug().
		Str("wallet_id", wallet.ID).
		Str("user_id", wallet.UserID).
		Str("address", wallet.Address).
		Msg("Found wallet for transaction")

	// 获取代币信息
	token, err := s.getTokenInfo(ctx, transaction.ChainID, transaction.TokenAddr.String)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get token info")
	}

	log.Debug().
		Int("token_id", token.ID).
		Str("token_symbol", token.TokenSymbol).
		Msg("Found token for transaction")

	// 创建 Credits 记录
	credit := &models.Credit{
		UserID:        wallet.UserID,
		Address:       transaction.ToAddr,
		TokenID:       token.ID,
		TokenSymbol:   token.TokenSymbol,
		Amount:        transaction.Amount,
		CreditType:    "deposit",
		BusinessType:  "blockchain",
		ReferenceID:   transaction.ID,
		ReferenceType: "blockchain_tx",
		ChainID:       null.IntFrom(transaction.ChainID),
		ChainType:     null.StringFrom("evm"),
		Status:        "finalized", // 充值交易已终结，直接标记为 finalized
		BlockNumber:   null.Int64From(transaction.BlockNo),
		TXHash:        null.StringFrom(transaction.TXHash),
		EventIndex:    null.Int{}, // ETH 转账没有事件索引
	}

	// 如果是 ERC20 转账，设置事件索引（从交易中获取，这里暂时设为 0）
	if transaction.TokenAddr.Valid && transaction.TokenAddr.String != "" {
		// TODO: 从交易日志中获取事件索引
		credit.EventIndex = null.IntFrom(0)
	}

	if err := credit.Insert(ctx, s.db, boil.Infer()); err != nil {
		return nil, errors.Wrap(err, "failed to insert credit")
	}

	log.Info().
		Str("user_id", wallet.UserID).
		Str("address", transaction.ToAddr).
		Str("token_symbol", token.TokenSymbol).
		Str("amount", transaction.Amount).
		Str("tx_hash", transaction.TXHash).
		Msg("Credit created for deposit")

	return credit, nil
}

// GetPendingDeposits 查询待确认的充值
func (s *service) GetPendingDeposits(ctx context.Context, chainID int) ([]*models.Transaction, error) {
	transactions, err := models.Transactions(
		models.TransactionWhere.ChainID.EQ(chainID),
		models.TransactionWhere.Type.EQ("deposit"),
		models.TransactionWhere.Status.IN([]string{"confirmed", "safe"}),
		qm.OrderBy("block_no ASC"),
	).All(ctx, s.db)

	if err != nil {
		return nil, errors.Wrap(err, "failed to query pending deposits")
	}

	return transactions, nil
}

// getTokenInfo 获取代币信息
func (s *service) getTokenInfo(ctx context.Context, chainID int, tokenAddr string) (*models.Token, error) {
	// 如果是原生代币（tokenAddr 为空），查找原生代币
	if tokenAddr == "" {
		chain, err := models.Chains(
			models.ChainWhere.ChainID.EQ(chainID),
		).One(ctx, s.db)

		if err != nil {
			return nil, errors.Wrap(err, "failed to get chain config")
		}

		// 查找原生代币
		token, err := models.Tokens(
			models.TokenWhere.ChainID.EQ(chainID),
			models.TokenWhere.IsNative.EQ(true),
		).One(ctx, s.db)

		if err != nil {
			// 如果原生代币不存在，创建一个默认记录
			return s.createNativeToken(ctx, chainID, chain.NativeTokenSymbol)
		}

		return token, nil
	}

	// 查找 ERC20 代币
	token, err := models.Tokens(
		models.TokenWhere.ChainID.EQ(chainID),
		models.TokenWhere.TokenAddress.EQ(null.StringFrom(strings.ToLower(tokenAddr))),
	).One(ctx, s.db)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 代币不存在，返回错误（需要先添加代币信息）
			return nil, errors.Errorf("token not found: chain_id=%d, token_addr=%s", chainID, tokenAddr)
		}
		return nil, errors.Wrap(err, "failed to get token")
	}

	return token, nil
}

// createNativeToken 创建原生代币记录（如果不存在）
func (s *service) createNativeToken(ctx context.Context, chainID int, symbol string) (*models.Token, error) {
	token := &models.Token{
		ChainID:      chainID,
		ChainType:    "evm",
		TokenAddress: null.String{},
		TokenSymbol:  symbol,
		TokenName:    null.StringFrom(symbol),
		Decimals:     nativeTokenDecimals,
		IsNative:     true,
		TokenType:    null.StringFrom("native"),
		IsActive:     true,
	}

	if err := token.Insert(ctx, s.db, boil.Infer()); err != nil {
		return nil, errors.Wrap(err, "failed to insert native token")
	}

	return token, nil
}

// creditExists 检查 Credits 记录是否存在
func (s *service) creditExists(ctx context.Context, transactionID string) (bool, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) 
		FROM credits 
		WHERE reference_id = $1 AND reference_type = 'blockchain_tx'
	`, transactionID).Scan(&count)

	if err != nil {
		return false, errors.Wrap(err, "failed to check credit existence")
	}

	return count > 0, nil
}
