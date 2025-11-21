package wallet

import (
	"context"
	"database/sql"
	"strings"

	"github.com/aarondl/sqlboiler/v4/boil"
	"github.com/pkg/errors"
	"github/chapool/go-wallet/internal/models"
	"github/chapool/go-wallet/internal/util"
	"github/chapool/go-wallet/internal/util/db"
	"github/chapool/go-wallet/internal/wallet/address"
	"github/chapool/go-wallet/internal/wallet/seed"
)

// Service provides wallet management functionality
type Service interface {
	// CreateWallet creates a wallet for user on specified chain
	CreateWallet(ctx context.Context, userID string, chainID int) (*Wallet, error)

	// GetWallet gets user's wallet on specified chain
	GetWallet(ctx context.Context, userID string, chainID int) (*Wallet, error)

	// ListWallets lists all wallets for a user across all chains
	ListWallets(ctx context.Context, userID string) ([]*Wallet, error)

	// GetWalletByAddress gets wallet by address and chain ID
	GetWalletByAddress(ctx context.Context, address string, chainID int) (*Wallet, error)
}

type service struct {
	db             *sql.DB
	seedManager    seed.Manager
	addressService address.Service
}

// NewService creates a new WalletService
//
//nolint:ireturn // Returning interface is intentional for dependency injection
func NewService(db *sql.DB, seedManager seed.Manager, addressService address.Service) (Service, error) {
	return &service{
		db:             db,
		seedManager:    seedManager,
		addressService: addressService,
	}, nil
}

// CreateWallet creates a wallet for user on specified chain
func (s *service) CreateWallet(ctx context.Context, userID string, chainID int) (*Wallet, error) {
	log := util.LogFromContext(ctx).With().
		Str("user_id", userID).
		Int("chain_id", chainID).
		Logger()

	// Check if chain exists and get chain name
	chain, err := models.Chains(
		models.ChainWhere.ChainID.EQ(chainID),
		models.ChainWhere.IsActive.EQ(true),
	).One(ctx, s.db)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("chain not found or inactive")
		}
		return nil, errors.Wrap(err, "failed to get chain")
	}

	// Check if wallet already exists
	existingWallet, err := models.Wallets(
		models.WalletWhere.UserID.EQ(userID),
		models.WalletWhere.ChainID.EQ(chainID),
	).One(ctx, s.db)

	if err == nil {
		// Wallet already exists, return it
		log.Info().Msg("Wallet already exists")
		return FromModel(existingWallet, chain.ChainName), nil
	}

	if !errors.Is(err, sql.ErrNoRows) {
		return nil, errors.Wrap(err, "failed to check existing wallet")
	}

	// Get seed from memory
	seed := s.seedManager.GetSeed()
	if seed == nil {
		return nil, errors.New("seed not initialized")
	}

	// Get next address index (shared across all EVM chains)
	addressIndex, err := s.addressService.GetNextAddressIndex(ctx, "evm", "")
	if err != nil {
		return nil, errors.Wrap(err, "failed to get next address index")
	}

	// Get BIP44 path
	path := s.addressService.GetBIP44Path(addressIndex)

	// Derive address from seed
	derivedAddress, err := s.addressService.DeriveAddress(ctx, seed, path, "evm")
	if err != nil {
		return nil, errors.Wrap(err, "failed to derive address")
	}

	// Create wallet record in database
	// Convert address to lowercase for consistent storage and querying
	addressLower := strings.ToLower(derivedAddress)
	var walletModel *models.Wallet
	err = db.WithTransaction(ctx, s.db, func(tx boil.ContextExecutor) error {
		walletModel = &models.Wallet{
			UserID:         userID,
			Address:        addressLower,
			ChainType:      "evm",
			ChainID:        chainID,
			DerivationPath: path,
			AddressIndex:   addressIndex,
			WalletType:     "user",
		}

		if err := walletModel.Insert(ctx, tx, boil.Infer()); err != nil {
			return errors.Wrap(err, "failed to insert wallet")
		}

		return nil
	})

	if err != nil {
		log.Error().Err(err).Msg("Failed to create wallet")
		return nil, err
	}

	log.Info().
		Str("address", derivedAddress).
		Int("address_index", addressIndex).
		Msg("Wallet created successfully")

	return FromModel(walletModel, chain.ChainName), nil
}

// GetWallet gets user's wallet on specified chain
func (s *service) GetWallet(ctx context.Context, userID string, chainID int) (*Wallet, error) {
	walletModel, err := models.Wallets(
		models.WalletWhere.UserID.EQ(userID),
		models.WalletWhere.ChainID.EQ(chainID),
	).One(ctx, s.db)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("wallet not found")
		}
		return nil, errors.Wrap(err, "failed to get wallet")
	}

	// Get chain name
	chain, err := models.Chains(
		models.ChainWhere.ChainID.EQ(chainID),
	).One(ctx, s.db)

	chainName := ""
	if err == nil {
		chainName = chain.ChainName
	}

	return FromModel(walletModel, chainName), nil
}

// ListWallets lists all wallets for a user across all chains
func (s *service) ListWallets(ctx context.Context, userID string) ([]*Wallet, error) {
	walletModels, err := models.Wallets(
		models.WalletWhere.UserID.EQ(userID),
	).All(ctx, s.db)

	if err != nil {
		return nil, errors.Wrap(err, "failed to list wallets")
	}

	// Get chain names for all chain IDs
	chainIDs := make([]int, 0, len(walletModels))
	chainMap := make(map[int]string)

	for _, w := range walletModels {
		chainIDs = append(chainIDs, w.ChainID)
	}

	if len(chainIDs) > 0 {
		chains, err := models.Chains(
			models.ChainWhere.ChainID.IN(chainIDs),
		).All(ctx, s.db)

		if err == nil {
			for _, chain := range chains {
				chainMap[chain.ChainID] = chain.ChainName
			}
		}
	}

	// Convert to Wallet slice
	wallets := make([]*Wallet, 0, len(walletModels))
	for _, w := range walletModels {
		chainName := chainMap[w.ChainID]
		wallets = append(wallets, FromModel(w, chainName))
	}

	return wallets, nil
}

// GetWalletByAddress gets wallet by address and chain ID
func (s *service) GetWalletByAddress(ctx context.Context, address string, chainID int) (*Wallet, error) {
	walletModel, err := models.Wallets(
		models.WalletWhere.Address.EQ(address),
		models.WalletWhere.ChainID.EQ(chainID),
	).One(ctx, s.db)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("wallet not found")
		}
		return nil, errors.Wrap(err, "failed to get wallet by address")
	}

	// Get chain name
	chain, err := models.Chains(
		models.ChainWhere.ChainID.EQ(chainID),
	).One(ctx, s.db)

	chainName := ""
	if err == nil {
		chainName = chain.ChainName
	}

	return FromModel(walletModel, chainName), nil
}
