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

// GetEmbedding fetches a concept's own latest stored embedding vector by id —
// the "what does THIS concept's vector look like" accessor UpsertEmbedding's
// write path has no counterpart for until now. Used by similar.SemanticSimilar
// to find a concept's nearest neighbours (as opposed to Vector, which embeds
// fresh query TEXT). Picks the latest epoch if a concept has been re-embedded.
func (s *Store) GetEmbedding(ctx context.Context, id string) ([]float32, error) {
	const q = `SELECT vec FROM embedding WHERE id = $1 ORDER BY epoch DESC LIMIT 1`
	var v pgvector.Vector
	if err := s.pool.QueryRow(ctx, q, id).Scan(&v); err != nil {
		return nil, fmt.Errorf("get embedding %q: %w", id, err)
	}
	return v.Slice(), nil
}
