package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"pixkb/internal/econindex"
)

// UpsertSeriesPoint inserts or updates a single econindex series point.
func (s *Store) UpsertSeriesPoint(ctx context.Context, p econindex.SeriesPoint) error {
	if err := p.Validate(); err != nil {
		return err
	}
	const q = `
INSERT INTO econindex_series_point (series_code, point_date, value_text, synced_at, updated_at)
VALUES ($1,$2,$3,$4, now())
ON CONFLICT (series_code, point_date) DO UPDATE SET
  value_text = EXCLUDED.value_text,
  synced_at  = EXCLUDED.synced_at,
  updated_at = now()`
	_, err := s.pool.Exec(ctx, q, p.SeriesCode, p.Date, p.Value, p.SyncedAt)
	if err != nil {
		return fmt.Errorf("upsert econindex point %s/%s: %w", p.SeriesCode, p.Date.Format("2006-01-02"), err)
	}
	return nil
}

// UpsertSeriesPoints upserts a batch of econindex series points, stopping at
// the first error.
func (s *Store) UpsertSeriesPoints(ctx context.Context, points []econindex.SeriesPoint) error {
	for _, p := range points {
		if err := s.UpsertSeriesPoint(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

func scanSeriesPoint(row pgx.Row) (econindex.SeriesPoint, error) {
	var p econindex.SeriesPoint
	err := row.Scan(&p.SeriesCode, &p.Date, &p.Value, &p.SyncedAt)
	return p, err
}

// GetLatestSeriesPoint returns the most recent point stored for seriesCode.
func (s *Store) GetLatestSeriesPoint(ctx context.Context, seriesCode string) (econindex.SeriesPoint, error) {
	const q = `SELECT series_code, point_date, value_text, synced_at
FROM econindex_series_point WHERE series_code = $1 ORDER BY point_date DESC LIMIT 1`
	row := s.pool.QueryRow(ctx, q, seriesCode)
	p, err := scanSeriesPoint(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return econindex.SeriesPoint{}, fmt.Errorf("series %s: %w", seriesCode, pgx.ErrNoRows)
	}
	if err != nil {
		return econindex.SeriesPoint{}, fmt.Errorf("get latest econindex point %s: %w", seriesCode, err)
	}
	return p, nil
}

// GetSeriesRange returns every stored point for seriesCode between from and
// to (inclusive), ordered by date ascending.
func (s *Store) GetSeriesRange(ctx context.Context, seriesCode string, from, to time.Time) ([]econindex.SeriesPoint, error) {
	const q = `SELECT series_code, point_date, value_text, synced_at
FROM econindex_series_point WHERE series_code = $1 AND point_date BETWEEN $2 AND $3 ORDER BY point_date`
	rows, err := s.pool.Query(ctx, q, seriesCode, from, to)
	if err != nil {
		return nil, fmt.Errorf("get econindex range %s: %w", seriesCode, err)
	}
	defer rows.Close()
	var out []econindex.SeriesPoint
	for rows.Next() {
		p, err := scanSeriesPoint(rows)
		if err != nil {
			return nil, fmt.Errorf("scan econindex row: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate econindex rows: %w", err)
	}
	return out, nil
}

// CountSeriesPoints returns the number of stored points for seriesCode.
func (s *Store) CountSeriesPoints(ctx context.Context, seriesCode string) (int, error) {
	var n int
	const q = `SELECT count(*) FROM econindex_series_point WHERE series_code = $1`
	if err := s.pool.QueryRow(ctx, q, seriesCode).Scan(&n); err != nil {
		return 0, fmt.Errorf("count econindex points %s: %w", seriesCode, err)
	}
	return n, nil
}
