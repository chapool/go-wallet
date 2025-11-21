package address

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/aarondl/null/v8"
	"github.com/aarondl/sqlboiler/v4/boil"
	"github.com/pkg/errors"
	"github/chapool/go-wallet/internal/models"
	"github/chapool/go-wallet/internal/util"
	"github/chapool/go-wallet/internal/util/db"
)

type service struct {
	db *sql.DB
}

// NewService creates a new AddressService
//
//nolint:ireturn // Returning interface is intentional for dependency injection
func NewService(db *sql.DB) (Service, error) {
	return &service{
		db: db,
	}, nil
}

// GetNextAddressIndex gets the next address index (shared across all EVM chains)
func (s *service) GetNextAddressIndex(ctx context.Context, chainType string, deviceName string) (int, error) {
	log := util.LogFromContext(ctx)

	// Find or create address index record for this chain type
	var deviceNameNull null.String
	if deviceName != "" {
		deviceNameNull = null.StringFrom(deviceName)
	}

	addressIndex, err := models.AddressIndexes(
		models.AddressIndexWhere.ChainType.EQ(chainType),
		models.AddressIndexWhere.DeviceName.EQ(deviceNameNull),
	).One(ctx, s.db)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Create new record with index 0
			addressIndex = &models.AddressIndex{
				ChainType:    chainType,
				DeviceName:   deviceNameNull,
				CurrentIndex: 0,
			}

			if err := addressIndex.Insert(ctx, s.db, boil.Infer()); err != nil {
				log.Error().Err(err).Msg("Failed to create address index")
				return 0, errors.Wrap(err, "failed to create address index")
			}

			return 0, nil
		}

		return 0, errors.Wrap(err, "failed to get address index")
	}

	// Atomically increment index using database transaction
	var nextIndex int
	err = db.WithTransaction(ctx, s.db, func(tx boil.ContextExecutor) error {
		// Reload to get latest value
		if err := addressIndex.Reload(ctx, tx); err != nil {
			return errors.Wrap(err, "failed to reload address index")
		}

		nextIndex = addressIndex.CurrentIndex + 1

		// Update index
		addressIndex.CurrentIndex = nextIndex
		rowsAffected, err := addressIndex.Update(ctx, tx, boil.Whitelist(models.AddressIndexColumns.CurrentIndex))
		if err != nil {
			return errors.Wrap(err, "failed to update address index")
		}

		if rowsAffected == 0 {
			return errors.New("failed to update address index: no rows affected")
		}

		return nil
	})

	if err != nil {
		log.Error().Err(err).Msg("Failed to increment address index")
		return 0, errors.Wrap(err, "failed to increment address index")
	}

	return nextIndex, nil
}

// GetBIP44Path gets BIP44 path (fixed format for EVM chains)
// Format: m/44'/60'/0'/0/{index}
func (s *service) GetBIP44Path(addressIndex int) string {
	return fmt.Sprintf("m/44'/60'/0'/0/%d", addressIndex)
}
