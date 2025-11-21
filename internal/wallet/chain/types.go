package chain

import (
	"context"

	"github/chapool/go-wallet/internal/models"
)

// Service 定义链配置服务接口
type Service interface {
	// GetChain 根据 chain_id 查询链配置
	GetChain(ctx context.Context, chainID int) (*models.Chain, error)

	// ListChains 查询所有链配置
	ListChains(ctx context.Context) ([]*models.Chain, error)

	// GetActiveChains 查询启用的链配置
	GetActiveChains(ctx context.Context) ([]*models.Chain, error)

	// ParseRPCURLs 解析 RPC URL（支持多个，逗号分隔）
	ParseRPCURLs(rpcURL string) []string
}
