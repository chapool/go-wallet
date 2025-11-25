package server

import (
	"context"
	"time"

	"github/chapool/go-wallet/internal/api"
	"github/chapool/go-wallet/internal/wallet"
	"github/chapool/go-wallet/internal/wallet/address"
	"github/chapool/go-wallet/internal/wallet/balance"
	"github/chapool/go-wallet/internal/wallet/chain"
	"github/chapool/go-wallet/internal/wallet/collect"
	"github/chapool/go-wallet/internal/wallet/deposit"
	"github/chapool/go-wallet/internal/wallet/hotwallet"
	"github/chapool/go-wallet/internal/wallet/keystore"
	"github/chapool/go-wallet/internal/wallet/rebalance"
	"github/chapool/go-wallet/internal/wallet/scan"
	"github/chapool/go-wallet/internal/wallet/seed"
	"github/chapool/go-wallet/internal/wallet/signer"
	"github/chapool/go-wallet/internal/wallet/withdraw"

	"github.com/rs/zerolog/log"

	"github.com/pkg/errors"
)

// initializeWallet initializes wallet keystore and seed manager at startup
//
//nolint:ireturn // Returning interface is intentional
func initializeWallet(ctx context.Context, s *api.Server) (seed.Manager, error) {
	// Initialize keystore service
	keystoreService, err := keystore.NewService(s.DB)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create keystore service")
	}

	// Initialize seed manager
	seedManager := seed.NewManager()

	// Initialize address service
	addressService, err := address.NewService(s.DB)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create address service")
	}

	// Initialize keystore (create or decrypt)
	if err := wallet.InitializeKeystore(ctx, s.DB, seedManager, keystoreService, addressService); err != nil {
		return nil, errors.Wrap(err, "failed to initialize keystore")
	}

	// Create wallet service
	walletService, err := wallet.NewService(s.DB, seedManager, addressService)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create wallet service")
	}

	// Create signer service with signing configuration
	signerService, err := signer.NewService(seedManager, addressService, s.Config.Wallet.EnableSigning)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create signer service")
	}

	// Store services in Server struct
	s.Wallet = walletService
	s.Signer = &signerServiceAdapter{signer: signerService}

	return seedManager, nil
}

const (
	// Default scan interval: 2 seconds (optimized for fast chains with ~0.5s block time)
	// For chains with 0.5s block time, 2s interval means checking every ~4 blocks
	defaultScanInterval = 2 * time.Second
	// Default block batch size: 1000 blocks per scan (optimized for fast chains)
	// Larger batch size for faster historical catch-up, but smaller batches per iteration
	defaultBlockBatchSize = 1000
	// Default deposit backfill interval: 30 seconds (reduced for faster processing)
	defaultDepositBackfillInterval = 30 * time.Second
	// Default collect interval for sweeping user addresses
	defaultCollectInterval = 5 * time.Minute
	// Default rebalance interval for hot wallets
	defaultRebalanceInterval = 10 * time.Minute
)

// initializeScanService initializes and starts the blockchain scan service
//
//nolint:unparam // Error return is kept for future error handling (e.g., validation checks)
func initializeScanService(ctx context.Context, s *api.Server, seedManager seed.Manager) error {
	log.Info().Msg("Initializing blockchain scan service")

	// Initialize chain configuration service
	chainService := chain.NewService(s.DB)

	// Initialize deposit service
	depositService := deposit.NewService(s.DB)
	s.Deposit = depositService

	// Initialize balance service
	balanceService := balance.NewService(s.DB)
	s.Balance = balanceService

	// --- Initialize Withdraw Related Services (needed for scan service) ---

	// Initialize address service (stateless, can be re-created)
	addressService, err := address.NewService(s.DB)
	if err != nil {
		return errors.Wrap(err, "failed to create address service for withdraw")
	}

	// Initialize hot wallet service
	hotWalletService := hotwallet.NewService(s.DB, addressService, seedManager)
	s.HotWallet = hotWalletService

	// Get signer service from adapter
	signerAdapter, ok := s.Signer.(*signerServiceAdapter)
	if !ok {
		return errors.New("failed to get signer service adapter")
	}
	signerService := signerAdapter.signer

	// Create a temporary scan service (without withdrawStatusUpdater) for withdraw service initialization
	// This is needed because withdrawService needs scanService, but scanService needs withdrawService
	tempScanService := scan.NewService(
		s.DB,
		chainService,
		depositService,
		nil, // withdrawStatusUpdater will be set later
		defaultScanInterval,
		defaultBlockBatchSize,
	)

	// Initialize withdraw service
	withdrawService := withdraw.NewService(
		s.DB,
		balanceService,
		hotWalletService,
		tempScanService,
		signerService,
	)
	s.Withdraw = withdrawService

	// Create scan service with withdrawService as WithdrawStatusUpdater
	// These can be made configurable via environment variables in the future
	scanService := scan.NewService(
		s.DB,
		chainService,
		depositService,
		withdrawService, // withdrawService implements WithdrawStatusUpdater interface
		defaultScanInterval,
		defaultBlockBatchSize,
	)

	// Store scan service in Server struct (optional, for API access)
	s.Scan = scanService

	// Update withdrawService to use the final scanService
	// Note: This assumes withdrawService stores scanService as a field that can be updated
	// If not, we may need to recreate withdrawService with the final scanService
	// For now, we'll use the final scanService for both

	// Start multi-chain scanning in background
	go func() {
		if err := scanService.StartMultiChainScan(ctx); err != nil {
			log.Error().
				Err(err).
				Msg("Failed to start multi-chain scan service")
		}
	}()

	startDepositBackfillWorker(ctx, chainService, depositService)

	collectService := collect.NewService(
		s.DB,
		chainService,
		scanService,
		hotWalletService,
		signerService,
	)
	s.Collect = collectService

	// 根据配置决定是否启动自动归集
	if s.Config.Wallet.EnableAutoCollect {
		log.Info().Msg("Auto collect is enabled, starting auto collect service")
		collectService.StartAutoCollect(ctx, defaultCollectInterval)
	} else {
		log.Info().Msg("Auto collect is disabled, skipping auto collect service startup")
	}

	rebalanceService := rebalance.NewService(
		s.DB,
		chainService,
		scanService,
		hotWalletService,
		signerService,
	)
	s.Rebalance = rebalanceService
	rebalanceService.StartAutoRebalance(ctx, defaultRebalanceInterval)

	log.Info().Msg("Blockchain scan and withdraw services started successfully")
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
