package scan

import (
	"context"
	"database/sql"
	"math/big"
	"time"

	"github/chapool/go-wallet/internal/models"
	"github/chapool/go-wallet/internal/wallet/deposit"

	"github.com/aarondl/sqlboiler/v4/boil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

// chainScanner 单个链的扫描器
type chainScanner struct {
	db             *sql.DB
	client         *RPCClient
	depositService deposit.Service
	chainID        int
	scanInterval   time.Duration
	blockBatchSize int
	stopCh         chan struct{}
}

// newChainScanner 创建新的链扫描器
func newChainScanner(db *sql.DB, client *RPCClient, depositService deposit.Service, chainID int, scanInterval time.Duration, blockBatchSize int) *chainScanner {
	return &chainScanner{
		db:             db,
		client:         client,
		depositService: depositService,
		chainID:        chainID,
		scanInterval:   scanInterval,
		blockBatchSize: blockBatchSize,
		stopCh:         make(chan struct{}),
	}
}

// start 启动扫描循环
func (s *chainScanner) start(ctx context.Context) error {
	log.Info().Int("chain_id", s.chainID).Msg("Starting chain scanner")

	// 获取起始区块号
	startBlock, err := s.getStartBlock(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get start block")
	}

	// 启动扫描循环
	go s.scanLoop(ctx, startBlock)

	return nil
}

// getStartBlock 获取起始扫描区块号
func (s *chainScanner) getStartBlock(ctx context.Context) (*big.Int, error) {
	// 查询已扫描的最大区块号
	var maxBlock sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT MAX(number) 
		FROM blocks 
		WHERE chain_id = $1 AND status != 'orphaned'
	`, s.chainID).Scan(&maxBlock)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, errors.Wrap(err, "failed to query max block")
	}

	if maxBlock.Valid {
		// 从下一个区块开始扫描
		return big.NewInt(maxBlock.Int64 + 1), nil
	}

	// 如果没有已扫描的区块，从最新区块开始扫描（向前扫描）
	// 注意：这会导致历史区块被跳过，如果需要扫描历史区块，应该手动触发扫描
	latestBlock, err := s.client.GetLatestBlockNumber(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get latest block number")
	}

	log.Info().
		Int("chain_id", s.chainID).
		Str("start_block", latestBlock.String()).
		Msg("No previous blocks found, starting from latest block")

	return latestBlock, nil
}

// scanLoop 扫描循环
func (s *chainScanner) scanLoop(ctx context.Context, startBlock *big.Int) {
	currentBlock := new(big.Int).Set(startBlock)
	ticker := time.NewTicker(s.scanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Int("chain_id", s.chainID).Msg("Chain scanner stopped by context")
			return
		case <-s.stopCh:
			log.Info().Int("chain_id", s.chainID).Msg("Chain scanner stopped")
			return
		case <-ticker.C:
			// 获取最新区块号
			latestBlock, err := s.client.GetLatestBlockNumber(ctx)
			if err != nil {
				log.Error().
					Int("chain_id", s.chainID).
					Err(err).
					Msg("Failed to get latest block number")
				continue
			}

			// 批量扫描区块
			for currentBlock.Cmp(latestBlock) <= 0 {
				// 计算批次结束区块号
				endBlock := new(big.Int).Add(currentBlock, big.NewInt(int64(s.blockBatchSize-1)))
				if endBlock.Cmp(latestBlock) > 0 {
					endBlock = new(big.Int).Set(latestBlock)
				}

				// 扫描批次
				if err := s.scanBlockRange(ctx, currentBlock, endBlock); err != nil {
					log.Error().
						Int("chain_id", s.chainID).
						Str("start_block", currentBlock.String()).
						Str("end_block", endBlock.String()).
						Err(err).
						Msg("Failed to scan block range")
					break
				}

				// 更新当前区块号
				currentBlock = new(big.Int).Add(endBlock, big.NewInt(1))
			}

			s.runPostScanHooks(ctx, latestBlock)
		}
	}
}

// scanBlockRange 扫描区块范围
func (s *chainScanner) scanBlockRange(ctx context.Context, startBlock, endBlock *big.Int) error {
	current := new(big.Int).Set(startBlock)

	for current.Cmp(endBlock) <= 0 {
		if err := s.scanBlock(ctx, current); err != nil {
			return errors.Wrapf(err, "failed to scan block %s", current.String())
		}

		current = new(big.Int).Add(current, big.NewInt(1))
	}

	return nil
}

// scanBlock 扫描单个区块
func (s *chainScanner) scanBlock(ctx context.Context, blockNumber *big.Int) error {
	// 获取区块
	block, err := s.client.GetBlockByNumber(ctx, blockNumber)
	if err != nil {
		return errors.Wrapf(err, "failed to get block %s", blockNumber.String())
	}

	// 检测区块重组
	reorgDetector := newReorgDetector(s.db, s.chainID)
	if err := reorgDetector.detectAndHandleReorg(ctx, block); err != nil {
		return errors.Wrap(err, "failed to detect and handle reorg")
	}

	// 检查区块是否已存在
	exists, err := s.blockExists(ctx, block.Hash().Hex(), blockNumber.Int64())
	if err != nil {
		return errors.Wrap(err, "failed to check block existence")
	}

	if exists {
		// 区块已存在，跳过
		return nil
	}

	// 保存区块信息
	if err := s.saveBlock(ctx, block); err != nil {
		return errors.Wrap(err, "failed to save block")
	}

	// 处理区块中的交易
	if err := s.processBlockTransactions(ctx, block); err != nil {
		return errors.Wrap(err, "failed to process block transactions")
	}

	log.Debug().
		Int("chain_id", s.chainID).
		Str("block_hash", block.Hash().Hex()).
		Int64("block_number", blockNumber.Int64()).
		Int("tx_count", len(block.Transactions())).
		Msg("Block scanned successfully")

	return nil
}

// blockExists 检查区块是否已存在
func (s *chainScanner) blockExists(ctx context.Context, blockHash string, blockNumber int64) (bool, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) 
		FROM blocks 
		WHERE chain_id = $1 AND (hash = $2 OR number = $3)
	`, s.chainID, blockHash, blockNumber).Scan(&count)

	if err != nil {
		return false, errors.Wrap(err, "failed to check block existence")
	}

	return count > 0, nil
}

