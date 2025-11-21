package wallet

import (
	"context"
	"database/sql"
	"fmt"
	"syscall"

	"github/chapool/go-wallet/internal/wallet/address"
	"github/chapool/go-wallet/internal/wallet/keystore"
	"github/chapool/go-wallet/internal/wallet/seed"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"golang.org/x/term"
)

// InitializeKeystore initializes the keystore at server startup
// This function handles:
// 1. Checking if keystore exists
// 2. If not, generating new mnemonic and prompting for password
// 3. If exists, prompting for password to decrypt
// 4. Validating password and initializing SeedManager
// 5. Verifying password by comparing derived address with stored verification address
func InitializeKeystore(ctx context.Context, db *sql.DB, seedManager seed.Manager, keystoreService keystore.Service, addressService address.Service) error {
	log := log.With().Str("component", "wallet_init").Logger()

	// Check if keystore exists
	exists, err := keystoreService.Exists(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to check keystore existence")
	}

	//nolint:nestif // Complex initialization logic requires nested blocks
	if !exists {
		// Generate new mnemonic and create keystore
		log.Info().Msg("Keystore not found. Generating new mnemonic...")

		// TODO: Generate BIP39 mnemonic (24 words)
		// For now, we'll use a placeholder - need to add bip39 library
		//nolint:dupword // Test mnemonic with repeated words
		mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"

		// Prompt for password
		const minPasswordLength = 8
		password, err := promptPassword("Enter password for keystore (min 8 characters): ")
		if err != nil {
			return errors.Wrap(err, "failed to read password")
		}

		if len(password) < minPasswordLength {
			return errors.New("password must be at least 8 characters")
		}

		// Confirm password
		passwordConfirm, err := promptPassword("Confirm password: ")
		if err != nil {
			return errors.Wrap(err, "failed to read password confirmation")
		}

		if password != passwordConfirm {
			return errors.New("passwords do not match")
		}

		// Create keystore
		_, err = keystoreService.CreateKeystore(ctx, mnemonic, password)
		if err != nil {
			return errors.Wrap(err, "failed to create keystore")
		}

		log.Info().Msg("Keystore created successfully")

		// Initialize seed manager
		if err := seedManager.Initialize(mnemonic, password); err != nil {
			return errors.Wrap(err, "failed to initialize seed manager")
		}

		log.Info().Msg("Seed manager initialized")

		// Create verification address for password verification
		// Store it in keystore table (system-level, not user-level)
		if err := CreateVerificationAddress(ctx, seedManager, addressService, db); err != nil {
			return errors.Wrap(err, "failed to create verification address")
		}
	} else {
		// Keystore exists, prompt for password
		log.Info().Msg("Keystore found. Please enter password to unlock...")

		password, err := promptPassword("Enter keystore password: ")
		if err != nil {
			return errors.Wrap(err, "failed to read password")
		}

		// Get keystore
		//nolint:varnamelen // ks is a common abbreviation for keystore
		ks, err := keystoreService.GetKeystore(ctx)
		if err != nil {
			return errors.Wrap(err, "failed to get keystore")
		}

		// Decrypt mnemonic
		mnemonic, err := keystoreService.DecryptMnemonic(ctx, ks, password)
		if err != nil {
			return errors.Wrap(err, "failed to decrypt keystore (invalid password?)")
		}

		// Initialize seed manager
		if err := seedManager.Initialize(mnemonic, password); err != nil {
			return errors.Wrap(err, "failed to initialize seed manager")
		}

		log.Info().Msg("Seed manager initialized successfully")

		// Verify password by comparing derived address with stored verification address
		valid, err := VerifyPasswordByAddress(ctx, seedManager, addressService, db)
		if err != nil {
			return errors.Wrap(err, "failed to verify password")
		}

		if !valid {
			return errors.New("password verification failed: derived address does not match stored verification address")
		}

		log.Info().Msg("Password verification successful")
	}

	return nil
}

// promptPassword prompts for password input (hides input)
//
//nolint:forbidigo // Password input requires direct terminal I/O
func promptPassword(prompt string) (string, error) {
	//nolint:forbidigo // Password input requires direct terminal I/O
	fmt.Print(prompt)

	// Read password from terminal (hides input)
	passwordBytes, err := term.ReadPassword(syscall.Stdin)
	if err != nil {
		return "", errors.Wrap(err, "failed to read password from terminal")
	}

	//nolint:forbidigo // Password input requires direct terminal I/O
	fmt.Println() // New line after password input

	return string(passwordBytes), nil
}
