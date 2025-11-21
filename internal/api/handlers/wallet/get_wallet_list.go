package wallet

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github/chapool/go-wallet/internal/api"
	"github/chapool/go-wallet/internal/auth"
	"github/chapool/go-wallet/internal/types"
	"github/chapool/go-wallet/internal/util"
)

func GetWalletListRoute(s *api.Server) *echo.Route {
	return s.Router.APIV1Wallet.GET("/list", getWalletListHandler(s))
}

func getWalletListHandler(s *api.Server) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		user := auth.UserFromContext(ctx)
		if user == nil {
			return echo.ErrUnauthorized
		}
		log := util.LogFromContext(ctx)

		wallets, err := s.Wallet.ListWallets(ctx, user.ID)
		if err != nil {
			log.Debug().Err(err).Msg("Failed to list wallets")
			return err
		}

		// Convert to WalletItem slice
		walletItems := make([]*types.WalletItem, 0, len(wallets))
		for _, w := range wallets {
			walletItems = append(walletItems, w.ToWalletItem())
		}

		response := &types.GetWalletListResponse{
			Wallets: walletItems,
		}

		return util.ValidateAndReturn(c, http.StatusOK, response)
	}
}
