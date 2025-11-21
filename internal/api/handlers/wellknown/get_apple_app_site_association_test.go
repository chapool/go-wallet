package wellknown_test

import (
	"io"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github/chapool/go-wallet/internal/api"
	"github/chapool/go-wallet/internal/api/httperrors"
	"github/chapool/go-wallet/internal/config"
	"github/chapool/go-wallet/internal/test"
	"github/chapool/go-wallet/internal/util"
)

func testGetWellKnown(t *testing.T, config config.Server, path string) {
	t.Helper()

	test.WithTestServerConfigurable(t, config, func(s *api.Server) {
		res := test.PerformRequest(t, s, "GET", path, nil, nil)
		require.Equal(t, http.StatusOK, res.Result().StatusCode)

		result, err := io.ReadAll(res.Body)
		require.NoError(t, err)

		test.Snapshoter.SaveString(t, string(result))
	})
}

func TestGetAppleWellKnown(t *testing.T) {
	config := config.DefaultServiceConfigFromEnv()
	config.Paths.AppleAppSiteAssociationFile = filepath.Join(util.GetProjectRootDir(), "test", "testdata", "apple-app-site-association.json")

	testGetWellKnown(t, config, "/.well-known/apple-app-site-association")
}

func TestGetAppleWellKnownNotFound(t *testing.T) {
	config := config.DefaultServiceConfigFromEnv()
	config.Paths.AppleAppSiteAssociationFile = ""

	test.WithTestServerConfigurable(t, config, func(s *api.Server) {
		res := test.PerformRequest(t, s, "GET", "/.well-known/apple-app-site-association", nil, nil)
		test.RequireHTTPError(t, res, httperrors.NewFromEcho(echo.ErrNotFound))
	})
}
