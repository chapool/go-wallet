package wallet

import (
	"net/http"

	"github/chapool/go-wallet/internal/api"
	"github/chapool/go-wallet/internal/api/httperrors"
	"github/chapool/go-wallet/internal/auth"
	"github/chapool/go-wallet/internal/types"
	"github/chapool/go-wallet/internal/util"

	"github.com/go-openapi/swag"
	"github.com/labstack/echo/v4"
)

func GetBalanceByTokenRoute(s *api.Server) *echo.Route {
	return s.Router.APIV1Wallet.GET("/balance/tokens", getBalanceByTokenHandler(s))
}

func getBalanceByTokenHandler(s *api.Server) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		user := auth.UserFromContext(ctx)
		if user == nil {
			return echo.ErrUnauthorized
		}
		log := util.LogFromContext(ctx)

		// 解析查询参数
		chainID, err := parseChainIDParam(c)
		if err != nil {
			return err
		}

		// 调用余额服务
		tokenBalances, err := s.Balance.GetBalanceByToken(ctx, user.ID, chainID)
		if err != nil {
			log.Error().Err(err).Msg("Failed to get balance by token")
			return httperrors.NewHTTPError(http.StatusInternalServerError, types.PublicHTTPErrorTypeGeneric, "Failed to get balance by token")
		}

		// 转换为 API 响应类型
		balanceItems := make([]*types.TokenBalanceItem, 0, len(tokenBalances))
		for _, tokenBalance := range tokenBalances {
			balanceItems = append(balanceItems, &types.TokenBalanceItem{
				TokenID:     swag.Int64(int64(tokenBalance.TokenID)),
				TokenSymbol: swag.String(tokenBalance.TokenSymbol),
				ChainID:     swag.Int64(int64(tokenBalance.ChainID)),
				Amount:      swag.String(tokenBalance.Amount.Text('f', -1)),
				Status:      swag.String(tokenBalance.Status),
			})
		}

		response := &types.GetBalanceByTokenResponse{
			Balances: balanceItems,
		}

		return util.ValidateAndReturn(c, http.StatusOK, response)
	}
}
