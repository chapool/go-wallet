package wallet

import (
	"context"
	"database/sql"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	"github.com/aarondl/sqlboiler/v4/queries/qm"
	"github.com/go-openapi/swag"
	"github.com/labstack/echo/v4"
	"github.com/pkg/errors"
	"github/chapool/go-wallet/internal/api"
	"github/chapool/go-wallet/internal/api/httperrors"
	"github/chapool/go-wallet/internal/auth"
	"github/chapool/go-wallet/internal/models"
	"github/chapool/go-wallet/internal/types"
	"github/chapool/go-wallet/internal/util"
)

const (
	// base10 is the base for decimal number parsing
	base10 = 10
)

func GetPendingDepositsRoute(s *api.Server) *echo.Route {
	return s.Router.APIV1Wallet.GET("/deposits/pending", getPendingDepositsHandler(s))
}

func getPendingDepositsHandler(s *api.Server) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		user := auth.UserFromContext(ctx)
		if user == nil {
			return echo.ErrUnauthorized
		}
		log := util.LogFromContext(ctx)

		// 解析查询参数
		chainIDStr := c.QueryParam("chain_id")

		// 构建查询条件：只查询 confirmed 或 safe 状态的充值交易（未 finalized）
		mods := []qm.QueryMod{
			models.TransactionWhere.Type.EQ(models.TransactionTypeDeposit),
			models.TransactionWhere.Status.IN([]string{
				models.TransactionStatusConfirmed,
				models.TransactionStatusSafe,
			}),
		}

		// 只查询当前用户的充值交易（通过钱包地址）
		userWallets, err := models.Wallets(
			models.WalletWhere.UserID.EQ(user.ID),
		).All(ctx, s.DB)

		if err != nil {
			log.Error().Err(err).Msg("Failed to get user wallets")
			return err
		}

		if len(userWallets) == 0 {
			// 用户没有钱包，返回空列表
			response := &types.GetPendingDepositsResponse{
				PendingDeposits: []*types.PendingDepositItem{},
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

		// 地址过滤（只查询充值到用户地址的交易）
		if len(addresses) > 0 {
			mods = append(mods, models.TransactionWhere.ToAddr.IN(addresses))
		}

		// 查询所有待确认的充值交易
		transactions, err := models.Transactions(mods...).All(ctx, s.DB)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response := &types.GetPendingDepositsResponse{
					PendingDeposits: []*types.PendingDepositItem{},
				}
				return util.ValidateAndReturn(c, http.StatusOK, response)
			}
			log.Error().Err(err).Msg("Failed to get pending deposits")
			return err
		}

		// 按 token_symbol 分组并聚合
		pendingDeposits, err := aggregatePendingDeposits(ctx, s.DB, transactions)
		if err != nil {
			log.Error().Err(err).Msg("Failed to aggregate pending deposits")
			return err
		}

		response := &types.GetPendingDepositsResponse{
			PendingDeposits: pendingDeposits,
		}

		return util.ValidateAndReturn(c, http.StatusOK, response)
	}
}

// aggregatePendingDeposits 按 token_symbol 分组并聚合待确认充值
func aggregatePendingDeposits(ctx context.Context, db *sql.DB, transactions []*models.Transaction) ([]*types.PendingDepositItem, error) {
	if len(transactions) == 0 {
		return []*types.PendingDepositItem{}, nil
	}

	// 构建 token 缓存
	tokenMap, err := buildTokenCacheForTransactions(ctx, db, transactions)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build token cache")
	}

	// 按 token_symbol 分组
	type tokenGroup struct {
		tokenSymbol string
		totalAmount *big.Int
		count       int64
	}

	groups := make(map[string]*tokenGroup)

	for _, tx := range transactions {
		// 获取 token_symbol
		tokenSymbol := getTokenSymbolForTransaction(tx, tokenMap)

		// 解析金额
		amount, ok := new(big.Int).SetString(tx.Amount, base10)
		if !ok {
			// 如果解析失败，跳过这条交易
			continue
		}

		// 分组聚合
		if group, exists := groups[tokenSymbol]; exists {
			group.totalAmount = new(big.Int).Add(group.totalAmount, amount)
			group.count++
		} else {
			groups[tokenSymbol] = &tokenGroup{
				tokenSymbol: tokenSymbol,
				totalAmount: new(big.Int).Set(amount),
				count:       1,
			}
		}
	}

	// 转换为响应类型
	result := make([]*types.PendingDepositItem, 0, len(groups))
	for _, group := range groups {
		item := &types.PendingDepositItem{
			TokenSymbol:      swag.String(group.tokenSymbol),
			PendingAmount:    swag.String(group.totalAmount.String()),
			TransactionCount: swag.Int64(group.count),
		}
		result = append(result, item)
	}

	return result, nil
}

// buildTokenCacheForTransactions 为交易列表构建 token 缓存
func buildTokenCacheForTransactions(ctx context.Context, db *sql.DB, transactions []*models.Transaction) (map[tokenCacheKey]*models.Token, error) {
	if len(transactions) == 0 {
		return map[tokenCacheKey]*models.Token{}, nil
	}

	chainSet := make(map[int]struct{})
	for _, tx := range transactions {
		chainSet[tx.ChainID] = struct{}{}
	}

	chainIDs := make([]int, 0, len(chainSet))
	for chainID := range chainSet {
		chainIDs = append(chainIDs, chainID)
	}

	tokens, err := models.Tokens(
		models.TokenWhere.ChainID.IN(chainIDs),
	).All(ctx, db)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load token metadata")
	}

	cache := make(map[tokenCacheKey]*models.Token, len(tokens))
	for _, token := range tokens {
		key := tokenCacheKey{
			chainID:  token.ChainID,
			tokenKey: normalizeTokenAddress(token.TokenAddress),
		}
		cache[key] = token
	}
	return cache, nil
}

// getTokenSymbolForTransaction 获取交易的 token_symbol
func getTokenSymbolForTransaction(tx *models.Transaction, tokenMap map[tokenCacheKey]*models.Token) string {
	// 尝试从 tokenMap 查找
	key := tokenCacheKey{
		chainID:  tx.ChainID,
		tokenKey: normalizeTokenAddress(tx.TokenAddr),
	}

	if token, ok := tokenMap[key]; ok && token.TokenSymbol != "" {
		return token.TokenSymbol
	}

	// 如果是原生代币（token_addr 为空），查找原生代币
	if !tx.TokenAddr.Valid || tx.TokenAddr.String == "" {
		nativeKey := tokenCacheKey{
			chainID:  tx.ChainID,
			tokenKey: "",
		}
		if token, ok := tokenMap[nativeKey]; ok && token.TokenSymbol != "" {
			return token.TokenSymbol
		}
	}

	// 如果找不到，返回空字符串（前端可以显示为 "UNKNOWN"）
	return ""
}
