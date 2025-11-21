package common_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github/chapool/go-wallet/internal/api"
	"github/chapool/go-wallet/internal/test"
)

func TestGetReadyReadiness(t *testing.T) {
	test.WithTestServer(t, func(s *api.Server) {
		res := test.PerformRequest(t, s, "GET", "/-/ready", nil, nil)
		require.Equal(t, http.StatusOK, res.Result().StatusCode)
		require.Equal(t, "Ready.", res.Body.String())
	})
}

func TestGetReadyReadinessBroken(t *testing.T) {
	test.WithTestServer(t, func(s *api.Server) {
		// forcefully remove an initialized component to check if ready state works
		s.Mailer = nil

		res := test.PerformRequest(t, s, "GET", "/-/ready", nil, nil)
		require.Equal(t, 521, res.Result().StatusCode)
		require.Equal(t, "Not ready.", res.Body.String())
	})
}

func TestGetReadyDBBrokenNotReady(t *testing.T) {
	test.WithTestServer(t, func(s *api.Server) {
		// forcefully remove pg
		err := s.DB.Close()
		require.NoError(t, err)

		res := test.PerformRequest(t, s, "GET", "/-/ready", nil, nil)
		require.Equal(t, 521, res.Result().StatusCode)
		require.Equal(t, "Not ready.", res.Body.String())
	})
}
