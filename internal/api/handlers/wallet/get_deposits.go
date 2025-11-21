package wallet

import (
	"context"
	"database/sql"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github/chapool/go-wallet/internal/api"
	"github/chapool/go-wallet/internal/api/httperrors"
	"github/chapool/go-wallet/internal/auth"
	"github/chapool/go-wallet/internal/models"
	"github/chapool/go-wallet/internal/types"
	"github/chapool/go-wallet/internal/util"

	"github.com/aarondl/null/v8"
	"github.com/aarondl/sqlboiler/v4/queries/qm"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/labstack/echo/v4"
	"github.com/pkg/errors"
)

const (
	maxDepositLimit = 500 // 最大分页限制
)

func GetDepositsRoute(s *api.Server) *echo.Route {
	return s.Router.APIV1Wallet.GET("/deposits", getDepositsHandler(s))
}

func getDepositsHandler(s *api.Server) echo.HandlerFunc {
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
			models.TransactionWhere.Type.EQ("deposit"),
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
			response := &types.GetDepositsResponse{
				Deposits: []*types.DepositItem{},
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
					chainAddresses = append(chainAddresses, wallet.Address)
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

		// 地址过滤（只查询充值到用户地址的交易）
		if len(addresses) > 0 {
			mods = append(mods, models.TransactionWhere.ToAddr.IN(addresses))
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
				case limitInt > maxDepositLimit:
					limit = maxDepositLimit
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
				response := &types.GetDepositsResponse{
					Deposits: []*types.DepositItem{},
				}
				return util.ValidateAndReturn(c, http.StatusOK, response)
			}
			log.Error().Err(err).Msg("Failed to get deposits")
			return err
		}

		depositItems, err := buildDepositResponse(ctx, s.DB, user.ID, transactions)
		if err != nil {
			log.Error().Err(err).Msg("Failed to build deposit response")
			return err
		}

		response := &types.GetDepositsResponse{
			Deposits: depositItems,
		}

		return util.ValidateAndReturn(c, http.StatusOK, response)
	}
}

// transactionToDepositItem 转换 Transaction 为 DepositItem
//
//nolint:varnamelen // tx is a common abbreviation for transaction
func buildDepositResponse(ctx context.Context, db *sql.DB, userID string, transactions []*models.Transaction) ([]*types.DepositItem, error) {
	if len(transactions) == 0 {
		return []*types.DepositItem{}, nil
	}

	creditsByTx, err := fetchCreditsByTransaction(ctx, db, userID, transactions)
	if err != nil {
		return nil, err
	}

	tokenMap, err := buildTokenCache(ctx, db, transactions)
	if err != nil {
		return nil, err
	}

	depositItems := make([]*types.DepositItem, 0, len(transactions))
	for _, tx := range transactions {
		credit := creditsByTx[tx.ID]
		tokenSymbol := resolveTokenSymbol(tx, credit, tokenMap)
		item := transactionToDepositItem(tx, credit, tokenSymbol)
		depositItems = append(depositItems, item)
	}
	return depositItems, nil
}

func fetchCreditsByTransaction(ctx context.Context, db *sql.DB, userID string, transactions []*models.Transaction) (map[string]*models.Credit, error) {
	refIDs := make([]string, 0, len(transactions))
	for _, tx := range transactions {
		refIDs = append(refIDs, tx.ID)
	}

	credits := map[string]*models.Credit{}
	if len(refIDs) == 0 {
		return credits, nil
	}

	records, err := models.Credits(
		models.CreditWhere.ReferenceID.IN(refIDs),
		models.CreditWhere.ReferenceType.EQ(models.ReferenceTypeBlockchainTX),
		models.CreditWhere.UserID.EQ(userID),
	).All(ctx, db)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load deposit credits")
	}

	for _, credit := range records {
		credits[credit.ReferenceID] = credit
	}
	return credits, nil
}

type tokenCacheKey struct {
	chainID  int
	tokenKey string
}

func buildTokenCache(ctx context.Context, db *sql.DB, transactions []*models.Transaction) (map[tokenCacheKey]*models.Token, error) {
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
	sort.Ints(chainIDs)

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

func normalizeTokenAddress(addr null.String) string {
	if !addr.Valid {
		return ""
	}
	return strings.ToLower(addr.String)
}

//nolint:varnamelen // tx is a common abbreviation for transaction
func transactionToDepositItem(tx *models.Transaction, credit *models.Credit, tokenSymbol string) *types.DepositItem {
	id := strfmt.UUID(tx.ID)
	createdAt := strfmt.DateTime(tx.CreatedAt)

	item := &types.DepositItem{
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

	if tx.TokenAddr.Valid {
		item.TokenAddr = tx.TokenAddr.String
	}

	if tx.ConfirmationCount.Valid {
		item.ConfirmationCount = int64(tx.ConfirmationCount.Int)
	}

	if credit != nil && credit.TokenSymbol != "" {
		item.TokenSymbol = swag.String(credit.TokenSymbol)
	} else {
		item.TokenSymbol = swag.String(tokenSymbol)
	}

	return item
}

func resolveTokenSymbol(tx *models.Transaction, credit *models.Credit, cache map[tokenCacheKey]*models.Token) string {
	if credit != nil && credit.TokenSymbol != "" {
		return credit.TokenSymbol
	}

	key := tokenCacheKey{
		chainID:  tx.ChainID,
		tokenKey: normalizeTokenAddress(tx.TokenAddr),
	}
	if token, ok := cache[key]; ok && token.TokenSymbol != "" {
		return token.TokenSymbol
	}

	nativeKey := tokenCacheKey{
		chainID:  tx.ChainID,
		tokenKey: "",
	}
	if token, ok := cache[nativeKey]; ok && token.TokenSymbol != "" {
		return token.TokenSymbol
	}

	return ""
}
