package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/pgvector/pgvector-go"
)

// UpsertEmbedding stores (or replaces on conflict) the vector for a concept
// at a given epoch, recording the embedding model, dimensionality, and the
// time it was computed.
func (s *Store) UpsertEmbedding(ctx context.Context, id string, epoch int, model string, vec []float32, at time.Time) error {
	const q = `
INSERT INTO embedding (id, epoch, embed_model, dim, vec, embedded_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (id, epoch) DO UPDATE SET
  embed_model = EXCLUDED.embed_model,
  dim         = EXCLUDED.dim,
  vec         = EXCLUDED.vec,
  embedded_at = EXCLUDED.embedded_at`
	_, err := s.pool.Exec(ctx, q, id, epoch, model, len(vec), pgvector.NewVector(vec), at)
	if err != nil {
		return fmt.Errorf("upsert embedding %q@%d: %w", id, epoch, err)
	}
	return nil
}
