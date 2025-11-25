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

func PostApproveWithdrawRoute(s *api.Server) *echo.Route {
	return s.Router.APIV1Wallet.POST("/withdraw/:withdrawId/approve", postApproveWithdrawHandler(s))
}

func postApproveWithdrawHandler(s *api.Server) echo.HandlerFunc {
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
				Msg("Non-admin user attempted to approve withdraw")
			return httperrors.NewHTTPError(
				http.StatusForbidden,
				types.PublicHTTPErrorTypeGeneric,
				"Only admin users can approve withdraw requests",
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

		withdrawRecord, err := s.Withdraw.ApproveWithdraw(ctx, withdrawID)
		if err != nil {
			log.Error().Err(err).Str("withdraw_id", withdrawID).Msg("Failed to approve withdraw")
			return httperrors.NewHTTPError(http.StatusInternalServerError, types.PublicHTTPErrorTypeGeneric, "Failed to approve withdraw request")
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
