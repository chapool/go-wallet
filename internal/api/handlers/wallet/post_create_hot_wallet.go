package wallet

import (
	"net/http"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/labstack/echo/v4"
	"github/chapool/go-wallet/internal/api"
	"github/chapool/go-wallet/internal/api/httperrors"
	"github/chapool/go-wallet/internal/auth"
	"github/chapool/go-wallet/internal/models"
	"github/chapool/go-wallet/internal/types"
	"github/chapool/go-wallet/internal/util"
)

const (
	// RoleAdmin is the admin role constant
	RoleAdmin = "admin"
)

func PostCreateHotWalletRoute(s *api.Server) *echo.Route {
	return s.Router.APIV1Wallet.POST("/hot-wallet", postCreateHotWalletHandler(s))
}

func postCreateHotWalletHandler(s *api.Server) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		user := auth.UserFromContext(ctx)
		if user == nil {
			return echo.ErrUnauthorized
		}
		log := util.LogFromContext(ctx)

		// Check if user is admin
		if user.Role != RoleAdmin {
			log.Warn().
				Str("user_id", user.ID).
				Str("user_role", user.Role).
				Msg("Non-admin user attempted to create hot wallet")
			return httperrors.NewHTTPError(
				http.StatusForbidden,
				types.PublicHTTPErrorTypeGeneric,
				"Only admin users can create hot wallets",
			)
		}

		var body types.PostCreateHotWalletPayload
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

		if body.DeviceName == nil || swag.StringValue(body.DeviceName) == "" {
			return httperrors.NewHTTPValidationError(
				http.StatusBadRequest,
				types.PublicHTTPErrorTypeGeneric,
				"device_name is required",
				[]*types.HTTPValidationErrorDetail{
					{
						Key:   swag.String("device_name"),
						In:    swag.String("body"),
						Error: swag.String("required"),
					},
				},
			)
		}

		// Create hot wallet
		hotWallet, err := s.HotWallet.CreateHotWallet(
			ctx,
			user.ID,
			int(swag.Int64Value(body.ChainID)),
			swag.StringValue(body.DeviceName),
		)
		if err != nil {
			log.Error().Err(err).Msg("Failed to create hot wallet")
			return httperrors.NewHTTPError(
				http.StatusInternalServerError,
				types.PublicHTTPErrorTypeGeneric,
				"Failed to create hot wallet",
			)
		}

		// Get chain info for response
		chain, err := models.Chains(
			models.ChainWhere.ChainID.EQ(int(swag.Int64Value(body.ChainID))),
		).One(ctx, s.DB)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to get chain config, using default chain name")
		}

		chainName := "Unknown Chain"
		if chain != nil {
			chainName = chain.ChainName
		}

		id := strfmt.UUID(hotWallet.ID)
		createdAt := strfmt.DateTime(hotWallet.CreatedAt)

		response := &types.CreateHotWalletResponse{
			ID:             &id,
			Address:        swag.String(hotWallet.Address),
			ChainType:      swag.String(hotWallet.ChainType),
			ChainID:        swag.Int64(int64(hotWallet.ChainID)),
			ChainName:      swag.String(chainName),
			DerivationPath: swag.String(hotWallet.DerivationPath),
			AddressIndex:   swag.Int64(int64(hotWallet.AddressIndex)),
			WalletType:     swag.String(hotWallet.WalletType),
			CreatedAt:      &createdAt,
		}

		if hotWallet.DeviceName.Valid {
			response.DeviceName = hotWallet.DeviceName.String
		}

		return util.ValidateAndReturn(c, http.StatusOK, response)
	}
}
