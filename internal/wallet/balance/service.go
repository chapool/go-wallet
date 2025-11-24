//nolint:ireturn // 返回接口类型是预期的设计
package balance

import (
	"context"
	"database/sql"
	"math/big"

	"github.com/pkg/errors"
)

const (
	// bigFloatBase 用于 big.ParseFloat 的基数（十进制）
	bigFloatBase = 10
	// bigFloatPrecision 用于 big.ParseFloat 的精度位数
	bigFloatPrecision = 256
	// chainIDFilterSQL SQL 查询中的 chain_id 过滤条件
	chainIDFilterSQL = ` AND chain_id = $2`
)

// Service 余额服务接口
type Service interface {
	// GetPendingDepositBalance 获取充值中余额（状态为 pending/confirmed 的充值）
	GetPendingDepositBalance(ctx context.Context, userID string, chainID *int) (*Balance, error)

	// GetTotalBalance 获取用户总余额（所有 finalized 状态的 credits）
	GetTotalBalance(ctx context.Context, userID string, chainID *int) (*Balance, error)

	// GetTokenBalance 获取指定代币的余额详情
	GetTokenBalance(ctx context.Context, userID string, chainID int, tokenID int) (*TokenBalance, error)

	// GetBalanceByToken 按代币分组获取余额列表
	GetBalanceByToken(ctx context.Context, userID string, chainID *int) ([]*TokenBalance, error)

	// GetAvailableBalance 获取可用余额（用于提现检查）
	GetAvailableBalance(ctx context.Context, userID string, chainID int, tokenID int) (*big.Float, error)
}

// service 实现 Service 接口
type service struct {
	db *sql.DB
}

// NewService 创建余额服务
//
//nolint:ireturn // 返回接口类型是预期的设计
func NewService(db *sql.DB) Service {
	return &service{
		db: db,
	}
}

// Balance 余额信息
type Balance struct {
	TotalAmount *big.Float // 总余额（字符串转 big.Float）
	TokenCount  int        // 代币种类数
}

// TokenBalance 代币余额详情
type TokenBalance struct {
	TokenID     int
	TokenSymbol string
	ChainID     int
	Amount      *big.Float
	Status      string // 余额状态：pending, confirmed, finalized
}

// GetPendingDepositBalance 获取充值中余额（状态为 pending/confirmed 的充值）
func (s *service) GetPendingDepositBalance(ctx context.Context, userID string, chainID *int) (*Balance, error) {
	// 使用 SQL 聚合查询计算总余额
	var totalAmountStr string
	var tokenCount int

	query := `
		SELECT 
			COALESCE(SUM(amount::numeric), 0)::text as total_amount,
			COUNT(DISTINCT token_id) as token_count
		FROM credits
		WHERE user_id = $1 
			AND credit_type = 'deposit'
			AND status IN ('pending', 'confirmed')
	`
	args := []interface{}{userID}

	if chainID != nil {
		query += chainIDFilterSQL
		args = append(args, *chainID)
	}

	err := s.db.QueryRowContext(ctx, query, args...).Scan(&totalAmountStr, &tokenCount)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query pending deposit balance")
	}

	totalAmount, _, err := big.ParseFloat(totalAmountStr, bigFloatBase, bigFloatPrecision, big.ToNearestEven)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse total amount: %s", totalAmountStr)
	}

	return &Balance{
		TotalAmount: totalAmount,
		TokenCount:  tokenCount,
	}, nil
}

// GetTotalBalance 获取用户总余额（所有 finalized 状态的 credits）
func (s *service) GetTotalBalance(ctx context.Context, userID string, chainID *int) (*Balance, error) {
	// 使用 SQL 聚合查询计算总余额
	var totalAmountStr string
	var tokenCount int

	query := `
		SELECT 
			COALESCE(SUM(amount::numeric), 0)::text as total_amount,
			COUNT(DISTINCT token_id) as token_count
		FROM credits
		WHERE user_id = $1 
			AND status = 'finalized'
	`
	args := []interface{}{userID}

	if chainID != nil {
		query += chainIDFilterSQL
		args = append(args, *chainID)
	}

	err := s.db.QueryRowContext(ctx, query, args...).Scan(&totalAmountStr, &tokenCount)

	if err != nil {
		return nil, errors.Wrap(err, "failed to query total balance")
	}

	totalAmount, _, err := big.ParseFloat(totalAmountStr, bigFloatBase, bigFloatPrecision, big.ToNearestEven)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse total amount: %s", totalAmountStr)
	}

	return &Balance{
		TotalAmount: totalAmount,
		TokenCount:  tokenCount,
	}, nil
}

