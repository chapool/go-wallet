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

	// 查询所有待更新的交易
	transactions, err := models.Transactions(
		models.TransactionWhere.ChainID.EQ(chainID),
		models.TransactionWhere.Status.IN([]string{"confirmed", "safe"}),
		qm.OrderBy("block_no ASC"),
	).All(ctx, p.db)

	if err != nil {
		return errors.Wrap(err, "failed to query transactions")
	}

	updatedCount := 0
	for _, tx := range transactions {
		// 计算确认数
		confirmationCount := latestBlockNumber.Int64() - tx.BlockNo

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

		// 如果状态发生变化，更新
		if tx.Status != newStatus {
			oldStatus := tx.Status
			tx.Status = newStatus
			_, err := tx.Update(ctx, p.db, boil.Whitelist(models.TransactionColumns.Status, models.TransactionColumns.ConfirmationCount, models.TransactionColumns.UpdatedAt))
			if err != nil {
				log.Error().
					Str("tx_hash", tx.TXHash).
					Err(err).
					Msg("Failed to update transaction status")
				continue
			}

			if err := p.updateCreditStatus(ctx, tx, newStatus); err != nil {
				log.Error().
					Str("tx_hash", tx.TXHash).
					Err(err).
					Msg("Failed to sync credit status with transaction status")
			}

			log.Debug().
				Int("chain_id", chainID).
				Str("tx_hash", tx.TXHash).
				Str("old_status", oldStatus).
				Str("new_status", newStatus).
				Int64("confirmation_count", confirmationCount).
				Msg("Transaction status updated")

			updatedCount++
		} else {
			// 只更新确认数
			_, err := tx.Update(ctx, p.db, boil.Whitelist(models.TransactionColumns.ConfirmationCount, models.TransactionColumns.UpdatedAt))
			if err != nil {
				log.Error().
					Str("tx_hash", tx.TXHash).
					Err(err).
					Msg("Failed to update transaction confirmation count")
				continue
			}
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
