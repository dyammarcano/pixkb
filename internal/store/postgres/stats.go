package postgres

import (
	"context"
	"fmt"
)

// Stats is a snapshot of KB size and health, surfaced by `pixkb stats`.
type Stats struct {
	Concepts    int            // rows in concept (current index)
	Embeddings  int            // rows in embedding
	Epochs      int            // rows in epoch
	LatestEpoch int            // highest epoch number (-1 if none)
	ByType      map[string]int // concept count per type, descending
	TypeOrder   []string       // types sorted by count desc (stable display)
}

// Stats returns aggregate counts for the current index. It is read-only.
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var st Stats
	st.ByType = map[string]int{}

	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM concept").Scan(&st.Concepts); err != nil {
		return st, fmt.Errorf("count concepts: %w", err)
	}
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM embedding").Scan(&st.Embeddings); err != nil {
		return st, fmt.Errorf("count embeddings: %w", err)
	}
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM epoch").Scan(&st.Epochs); err != nil {
		return st, fmt.Errorf("count epochs: %w", err)
	}
	// COALESCE so an empty epoch table yields -1 rather than a NULL scan error.
	if err := s.pool.QueryRow(ctx, "SELECT COALESCE(max(n), -1) FROM epoch").Scan(&st.LatestEpoch); err != nil {
		return st, fmt.Errorf("latest epoch: %w", err)
	}

	rows, err := s.pool.Query(ctx,
		"SELECT type, count(*) FROM concept GROUP BY type ORDER BY count(*) DESC, type")
	if err != nil {
		return st, fmt.Errorf("count by type: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var typ string
		var n int
		if err := rows.Scan(&typ, &n); err != nil {
			return st, fmt.Errorf("scan type row: %w", err)
		}
		st.ByType[typ] = n
		st.TypeOrder = append(st.TypeOrder, typ)
	}
	if err := rows.Err(); err != nil {
		return st, fmt.Errorf("iterate type rows: %w", err)
	}
	return st, nil
}
