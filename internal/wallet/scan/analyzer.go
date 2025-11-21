package scan

import (
	"context"
	"database/sql"
	"math/big"
	"strings"

	"github/chapool/go-wallet/internal/models"

	"github.com/aarondl/null/v8"
	"github.com/aarondl/sqlboiler/v4/boil"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

const (
	minTransferEventTopics = 3 // ERC20 Transfer 事件至少需要 3 个 topics
)

// ERC20 Transfer 事件签名
// Transfer(address indexed from, address indexed to, uint256 value)
var transferEventSignature = common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")

// analyzer 交易分析器
type analyzer struct {
	db *sql.DB
}

// newAnalyzer 创建交易分析器
func newAnalyzer(db *sql.DB) *analyzer {
	return &analyzer{db: db}
}

// analyzeTransaction 分析交易
func (a *analyzer) analyzeTransaction(ctx context.Context, chainID int, tx *types.Transaction, receipt *types.Receipt, blockNumber *big.Int, blockHash common.Hash) error {
	// 检查交易是否已存在
	exists, err := a.transactionExists(ctx, tx.Hash().Hex(), chainID)
	if err != nil {
		return errors.Wrap(err, "failed to check transaction existence")
	}

	if exists {
		// 交易已存在，跳过
		return nil
	}

	// 分析 ETH 转账
	if err := a.analyzeETHTransfer(ctx, chainID, tx, receipt, blockNumber, blockHash); err != nil {
		return errors.Wrap(err, "failed to analyze ETH transfer")
	}

	// 分析 ERC20 转账
	if err := a.analyzeERC20Transfers(ctx, chainID, tx, receipt, blockNumber, blockHash); err != nil {
		return errors.Wrap(err, "failed to analyze ERC20 transfers")
	}

	return nil
}

// analyzeETHTransfer 分析 ETH 转账
func (a *analyzer) analyzeETHTransfer(ctx context.Context, chainID int, tx *types.Transaction, receipt *types.Receipt, blockNumber *big.Int, blockHash common.Hash) error {
	// 检查交易是否成功
	if receipt.Status != types.ReceiptStatusSuccessful {
		return nil // 交易失败，跳过
	}

	// 获取交易发送方
	// 使用函数参数中的 chainID，而不是 tx.ChainId()，因为某些交易可能没有设置 chainID（返回 0）
	chainIDBig := big.NewInt(int64(chainID))
	from, err := types.Sender(types.LatestSignerForChainID(chainIDBig), tx)
	if err != nil {
		return errors.Wrap(err, "failed to get transaction sender")
	}

	to := tx.To()
	if to == nil {
		// 合约创建交易，跳过
		return nil
	}

	// 检查是否是充值交易（to 地址是用户钱包地址）
	toAddr := strings.ToLower(to.Hex())
	isDeposit, err := a.isUserAddress(ctx, chainID, toAddr)
	if err != nil {
		return errors.Wrap(err, "failed to check if address is user address")
	}

	if !isDeposit {
		// 不是充值交易，跳过
		log.Debug().
			Int("chain_id", chainID).
			Str("to_addr", toAddr).
			Msg("ETH transfer to non-user address, skipping")
		return nil
	}

	log.Info().
		Int("chain_id", chainID).
		Str("tx_hash", tx.Hash().Hex()).
		Str("to_addr", toAddr).
		Str("amount", tx.Value().String()).
		Msg("ETH deposit detected")

	txHash := strings.ToLower(tx.Hash().Hex())

	exists, err := a.transactionExists(ctx, txHash, chainID)
	if err != nil {
		return errors.Wrap(err, "failed to check transaction existence")
	}

	if exists {
		log.Debug().
			Int("chain_id", chainID).
			Str("tx_hash", txHash).
			Msg("ETH deposit transaction already recorded")
		return nil
	}

	fromAddr := strings.ToLower(from.Hex())
	transaction := &models.Transaction{
		ChainID:           chainID,
		BlockHash:         blockHash.Hex(),
		BlockNo:           blockNumber.Int64(),
		TXHash:            txHash,
		FromAddr:          fromAddr,
		ToAddr:            toAddr,
		TokenAddr:         null.String{}, // ETH 转账，token_addr 为空
		Amount:            tx.Value().String(),
		Type:              "deposit",
		Status:            "confirmed",
		ConfirmationCount: null.Int{},
	}

	if err := transaction.Insert(ctx, a.db, boil.Infer()); err != nil {
		return errors.Wrap(err, "failed to insert ETH transfer transaction")
	}

	return nil
}

// analyzeERC20Transfers 分析 ERC20 转账
func (a *analyzer) analyzeERC20Transfers(ctx context.Context, chainID int, tx *types.Transaction, receipt *types.Receipt, blockNumber *big.Int, blockHash common.Hash) error {
	// 检查交易是否成功
	if receipt.Status != types.ReceiptStatusSuccessful {
		return nil // 交易失败，跳过
	}

	// 解析 Transfer 事件
	for _, logEntry := range receipt.Logs {
		if len(logEntry.Topics) < minTransferEventTopics {
			continue
		}

		// 检查是否是 Transfer 事件
		if logEntry.Topics[0] != transferEventSignature {
			continue
		}

		// 解析 Transfer 事件参数
		// Transfer(address indexed from, address indexed to, uint256 value)
		// Topics[0] = event signature
		// Topics[1] = from address (indexed)
		// Topics[2] = to address (indexed)
		// Data = value (uint256)

		from := common.BytesToAddress(logEntry.Topics[1].Bytes())
		to := common.BytesToAddress(logEntry.Topics[2].Bytes())
		tokenAddr := logEntry.Address.Hex()

		// 解析 amount
		amount := new(big.Int).SetBytes(logEntry.Data)

		// 检查是否是充值交易（to 地址是用户钱包地址）
		toAddr := strings.ToLower(to.Hex())
		isDeposit, err := a.isUserAddress(ctx, chainID, toAddr)
		if err != nil {
			return errors.Wrap(err, "failed to check if address is user address")
		}

		if !isDeposit {
			// 不是充值交易，跳过
			log.Debug().
				Int("chain_id", chainID).
				Str("to_addr", toAddr).
				Str("token_addr", tokenAddr).
				Msg("ERC20 transfer to non-user address, skipping")
			continue
		}

		log.Info().
			Int("chain_id", chainID).
			Str("tx_hash", tx.Hash().Hex()).
			Str("to_addr", toAddr).
			Str("token_addr", tokenAddr).
			Str("amount", amount.String()).
			Msg("ERC20 deposit detected")

		txHash := strings.ToLower(tx.Hash().Hex())

		exists, err := a.transactionExists(ctx, txHash, chainID)
		if err != nil {
			return errors.Wrap(err, "failed to check transaction existence")
		}

		if exists {
			log.Debug().
				Int("chain_id", chainID).
				Str("tx_hash", txHash).
				Msg("ERC20 deposit transaction already recorded")
			continue
		}

		fromAddr := strings.ToLower(from.Hex())
		transaction := &models.Transaction{
			ChainID:           chainID,
			BlockHash:         blockHash.Hex(),
			BlockNo:           blockNumber.Int64(),
			TXHash:            txHash,
			FromAddr:          fromAddr,
			ToAddr:            toAddr,
			TokenAddr:         null.StringFrom(strings.ToLower(tokenAddr)),
			Amount:            amount.String(),
			Type:              "deposit",
			Status:            "confirmed",
			ConfirmationCount: null.Int{},
		}

		if err := transaction.Insert(ctx, a.db, boil.Infer()); err != nil {
			return errors.Wrap(err, "failed to insert ERC20 transfer transaction")
		}

		log.Debug().
			Int("chain_id", chainID).
			Str("tx_hash", tx.Hash().Hex()).
			Str("token_addr", tokenAddr).
			Str("to_addr", to.Hex()).
			Str("amount", amount.String()).
			Msg("ERC20 deposit transaction recorded")
	}

	return nil
}

// isUserAddress 检查地址是否是用户钱包地址
// 使用 LOWER() 函数确保不区分大小写比较（兼容旧数据）
func (a *analyzer) isUserAddress(ctx context.Context, chainID int, address string) (bool, error) {
	var count int64
	addressLower := strings.ToLower(address)
	err := a.db.QueryRowContext(ctx, `
		SELECT COUNT(*) 
		FROM wallets 
		WHERE chain_id = $1 AND LOWER(address) = $2
	`, chainID, addressLower).Scan(&count)

	if err != nil {
		return false, errors.Wrap(err, "failed to check user address")
	}

	return count > 0, nil
}

// transactionExists 检查交易是否已存在
func (a *analyzer) transactionExists(ctx context.Context, txHash string, chainID int) (bool, error) {
	var count int64
	txHashLower := strings.ToLower(txHash)
	err := a.db.QueryRowContext(ctx, `
		SELECT COUNT(*) 
		FROM transactions 
		WHERE chain_id = $1 AND LOWER(tx_hash) = $2
	`, chainID, txHashLower).Scan(&count)

	if err != nil {
		return false, errors.Wrap(err, "failed to check transaction existence")
	}

	return count > 0, nil
}

// 移除未使用的导入
var _ = abi.ABI{}
