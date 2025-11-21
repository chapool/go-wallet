package fixtures_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	data "github/chapool/go-wallet/internal/data/fixtures"
	"github/chapool/go-wallet/internal/models"
)

func TestUpsertableInterface(t *testing.T) {
	var user any = &models.AppUserProfile{
		UserID: "62b13d29-5c4e-420e-b991-a631d3938776",
	}

	_, ok := user.(data.Upsertable)
	assert.True(t, ok, "AppUserProfile should implement the Upsertable interface")
}
