package wallet

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

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

const (
	maxCollectLimit = 500 // 最大分页限制
)

func GetCollectsRoute(s *api.Server) *echo.Route {
	return s.Router.APIV1Wallet.GET("/collects", getCollectsHandler(s))
}

func getCollectsHandler(s *api.Server) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		user := auth.UserFromContext(ctx)
		if user == nil {
			return echo.ErrUnauthorized
		}
		log := util.LogFromContext(ctx)

		// 解析查询参数
		chainIDStr := c.QueryParam("chain_id")
		status := c.QueryParam("status")
		offsetStr := c.QueryParam("offset")
		limitStr := c.QueryParam("limit")

		// 构建查询条件
		mods := []qm.QueryMod{
			models.TransactionWhere.Type.EQ(models.TransactionTypeCollect),
		}

		// 只查询当前用户的归集交易（通过钱包地址）
		userWallets, err := models.Wallets(
			models.WalletWhere.UserID.EQ(user.ID),
			models.WalletWhere.WalletType.EQ("user"),
		).All(ctx, s.DB)

		if err != nil {
			log.Error().Err(err).Msg("Failed to get user wallets")
			return err
		}

		if len(userWallets) == 0 {
			// 用户没有钱包，返回空列表
			response := &types.GetCollectsResponse{
				Collects: []*types.CollectItem{},
			}
			return util.ValidateAndReturn(c, http.StatusOK, response)
		}

		// 构建地址列表
		addresses := make([]string, 0, len(userWallets))
		for _, wallet := range userWallets {
			addresses = append(addresses, strings.ToLower(wallet.Address))
		}

		// 按 chain_id 过滤
		if chainIDStr != "" {
			chainID, err := strconv.Atoi(chainIDStr)
			if err != nil {
				return httperrors.NewHTTPValidationError(
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
			mods = append(mods, models.TransactionWhere.ChainID.EQ(chainID))

			// 只查询该链的钱包地址
			chainAddresses := make([]string, 0)
			for _, wallet := range userWallets {
				if wallet.ChainID == chainID {
					chainAddresses = append(chainAddresses, strings.ToLower(wallet.Address))
				}
			}
			if len(chainAddresses) > 0 {
				addresses = chainAddresses
			}
		}

		// 按状态过滤
		if status != "" {
			mods = append(mods, models.TransactionWhere.Status.EQ(status))
		}

		// 地址过滤（只查询从用户地址发起的归集交易）
		if len(addresses) > 0 {
			mods = append(mods, models.TransactionWhere.FromAddr.IN(addresses))
		}

		// 分页
		offset := 0
		if offsetStr != "" {
			offsetInt, err := strconv.Atoi(offsetStr)
			if err == nil && offsetInt >= 0 {
				offset = offsetInt
			}
		}
		limit := 50
		if limitStr != "" {
			limitInt, err := strconv.Atoi(limitStr)
			if err == nil {
				switch {
				case limitInt > maxCollectLimit:
					limit = maxCollectLimit
				case limitInt < 1:
					limit = 1
				default:
					limit = limitInt
				}
			}
		}

		mods = append(mods, qm.Offset(offset), qm.Limit(limit))
		mods = append(mods, qm.OrderBy(models.TransactionColumns.CreatedAt+" DESC"))

		// 查询交易
		transactions, err := models.Transactions(mods...).All(ctx, s.DB)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response := &types.GetCollectsResponse{
					Collects: []*types.CollectItem{},
				}
				return util.ValidateAndReturn(c, http.StatusOK, response)
			}
			log.Error().Err(err).Msg("Failed to get collects")
			return err
		}

		// 转换为响应格式
		collectItems := make([]*types.CollectItem, 0, len(transactions))
		for _, tx := range transactions {
			id := strfmt.UUID(tx.ID)
			createdAt := strfmt.DateTime(tx.CreatedAt)

			item := &types.CollectItem{
				ID:        &id,
				ChainID:   swag.Int64(int64(tx.ChainID)),
				TxHash:    swag.String(tx.TXHash),
				FromAddr:  swag.String(tx.FromAddr),
				ToAddr:    swag.String(tx.ToAddr),
				Amount:    swag.String(tx.Amount),
				Status:    swag.String(tx.Status),
				BlockNo:   swag.Int64(tx.BlockNo),
				CreatedAt: &createdAt,
			}

			if tx.ConfirmationCount.Valid {
				item.ConfirmationCount = int64(tx.ConfirmationCount.Int)
			}

			collectItems = append(collectItems, item)
		}

		response := &types.GetCollectsResponse{
			Collects: collectItems,
		}

		return util.ValidateAndReturn(c, http.StatusOK, response)
	}
}
