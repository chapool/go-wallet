package scan

import (
	"context"
	"database/sql"

	"github.com/aarondl/sqlboiler/v4/queries/qm"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github/chapool/go-wallet/internal/models"
)

// reorgDetector 区块重组检测器
type reorgDetector struct {
	db      *sql.DB
	chainID int
}

// newReorgDetector 创建重组检测器
func newReorgDetector(db *sql.DB, chainID int) *reorgDetector {
	return &reorgDetector{
		db:      db,
		chainID: chainID,
	}
}

// detectAndHandleReorg 检测并处理区块重组
func (r *reorgDetector) detectAndHandleReorg(ctx context.Context, block *types.Block) error {
	// 检查父区块是否存在且匹配
	parentBlock, err := r.getBlockByNumber(ctx, block.Number().Int64()-1)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return errors.Wrap(err, "failed to get parent block")
	}

	// 如果是第一个区块，不需要检查
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}

	// 检查父区块哈希是否匹配
	if parentBlock.Hash != block.ParentHash().Hex() {
		log.Warn().
			Int("chain_id", r.chainID).
			Int64("block_number", block.Number().Int64()).
			Str("expected_parent", parentBlock.Hash).
			Str("actual_parent", block.ParentHash().Hex()).
			Msg("Block reorg detected")

		// 处理重组：回滚到父区块之前
		if err := r.handleReorg(ctx, block.Number().Int64()-1); err != nil {
			return errors.Wrap(err, "failed to handle reorg")
		}
	}

	return nil
}

// handleReorg 处理区块重组
func (r *reorgDetector) handleReorg(ctx context.Context, reorgBlockNumber int64) error {
	log.Info().
		Int("chain_id", r.chainID).
		Int64("reorg_block_number", reorgBlockNumber).
		Msg("Handling block reorg")

	// 查找需要回滚的区块（从 reorgBlockNumber + 1 开始的所有后续区块）
	orphanedBlocks, err := models.Blocks(
		models.BlockWhere.ChainID.EQ(r.chainID),
		models.BlockWhere.Number.GT(reorgBlockNumber),
		models.BlockWhere.Status.NEQ("orphaned"),
		qm.OrderBy("number DESC"),
	).All(ctx, r.db)

	if err != nil {
		return errors.Wrap(err, "failed to query orphaned blocks")
	}

	if len(orphanedBlocks) == 0 {
		return nil
	}

	log.Info().
		Int("chain_id", r.chainID).
		Int("orphaned_count", len(orphanedBlocks)).
		Msg("Found orphaned blocks to rollback")

	// 回滚每个区块
	for _, block := range orphanedBlocks {
		if err := r.rollbackBlock(ctx, block); err != nil {
			return errors.Wrapf(err, "failed to rollback block %d", block.Number)
		}
	}

	return nil
}

// rollbackBlock 回滚单个区块
func (r *reorgDetector) rollbackBlock(ctx context.Context, block *models.Block) error {
	log.Info().
		Int("chain_id", r.chainID).
		Int64("block_number", block.Number).
		Str("block_hash", block.Hash).
		Msg("Rolling back block")

	// 使用事务确保原子性
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to begin transaction")
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			log.Error().Err(rollbackErr).Msg("Failed to rollback transaction")
		}
	}()

	// 标记区块为 orphaned
	_, err = tx.ExecContext(ctx, `
		UPDATE blocks 
		SET status = 'orphaned', updated_at = NOW()
		WHERE chain_id = $1 AND hash = $2
	`, r.chainID, block.Hash)
	if err != nil {
		return errors.Wrap(err, "failed to update block status")
	}

	// 回滚相关交易状态
	_, err = tx.ExecContext(ctx, `
		UPDATE transactions 
		SET status = 'failed', updated_at = NOW()
		WHERE chain_id = $1 AND block_hash = $2
	`, r.chainID, block.Hash)
	if err != nil {
		return errors.Wrap(err, "failed to update transaction status")
	}

	// 回滚相关 Credits 记录（如果有）
	_, err = tx.ExecContext(ctx, `
		UPDATE credits 
		SET status = 'failed', updated_at = NOW()
		WHERE chain_id = $1 AND block_number = $2
	`, r.chainID, block.Number)
	if err != nil {
		return errors.Wrap(err, "failed to update credits status")
	}

	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit transaction")
	}
	return nil
}

// getBlockByNumber 根据区块号获取区块
func (r *reorgDetector) getBlockByNumber(ctx context.Context, blockNumber int64) (*models.Block, error) {
	block, err := models.Blocks(
		models.BlockWhere.ChainID.EQ(r.chainID),
		models.BlockWhere.Number.EQ(blockNumber),
		models.BlockWhere.Status.NEQ("orphaned"),
	).One(ctx, r.db)

	if err != nil {
		return nil, err
	}

	return block, nil
}
