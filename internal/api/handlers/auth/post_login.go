package auth

import (
	"net/http"

	"github.com/go-openapi/swag"
	"github.com/labstack/echo/v4"
	"github/chapool/go-wallet/internal/api"
	"github/chapool/go-wallet/internal/data/dto"
	"github/chapool/go-wallet/internal/types"
	"github/chapool/go-wallet/internal/util"
)

func PostLoginRoute(s *api.Server) *echo.Route {
	return s.Router.APIV1Auth.POST("/login", postLoginHandler(s))
}

func postLoginHandler(s *api.Server) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		log := util.LogFromContext(ctx)

		var body types.PostLoginPayload
		if err := util.BindAndValidateBody(c, &body); err != nil {
			return err
		}

		result, err := s.Auth.Login(ctx, dto.LoginRequest{
			Username: dto.NewUsername(body.Username.String()),
			Password: swag.StringValue(body.Password),
		})
		if err != nil {
			log.Debug().Err(err).Msg("Failed to authenticate user")
			return err
		}

		return util.ValidateAndReturn(c, http.StatusOK, result.ToTypes())
	}
}
