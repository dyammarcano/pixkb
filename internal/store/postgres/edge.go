package postgres

import (
	"context"
	"fmt"
)

// UpsertEdge idempotently inserts a single (src, dst, kind) edge. The edge
// table carries no unique constraint (it also holds multi-row link sets), so
// idempotency is enforced with an INSERT ... WHERE NOT EXISTS guard rather
// than ON CONFLICT: re-running the citation linker never duplicates a
// previously written edge. Returns true when a new row was inserted.
func (s *Store) UpsertEdge(ctx context.Context, src, dst, kind string) (bool, error) {
	const q = `
INSERT INTO edge (src, dst, kind)
SELECT $1, $2, $3
 WHERE NOT EXISTS (
   SELECT 1 FROM edge WHERE src = $1 AND dst = $2 AND kind = $3
 )`
	tag, err := s.pool.Exec(ctx, q, src, dst, kind)
	if err != nil {
		return false, fmt.Errorf("upsert edge %q->%q (%s): %w", src, dst, kind, err)
	}
	return tag.RowsAffected() > 0, nil
}
