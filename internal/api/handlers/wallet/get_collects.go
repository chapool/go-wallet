package wallet

import (
	"context"
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
	maxCollectLimit  = 500 // 最大分页限制
	debugSampleLimit = 10  // 调试时查询的示例交易数量
	defaultLimit     = 50  // 默认分页限制
)

func GetCollectsRoute(s *api.Server) *echo.Route {
	return s.Router.APIV1Wallet.GET("/collects", getCollectsHandler(s))
}

// getUserWalletsForCollect 获取用户的钱包地址列表（仅普通用户）
func getUserWalletsForCollect(ctx context.Context, db *sql.DB, userID string) ([]string, error) {
	userWallets, err := models.Wallets(
		models.WalletWhere.UserID.EQ(userID),
		models.WalletWhere.WalletType.EQ(string(auth.RoleUser)),
	).All(ctx, db)

	if err != nil {
		return nil, err
	}

	if len(userWallets) == 0 {
		return []string{}, nil
	}

	// 构建地址列表（确保地址都是小写，与数据库中的格式一致）
	addresses := make([]string, 0, len(userWallets))
	addressMap := make(map[string]bool) // 用于去重
	for _, wallet := range userWallets {
		lowerAddr := strings.ToLower(wallet.Address)
		if !addressMap[lowerAddr] {
			addresses = append(addresses, lowerAddr)
			addressMap[lowerAddr] = true
		}
	}

	return addresses, nil
}

// getUserWalletsForChain 获取用户在指定链上的钱包地址列表（仅普通用户）
func getUserWalletsForChain(ctx context.Context, db *sql.DB, userID string, chainID int) ([]string, error) {
	userWallets, err := models.Wallets(
		models.WalletWhere.UserID.EQ(userID),
		models.WalletWhere.WalletType.EQ(string(auth.RoleUser)),
		models.WalletWhere.ChainID.EQ(chainID),
	).All(ctx, db)

	if err != nil {
		return nil, err
	}

	if len(userWallets) == 0 {
		return []string{}, nil
	}

	// 构建地址列表
	addresses := make([]string, 0, len(userWallets))
	addressMap := make(map[string]bool)
	for _, wallet := range userWallets {
		lowerAddr := strings.ToLower(wallet.Address)
		if !addressMap[lowerAddr] {
			addresses = append(addresses, lowerAddr)
			addressMap[lowerAddr] = true
		}
	}

	return addresses, nil
}

