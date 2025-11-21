package wallet

import (
	"net/http"

	"github.com/go-openapi/swag"
	"github.com/labstack/echo/v4"
	"github/chapool/go-wallet/internal/api"
	"github/chapool/go-wallet/internal/api/httperrors"
	"github/chapool/go-wallet/internal/auth"
	"github/chapool/go-wallet/internal/types"
	"github/chapool/go-wallet/internal/util"
)

func PostCreateWalletRoute(s *api.Server) *echo.Route {
	return s.Router.APIV1Wallet.POST("/create", postCreateWalletHandler(s))
}

func postCreateWalletHandler(s *api.Server) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		user := auth.UserFromContext(ctx)
		if user == nil {
			return echo.ErrUnauthorized
		}
		log := util.LogFromContext(ctx)

		var body types.PostCreateWalletPayload
		if err := util.BindAndValidateBody(c, &body); err != nil {
			return err
		}

		if body.ChainID == nil {
			return httperrors.NewHTTPValidationError(
				http.StatusBadRequest,
				types.PublicHTTPErrorTypeGeneric,
				"chain_id is required",
				[]*types.HTTPValidationErrorDetail{
					{
						Key:   swag.String("chain_id"),
						In:    swag.String("body"),
						Error: swag.String("required"),
					},
				},
			)
		}

		wallet, err := s.Wallet.CreateWallet(ctx, user.ID, int(swag.Int64Value(body.ChainID)))
		if err != nil {
			log.Debug().Err(err).Msg("Failed to create wallet")
			if err.Error() == "chain not found or inactive" {
				return httperrors.NewHTTPValidationError(
					http.StatusBadRequest,
					types.PublicHTTPErrorTypeGeneric,
					"Chain not found or inactive",
					[]*types.HTTPValidationErrorDetail{
						{
							Key:   swag.String("chain_id"),
							In:    swag.String("body"),
							Error: swag.String("chain not found or inactive"),
						},
					},
				)
			}
			return err
		}

		return util.ValidateAndReturn(c, http.StatusOK, wallet.ToCreateWalletResponse())
	}
}
