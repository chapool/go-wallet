package keystore

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/aarondl/sqlboiler/v4/boil"
	"github.com/pkg/errors"
	"github/chapool/go-wallet/internal/models"
	"github/chapool/go-wallet/internal/util"
)

// Service provides keystore encryption and decryption functionality
type Service interface {
	// CreateKeystore creates and encrypts a mnemonic to keystore
	CreateKeystore(ctx context.Context, mnemonic string, password string) (*Keystore, error)

	// DecryptMnemonic decrypts mnemonic from keystore
	DecryptMnemonic(ctx context.Context, keystore *Keystore, password string) (string, error)

	// GetKeystore gets the system keystore (single record)
	GetKeystore(ctx context.Context) (*Keystore, error)

	// Exists checks if keystore exists
	Exists(ctx context.Context) (bool, error)
}

type service struct {
	db *sql.DB
}

// NewService creates a new KeystoreService
//
//nolint:ireturn // Returning interface is intentional for dependency injection
func NewService(db *sql.DB) (Service, error) {
	return &service{
		db: db,
	}, nil
}

// CreateKeystore creates and encrypts a mnemonic to keystore
func (s *service) CreateKeystore(ctx context.Context, mnemonic string, password string) (*Keystore, error) {
	log := util.LogFromContext(ctx)

	// Check if keystore already exists
	exists, err := s.Exists(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to check keystore existence")
	}
	if exists {
		return nil, errors.New("keystore already exists")
	}

	// Encrypt mnemonic
	keystoreJSON, err := s.encryptMnemonic(mnemonic, password)
	if err != nil {
		log.Error().Err(err).Msg("Failed to encrypt mnemonic")
		return nil, errors.Wrap(err, "failed to encrypt mnemonic")
	}

	// Convert to JSONB
	keystoreData, err := json.Marshal(keystoreJSON)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal keystore JSON")
	}

	// Create keystore record
	keystoreModel := &models.Keystore{
		ID:           "00000000-0000-0000-0000-000000000001", // Fixed ID for single record
		KeystoreData: keystoreData,
		//nolint:mnd // 3 is the Ethereum keystore v3 version number
		Version: 3,
		Cipher:  "aes-128-ctr",
		KDF:     "scrypt",
	}

	if err := keystoreModel.Insert(ctx, s.db, boil.Infer()); err != nil {
		log.Error().Err(err).Msg("Failed to insert keystore")
		return nil, errors.Wrap(err, "failed to insert keystore")
	}

	return &Keystore{Keystore: keystoreModel}, nil
}

// DecryptMnemonic decrypts mnemonic from keystore
func (s *service) DecryptMnemonic(ctx context.Context, keystore *Keystore, password string) (string, error) {
	log := util.LogFromContext(ctx)

	// Parse keystore JSON
	var keystoreJSON KeystoreJSON
	if err := json.Unmarshal(keystore.KeystoreData, &keystoreJSON); err != nil {
		return "", errors.Wrap(err, "failed to unmarshal keystore JSON")
	}

	// Decrypt mnemonic
	mnemonic, err := s.decryptMnemonic(&keystoreJSON, password)
	if err != nil {
		log.Error().Err(err).Msg("Failed to decrypt mnemonic")
		return "", errors.Wrap(err, "failed to decrypt mnemonic")
	}

	return mnemonic, nil
}

// GetKeystore gets the system keystore (single record)
func (s *service) GetKeystore(ctx context.Context) (*Keystore, error) {
	keystoreModel, err := models.Keystores().One(ctx, s.db)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, errors.Wrap(err, "failed to get keystore")
	}

	return &Keystore{Keystore: keystoreModel}, nil
}

// Exists checks if keystore exists
func (s *service) Exists(ctx context.Context) (bool, error) {
	count, err := models.Keystores().Count(ctx, s.db)
	if err != nil {
		return false, errors.Wrap(err, "failed to count keystores")
	}

	return count > 0, nil
}
