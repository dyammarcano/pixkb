package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/stretchr/testify/require"
)

// toPgx5DSN converts postgres:// or postgresql:// schemes to pgx5://
// so golang-migrate uses the pgx/v5 driver.
func toPgx5DSN(dsn string) string {
	if len(dsn) >= 11 && dsn[:11] == "postgres://" {
		return "pgx5://" + dsn[11:]
	}
	if len(dsn) >= 13 && dsn[:13] == "postgresql://" {
		return "pgx5://" + dsn[13:]
	}
	return dsn
}

// applyTestSchema applies the embedded migration (idempotent) so tests
// exercise the SAME schema as production — no duplicated DDL strings.
func applyTestSchema(t *testing.T, dsn string) {
	t.Helper()
	src, err := iofs.New(SchemaFS, "schema")
	require.NoError(t, err)
	m, err := migrate.NewWithSourceInstance("iofs", src, toPgx5DSN(dsn))
	require.NoError(t, err)
	defer func() { _, _ = m.Close() }()

	err = m.Up()
	if err == nil || errors.Is(err, migrate.ErrNoChange) {
		return
	}
	// Self-heal a database left dirty by an interrupted/failed prior run (common
	// when integration tests share one DB): force the recorded version clean and
	// retry once. The single migration is idempotent at the steady state.
	var dirty migrate.ErrDirty
	if errors.As(err, &dirty) {
		require.NoError(t, m.Force(dirty.Version))
		if err2 := m.Up(); err2 != nil && !errors.Is(err2, migrate.ErrNoChange) {
			require.NoError(t, err2)
		}
		return
	}
	require.NoError(t, err)
}

// truncateAll clears all tables between tests (mirrors Store.Truncate, including
// concept_fact so bitemporal rows do not leak across tests).
func truncateAll(t *testing.T, s *Store) {
	t.Helper()
	_, err := s.pool.Exec(context.Background(),
		"TRUNCATE concept, embedding, epoch, edge, concept_fact RESTART IDENTITY CASCADE")
	require.NoError(t, err)
}

func TestSchema_Applies(t *testing.T) {
	dsn := testDSN(t)
	applyTestSchema(t, dsn)

	s, err := Open(context.Background(), dsn)
	require.NoError(t, err)
	defer s.Close()

	truncateAll(t, s)

	tables := []string{"concept", "embedding", "epoch", "edge", "concept_fact"}
	for _, tbl := range tables {
		var n int
		err = s.pool.QueryRow(context.Background(),
			"SELECT count(*) FROM "+tbl).Scan(&n)
		require.NoError(t, err, "table %s", tbl)
		require.Equal(t, 0, n, "table %s should be empty", tbl)
	}
}
