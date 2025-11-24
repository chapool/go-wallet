package deposit

import (
	"context"
	"database/sql"
	"math/big"
	"time"

	"github.com/aarondl/null/v8"
	"github.com/aarondl/sqlboiler/v4/boil"
	"github.com/aarondl/sqlboiler/v4/queries/qm"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github/chapool/go-wallet/internal/models"
)

const (
	defaultConfirmationBlocks = 12
	defaultFinalizedBlocks    = 32
)

// transactionStatusProcessor 交易状态处理器
type transactionStatusProcessor struct {
	db *sql.DB
}

// newTransactionStatusProcessor 创建交易状态处理器
func newTransactionStatusProcessor(db *sql.DB) *transactionStatusProcessor {
	return &transactionStatusProcessor{db: db}
}

// updateTransactionStatus 更新交易状态（根据确认数）
func (p *transactionStatusProcessor) updateTransactionStatus(ctx context.Context, chainID int, latestBlockNumber *big.Int) error {
	// 获取链配置
	chain, err := models.Chains(
		models.ChainWhere.ChainID.EQ(chainID),
	).One(ctx, p.db)

	if err != nil {
		return errors.Wrapf(err, "failed to get chain config for chain_id=%d", chainID)
	}

	// 获取确认区块数和终结区块数
	confirmationBlocks := int64(defaultConfirmationBlocks)
	if chain.ConfirmationBlocks.Valid {
		confirmationBlocks = int64(chain.ConfirmationBlocks.Int)
	}

	finalizedBlocks := int64(defaultFinalizedBlocks)
	if chain.FinalizedBlocks.Valid {
		finalizedBlocks = int64(chain.FinalizedBlocks.Int)
	}

	// 查询所有待更新的交易（包括所有状态，因为确认数需要持续更新）
	// 注意：finalized 状态的交易也需要更新确认数，以便追踪
	transactions, err := models.Transactions(
		models.TransactionWhere.ChainID.EQ(chainID),
		models.TransactionWhere.Status.IN([]string{"confirmed", "safe", "finalized"}),
		qm.OrderBy("block_no ASC"),
	).All(ctx, p.db)

	if err != nil {
		return errors.Wrap(err, "failed to query transactions")
	}

	log.Info().
		Int("chain_id", chainID).
		Int64("latest_block", latestBlockNumber.Int64()).
		Int64("confirmation_blocks", confirmationBlocks).
		Int64("finalized_blocks", finalizedBlocks).
		Int("pending_tx_count", len(transactions)).
		Msg("Updating transaction confirmation status")

	updatedCount := 0
	for _, tx := range transactions {
		// 计算确认数
		confirmationCount := latestBlockNumber.Int64() - tx.BlockNo

		// 获取当前确认数（用于对比）
		currentConfirmationCount := int64(0)
		if tx.ConfirmationCount.Valid {
			currentConfirmationCount = int64(tx.ConfirmationCount.Int)
		}

		log.Info().
			Int("chain_id", chainID).
			Str("tx_hash", tx.TXHash).
			Str("tx_id", tx.ID).
			Int64("tx_block_no", tx.BlockNo).
			Int64("latest_block", latestBlockNumber.Int64()).
			Int64("current_confirmation_count", currentConfirmationCount).
			Int64("calculated_confirmation_count", confirmationCount).
			Str("current_status", tx.Status).
			Msg("Calculating confirmation count for transaction")

		// 确保确认数不为负数（防止区块重组等情况）
		if confirmationCount < 0 {
			log.Warn().
				Int("chain_id", chainID).
				Str("tx_hash", tx.TXHash).
				Int64("block_no", tx.BlockNo).
				Int64("latest_block", latestBlockNumber.Int64()).
				Int64("confirmation_count", confirmationCount).
				Msg("Negative confirmation count detected, skipping status update")
			continue
		}

		// 更新确认数
		tx.ConfirmationCount = null.IntFrom(int(confirmationCount))

		// 根据确认数更新状态
		var newStatus string
		switch {
		case confirmationCount >= finalizedBlocks:
			newStatus = "finalized"
		case confirmationCount >= confirmationBlocks:
			newStatus = "safe"
		default:
			newStatus = "confirmed"
		}

		log.Debug().
			Int("chain_id", chainID).
			Str("tx_hash", tx.TXHash).
			Str("current_status", tx.Status).
			Str("calculated_status", newStatus).
			Int64("confirmation_count", confirmationCount).
			Int64("block_no", tx.BlockNo).
			Int64("latest_block", latestBlockNumber.Int64()).
			Int64("finalized_blocks", finalizedBlocks).
			Msg("Evaluating transaction status update")

		// 确保 ConfirmationCount 字段被正确设置
		tx.ConfirmationCount = null.IntFrom(int(confirmationCount))

		// 如果状态发生变化，更新状态和确认数
		if tx.Status != newStatus {
			if err := p.updateTransactionStatusAndConfirmation(ctx, tx, newStatus, confirmationCount, chainID, latestBlockNumber.Int64()); err != nil {
				log.Error().
					Str("tx_hash", tx.TXHash).
					Err(err).
					Msg("Failed to update transaction status and confirmation count")
				continue
			}
			updatedCount++
			continue
		}

		// 状态没有变化，只更新确认数
		if err := p.updateConfirmationCountOnly(ctx, tx, confirmationCount, chainID, latestBlockNumber.Int64()); err != nil {
			log.Error().
				Int("chain_id", chainID).
				Str("tx_hash", tx.TXHash).
				Int64("confirmation_count", confirmationCount).
				Err(err).
				Msg("Failed to update transaction confirmation count")
			continue
		}
	}

	if updatedCount > 0 {
		log.Info().
			Int("chain_id", chainID).
			Int("updated_count", updatedCount).
			Msg("Transaction statuses updated")
	}

	return nil
}

