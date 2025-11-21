package deposit

import (
	"context"

	"github/chapool/go-wallet/internal/models"
)

// Service 定义充值服务接口
type Service interface {
	// ProcessDeposit 处理充值交易（创建 Credits 记录）
	ProcessDeposit(ctx context.Context, transaction *models.Transaction) error

	// UpdateConfirmationStatus 更新交易确认状态
	UpdateConfirmationStatus(ctx context.Context, chainID int, latestBlockNumber int64) error

	// CreateCredit 创建 Credits 记录
	CreateCredit(ctx context.Context, transaction *models.Transaction) (*models.Credit, error)

	// GetPendingDeposits 查询待确认的充值
	GetPendingDeposits(ctx context.Context, chainID int) ([]*models.Transaction, error)

	// ProcessFinalizedDeposits 处理已终结的充值（确保生成 Credits 记录）
	ProcessFinalizedDeposits(ctx context.Context, chainID int) error
}
