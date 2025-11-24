package hotwallet

import (
	"context"
	"database/sql"
	"time"

	"github/chapool/go-wallet/internal/models"
	"github/chapool/go-wallet/internal/wallet/address"
	"github/chapool/go-wallet/internal/wallet/seed"

	"github.com/aarondl/null/v8"
	"github.com/aarondl/sqlboiler/v4/boil"
	"github.com/aarondl/sqlboiler/v4/queries/qm"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

// Service 热钱包服务接口
type Service interface {
	// CreateHotWallet 创建热钱包
	CreateHotWallet(ctx context.Context, userID string, chainID int, deviceName string) (*models.Wallet, error)

	// GetHotWallet 获取指定链的热钱包（目前简单返回第一个）
	GetHotWallet(ctx context.Context, chainID int) (*models.Wallet, error)

	// GetNextNonce 获取并锁定下一个 Nonce
	GetNextNonce(ctx context.Context, address string, chainID int) (int, error)
}

type service struct {
	db             *sql.DB
	addressService address.Service
	seedManager    seed.Manager
}

// NewService 创建热钱包服务
//
//nolint:ireturn // 返回接口类型是预期的设计
func NewService(db *sql.DB, addressService address.Service, seedManager seed.Manager) Service {
	return &service{
		db:             db,
		addressService: addressService,
		seedManager:    seedManager,
	}
}

// CreateHotWallet 创建热钱包
func (s *service) CreateHotWallet(ctx context.Context, userID string, chainID int, deviceName string) (*models.Wallet, error) {
	// 1. 获取种子
	seed := s.seedManager.GetSeed()
	if seed == nil {
		return nil, errors.New("seed not initialized")
	}

	// 2. 获取下一个地址索引
	index, err := s.addressService.GetNextAddressIndex(ctx, "evm", deviceName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get next address index")
	}

	// 3. 计算派生路径
	derivationPath := s.addressService.GetBIP44Path(index)

	// 4. 生成地址
	addr, err := s.addressService.DeriveAddress(ctx, seed, derivationPath, "evm")
	if err != nil {
		return nil, errors.Wrap(err, "failed to derive address for hot wallet")
	}

	// 5. 插入 wallets 表
	wallet := &models.Wallet{
		UserID:         userID,
		Address:        addr,
		ChainType:      "evm",
		ChainID:        chainID,
		DerivationPath: derivationPath,
		AddressIndex:   index,
		WalletType:     "hot",
		DeviceName:     null.StringFrom(deviceName),
	}

	// 开启事务
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin transaction")
	}
	defer func() { _ = tx.Rollback() }()

	if err := wallet.Insert(ctx, tx, boil.Infer()); err != nil {
		return nil, errors.Wrap(err, "failed to insert hot wallet")
	}

	// 3. 初始化 wallet_nonces 表
	nonce := &models.WalletNonce{
		Address: addr,
		ChainID: chainID,
		Nonce:   0,
	}
	if err := nonce.Insert(ctx, tx, boil.Infer()); err != nil {
		return nil, errors.Wrap(err, "failed to insert wallet nonce")
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit transaction")
	}

	log.Info().
		Str("address", addr).
		Int("chain_id", chainID).
		Str("device_name", deviceName).
		Msg("Hot wallet created successfully")

	return wallet, nil
}

// GetHotWallet 获取指定链的热钱包
func (s *service) GetHotWallet(ctx context.Context, chainID int) (*models.Wallet, error) {
	// 简单策略：获取该链下第一个可用的热钱包
	wallet, err := models.Wallets(
		models.WalletWhere.ChainID.EQ(chainID),
		models.WalletWhere.WalletType.EQ("hot"),
	).One(ctx, s.db)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("no hot wallet found for this chain")
		}
		return nil, errors.Wrap(err, "failed to get hot wallet")
	}

	return wallet, nil
}

// GetNextNonce 获取并锁定下一个 Nonce
// 这个方法必须原子性地增加 Nonce 并返回旧值（或者新值，取决于具体需求，通常是当前可用的 nonce）
// 这里我们实现：返回当前 nonce，并将数据库中的 nonce + 1
func (s *service) GetNextNonce(ctx context.Context, address string, chainID int) (int, error) {
	// 使用事务和行锁来保证原子性
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, errors.Wrap(err, "failed to begin transaction")
	}
	defer func() { _ = tx.Rollback() }()

	// 查询并锁定行 (FOR UPDATE)
	nonceRecord, err := models.WalletNonces(
		models.WalletNonceWhere.Address.EQ(address),
		models.WalletNonceWhere.ChainID.EQ(chainID),
		qm.For("UPDATE"),
	).One(ctx, tx)

	if err != nil {
		return 0, errors.Wrap(err, "failed to get wallet nonce record")
	}

	currentNonce := nonceRecord.Nonce

	// 更新 Nonce
	nonceRecord.Nonce = currentNonce + 1
	nonceRecord.LastUsedAt = null.TimeFrom(time.Now())

	if _, err := nonceRecord.Update(ctx, tx, boil.Infer()); err != nil {
		return 0, errors.Wrap(err, "failed to update wallet nonce")
	}

	if err := tx.Commit(); err != nil {
		return 0, errors.Wrap(err, "failed to commit transaction")
	}

	return currentNonce, nil
}
