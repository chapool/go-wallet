package address

import (
	"context"
	"crypto/ecdsa"
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/pkg/errors"
	"github.com/tyler-smith/go-bip32"
)

// DeriveAddress derives an EVM address from seed and BIP44 path
func (s *service) DeriveAddress(ctx context.Context, seed []byte, path string, chainType string) (string, error) {
	if chainType != "evm" {
		return "", fmt.Errorf("unsupported chain type: %s", chainType)
	}

	// Derive private key from seed and path
	privateKey, err := s.DerivePrivateKey(ctx, seed, path, chainType)
	if err != nil {
		return "", errors.Wrap(err, "failed to derive private key")
	}

	// Clear private key after use
	defer func() {
		for i := range privateKey {
			privateKey[i] = 0
		}
	}()

	// Convert to ECDSA private key
	ecdsaPrivateKey, err := crypto.ToECDSA(privateKey)
	if err != nil {
		return "", errors.Wrap(err, "failed to convert to ECDSA private key")
	}

	// Get public key
	publicKey := ecdsaPrivateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return "", errors.New("failed to cast public key to ECDSA")
	}

	// Derive address from public key
	address := crypto.PubkeyToAddress(*publicKeyECDSA)

	return address.Hex(), nil
}

// DerivePrivateKey derives a private key from seed and BIP44 path
// WARNING: Caller must clear the private key after use
func (s *service) DerivePrivateKey(_ context.Context, seed []byte, path string, chainType string) ([]byte, error) {
	if chainType != "evm" {
		return nil, fmt.Errorf("unsupported chain type: %s", chainType)
	}

	// Create master key from seed
	masterKey, err := bip32.NewMasterKey(seed)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create master key")
	}

	// Derive key from path
	derivedKey, err := deriveKeyFromPath(masterKey, path)
	if err != nil {
		return nil, errors.Wrap(err, "failed to derive key from path")
	}

	// Return private key (32 bytes)
	return derivedKey.Key, nil
}

// deriveKeyFromPath derives a key from BIP44 path
// Path format: m/44'/60'/0'/0/{index}
func deriveKeyFromPath(masterKey *bip32.Key, path string) (*bip32.Key, error) {
	// Parse path
	indices, err := parseBIP44Path(path)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse BIP44 path")
	}

	// Derive key step by step
	key := masterKey
	for _, index := range indices {
		key, err = key.NewChildKey(index)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to derive child key at index %d", index)
		}
	}

	return key, nil
}

// parseBIP44Path parses a BIP44 path string into indices
// Example: "m/44'/60'/0'/0/0" -> [2147483692, 2147483708, 2147483648, 0, 0]
func parseBIP44Path(path string) ([]uint32, error) {
	if len(path) == 0 || path[0] != 'm' {
		return nil, fmt.Errorf("invalid BIP44 path: %s", path)
	}

	// Remove 'm/' prefix
	if len(path) > 2 && path[1] == '/' {
		path = path[2:]
	}

	// Split by '/'
	parts := []string{}
	current := ""
	for _, char := range path {
		if char == '/' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(char)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}

	// Parse each part
	indices := make([]uint32, 0, len(parts))
	for _, part := range parts {
		hardened := false
		if len(part) > 0 && part[len(part)-1] == '\'' {
			hardened = true
			part = part[:len(part)-1]
		}

		var index uint32
		_, err := fmt.Sscanf(part, "%d", &index)
		if err != nil {
			return nil, fmt.Errorf("invalid path segment: %s", part)
		}

		// Add hardened flag (0x80000000)
		if hardened {
			index += 0x80000000
		}

		indices = append(indices, index)
	}

	return indices, nil
}
