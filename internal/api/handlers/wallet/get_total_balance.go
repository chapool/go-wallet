//nolint:dupl // 与 get_pending_deposit_balance.go 相似但调用不同的服务方法
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

func GetTotalBalanceRoute(s *api.Server) *echo.Route {
	return s.Router.APIV1Wallet.GET("/balance/total", getTotalBalanceHandler(s))
}

// getTotalBalanceHandler 获取用户总余额
//
//nolint:dupl // 与 getPendingDepositBalanceHandler 相似但调用不同的服务方法
func getTotalBalanceHandler(s *api.Server) echo.HandlerFunc {
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
		balance, err := s.Balance.GetTotalBalance(ctx, user.ID, chainID)
		if err != nil {
			log.Error().Err(err).Msg("Failed to get total balance")
			return httperrors.NewHTTPError(http.StatusInternalServerError, types.PublicHTTPErrorTypeGeneric, "Failed to get total balance")
		}

		// 构建响应
		response := &types.TotalBalanceResponse{
			TotalAmount: swag.String(balance.TotalAmount.Text('f', -1)),
			TokenCount:  swag.Int64(int64(balance.TokenCount)),
		}

		return util.ValidateAndReturn(c, http.StatusOK, response)
	}
}
