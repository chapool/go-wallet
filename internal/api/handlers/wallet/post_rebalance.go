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
	"github/chapool/go-wallet/internal/wallet/rebalance"

	"github.com/go-openapi/swag"
	"github.com/labstack/echo/v4"
)

func PostRebalanceRoute(s *api.Server) *echo.Route {
	return s.Router.APIV1Wallet.POST("/rebalance", postRebalanceHandler(s))
}

func postRebalanceHandler(s *api.Server) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		user := auth.UserFromContext(ctx)
		if user == nil {
			return echo.ErrUnauthorized
		}
		log := util.LogFromContext(ctx)

		var body types.PostRebalancePayload
		if err := util.BindAndValidateBody(c, &body); err != nil {
			return err
		}

		// 解析金额（wei）
		//nolint:mnd // 10 and 256 are standard constants for big.ParseFloat
		amountFloat, _, err := big.ParseFloat(*body.Amount, 10, 256, big.ToNearestEven)
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

		// 转换为 big.Int (wei)
		amountWei := new(big.Int)
		amountFloat.Int(amountWei)

		req := &rebalance.Request{
			ChainID:     int(*body.ChainID),
			FromAddress: swag.StringValue(body.FromAddress),
			ToAddress:   swag.StringValue(body.ToAddress),
			Amount:      amountWei,
		}

		// 执行调度（异步，因为可能需要等待交易确认）
		go func() {
			// 使用背景上下文，因为请求上下文会随请求结束而取消
			if err := s.Rebalance.Rebalance(context.Background(), req); err != nil {
				log.Error().Err(err).
					Str("from", req.FromAddress).
					Str("to", req.ToAddress).
					Int("chain_id", req.ChainID).
					Msg("Async rebalance failed")
			}
		}()

		// 构建响应（注意：由于是异步执行，这里无法立即返回 tx_hash）
		response := &types.RebalanceResponse{
			Message:     swag.String("Rebalance initiated successfully"),
			FromAddress: swag.StringValue(body.FromAddress),
			ToAddress:   swag.StringValue(body.ToAddress),
			Amount:      swag.StringValue(body.Amount),
			TxHash:      swag.String(""), // 异步执行，暂时为空
		}

		return util.ValidateAndReturn(c, http.StatusOK, response)
	}
}
