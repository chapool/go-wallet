package wallet

import (
	"database/sql"
	"net/http"

	"github/chapool/go-wallet/internal/api"
	"github/chapool/go-wallet/internal/api/httperrors"
	"github/chapool/go-wallet/internal/auth"
	"github/chapool/go-wallet/internal/models"
	"github/chapool/go-wallet/internal/types"
	"github/chapool/go-wallet/internal/util"

	"github.com/go-openapi/swag"
	"github.com/labstack/echo/v4"
	"github.com/pkg/errors"
)

func PostCollectRoute(s *api.Server) *echo.Route {
	return s.Router.APIV1Wallet.POST("/collect", postCollectHandler(s))
}

func postCollectHandler(s *api.Server) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		user := auth.UserFromContext(ctx)
		if user == nil {
			return echo.ErrUnauthorized
		}
		log := util.LogFromContext(ctx)

		var body types.PostCollectPayload
		if err := util.BindAndValidateBody(c, &body); err != nil {
			return err
		}

		// 验证钱包是否属于当前用户
		walletID := string(*body.WalletID)
		wallet, err := models.Wallets(
			models.WalletWhere.ID.EQ(walletID),
			models.WalletWhere.UserID.EQ(user.ID),
		).One(ctx, s.DB)

		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return httperrors.NewHTTPError(http.StatusNotFound, types.PublicHTTPErrorTypeGeneric, "Wallet not found")
			}
			log.Error().Err(err).Str("wallet_id", walletID).Msg("Failed to get wallet")
			return httperrors.NewHTTPError(http.StatusInternalServerError, types.PublicHTTPErrorTypeGeneric, "Failed to get wallet")
		}

		// 验证钱包类型（只允许用户钱包）
		if wallet.WalletType != "user" {
			return httperrors.NewHTTPError(http.StatusBadRequest, types.PublicHTTPErrorTypeGeneric, "Only user wallets can be collected")
		}

		// 触发归集
		if err := s.Collect.CollectWallet(ctx, wallet.ID); err != nil {
			log.Error().Err(err).Str("wallet_id", wallet.ID).Msg("Failed to collect wallet")
			return httperrors.NewHTTPError(http.StatusInternalServerError, types.PublicHTTPErrorTypeGeneric, "Failed to collect funds")
		}

		response := &types.CollectResponse{
			Message:  swag.String("Collection initiated successfully"),
			WalletID: body.WalletID,
		}

		return util.ValidateAndReturn(c, http.StatusOK, response)
	}
}