// saveBlock 保存区块信息
func (s *chainScanner) saveBlock(ctx context.Context, block *types.Block) error {
	blockModel := &models.Block{
		Hash:       block.Hash().Hex(),
		ChainID:    s.chainID,
		ParentHash: block.ParentHash().Hex(),
		Number:     block.Number().Int64(),
		Timestamp:  int64(block.Time()), //nolint:gosec // Block timestamp is safe to convert
		Status:     "confirmed",         // 初始状态为 confirmed
	}

	return blockModel.Insert(ctx, s.db, boil.Infer())
}

// processBlockTransactions 处理区块中的交易
//
//nolint:unparam // Error return is required for future error handling
func (s *chainScanner) processBlockTransactions(ctx context.Context, block *types.Block) error {
	analyzer := newAnalyzer(s.db)

	// 获取所有交易的收据
	for _, tx := range block.Transactions() {
		receipt, err := s.client.GetTransactionReceipt(ctx, tx.Hash())
		if err != nil {
			log.Warn().
				Str("tx_hash", tx.Hash().Hex()).
				Err(err).
				Msg("Failed to get transaction receipt, skipping")
			continue
		}

		// 分析交易
		if err := analyzer.analyzeTransaction(ctx, s.chainID, tx, receipt, block.Number(), block.Hash()); err != nil {
			log.Warn().
				Str("tx_hash", tx.Hash().Hex()).
				Err(err).
				Msg("Failed to analyze transaction, skipping")
			// 继续处理其他交易
			continue
		}
	}

	return nil
}

func (s *chainScanner) runPostScanHooks(ctx context.Context, latestBlock *big.Int) {
	if s.depositService == nil || latestBlock == nil {
		return
	}

	if err := s.depositService.UpdateConfirmationStatus(ctx, s.chainID, latestBlock.Int64()); err != nil {
		log.Error().
			Int("chain_id", s.chainID).
			Err(err).
			Msg("Failed to update deposit confirmations")
	}

	if err := s.depositService.ProcessFinalizedDeposits(ctx, s.chainID); err != nil {
		log.Error().
			Int("chain_id", s.chainID).
			Err(err).
			Msg("Failed to process finalized deposits")
	}
}
