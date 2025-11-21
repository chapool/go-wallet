package wellknown_test

import (
	"path/filepath"
	"testing"

	"github.com/labstack/echo/v4"
	"github/chapool/go-wallet/internal/api"
	"github/chapool/go-wallet/internal/api/httperrors"
	"github/chapool/go-wallet/internal/config"
	"github/chapool/go-wallet/internal/test"
	"github/chapool/go-wallet/internal/util"
)

func TestGetAndroidWellKnown(t *testing.T) {
	config := config.DefaultServiceConfigFromEnv()
	config.Paths.AndroidAssetlinksFile = filepath.Join(util.GetProjectRootDir(), "test", "testdata", "android-assetlinks.json")

	testGetWellKnown(t, config, "/.well-known/assetlinks.json")
}

func TestGetAndroidWellKnownNotFound(t *testing.T) {
	config := config.DefaultServiceConfigFromEnv()
	config.Paths.AndroidAssetlinksFile = ""

	test.WithTestServerConfigurable(t, config, func(s *api.Server) {
		res := test.PerformRequest(t, s, "GET", "/.well-known/assetlinks.json", nil, nil)
		test.RequireHTTPError(t, res, httperrors.NewFromEcho(echo.ErrNotFound))
	})
}
