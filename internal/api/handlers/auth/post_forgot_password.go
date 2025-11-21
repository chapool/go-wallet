package auth

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
	"github/chapool/go-wallet/internal/api"
	"github/chapool/go-wallet/internal/data/dto"
	"github/chapool/go-wallet/internal/types"
	"github/chapool/go-wallet/internal/util"
	"github/chapool/go-wallet/internal/util/url"
)

func PostForgotPasswordRoute(s *api.Server) *echo.Route {
	return s.Router.APIV1Auth.POST("/forgot-password", postForgotPasswordHandler(s))
}

func postForgotPasswordHandler(s *api.Server) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()

		var body types.PostForgotPasswordPayload
		if err := util.BindAndValidateBody(c, &body); err != nil {
			return err
		}

		username := dto.NewUsername(body.Username.String())

		result, err := s.Auth.InitPasswordReset(ctx, dto.InitPasswordResetRequest{
			Username: username,
		})
		if err != nil {
			log.Debug().Err(err).Msg("Failed to initiate password reset")
			return err
		}

		if result.ResetToken.IsZero() {
			log.Debug().Msg("Failed to initiate password reset, no token returned")
			// Return success status to prevent user enumeration
			return c.NoContent(http.StatusNoContent)
		}

		resetLink, err := url.PasswordResetDeeplinkURL(s.Config, result.ResetToken.String)
		if err != nil {
			log.Debug().Err(err).Msg("Failed to generate password reset link")
			return err
		}

		if err := s.Mailer.SendPasswordReset(ctx, username.String(), resetLink.String()); err != nil {
			log.Debug().Err(err).Msg("Failed to send password reset email")
			return err
		}

		return c.NoContent(http.StatusNoContent)
	}
}
