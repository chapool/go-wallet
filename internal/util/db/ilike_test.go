package db_test

import (
	"testing"

	"github.com/aarondl/sqlboiler/v4/queries"
	"github.com/aarondl/sqlboiler/v4/queries/qm"
	"github.com/stretchr/testify/assert"
	"github/chapool/go-wallet/internal/models"
	"github/chapool/go-wallet/internal/test"
	"github/chapool/go-wallet/internal/util/db"
)

func TestILike(t *testing.T) {
	query := models.NewQuery(
		qm.Select("*"),
		qm.From("users"),
		db.InnerJoin("users", "id", "app_user_profiles", "user_id"),
		db.ILike("%Max.Muster%", "users", "username"),
		db.ILike("Max", "users", "app_user_profiles", "first_name"),
	)

	sql, args := queries.BuildQuery(query)

	test.Snapshoter.Label("SQL").Save(t, sql)
	test.Snapshoter.Label("Args").Save(t, args...)
}

func TestEscapeLike(t *testing.T) {
	res := db.EscapeLike("%foo% _b%a_r%")
	assert.Equal(t, "\\%foo\\% \\_b\\%a\\_r\\%", res)
}

func TestILikeSearch(t *testing.T) {
	query := models.NewQuery(
		qm.Select("*"),
		qm.From("users"),
		db.InnerJoin("users", "id", "app_user_profiles", "user_id"),
		db.ILikeSearch("  mus%ter m_ax  ", "users", "username"),
	)

	sql, args := queries.BuildQuery(query)

	test.Snapshoter.Label("SQL").Save(t, sql)
	test.Snapshoter.Label("Args").Save(t, args...)
}