// GetTokenBalance 获取指定代币的余额详情
func (s *service) GetTokenBalance(ctx context.Context, userID string, chainID int, tokenID int) (*TokenBalance, error) {
	var totalAmountStr string
	var tokenSymbol string

	query := `
		SELECT 
			COALESCE(SUM(amount::numeric), 0)::text as total_amount,
			MAX(token_symbol) as token_symbol
		FROM credits
		WHERE user_id = $1 
			AND chain_id = $2
			AND token_id = $3
			AND status = 'finalized'
		GROUP BY token_id
	`

	err := s.db.QueryRowContext(ctx, query, userID, chainID, tokenID).Scan(&totalAmountStr, &tokenSymbol)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 余额为 0，返回空余额
			return &TokenBalance{
				TokenID:     tokenID,
				TokenSymbol: "",
				ChainID:     chainID,
				Amount:      big.NewFloat(0),
				Status:      "finalized",
			}, nil
		}
		return nil, errors.Wrap(err, "failed to query token balance")
	}

	totalAmount, _, err := big.ParseFloat(totalAmountStr, bigFloatBase, bigFloatPrecision, big.ToNearestEven)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse amount: %s", totalAmountStr)
	}

	return &TokenBalance{
		TokenID:     tokenID,
		TokenSymbol: tokenSymbol,
		ChainID:     chainID,
		Amount:      totalAmount,
		Status:      "finalized",
	}, nil
}

// GetBalanceByToken 按代币分组获取余额列表
func (s *service) GetBalanceByToken(ctx context.Context, userID string, chainID *int) ([]*TokenBalance, error) {
	query := `
		SELECT 
			token_id,
			token_symbol,
			chain_id,
			COALESCE(SUM(amount::numeric), 0)::text as total_amount,
			'finalized' as status
		FROM credits
		WHERE user_id = $1 
			AND status = 'finalized'
	`
	args := []interface{}{userID}

	if chainID != nil {
		query += chainIDFilterSQL
		args = append(args, *chainID)
	}

	query += `
		GROUP BY token_id, token_symbol, chain_id
		HAVING SUM(amount::numeric) > 0
		ORDER BY chain_id, token_id
	`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query balance by token")
	}
	defer rows.Close()

	var balances []*TokenBalance
	for rows.Next() {
		var tokenID, chainIDVal int
		var tokenSymbol, totalAmountStr, status string

		if err := rows.Scan(&tokenID, &tokenSymbol, &chainIDVal, &totalAmountStr, &status); err != nil {
			return nil, errors.Wrap(err, "failed to scan token balance")
		}

		totalAmount, _, err := big.ParseFloat(totalAmountStr, bigFloatBase, bigFloatPrecision, big.ToNearestEven)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to parse amount: %s", totalAmountStr)
		}

		balances = append(balances, &TokenBalance{
			TokenID:     tokenID,
			TokenSymbol: tokenSymbol,
			ChainID:     chainIDVal,
			Amount:      totalAmount,
			Status:      status,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate token balances")
	}

	return balances, nil
}

// GetAvailableBalance 获取可用余额
// 计算逻辑：SUM(finalized credits) + SUM(pending/processing/frozen withdraw credits)
// 前提：提现时必须创建负数金额的 credits 记录
func (s *service) GetAvailableBalance(ctx context.Context, userID string, chainID int, tokenID int) (*big.Float, error) {
	var totalAmountStr string

	query := `
		SELECT COALESCE(SUM(amount::numeric), 0)::text
		FROM credits
		WHERE user_id = $1 
			AND chain_id = $2
			AND token_id = $3
			AND (
				status = 'finalized' 
				OR (credit_type = 'withdraw' AND status IN ('pending', 'processing', 'frozen', 'signing'))
			)
	`

	err := s.db.QueryRowContext(ctx, query, userID, chainID, tokenID).Scan(&totalAmountStr)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query available balance")
	}

	amount, _, err := big.ParseFloat(totalAmountStr, bigFloatBase, bigFloatPrecision, big.ToNearestEven)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse amount: %s", totalAmountStr)
	}

	return amount, nil
}
