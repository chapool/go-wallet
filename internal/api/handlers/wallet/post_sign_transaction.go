package wallet

import (
	"encoding/hex"
	"net/http"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/labstack/echo/v4"
	"github/chapool/go-wallet/internal/api"
	"github/chapool/go-wallet/internal/api/httperrors"
	"github/chapool/go-wallet/internal/auth"
	"github/chapool/go-wallet/internal/types"
	"github/chapool/go-wallet/internal/util"
)

func PostSignTransactionRoute(s *api.Server) *echo.Route {
	return s.Router.APIV1Wallet.POST("/sign-transaction", postSignTransactionHandler(s))
}

func postSignTransactionHandler(s *api.Server) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		user := auth.UserFromContext(ctx)
		if user == nil {
			return echo.ErrUnauthorized
		}
		log := util.LogFromContext(ctx)

		var body types.PostSignTransactionPayload
		if err := util.BindAndValidateBody(c, &body); err != nil {
			return err
		}

		// Get wallet by address to get derivation path
		walletResult, err := s.Wallet.GetWalletByAddress(ctx, swag.StringValue(body.FromAddress), int(swag.Int64Value(body.ChainID)))
		if err != nil {
			log.Debug().Err(err).Msg("Failed to get wallet by address")
			if err.Error() == "wallet not found" {
				return httperrors.NewHTTPError(http.StatusNotFound, types.PublicHTTPErrorTypeGeneric, "Wallet not found")
			}
			return err
		}

		// Verify wallet belongs to user
		if walletResult.UserID != user.ID {
			return httperrors.NewHTTPError(http.StatusForbidden, types.PublicHTTPErrorTypeGeneric, "Wallet does not belong to user")
		}

		// Convert data from base64 to bytes
		var data []byte
		if body.Data != nil {
			data = []byte(body.Data)
		}

		// Create sign request
		gasLimitInt64 := swag.Int64Value(body.GasLimit)
		if gasLimitInt64 < 0 {
			return httperrors.NewHTTPValidationError(
				http.StatusBadRequest,
				types.PublicHTTPErrorTypeGeneric,
				"Invalid gas_limit",
				[]*types.HTTPValidationErrorDetail{
					{
						Key:   swag.String("gas_limit"),
						In:    swag.String("body"),
						Error: swag.String("must be non-negative"),
					},
				},
			)
		}

		nonceInt64 := swag.Int64Value(body.Nonce)
		if nonceInt64 < 0 {
			return httperrors.NewHTTPValidationError(
				http.StatusBadRequest,
				types.PublicHTTPErrorTypeGeneric,
				"Invalid nonce",
				[]*types.HTTPValidationErrorDetail{
					{
						Key:   swag.String("nonce"),
						In:    swag.String("body"),
						Error: swag.String("must be non-negative"),
					},
				},
			)
		}

		signReq := &api.SignEVMRequest{
			ChainID:              swag.Int64Value(body.ChainID),
			To:                   swag.StringValue(body.To),
			Value:                swag.StringValue(body.Value),
			GasLimit:             uint64(gasLimitInt64),
			MaxFeePerGas:         swag.StringValue(body.MaxFeePerGas),
			MaxPriorityFeePerGas: swag.StringValue(body.MaxPriorityFeePerGas),
			Nonce:                uint64(nonceInt64),
			Data:                 data,
			FromAddress:          swag.StringValue(body.FromAddress),
			DerivationPath:       walletResult.DerivationPath,
		}

		// Sign transaction
		signResp, err := s.Signer.SignEVMTransaction(ctx, signReq)
		if err != nil {
			log.Debug().Err(err).Msg("Failed to sign transaction")
			return err
		}

		// Convert response
		rawTxBase64 := strfmt.Base64(hex.EncodeToString(signResp.RawTransaction))
		response := &types.SignTransactionResponse{
			RawTransaction: &rawTxBase64,
			TxHash:         swag.String(signResp.TxHash),
		}

		return util.ValidateAndReturn(c, http.StatusOK, response)
	}
}
