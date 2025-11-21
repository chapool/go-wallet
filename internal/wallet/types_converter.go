package wallet

import (
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github/chapool/go-wallet/internal/models"
	"github/chapool/go-wallet/internal/types"
)

// ToCreateWalletResponse converts Wallet to CreateWalletResponse
func (w *Wallet) ToCreateWalletResponse() *types.CreateWalletResponse {
	id := strfmt.UUID(w.ID)
	userID := strfmt.UUID(w.UserID)
	createdAt := strfmt.DateTime(w.CreatedAt)

	return &types.CreateWalletResponse{
		ID:             &id,
		UserID:         &userID,
		Address:        swag.String(w.Address),
		ChainType:      swag.String(w.ChainType),
		ChainID:        swag.Int64(int64(w.ChainID)),
		ChainName:      swag.String(w.ChainName),
		DerivationPath: swag.String(w.DerivationPath),
		AddressIndex:   swag.Int64(int64(w.AddressIndex)),
		CreatedAt:      &createdAt,
	}
}

// ToGetWalletAddressResponse converts Wallet to GetWalletAddressResponse
func (w *Wallet) ToGetWalletAddressResponse() *types.GetWalletAddressResponse {
	return &types.GetWalletAddressResponse{
		Address:        swag.String(w.Address),
		ChainType:      swag.String(w.ChainType),
		ChainID:        swag.Int64(int64(w.ChainID)),
		ChainName:      swag.String(w.ChainName),
		DerivationPath: swag.String(w.DerivationPath),
	}
}

// ToWalletItem converts Wallet to WalletItem
func (w *Wallet) ToWalletItem() *types.WalletItem {
	id := strfmt.UUID(w.ID)
	userID := strfmt.UUID(w.UserID)
	createdAt := strfmt.DateTime(w.CreatedAt)

	return &types.WalletItem{
		ID:             &id,
		UserID:         &userID,
		Address:        swag.String(w.Address),
		ChainType:      swag.String(w.ChainType),
		ChainID:        swag.Int64(int64(w.ChainID)),
		ChainName:      swag.String(w.ChainName),
		DerivationPath: swag.String(w.DerivationPath),
		AddressIndex:   swag.Int64(int64(w.AddressIndex)),
		CreatedAt:      &createdAt,
	}
}

// ChainToChainItem converts models.Chain to types.ChainItem
func ChainToChainItem(chain *models.Chain) *types.ChainItem {
	item := &types.ChainItem{
		ID:                swag.Int64(int64(chain.ID)),
		ChainID:           swag.Int64(int64(chain.ChainID)),
		ChainName:         swag.String(chain.ChainName),
		ChainType:         swag.String(chain.ChainType),
		NativeTokenSymbol: swag.String(chain.NativeTokenSymbol),
		IsActive:          swag.Bool(chain.IsActive),
	}

	// Set optional fields if they are valid
	if chain.BlockTimeSeconds.Valid {
		item.BlockTimeSeconds = int64(chain.BlockTimeSeconds.Int)
	}
	if chain.ConfirmationBlocks.Valid {
		item.ConfirmationBlocks = int64(chain.ConfirmationBlocks.Int)
	}
	if chain.FinalizedBlocks.Valid {
		item.FinalizedBlocks = int64(chain.FinalizedBlocks.Int)
	}

	return item
}
