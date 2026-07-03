package postgres

import (
	"context"
	"fmt"
)

// Truncate resets the entire derived index so reindex can replay the canonical
// bundle from scratch. concept_fact is included so bitemporal history is reset
// alongside the rest of the derived state.
func (s *Store) Truncate(ctx context.Context) error {
	const q = `TRUNCATE concept, embedding, epoch, edge, concept_fact RESTART IDENTITY CASCADE`
	if _, err := s.pool.Exec(ctx, q); err != nil {
		return fmt.Errorf("truncate index: %w", err)
	}
	return nil
}

// PruneEmbeddings deletes superseded embeddings, keeping only the latest epoch
// per concept id. Vector search already reads the latest per id, but the
// embedding table otherwise grows by ~one full set per epoch; the DISTINCT ON
// scan over that backlog is what makes hybrid search slow (and time out) as
// epochs accumulate. Pruning bounds the table to the current concept set.
func (s *Store) PruneEmbeddings(ctx context.Context) error {
	const q = `
DELETE FROM embedding e
 USING (SELECT id, max(epoch) AS m FROM embedding GROUP BY id) k
 WHERE e.id = k.id AND e.epoch < k.m`
	if _, err := s.pool.Exec(ctx, q); err != nil {
		return fmt.Errorf("prune embeddings: %w", err)
	}
	return nil
}

// SetEpochCommit backfills the git_commit SHA on an already-cut epoch row. The
// runner allocates the epoch (NextEpoch) before the git commit exists, then
// stamps the resulting SHA here so epoch.git_commit is never left empty.
func (s *Store) SetEpochCommit(ctx context.Context, n int, sha string) error {
	if _, err := s.pool.Exec(ctx,
		`UPDATE epoch SET git_commit = $2 WHERE n = $1`, n, sha); err != nil {
		return fmt.Errorf("set epoch %d commit: %w", n, err)
	}
	return nil
}
