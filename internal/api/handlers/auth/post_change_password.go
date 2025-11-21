package auth

import (
	"net/http"

	"github.com/go-openapi/swag"
	"github.com/labstack/echo/v4"
	"github/chapool/go-wallet/internal/api"
	"github/chapool/go-wallet/internal/auth"
	"github/chapool/go-wallet/internal/data/dto"
	"github/chapool/go-wallet/internal/types"
	"github/chapool/go-wallet/internal/util"
)

func PostChangePasswordRoute(s *api.Server) *echo.Route {
	return s.Router.APIV1Auth.POST("/change-password", postChangePasswordHandler(s))
}

func postChangePasswordHandler(s *api.Server) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		user := auth.UserFromEchoContext(c)
		log := util.LogFromContext(ctx)

		var body types.PostChangePasswordPayload
		if err := util.BindAndValidateBody(c, &body); err != nil {
			return err
		}

		result, err := s.Auth.UpdatePassword(ctx, dto.UpdatePasswordRequest{
			User:            *user,
			CurrentPassword: swag.StringValue(body.CurrentPassword),
			NewPassword:     swag.StringValue(body.NewPassword),
		})
		if err != nil {
			log.Debug().Err(err).Msg("Failed to update password")
			return err
		}

		return util.ValidateAndReturn(c, http.StatusOK, result.ToTypes())
	}
}
