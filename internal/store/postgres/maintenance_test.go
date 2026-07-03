package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTruncate_ClearsAllTables(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	applyTestSchema(t, dsn)
	truncateAll(t, s)

	seedConcept(t, s, "messages/pacs.008.md", "message", "Credit Transfer", "body", []string{"pix"}, 0)
	_, _, err = s.NextEpoch(ctx, "iso", "", 1, 0, 0)
	require.NoError(t, err)

	require.NoError(t, s.Truncate(ctx))

	for _, tbl := range []string{"concept", "embedding", "epoch", "edge", "concept_fact"} {
		var n int
		require.NoError(t, s.pool.QueryRow(ctx, "SELECT count(*) FROM "+tbl).Scan(&n))
		require.Equalf(t, 0, n, "table %s not truncated", tbl)
	}
}

func TestSetEpochCommit_PersistsSHA(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	applyTestSchema(t, dsn)
	truncateAll(t, s)

	n, _, err := s.NextEpoch(ctx, "iso", "", 1, 0, 0)
	require.NoError(t, err)

	require.NoError(t, s.SetEpochCommit(ctx, n, "deadbeefcafe"))

	var sha string
	require.NoError(t, s.pool.QueryRow(ctx, "SELECT git_commit FROM epoch WHERE n=$1", n).Scan(&sha))
	require.Equal(t, "deadbeefcafe", sha)
}