// applyChainIDFilter 应用 chain_id 过滤条件
func applyChainIDFilter(
	ctx context.Context,
	db *sql.DB,
	mods []qm.QueryMod,
	chainIDStr string,
	isAdmin bool,
	userID string,
	addresses []string,
) ([]qm.QueryMod, []string, error) {
	if chainIDStr == "" {
		return mods, addresses, nil
	}

	chainID, err := strconv.Atoi(chainIDStr)
	if err != nil {
		return nil, nil, httperrors.NewHTTPValidationError(
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

	// 管理员不需要按地址过滤
	if isAdmin {
		return mods, addresses, nil
	}

	// 普通用户：只查询该链的钱包地址
	chainAddresses, err := getUserWalletsForChain(ctx, db, userID, chainID)
	if err != nil {
		return nil, nil, err
	}

	if len(chainAddresses) == 0 {
		return nil, nil, nil // 返回 nil 表示用户在该链上没有钱包
	}

	return mods, chainAddresses, nil
}

// parsePaginationParams 解析分页参数
func parsePaginationParams(offsetStr, limitStr string) (int, int) {
	offset := 0
	if offsetStr != "" {
		if offsetInt, err := strconv.Atoi(offsetStr); err == nil && offsetInt >= 0 {
			offset = offsetInt
		}
	}

	limit := defaultLimit
	if limitStr != "" {
		if limitInt, err := strconv.Atoi(limitStr); err == nil {
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

	return offset, limit
}

// convertTransactionsToCollectItems 将交易转换为响应格式
func convertTransactionsToCollectItems(transactions []*models.Transaction) []*types.CollectItem {
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

	return collectItems
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

		// 检查用户是否为管理员
		isAdmin := user.Role == string(auth.RoleAdmin)
		log.Debug().
			Str("user_id", user.ID).
			Str("user_role", user.Role).
			Bool("is_admin", isAdmin).
			Msg("User role check for collect query")

		var addresses []string

		if !isAdmin {
			// 普通用户：只查询当前用户的归集交易（通过钱包地址）
			var err error
			addresses, err = getUserWalletsForCollect(ctx, s.DB, user.ID)
			if err != nil {
				log.Error().Err(err).Msg("Failed to get user wallets")
				return err
			}

			if len(addresses) == 0 {
				// 用户没有钱包，返回空列表
				log.Debug().
					Str("user_id", user.ID).
					Msg("User has no wallets with type 'user', returning empty collects list")
				response := &types.GetCollectsResponse{
					Collects: []*types.CollectItem{},
				}
				return util.ValidateAndReturn(c, http.StatusOK, response)
			}

			log.Debug().
				Str("user_id", user.ID).
				Int("unique_address_count", len(addresses)).
				Strs("addresses", addresses).
				Msg("Found user wallets for collect query")
		} else {
			// 管理员：可以查询所有归集交易，不需要按地址过滤
			log.Debug().
				Str("user_id", user.ID).
				Msg("Admin user, querying all collect transactions")
		}

		// 按 chain_id 过滤
		var err error
		mods, addresses, err = applyChainIDFilter(ctx, s.DB, mods, chainIDStr, isAdmin, user.ID, addresses)
		if err != nil {
			return err
		}

		// 如果返回的 mods 为 nil，说明用户在该链上没有钱包
		if mods == nil {
			log.Debug().
				Str("chain_id", chainIDStr).
				Str("user_id", user.ID).
				Msg("User has no wallets on this chain, returning empty list")
			response := &types.GetCollectsResponse{
				Collects: []*types.CollectItem{},
			}
			return util.ValidateAndReturn(c, http.StatusOK, response)
		}

		if chainIDStr != "" && !isAdmin && len(addresses) > 0 {
			log.Debug().
				Str("chain_id", chainIDStr).
				Int("chain_wallet_count", len(addresses)).
				Strs("chain_addresses", addresses).
				Msg("Filtered addresses for chain_id")
		}

		// 按状态过滤
		if status != "" {
			mods = append(mods, models.TransactionWhere.Status.EQ(status))
		}

		// 地址过滤（只查询从用户地址发起的归集交易）
		// 管理员不需要按地址过滤，可以查询所有归集交易
		if !isAdmin {
			if len(addresses) == 0 {
				// 如果没有地址，直接返回空列表
				log.Debug().Msg("No addresses to query, returning empty list")
				response := &types.GetCollectsResponse{
					Collects: []*types.CollectItem{},
				}
				return util.ValidateAndReturn(c, http.StatusOK, response)
			}
			mods = append(mods, models.TransactionWhere.FromAddr.IN(addresses))
		}

		// 分页
		offset, limit := parsePaginationParams(offsetStr, limitStr)

		mods = append(mods, qm.Offset(offset), qm.Limit(limit))
		mods = append(mods, qm.OrderBy(models.TransactionColumns.CreatedAt+" DESC"))

		// 记录查询条件用于调试
		log.Debug().
			Str("user_id", user.ID).
			Strs("addresses", addresses).
			Str("chain_id", chainIDStr).
			Str("status", status).
			Int("offset", offset).
			Int("limit", limit).
			Int("query_mods_count", len(mods)).
			Msg("Querying collect transactions")

		// 调试：先查询所有归集交易（不按地址过滤）看看是否有数据
		allCollects, _ := models.Transactions(
			models.TransactionWhere.Type.EQ(models.TransactionTypeCollect),
		).Count(ctx, s.DB)
		log.Debug().Int64("total_collects_in_db", allCollects).Msg("Total collect transactions in database")

		// 调试：查询所有归集交易的地址，看看数据库中的地址格式
		if allCollects > 0 {
			allCollectTxs, _ := models.Transactions(
				models.TransactionWhere.Type.EQ(models.TransactionTypeCollect),
				qm.Limit(debugSampleLimit),
			).All(ctx, s.DB)
			for _, tx := range allCollectTxs {
				log.Debug().
					Str("tx_hash", tx.TXHash).
					Str("from_addr", tx.FromAddr).
					Str("to_addr", tx.ToAddr).
					Int("chain_id", tx.ChainID).
					Str("status", tx.Status).
					Msg("Sample collect transaction from database")
			}
		}

		// 调试：检查地址匹配（仅对普通用户）
		if !isAdmin && len(addresses) > 0 {
			// 查询匹配地址的归集交易数量（不按 chain_id 和 status 过滤）
			matchingCollectsAll, _ := models.Transactions(
				models.TransactionWhere.Type.EQ(models.TransactionTypeCollect),
				models.TransactionWhere.FromAddr.IN(addresses),
			).Count(ctx, s.DB)

			// 查询匹配地址和 chain_id 的归集交易数量（不按 status 过滤）
			var matchingCollectsByChain int64
			if chainIDStr != "" {
				chainID, _ := strconv.Atoi(chainIDStr)
				matchingCollectsByChain, _ = models.Transactions(
					models.TransactionWhere.Type.EQ(models.TransactionTypeCollect),
					models.TransactionWhere.ChainID.EQ(chainID),
					models.TransactionWhere.FromAddr.IN(addresses),
				).Count(ctx, s.DB)
			}

			log.Debug().
				Int64("matching_collects_all", matchingCollectsAll).
				Int64("matching_collects_by_chain", matchingCollectsByChain).
				Strs("query_addresses", addresses).
				Str("chain_id", chainIDStr).
				Str("status", status).
				Msg("Collect transactions matching user addresses")
		} else if isAdmin {
			log.Debug().
				Str("chain_id", chainIDStr).
				Str("status", status).
				Msg("Admin querying all collect transactions")
		}

		// 查询交易
		transactions, err := models.Transactions(mods...).All(ctx, s.DB)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				log.Debug().
					Strs("addresses", addresses).
					Str("chain_id", chainIDStr).
					Str("status", status).
					Msg("No collect transactions found matching criteria")
				response := &types.GetCollectsResponse{
					Collects: []*types.CollectItem{},
				}
				return util.ValidateAndReturn(c, http.StatusOK, response)
			}
			log.Error().Err(err).Msg("Failed to get collects")
			return err
		}

		log.Debug().
			Int("transaction_count", len(transactions)).
			Strs("addresses", addresses).
			Msg("Found collect transactions")

		// 转换为响应格式
		collectItems := convertTransactionsToCollectItems(transactions)

		response := &types.GetCollectsResponse{
			Collects: collectItems,
		}

		return util.ValidateAndReturn(c, http.StatusOK, response)
	}
}
