package address

import "context"

// Service provides address derivation and management functionality
type Service interface {
	// GetNextAddressIndex gets the next address index (shared across all EVM chains)
	GetNextAddressIndex(ctx context.Context, chainType string, deviceName string) (int, error)

	// DeriveAddress derives an address from seed (all EVM chains use same path, same address)
	DeriveAddress(ctx context.Context, seed []byte, path string, chainType string) (string, error)

	// DerivePrivateKey derives a private key from seed (all EVM chains use same path, same private key)
	// WARNING: Private key should be cleared after use
	DerivePrivateKey(ctx context.Context, seed []byte, path string, chainType string) ([]byte, error)

	// GetBIP44Path gets BIP44 path (fixed format for EVM chains)
	GetBIP44Path(addressIndex int) string
}
