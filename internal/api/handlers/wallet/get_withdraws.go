package wallet

import (
	"net/http"
	"strconv"

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
)

func GetWithdrawsRoute(s *api.Server) *echo.Route {
	return s.Router.APIV1Wallet.GET("/withdraws", getWithdrawsHandler(s))
}

func getWithdrawsHandler(s *api.Server) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		user := auth.UserFromContext(ctx)
		if user == nil {
			return echo.ErrUnauthorized
		}
		log := util.LogFromContext(ctx)

		// 解析查询参数
		chainIDStr := c.QueryParam("chain_id")
		tokenIDStr := c.QueryParam("token_id")
		status := c.QueryParam("status")
		offsetStr := c.QueryParam("offset")
		limitStr := c.QueryParam("limit")

		mods := []qm.QueryMod{
			models.WithdrawWhere.UserID.EQ(user.ID),
		}

		if chainIDStr != "" {
			chainID, err := strconv.Atoi(chainIDStr)
			if err == nil {
				mods = append(mods, models.WithdrawWhere.ChainID.EQ(chainID))
			}
		}

		if tokenIDStr != "" {
			tokenID, err := strconv.Atoi(tokenIDStr)
			if err == nil {
				mods = append(mods, models.WithdrawWhere.TokenID.EQ(tokenID))
			}
		}

		if status != "" {
			mods = append(mods, models.WithdrawWhere.Status.EQ(status))
		}

		// 分页
		offset := 0
		if offsetStr != "" {
			if v, err := strconv.Atoi(offsetStr); err == nil && v >= 0 {
				offset = v
			}
		}
		limit := 20
		if limitStr != "" {
			if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 100 {
				limit = v
			}
		}

		mods = append(mods, qm.OrderBy(models.WithdrawColumns.CreatedAt+" DESC"))
		mods = append(mods, qm.Limit(limit), qm.Offset(offset))

		withdraws, err := models.Withdraws(mods...).All(ctx, s.DB)
		if err != nil {
			log.Error().Err(err).Msg("Failed to get withdraws")
			return httperrors.NewHTTPError(http.StatusInternalServerError, types.PublicHTTPErrorTypeGeneric, "Failed to get withdraws")
		}

		items := make([]*types.WithdrawItem, 0, len(withdraws))
		for _, withdrawRecord := range withdraws {
			id := strfmt.UUID(withdrawRecord.ID)
			userID := strfmt.UUID(withdrawRecord.UserID)
			createdAt := strfmt.DateTime(withdrawRecord.CreatedAt)
			item := &types.WithdrawItem{
				ID:        &id,
				UserID:    &userID,
				ToAddress: swag.String(withdrawRecord.ToAddress),
				TokenID:   swag.Int64(int64(withdrawRecord.TokenID)),
				Amount:    swag.String(withdrawRecord.Amount),
				Fee:       withdrawRecord.Fee,
				Status:    swag.String(withdrawRecord.Status),
				CreatedAt: &createdAt,
			}
			if withdrawRecord.TXHash.Valid {
				item.TxHash = withdrawRecord.TXHash.String
			}
			items = append(items, item)
		}

		response := &types.GetWithdrawsResponse{
			Withdraws: items,
		}

		return util.ValidateAndReturn(c, http.StatusOK, response)
	}
}
