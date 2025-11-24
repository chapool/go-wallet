package wallet

import (
	"context"
	"math/big"
	"net/http"

	"github/chapool/go-wallet/internal/api"
	"github/chapool/go-wallet/internal/api/httperrors"
	"github/chapool/go-wallet/internal/auth"
	"github/chapool/go-wallet/internal/types"
	"github/chapool/go-wallet/internal/util"
	"github/chapool/go-wallet/internal/wallet/withdraw"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/labstack/echo/v4"
)

func PostWithdrawRoute(s *api.Server) *echo.Route {
	return s.Router.APIV1Wallet.POST("/withdraw", postWithdrawHandler(s))
}

func postWithdrawHandler(s *api.Server) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		user := auth.UserFromContext(ctx)
		if user == nil {
			return echo.ErrUnauthorized
		}
		log := util.LogFromContext(ctx)

		var body types.PostWithdrawPayload
		if err := util.BindAndValidateBody(c, &body); err != nil {
			return err
		}

		//nolint:mnd // 10 and 256 are standard constants for big.ParseFloat
		amount, _, err := big.ParseFloat(*body.Amount, 10, 256, big.ToNearestEven)
		if err != nil {
			return httperrors.NewHTTPValidationError(
				http.StatusBadRequest,
				types.PublicHTTPErrorTypeGeneric,
				"Invalid amount format",
				[]*types.HTTPValidationErrorDetail{
					{
						Key:   swag.String("amount"),
						In:    swag.String("body"),
						Error: swag.String("must be a valid number"),
					},
				},
			)
		}

		req := &withdraw.Request{
			ToAddress: *body.ToAddress,
			TokenID:   int(*body.TokenID),
			Amount:    amount,
		}

		withdrawRecord, err := s.Withdraw.RequestWithdraw(ctx, user.ID, req)
		if err != nil {
			log.Error().Err(err).Msg("Failed to request withdraw")
			return httperrors.NewHTTPError(http.StatusInternalServerError, types.PublicHTTPErrorTypeGeneric, "Failed to process withdraw request")
		}

		// 异步触发提现处理
		go func() {
			// 使用背景上下文，因为请求上下文会随请求结束而取消
			// TODO: 应该使用更健壮的 worker 机制
			if err := s.Withdraw.ProcessWithdraw(context.Background(), withdrawRecord.ID); err != nil {
				log.Error().Err(err).Str("withdraw_id", withdrawRecord.ID).Msg("Async process withdraw failed")
			}
		}()

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
