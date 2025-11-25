package scan

import (
	"context"
	"database/sql"
	"math/big"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github/chapool/go-wallet/internal/wallet/chain"
	"github/chapool/go-wallet/internal/wallet/deposit"
)

// service 实现 Service 接口
type service struct {
	db                    *sql.DB
	chainService          chain.Service
	depositService        deposit.Service
	withdrawStatusUpdater WithdrawStatusUpdater
	clients               map[int]*RPCClient // chainID -> RPCClient
	clientsMu             sync.RWMutex
	scanners              map[int]*chainScanner // chainID -> scanner
	scannersMu            sync.RWMutex
	scanInterval          time.Duration
	blockBatchSize        int
}

// NewService 创建扫描服务
//
//nolint:ireturn
func NewService(db *sql.DB, chainService chain.Service, depositService deposit.Service, withdrawStatusUpdater WithdrawStatusUpdater, scanInterval time.Duration, blockBatchSize int) Service {
	return &service{
		db:                    db,
		chainService:          chainService,
		depositService:        depositService,
		withdrawStatusUpdater: withdrawStatusUpdater,
		clients:               make(map[int]*RPCClient),
		scanners:              make(map[int]*chainScanner),
		scanInterval:          scanInterval,
		blockBatchSize:        blockBatchSize,
	}
}

// GetScanProgress 获取扫描进度
func (s *service) GetScanProgress(ctx context.Context, chainID int) (*ScanProgress, error) {
	// 获取最新区块号
	client, err := s.getOrCreateClient(ctx, chainID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get RPC client for chain_id=%d", chainID)
	}

	latestBlock, err := client.GetLatestBlockNumber(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get latest block number")
	}

	// 查询已扫描的最大区块号
	var scannedTo sql.NullInt64
	err = s.db.QueryRowContext(ctx, `
		SELECT MAX(number) 
		FROM blocks 
		WHERE chain_id = $1 AND status != 'orphaned'
	`, chainID).Scan(&scannedTo)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, errors.Wrap(err, "failed to query scanned block")
	}

	progress := &ScanProgress{
		ChainID:     chainID,
		LatestBlock: latestBlock,
		Status:      "scanning",
	}

	if scannedTo.Valid {
		progress.ScannedTo = big.NewInt(scannedTo.Int64)
	} else {
		progress.ScannedTo = big.NewInt(0)
	}

	return progress, nil
}

// StartMultiChainScan 启动多链并发扫描
func (s *service) StartMultiChainScan(ctx context.Context) error {
	log.Info().Msg("Starting multi-chain scan")

	// 获取所有启用的链
	chains, err := s.chainService.GetActiveChains(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get active chains")
	}

	if len(chains) == 0 {
		log.Warn().Msg("No active chains found")
		return nil
	}

	log.Info().Int("active_chains_count", len(chains)).Msg("Found active chains")

	// 为每个链启动独立的扫描 goroutine
	for _, chainConfig := range chains {
		chainID := chainConfig.ChainID
		go func(cid int) {
			if err := s.StartChainScan(ctx, cid); err != nil {
				log.Error().
					Int("chain_id", cid).
					Err(err).
					Msg("Failed to start chain scan")
			}
		}(chainID)
	}

	log.Info().Msg("Multi-chain scan started, all scanners running in background")
	return nil
}

// StartChainScan 启动指定链的扫描
func (s *service) StartChainScan(ctx context.Context, chainID int) error {
	log.Info().Int("chain_id", chainID).Msg("Starting chain scan")

	// 检查链是否启用
	chainConfig, err := s.chainService.GetChain(ctx, chainID)
	if err != nil {
		return errors.Wrapf(err, "failed to get chain config for chain_id=%d", chainID)
	}

	if !chainConfig.IsActive {
		return errors.Errorf("chain %d is not active, cannot start scan", chainID)
	}

	// 获取或创建 RPC 客户端
	client, err := s.getOrCreateClient(ctx, chainID)
	if err != nil {
		return errors.Wrapf(err, "failed to get RPC client for chain_id=%d", chainID)
	}

	// 创建或获取扫描器
	s.scannersMu.Lock()
	scanner, exists := s.scanners[chainID]
	if !exists {
		scanner = newChainScanner(s.db, client, s.depositService, s.withdrawStatusUpdater, chainID, s.scanInterval, s.blockBatchSize)
		s.scanners[chainID] = scanner
	}
	s.scannersMu.Unlock()

	// 启动扫描
	return scanner.start(ctx)
}

// ScanChainBlock 扫描指定链的单个区块
func (s *service) ScanChainBlock(ctx context.Context, chainID int, blockNumber *big.Int) error {
	// 获取或创建 RPC 客户端
	client, err := s.getOrCreateClient(ctx, chainID)
	if err != nil {
		return errors.Wrapf(err, "failed to get RPC client for chain_id=%d", chainID)
	}

	// 创建临时扫描器
	scanner := newChainScanner(s.db, client, s.depositService, s.withdrawStatusUpdater, chainID, s.scanInterval, s.blockBatchSize)
	if err := scanner.scanBlock(ctx, blockNumber); err != nil {
		return err
	}
	scanner.runPostScanHooks(ctx, blockNumber)
	return nil
}

// GetClient 获取指定链的 RPC 客户端
func (s *service) GetClient(ctx context.Context, chainID int) (*RPCClient, error) {
	return s.getOrCreateClient(ctx, chainID)
}

// getOrCreateClient 获取或创建 RPC 客户端
func (s *service) getOrCreateClient(ctx context.Context, chainID int) (*RPCClient, error) {
	s.clientsMu.RLock()
	client, exists := s.clients[chainID]
	s.clientsMu.RUnlock()

	if exists && client != nil {
		return client, nil
	}

	// 获取链配置
	chainConfig, err := s.chainService.GetChain(ctx, chainID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get chain config for chain_id=%d", chainID)
	}

	// 解析 RPC URLs
	urls := s.chainService.ParseRPCURLs(chainConfig.RPCURL)
	if len(urls) == 0 {
		return nil, errors.Errorf("no valid RPC URLs for chain_id=%d", chainID)
	}

	// 创建 RPC 客户端
	client, err = NewRPCClient(urls)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create RPC client for chain_id=%d", chainID)
	}

	s.clientsMu.Lock()
	s.clients[chainID] = client
	s.clientsMu.Unlock()

	return client, nil
}
