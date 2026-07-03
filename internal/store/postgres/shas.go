package postgres

import (
	"context"
	"fmt"
)

// CurrentSHAs returns the id -> content_sha map of every concept currently in
// the index (the latest written state). The epoch runner diffs this against the
// incoming concept set to compute added/changed/removed.
func (s *Store) CurrentSHAs(ctx context.Context) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, "SELECT id, content_sha FROM concept")
	if err != nil {
		return nil, fmt.Errorf("current shas query: %w", err)
	}
	defer func() { rows.Close() }()

	out := make(map[string]string)
	for rows.Next() {
		var id, sha string
		if err := rows.Scan(&id, &sha); err != nil {
			return nil, fmt.Errorf("scan sha row: %w", err)
		}
		out[id] = sha
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sha rows: %w", err)
	}
	return out, nil
}
