package wallet

import (
	"database/sql"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/pkg/errors"
	"github/chapool/go-wallet/internal/api"
	"github/chapool/go-wallet/internal/models"
	"github/chapool/go-wallet/internal/types"
	"github/chapool/go-wallet/internal/util"
	"github/chapool/go-wallet/internal/wallet"
)

func GetChainsRoute(s *api.Server) *echo.Route {
	return s.Router.APIV1Wallet.GET("/chains", getChainsHandler(s))
}

func getChainsHandler(s *api.Server) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		log := util.LogFromContext(ctx)

		// Query active chains
		chains, err := models.Chains(
			models.ChainWhere.IsActive.EQ(true),
		).All(ctx, s.DB)

		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				// Return empty list if no chains found
				response := &types.GetChainsResponse{
					Chains: []*types.ChainItem{},
				}
				return util.ValidateAndReturn(c, http.StatusOK, response)
			}
			log.Debug().Err(err).Msg("Failed to get chains")
			return err
		}

		// Convert to ChainItem slice
		chainItems := make([]*types.ChainItem, 0, len(chains))
		for _, chain := range chains {
			chainItems = append(chainItems, wallet.ChainToChainItem(chain))
		}

		response := &types.GetChainsResponse{
			Chains: chainItems,
		}

		return util.ValidateAndReturn(c, http.StatusOK, response)
	}
}
