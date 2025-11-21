package chain

import (
	"context"
	"database/sql"
	"strings"

	"github.com/aarondl/sqlboiler/v4/queries/qm"
	"github.com/pkg/errors"
	"github/chapool/go-wallet/internal/models"
)

// service 实现 Service 接口
type service struct {
	db *sql.DB
}

// NewService 创建链配置服务
//
//nolint:ireturn
func NewService(db *sql.DB) Service {
	return &service{db: db}
}

// GetChain 根据 chain_id 查询链配置
func (s *service) GetChain(ctx context.Context, chainID int) (*models.Chain, error) {
	chain, err := models.Chains(
		models.ChainWhere.ChainID.EQ(chainID),
	).One(ctx, s.db)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("chain not found")
		}
		return nil, errors.Wrap(err, "failed to get chain")
	}

	return chain, nil
}

// ListChains 查询所有链配置
func (s *service) ListChains(ctx context.Context) ([]*models.Chain, error) {
	chains, err := models.Chains().All(ctx, s.db)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list chains")
	}

	return chains, nil
}

// GetActiveChains 查询启用的链配置
func (s *service) GetActiveChains(ctx context.Context) ([]*models.Chain, error) {
	chains, err := models.Chains(
		models.ChainWhere.IsActive.EQ(true),
		qm.OrderBy("chain_id ASC"),
	).All(ctx, s.db)

	if err != nil {
		return nil, errors.Wrap(err, "failed to get active chains")
	}

	return chains, nil
}

// ParseRPCURLs 解析 RPC URL（支持多个，逗号分隔）
func (s *service) ParseRPCURLs(rpcURL string) []string {
	if rpcURL == "" {
		return nil
	}

	urls := strings.Split(rpcURL, ",")
	result := make([]string, 0, len(urls))

	for _, url := range urls {
		url = strings.TrimSpace(url)
		if url != "" {
			result = append(result, url)
		}
	}

	return result
}
