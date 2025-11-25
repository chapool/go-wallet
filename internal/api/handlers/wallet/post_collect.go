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

	"github.com/aarondl/sqlboiler/v4/queries/qm"
	"github.com/go-openapi/strfmt"
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

		// 检查用户是否为管理员（手动触发归集需要管理员权限）
		if user.Role != string(auth.RoleAdmin) {
			log.Warn().
				Str("user_id", user.ID).
				Str("user_role", user.Role).
				Msg("Non-admin user attempted to manually trigger collection")
			return httperrors.NewHTTPError(
				http.StatusForbidden,
				types.PublicHTTPErrorTypeGeneric,
				"Only admin users can manually trigger collection",
			)
		}

		var body types.PostCollectPayload
		if err := util.BindAndValidateBody(c, &body); err != nil {
			return err
		}

		var wallet *models.Wallet
		var err error

		// 根据提供的参数查找钱包
		hasWalletID := !swag.IsZero(body.WalletID) && body.WalletID.String() != ""
		hasChainID := body.ChainID != 0

		switch {
		case hasWalletID:
			// 如果提供了 wallet_id，使用 wallet_id 查找
			walletID := body.WalletID.String()
			// 管理员可以触发任何钱包的归集，普通用户只能触发自己的钱包
			queryMods := []qm.QueryMod{
				models.WalletWhere.ID.EQ(walletID),
			}
			if user.Role != string(auth.RoleAdmin) {
				queryMods = append(queryMods, models.WalletWhere.UserID.EQ(user.ID))
			}

			wallet, err = models.Wallets(queryMods...).One(ctx, s.DB)

			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					log.Warn().
						Str("wallet_id", walletID).
						Str("user_id", user.ID).
						Str("user_role", user.Role).
						Msg("Wallet not found")
					return httperrors.NewHTTPError(http.StatusNotFound, types.PublicHTTPErrorTypeGeneric, "Wallet not found")
				}
				log.Error().Err(err).Str("wallet_id", walletID).Msg("Failed to get wallet")
				return httperrors.NewHTTPError(http.StatusInternalServerError, types.PublicHTTPErrorTypeGeneric, "Failed to get wallet")
			}
		case hasChainID:
			// 如果没有提供 wallet_id 但提供了 chain_id
			// 管理员需要指定 wallet_id（因为可能有多用户），普通用户查找自己的钱包
			chainID := int(body.ChainID)
			queryMods := []qm.QueryMod{
				models.WalletWhere.ChainID.EQ(chainID),
				models.WalletWhere.WalletType.EQ(string(auth.RoleUser)),
			}
			if user.Role != string(auth.RoleAdmin) {
				queryMods = append(queryMods, models.WalletWhere.UserID.EQ(user.ID))
			}

			wallet, err = models.Wallets(queryMods...).One(ctx, s.DB)

			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					if user.Role == string(auth.RoleAdmin) {
						return httperrors.NewHTTPError(
							http.StatusBadRequest,
							types.PublicHTTPErrorTypeGeneric,
							"Multiple wallets found for this chain. Please specify wallet_id",
						)
					}
					log.Warn().
						Str("user_id", user.ID).
						Int("chain_id", chainID).
						Msg("User wallet not found for chain")
					return httperrors.NewHTTPError(
						http.StatusNotFound,
						types.PublicHTTPErrorTypeGeneric,
						"Wallet not found for this chain. Please create a wallet first using POST /api/v1/wallet/create",
					)
				}
				log.Error().Err(err).Int("chain_id", chainID).Msg("Failed to get wallet")
				return httperrors.NewHTTPError(http.StatusInternalServerError, types.PublicHTTPErrorTypeGeneric, "Failed to get wallet")
			}
		default:
			// 既没有 wallet_id 也没有 chain_id
			return httperrors.NewHTTPValidationError(
				http.StatusBadRequest,
				types.PublicHTTPErrorTypeGeneric,
				"Either wallet_id or chain_id must be provided",
				[]*types.HTTPValidationErrorDetail{
					{
						Key:   swag.String("wallet_id"),
						In:    swag.String("body"),
						Error: swag.String("required if chain_id is not provided"),
					},
					{
						Key:   swag.String("chain_id"),
						In:    swag.String("body"),
						Error: swag.String("required if wallet_id is not provided"),
					},
				},
			)
		}

		// 验证钱包类型（只允许用户钱包）
		if wallet.WalletType != string(auth.RoleUser) {
			return httperrors.NewHTTPError(http.StatusBadRequest, types.PublicHTTPErrorTypeGeneric, "Only user wallets can be collected")
		}

		// 触发归集
		if err := s.Collect.CollectWallet(ctx, wallet.ID); err != nil {
			log.Error().Err(err).Str("wallet_id", wallet.ID).Msg("Failed to collect wallet")
			return httperrors.NewHTTPError(http.StatusInternalServerError, types.PublicHTTPErrorTypeGeneric, "Failed to collect funds")
		}

		walletIDResponse := strfmt.UUID(wallet.ID)
		response := &types.CollectResponse{
			Message:  swag.String("Collection initiated successfully"),
			WalletID: &walletIDResponse,
		}

		return util.ValidateAndReturn(c, http.StatusOK, response)
	}
}