func (p *transactionStatusProcessor) updateCreditStatus(ctx context.Context, tx *models.Transaction, txStatus string) error {
	creditStatus, ok := mapTransactionToCreditStatus(txStatus)
	if !ok {
		return nil
	}

	_, err := models.Credits(
		models.CreditWhere.ReferenceID.EQ(tx.ID),
		models.CreditWhere.ReferenceType.EQ(models.ReferenceTypeBlockchainTX),
	).UpdateAll(ctx, p.db, models.M{
		models.CreditColumns.Status:    creditStatus,
		models.CreditColumns.UpdatedAt: time.Now(),
	})
	if err != nil {
		return errors.Wrap(err, "failed to update credit status")
	}
	return nil
}

func mapTransactionToCreditStatus(txStatus string) (string, bool) {
	switch txStatus {
	case models.TransactionStatusConfirmed, models.TransactionStatusSafe:
		return models.CreditStatusConfirmed, true
	case models.TransactionStatusFinalized:
		return models.CreditStatusFinalized, true
	case models.TransactionStatusFailed:
		return models.CreditStatusFailed, true
	default:
		return "", false
	}
}

// updateTransactionStatusAndConfirmation 更新交易状态和确认数
func (p *transactionStatusProcessor) updateTransactionStatusAndConfirmation(
	ctx context.Context,
	tx *models.Transaction,
	newStatus string,
	confirmationCount int64,
	chainID int,
	latestBlock int64,
) error {
	oldStatus := tx.Status
	tx.Status = newStatus
	tx.ConfirmationCount = null.IntFrom(int(confirmationCount))

	_, err := tx.Update(ctx, p.db, boil.Whitelist(
		models.TransactionColumns.Status,
		models.TransactionColumns.ConfirmationCount,
		models.TransactionColumns.UpdatedAt,
	))
	if err != nil {
		return errors.Wrap(err, "failed to update transaction status and confirmation count")
	}

	// 同步更新 credits 状态
	if err := p.updateCreditStatus(ctx, tx, newStatus); err != nil {
		log.Error().
			Str("tx_hash", tx.TXHash).
			Err(err).
			Msg("Failed to sync credit status with transaction status")
		// 不返回错误，因为交易状态已经更新成功
	}

	log.Info().
		Int("chain_id", chainID).
		Str("tx_hash", tx.TXHash).
		Str("old_status", oldStatus).
		Str("new_status", newStatus).
		Int64("confirmation_count", confirmationCount).
		Int64("block_no", tx.BlockNo).
		Int64("latest_block", latestBlock).
		Msg("Transaction status updated")

	return nil
}

// updateConfirmationCountOnly 只更新确认数（状态不变）
func (p *transactionStatusProcessor) updateConfirmationCountOnly(
	ctx context.Context,
	tx *models.Transaction,
	confirmationCount int64,
	chainID int,
	latestBlock int64,
) error {
	tx.ConfirmationCount = null.IntFrom(int(confirmationCount))

	rowsAffected, err := tx.Update(ctx, p.db, boil.Whitelist(
		models.TransactionColumns.ConfirmationCount,
		models.TransactionColumns.UpdatedAt,
	))
	if err != nil {
		return errors.Wrap(err, "failed to update transaction confirmation count")
	}

	if rowsAffected == 0 {
		log.Warn().
			Int("chain_id", chainID).
			Str("tx_hash", tx.TXHash).
			Str("tx_id", tx.ID).
			Int64("confirmation_count", confirmationCount).
			Msg("No rows affected when updating confirmation count")
	}

	log.Info().
		Int("chain_id", chainID).
		Str("tx_hash", tx.TXHash).
		Str("status", tx.Status).
		Int64("confirmation_count", confirmationCount).
		Int64("block_no", tx.BlockNo).
		Int64("latest_block", latestBlock).
		Int64("rows_affected", rowsAffected).
		Msg("Transaction confirmation count updated (status unchanged)")

	return nil
}
