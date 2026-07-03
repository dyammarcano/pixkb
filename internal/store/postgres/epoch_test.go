package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNextEpoch_MonotonicFromZero(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	applyTestSchema(t, dsn)
	truncateAll(t, s)

	before := time.Now().Add(-time.Minute)

	n0, t0, err := s.NextEpoch(ctx, "git", "deadbeef", 10, 2, 1)
	require.NoError(t, err)
	require.Equal(t, 0, n0, "first epoch must be 0")
	require.WithinRange(t, t0, before, time.Now().Add(time.Minute))

	n1, _, err := s.NextEpoch(ctx, "git", "cafef00d", 0, 3, 0)
	require.NoError(t, err)
	require.Equal(t, 1, n1, "second epoch must be 1")

	n2, _, err := s.NextEpoch(ctx, "pdf", "", 5, 0, 0)
	require.NoError(t, err)
	require.Equal(t, 2, n2)

	// Row persisted with its metadata.
	var (
		src, commit             string
		added, changed, removed int
	)
	require.NoError(t, s.pool.QueryRow(ctx,
		"SELECT source, git_commit, added, changed, removed FROM epoch WHERE n=$1", n0).
		Scan(&src, &commit, &added, &changed, &removed))
	require.Equal(t, "git", src)
	require.Equal(t, "deadbeef", commit)
	require.Equal(t, 10, added)
	require.Equal(t, 2, changed)
	require.Equal(t, 1, removed)

	var cnt int
	require.NoError(t, s.pool.QueryRow(ctx, "SELECT count(*) FROM epoch").Scan(&cnt))
	require.Equal(t, 3, cnt)
}
