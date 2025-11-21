package signer

import (
	"context"

	"github.com/pkg/errors"
	"github/chapool/go-wallet/internal/wallet/address"
	"github/chapool/go-wallet/internal/wallet/seed"
)

type service struct {
	seedManager    seed.Manager
	addressService address.Service
}

// NewService creates a new SignerService
//
//nolint:ireturn // Returning interface is intentional for dependency injection
func NewService(seedManager seed.Manager, addressService address.Service) (Service, error) {
	return &service{
		seedManager:    seedManager,
		addressService: addressService,
	}, nil
}

// SignEVMTransaction signs an EVM transaction (EIP-1559)
func (s *service) SignEVMTransaction(ctx context.Context, req *SignEVMRequest) (*SignEVMResponse, error) {
	// Get seed from memory
	seed := s.seedManager.GetSeed()
	if seed == nil {
		return nil, errors.New("seed not initialized")
	}

	// Derive private key from seed and derivation path
	privateKey, err := s.addressService.DerivePrivateKey(ctx, seed, req.DerivationPath, "evm")
	if err != nil {
		return nil, errors.Wrap(err, "failed to derive private key")
	}

	// Clear private key after use
	defer func() {
		for i := range privateKey {
			privateKey[i] = 0
		}
	}()

	// Sign transaction
	return s.signEIP1559Transaction(ctx, req, privateKey)
}
