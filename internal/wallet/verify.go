package wallet

import (
	"context"
	"database/sql"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github/chapool/go-wallet/internal/wallet/address"
	"github/chapool/go-wallet/internal/wallet/seed"
)

const (
	// VerificationAddressIndex is the address index used for password verification
	VerificationAddressIndex = 0
)

// VerifyPasswordByAddress verifies password by deriving verification address and comparing with stored address
// This is used during startup to ensure the password is correct
func VerifyPasswordByAddress(ctx context.Context, seedManager seed.Manager, addressService address.Service, db *sql.DB) (bool, error) {
	log := log.With().Str("component", "password_verification").Logger()

	// Get seed from memory
	seed := seedManager.GetSeed()
	if seed == nil {
		return false, errors.New("seed not initialized")
	}

	// Derive verification address (index 0)
	path := addressService.GetBIP44Path(VerificationAddressIndex)
	derivedAddress, err := addressService.DeriveAddress(ctx, seed, path, "evm")
	if err != nil {
		log.Error().Err(err).Msg("Failed to derive verification address")
		return false, errors.Wrap(err, "failed to derive verification address")
	}

	// Check if verification address exists in keystore table
	var storedAddress sql.NullString
	err = db.QueryRowContext(ctx, `
		SELECT verification_address 
		FROM keystore 
		LIMIT 1
	`).Scan(&storedAddress)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// No verification address stored yet - this is first startup
			// We'll create it later
			log.Info().Msg("No verification address found - this is first startup")
			return true, nil // Allow first startup to proceed
		}
		return false, errors.Wrap(err, "failed to query verification address")
	}

	// Check if verification address is set
	if !storedAddress.Valid || storedAddress.String == "" {
		// No verification address stored yet - this is first startup
		log.Info().Msg("No verification address found - this is first startup")
		return true, nil // Allow first startup to proceed
	}

	// Compare addresses
	if derivedAddress != storedAddress.String {
		log.Warn().
			Str("derived", derivedAddress).
			Str("stored", storedAddress.String).
			Msg("Password verification failed: addresses do not match")
		return false, nil
	}

	log.Info().Msg("Password verification successful")
	return true, nil
}

// CreateVerificationAddress creates and stores the verification address (index 0) for password verification
// This should be called after first keystore creation
// The verification address is stored in the keystore table, not wallets table
func CreateVerificationAddress(ctx context.Context, seedManager seed.Manager, addressService address.Service, db *sql.DB) error {
	log := log.With().Str("component", "password_verification").Logger()

	// Get seed from memory
	seed := seedManager.GetSeed()
	if seed == nil {
		return errors.New("seed not initialized")
	}

	// Derive verification address (index 0)
	path := addressService.GetBIP44Path(VerificationAddressIndex)
	verificationAddress, err := addressService.DeriveAddress(ctx, seed, path, "evm")
	if err != nil {
		log.Error().Err(err).Msg("Failed to derive verification address")
		return errors.Wrap(err, "failed to derive verification address")
	}

	// Check if verification address already exists
	var existingAddress sql.NullString
	err = db.QueryRowContext(ctx, `
		SELECT verification_address 
		FROM keystore 
		LIMIT 1
	`).Scan(&existingAddress)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return errors.Wrap(err, "failed to check verification address existence")
	}

	if existingAddress.Valid && existingAddress.String != "" {
		log.Info().Msg("Verification address already exists")
		return nil
	}

	// Update verification address in keystore table
	_, err = db.ExecContext(ctx, `
		UPDATE keystore 
		SET verification_address = $1, updated_at = NOW()
		WHERE id = '00000000-0000-0000-0000-000000000001'::uuid
	`, verificationAddress)

	if err != nil {
		log.Error().Err(err).Msg("Failed to create verification address")
		return errors.Wrap(err, "failed to create verification address")
	}

	log.Info().
		Str("address", verificationAddress).
		Int("index", VerificationAddressIndex).
		Msg("Verification address created successfully")

	return nil
}
