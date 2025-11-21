package server

import (
	"context"
	"time"

	"github/chapool/go-wallet/internal/api"
	"github/chapool/go-wallet/internal/wallet"
	"github/chapool/go-wallet/internal/wallet/address"
	"github/chapool/go-wallet/internal/wallet/chain"
	"github/chapool/go-wallet/internal/wallet/deposit"
	"github/chapool/go-wallet/internal/wallet/keystore"
	"github/chapool/go-wallet/internal/wallet/scan"
	"github/chapool/go-wallet/internal/wallet/seed"
	"github/chapool/go-wallet/internal/wallet/signer"

	"github.com/rs/zerolog/log"

	"github.com/pkg/errors"
)

// initializeWallet initializes wallet keystore and seed manager at startup
func initializeWallet(ctx context.Context, s *api.Server) error {
	// Initialize keystore service
	keystoreService, err := keystore.NewService(s.DB)
	if err != nil {
		return errors.Wrap(err, "failed to create keystore service")
	}

	// Initialize seed manager
	seedManager := seed.NewManager()

	// Initialize address service
	addressService, err := address.NewService(s.DB)
	if err != nil {
		return errors.Wrap(err, "failed to create address service")
	}

	// Initialize keystore (create or decrypt)
	if err := wallet.InitializeKeystore(ctx, s.DB, seedManager, keystoreService, addressService); err != nil {
		return errors.Wrap(err, "failed to initialize keystore")
	}

	// Create wallet service
	walletService, err := wallet.NewService(s.DB, seedManager, addressService)
	if err != nil {
		return errors.Wrap(err, "failed to create wallet service")
	}

	// Create signer service
	signerService, err := signer.NewService(seedManager, addressService)
	if err != nil {
		return errors.Wrap(err, "failed to create signer service")
	}

	// Store services in Server struct
	s.Wallet = walletService
	s.Signer = &signerServiceAdapter{signer: signerService}

	return nil
}

const (
	// Default scan interval: 10 seconds
	defaultScanInterval = 10 * time.Second
	// Default block batch size: 100 blocks per scan
	defaultBlockBatchSize = 100
	// Default deposit backfill interval: 1 minute
	defaultDepositBackfillInterval = time.Minute
)

// initializeScanService initializes and starts the blockchain scan service
//
//nolint:unparam // Error return is kept for future error handling (e.g., validation checks)
func initializeScanService(ctx context.Context, s *api.Server) error {
	log.Info().Msg("Initializing blockchain scan service")

	// Initialize chain configuration service
	chainService := chain.NewService(s.DB)

	// Initialize deposit service
	depositService := deposit.NewService(s.DB)
	s.Deposit = depositService

	// Create scan service with default configuration
	// These can be made configurable via environment variables in the future
	scanService := scan.NewService(
		s.DB,
		chainService,
		depositService,
		defaultScanInterval,
		defaultBlockBatchSize,
	)

	// Store scan service in Server struct (optional, for API access)
	s.Scan = scanService

	// Start multi-chain scanning in background
	go func() {
		if err := scanService.StartMultiChainScan(ctx); err != nil {
			log.Error().
				Err(err).
				Msg("Failed to start multi-chain scan service")
		}
	}()

	startDepositBackfillWorker(ctx, chainService, depositService)

	log.Info().Msg("Blockchain scan service started successfully")
	return nil
}

func startDepositBackfillWorker(ctx context.Context, chainService chain.Service, depositService deposit.Service) {
	if depositService == nil {
		return
	}

	runOnce := func() {
		chains, err := chainService.GetActiveChains(ctx)
		if err != nil {
			log.Error().Err(err).Msg("Backfill worker failed to load active chains")
			return
		}

		for _, ch := range chains {
			if err := depositService.ProcessFinalizedDeposits(ctx, ch.ChainID); err != nil {
				log.Error().
					Int("chain_id", ch.ChainID).
					Err(err).
					Msg("Backfill worker failed to process finalized deposits")
			}
		}
	}

	go func() {
		log.Info().Msg("Starting deposit backfill worker")
		runOnce()

		ticker := time.NewTicker(defaultDepositBackfillInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Info().Msg("Deposit backfill worker stopped")
				return
			case <-ticker.C:
				runOnce()
			}
		}
	}()
}

// signerServiceAdapter adapts signer.Service to api.SignerService
type signerServiceAdapter struct {
	signer signer.Service
}

func (a *signerServiceAdapter) SignEVMTransaction(ctx context.Context, req *api.SignEVMRequest) (*api.SignEVMResponse, error) {
	// Convert api.SignEVMRequest to signer.SignEVMRequest
	signerReq := &signer.SignEVMRequest{
		ChainID:              req.ChainID,
		To:                   req.To,
		Value:                req.Value,
		GasLimit:             req.GasLimit,
		MaxFeePerGas:         req.MaxFeePerGas,
		MaxPriorityFeePerGas: req.MaxPriorityFeePerGas,
		Nonce:                req.Nonce,
		Data:                 req.Data,
		FromAddress:          req.FromAddress,
		DerivationPath:       req.DerivationPath,
	}

	// Call signer service
	signerResp, err := a.signer.SignEVMTransaction(ctx, signerReq)
	if err != nil {
		return nil, err
	}

	// Convert signer.SignEVMResponse to api.SignEVMResponse
	return &api.SignEVMResponse{
		RawTransaction: signerResp.RawTransaction,
		TxHash:         signerResp.TxHash,
	}, nil
}
