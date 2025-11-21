package wallet

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github/chapool/go-wallet/internal/api"
	"github/chapool/go-wallet/internal/api/httperrors"
	"github/chapool/go-wallet/internal/auth"
	"github/chapool/go-wallet/internal/types"
	walletTypes "github/chapool/go-wallet/internal/types/wallet"
	"github/chapool/go-wallet/internal/util"
)

func GetWalletAddressRoute(s *api.Server) *echo.Route {
	return s.Router.APIV1Wallet.GET("/address", getWalletAddressHandler(s))
}

func getWalletAddressHandler(s *api.Server) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		user := auth.UserFromContext(ctx)
		if user == nil {
			return echo.ErrUnauthorized
		}
		log := util.LogFromContext(ctx)

		params := walletTypes.NewGetWalletAddressRouteParams()
		if err := util.BindAndValidateQueryParams(c, &params); err != nil {
			return err
		}

		walletResult, err := s.Wallet.GetWallet(ctx, user.ID, int(params.ChainID))
		if err != nil {
			log.Debug().Err(err).Msg("Failed to get wallet")
			if err.Error() == "wallet not found" {
				return httperrors.NewHTTPError(http.StatusNotFound, types.PublicHTTPErrorTypeGeneric, "Wallet not found")
			}
			return err
		}

		return util.ValidateAndReturn(c, http.StatusOK, walletResult.ToGetWalletAddressResponse())
	}
}
