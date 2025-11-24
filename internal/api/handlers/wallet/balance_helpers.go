package wallet

import (
	"net/http"
	"strconv"

	"github/chapool/go-wallet/internal/api/httperrors"
	"github/chapool/go-wallet/internal/types"

	"github.com/go-openapi/swag"
	"github.com/labstack/echo/v4"
)

// parseChainIDParam 解析 chain_id 查询参数
// 如果 chain_id 参数不存在，返回 nil, nil（表示不过滤）
func parseChainIDParam(c echo.Context) (*int, error) {
	chainIDStr := c.QueryParam("chain_id")
	if chainIDStr == "" {
		//nolint:nilnil // 返回 nil, nil 表示参数不存在，这是预期的行为
		return nil, nil
	}

	chainIDInt, err := strconv.Atoi(chainIDStr)
	if err != nil {
		return nil, httperrors.NewHTTPValidationError(
			http.StatusBadRequest,
			types.PublicHTTPErrorTypeGeneric,
			"Invalid chain_id parameter",
			[]*types.HTTPValidationErrorDetail{
				{
					Key:   swag.String("chain_id"),
					In:    swag.String("query"),
					Error: swag.String("must be a valid integer"),
				},
			},
		)
	}

	return &chainIDInt, nil
}
