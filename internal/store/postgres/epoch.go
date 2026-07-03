package postgres

import (
	"context"
	"fmt"
	"time"
)

// NextEpoch allocates the next epoch number (0 on an empty table, else
// max(n)+1), inserts the epoch row with its metadata, and returns the new
// number and creation timestamp. The INSERT ... SELECT computes n atomically
// against the current max — no separate SELECT is needed, and the RETURNING
// clause avoids any client-side clock skew.
//
// Concurrency note: two concurrent callers racing on an empty table will both
// compute coalesce(max(n)+1, 0) = 0 and one will fail the PRIMARY KEY
// constraint. The caller is responsible for serialising epoch creation (e.g.
// via a single ingest goroutine or an advisory lock). Retrying on conflict is
// intentionally left to the caller.
func (s *Store) NextEpoch(ctx context.Context, source, gitCommit string, added, changed, removed int) (n int, createdAt time.Time, err error) {
	const q = `
INSERT INTO epoch (n, created_at, source, git_commit, added, changed, removed)
SELECT coalesce(max(n) + 1, 0), now(), $1, $2, $3, $4, $5
FROM epoch
RETURNING n, created_at`

	if err = s.pool.QueryRow(ctx, q, source, gitCommit, added, changed, removed).
		Scan(&n, &createdAt); err != nil {
		return 0, time.Time{}, fmt.Errorf("next epoch: %w", err)
	}
	return n, createdAt, nil
}
