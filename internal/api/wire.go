//go:build wireinject

package api

import (
	"database/sql"
	"testing"

	"github.com/google/wire"
	"github/chapool/go-wallet/internal/auth"
	"github/chapool/go-wallet/internal/config"
	"github/chapool/go-wallet/internal/data/local"
	"github/chapool/go-wallet/internal/metrics"
)

// INJECTORS - https://github.com/google/wire/blob/main/docs/guide.md#injectors

// serviceSet groups the default set of providers that are required for initing a server
var serviceSet = wire.NewSet(
	newServerWithComponents,
	NewPush,
	NewMailer,
	NewI18N,
	authServiceSet,
	local.NewService,
	metrics.New,
	NewClock,
)

var authServiceSet = wire.NewSet(
	NewAuthService,
	wire.Bind(new(AuthService), new(*auth.Service)),
)

// Note: Wallet services are initialized manually in cmd/server/wallet_init.go
// because SeedManager needs to be initialized with password at startup.
// We don't include wallet services in Wire to avoid circular dependencies.

// InitNewServer returns a new Server instance.
func InitNewServer(
	_ config.Server,
) (*Server, error) {
	wire.Build(serviceSet, NewDB, NoTest)
	return new(Server), nil
}

// InitNewServerWithDB returns a new Server instance with the given DB instance.
// All the other components are initialized via go wire according to the configuration.
func InitNewServerWithDB(
	_ config.Server,
	_ *sql.DB,
	t ...*testing.T,
) (*Server, error) {
	wire.Build(serviceSet)
	return new(Server), nil
}
