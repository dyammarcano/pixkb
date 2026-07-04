package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/econindex"
)

func truncateEconIndex(t *testing.T, s *Store) {
	t.Helper()
	_, err := s.pool.Exec(context.Background(), "TRUNCATE econindex_series_point")
	require.NoError(t, err)
}

func TestUpsertSeriesPoint_ThenGetLatest(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateEconIndex(t, s)

	synced := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	p1 := econindex.SeriesPoint{SeriesCode: "11", Date: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), Value: "0.05", SyncedAt: synced}
	p2 := econindex.SeriesPoint{SeriesCode: "11", Date: time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC), Value: "0.06", SyncedAt: synced}
	require.NoError(t, s.UpsertSeriesPoint(ctx, p1))
	require.NoError(t, s.UpsertSeriesPoint(ctx, p2))

	got, err := s.GetLatestSeriesPoint(ctx, "11")
	require.NoError(t, err)
	assert.True(t, got.Date.Equal(p2.Date))
	assert.Equal(t, "0.06", got.Value)
}

func TestUpsertSeriesPoint_OverwritesOnConflict(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateEconIndex(t, s)

	date := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	require.NoError(t, s.UpsertSeriesPoint(ctx, econindex.SeriesPoint{SeriesCode: "1", Date: date, Value: "5.10", SyncedAt: time.Now()}))
	require.NoError(t, s.UpsertSeriesPoint(ctx, econindex.SeriesPoint{SeriesCode: "1", Date: date, Value: "5.19", SyncedAt: time.Now()}))

	got, err := s.GetLatestSeriesPoint(ctx, "1")
	require.NoError(t, err)
	assert.Equal(t, "5.19", got.Value, "later upsert of the same date must win")

	n, err := s.CountSeriesPoints(ctx, "1")
	require.NoError(t, err)
	assert.Equal(t, 1, n, "same (series_code, point_date) must not duplicate rows")
}

func TestUpsertSeriesPoints_Batch(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateEconIndex(t, s)

	pts := []econindex.SeriesPoint{
		{SeriesCode: "432", Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Value: "14.25", SyncedAt: time.Now()},
		{SeriesCode: "432", Date: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), Value: "14.25", SyncedAt: time.Now()},
	}
	require.NoError(t, s.UpsertSeriesPoints(ctx, pts))

	n, err := s.CountSeriesPoints(ctx, "432")
	require.NoError(t, err)
	assert.Equal(t, 2, n)
}

func TestGetSeriesRange(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateEconIndex(t, s)

	for _, d := range []int{1, 2, 3, 15} {
		require.NoError(t, s.UpsertSeriesPoint(ctx, econindex.SeriesPoint{
			SeriesCode: "11", Date: time.Date(2026, 7, d, 0, 0, 0, 0, time.UTC), Value: "0.05", SyncedAt: time.Now(),
		}))
	}

	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	list, err := s.GetSeriesRange(ctx, "11", from, to)
	require.NoError(t, err)
	require.Len(t, list, 3, "the 15th falls outside the range")
	assert.True(t, list[0].Date.Equal(from))
}

func TestGetLatestSeriesPoint_NotFound(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateEconIndex(t, s)

	_, err = s.GetLatestSeriesPoint(ctx, "999")
	require.Error(t, err)
	assert.True(t, errors.Is(err, pgx.ErrNoRows))
}
