package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/dropbox/godropbox/time2"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
	"github/chapool/go-wallet/internal/config"
	"github/chapool/go-wallet/internal/data/dto"
	"github/chapool/go-wallet/internal/data/local"
	"github/chapool/go-wallet/internal/i18n"
	"github/chapool/go-wallet/internal/mailer"
	"github/chapool/go-wallet/internal/metrics"
	"github/chapool/go-wallet/internal/push"
	"github/chapool/go-wallet/internal/util"
	"github/chapool/go-wallet/internal/wallet"
	"github/chapool/go-wallet/internal/wallet/balance"
	"github/chapool/go-wallet/internal/wallet/collect"
	"github/chapool/go-wallet/internal/wallet/deposit"
	"github/chapool/go-wallet/internal/wallet/hotwallet"
	"github/chapool/go-wallet/internal/wallet/rebalance"
	"github/chapool/go-wallet/internal/wallet/scan"
	"github/chapool/go-wallet/internal/wallet/withdraw"

	// Import postgres driver for database/sql package
	_ "github.com/lib/pq"
)

// WalletService interface for wallet operations
type WalletService interface {
	CreateWallet(ctx context.Context, userID string, chainID int) (*wallet.Wallet, error)
	GetWallet(ctx context.Context, userID string, chainID int) (*wallet.Wallet, error)
	ListWallets(ctx context.Context, userID string) ([]*wallet.Wallet, error)
	GetWalletByAddress(ctx context.Context, address string, chainID int) (*wallet.Wallet, error)
}

// SignerService interface for transaction signing operations
type SignerService interface {
	SignEVMTransaction(ctx context.Context, req *SignEVMRequest) (*SignEVMResponse, error)
}

// ScanService interface for blockchain scanning operations
// This is an alias to scan.Service for API access
type ScanService = scan.Service

// DepositService interface for deposit operations
// Alias to deposit.Service for API access
type DepositService = deposit.Service

// BalanceService interface for balance operations
// Alias to balance.Service for API access
type BalanceService = balance.Service

// WithdrawService interface for withdraw operations
// Alias to withdraw.Service for API access
type WithdrawService = withdraw.Service

// CollectService interface for collect operations
type CollectService = collect.Service

// RebalanceService interface for rebalance operations
type RebalanceService = rebalance.Service

// HotWalletService interface for managing hot wallets
type HotWalletService = hotwallet.Service

// SignEVMRequest represents a request to sign an EVM transaction
type SignEVMRequest struct {
	ChainID              int64
	To                   string
	Value                string
	GasLimit             uint64
	MaxFeePerGas         string
	MaxPriorityFeePerGas string
	Nonce                uint64
	Data                 []byte
	FromAddress          string
	DerivationPath       string
}

// SignEVMResponse represents a signed EVM transaction
type SignEVMResponse struct {
	RawTransaction []byte
	TxHash         string
}

type Router struct {
	Routes      []*echo.Route
	Root        *echo.Group
	Management  *echo.Group
	APIV1Auth   *echo.Group
	APIV1Push   *echo.Group
	APIV1Wallet *echo.Group
	WellKnown   *echo.Group
}

// Server is a central struct keeping all the dependencies.
// It is initialized with wire, which handles making the new instances of the components
// in the right order. To add a new component, 3 steps are required:
// - declaring it in this struct
// - adding a provider function in providers.go
// - adding the provider's function name to the arguments of wire.Build() in wire.go
//
// Components labeled as `wire:"-"` will be skipped and have to be initialized after the InitNewServer* call.
// For more information about wire refer to https://pkg.go.dev/github.com/google/wire
type Server struct {
	// skip wire:
	// -> initialized with router.Init(s) function
	Echo   *echo.Echo `wire:"-"`
	Router *Router    `wire:"-"`

	Config    config.Server
	DB        *sql.DB
	Mailer    *mailer.Mailer
	Push      *push.Service
	I18n      *i18n.Service
	Clock     time2.Clock
	Auth      AuthService
	Local     *local.Service
	Metrics   *metrics.Service
	Wallet    WalletService   // Wallet service
	Signer    SignerService   // Signer service
	Scan      ScanService     // Blockchain scan service
	Deposit   DepositService  // Deposit service
	Balance   BalanceService  // Balance service
	Withdraw  WithdrawService // Withdraw service
	HotWallet HotWalletService
	Collect   CollectService
	Rebalance RebalanceService
}

// newServerWithComponents is used by wire to initialize the server components.
// Components not listed here won't be handled by wire and should be initialized separately.
// Components which shouldn't be handled must be labeled `wire:"-"` in Server struct.
func newServerWithComponents(
	cfg config.Server,
	db *sql.DB,
	mail *mailer.Mailer,
	pusher *push.Service,
	i18n *i18n.Service,
	clock time2.Clock,
	auth AuthService,
	local *local.Service,
	metrics *metrics.Service,
) *Server {
	return &Server{
		Config:  cfg,
		DB:      db,
		Mailer:  mail,
		Push:    pusher,
		I18n:    i18n,
		Clock:   clock,
		Auth:    auth,
		Local:   local,
		Metrics: metrics,
	}
}

type AuthService interface {
	GetAppUserProfile(ctx context.Context, id string) (*dto.AppUserProfile, error)
	InitPasswordReset(ctx context.Context, request dto.InitPasswordResetRequest) (dto.InitPasswordResetResult, error)
	Login(ctx context.Context, request dto.LoginRequest) (dto.LoginResult, error)
	Logout(ctx context.Context, request dto.LogoutRequest) error
	Refresh(ctx context.Context, request dto.RefreshRequest) (dto.LoginResult, error)
	Register(ctx context.Context, request dto.RegisterRequest) (dto.RegisterResult, error)
	CompleteRegister(ctx context.Context, request dto.CompleteRegisterRequest) (dto.LoginResult, error)
	DeleteUserAccount(ctx context.Context, request dto.DeleteUserAccountRequest) error
	ResetPassword(ctx context.Context, request dto.ResetPasswordRequest) (dto.LoginResult, error)
	UpdatePassword(ctx context.Context, request dto.UpdatePasswordRequest) (dto.LoginResult, error)
}

func NewServer(config config.Server) *Server {
	s := &Server{
		Config: config,
	}

	return s
}

func (s *Server) Ready() bool {
	if err := util.IsStructInitialized(s); err != nil {
		log.Debug().Err(err).Msg("Server is not fully initialized")
		return false
	}

	return true
}

func (s *Server) Start() error {
	if !s.Ready() {
		return errors.New("server is not ready")
	}

	if err := s.Echo.Start(s.Config.Echo.ListenAddress); err != nil {
		return fmt.Errorf("failed to start echo server: %w", err)
	}

	return nil
}

func (s *Server) Shutdown(ctx context.Context) []error {
	log.Warn().Msg("Shutting down server")

	var errs []error

	if s.DB != nil {
		log.Debug().Msg("Closing database connection")

		if err := s.DB.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
			log.Error().Err(err).Msg("Failed to close database connection")
			errs = append(errs, err)
		}
	}

	if s.Echo != nil {
		log.Debug().Msg("Shutting down echo server")

		if err := s.Echo.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error().Err(err).Msg("Failed to shutdown echo server")
			errs = append(errs, err)
		}
	}

	return errs
}
