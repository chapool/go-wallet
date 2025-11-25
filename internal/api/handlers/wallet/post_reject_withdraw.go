package wallet

import (
	"net/http"

	"github/chapool/go-wallet/internal/api"
	"github/chapool/go-wallet/internal/api/httperrors"
	"github/chapool/go-wallet/internal/auth"
	"github/chapool/go-wallet/internal/types"
	"github/chapool/go-wallet/internal/util"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/labstack/echo/v4"
)

func PostRejectWithdrawRoute(s *api.Server) *echo.Route {
	return s.Router.APIV1Wallet.POST("/withdraw/:withdrawId/reject", postRejectWithdrawHandler(s))
}

func postRejectWithdrawHandler(s *api.Server) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		user := auth.UserFromContext(ctx)
		if user == nil {
			return echo.ErrUnauthorized
		}
		log := util.LogFromContext(ctx)

		// 检查用户是否为管理员
		if user.Role != string(auth.RoleAdmin) {
			log.Warn().
				Str("user_id", user.ID).
				Str("user_role", user.Role).
				Msg("Non-admin user attempted to reject withdraw")
			return httperrors.NewHTTPError(
				http.StatusForbidden,
				types.PublicHTTPErrorTypeGeneric,
				"Only admin users can reject withdraw requests",
			)
		}

		withdrawID := c.Param("withdrawId")
		if withdrawID == "" {
			return httperrors.NewHTTPValidationError(
				http.StatusBadRequest,
				types.PublicHTTPErrorTypeGeneric,
				"withdrawId is required",
				[]*types.HTTPValidationErrorDetail{
					{
						Key:   swag.String("withdrawId"),
						In:    swag.String("path"),
						Error: swag.String("required"),
					},
				},
			)
		}

		// 可选：读取拒绝原因（从 query 参数或 body）
		reason := c.QueryParam("reason")
		if reason == "" && c.Request().ContentLength > 0 {
			// 尝试从 body 读取（如果提供了）
			var body map[string]interface{}
			if err := c.Bind(&body); err == nil {
				if r, ok := body["reason"].(string); ok && r != "" {
					reason = r
				}
			}
		}

		withdrawRecord, err := s.Withdraw.RejectWithdraw(ctx, withdrawID, reason)
		if err != nil {
			log.Error().Err(err).Str("withdraw_id", withdrawID).Msg("Failed to reject withdraw")
			return httperrors.NewHTTPError(http.StatusInternalServerError, types.PublicHTTPErrorTypeGeneric, "Failed to reject withdraw request")
		}

		// 构建响应
		id := strfmt.UUID(withdrawRecord.ID)
		userID := strfmt.UUID(withdrawRecord.UserID)
		createdAt := strfmt.DateTime(withdrawRecord.CreatedAt)
		response := &types.WithdrawResponse{
			Withdraw: &types.WithdrawItem{
				ID:        &id,
				UserID:    &userID,
				ToAddress: swag.String(withdrawRecord.ToAddress),
				TokenID:   swag.Int64(int64(withdrawRecord.TokenID)),
				Amount:    swag.String(withdrawRecord.Amount),
				Fee:       withdrawRecord.Fee,
				Status:    swag.String(withdrawRecord.Status),
				CreatedAt: &createdAt,
			},
		}

		if withdrawRecord.TXHash.Valid {
			response.Withdraw.TxHash = withdrawRecord.TXHash.String
		}

		return util.ValidateAndReturn(c, http.StatusOK, response)
	}
}
