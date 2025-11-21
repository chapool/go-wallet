package wallet

import (
	"time"

	"github/chapool/go-wallet/internal/models"
)

// Wallet represents a wallet with chain information
type Wallet struct {
	ID             string
	UserID         string
	Address        string
	ChainType      string
	ChainID        int
	ChainName      string
	DerivationPath string
	AddressIndex   int
	WalletType     string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ToModel converts Wallet to models.Wallet
func (w *Wallet) ToModel() *models.Wallet {
	return &models.Wallet{
		ID:             w.ID,
		UserID:         w.UserID,
		Address:        w.Address,
		ChainType:      w.ChainType,
		ChainID:        w.ChainID,
		DerivationPath: w.DerivationPath,
		AddressIndex:   w.AddressIndex,
		WalletType:     w.WalletType,
		CreatedAt:      w.CreatedAt,
		UpdatedAt:      w.UpdatedAt,
	}
}

// FromModel creates Wallet from models.Wallet
//
//nolint:varnamelen // m is a common abbreviation for model
func FromModel(m *models.Wallet, chainName string) *Wallet {
	return &Wallet{
		ID:             m.ID,
		UserID:         m.UserID,
		Address:        m.Address,
		ChainType:      m.ChainType,
		ChainID:        m.ChainID,
		ChainName:      chainName,
		DerivationPath: m.DerivationPath,
		AddressIndex:   m.AddressIndex,
		WalletType:     m.WalletType,
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
	}
}
