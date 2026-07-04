package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGraphSparsity_FindsConceptsWithNoEdges(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()

	_, err = s.pool.Exec(ctx, `
INSERT INTO concept (id, type, title, body, content_sha, first_epoch, last_epoch, updated_at) VALUES
  ('linked-a.md',   'Reference',   'A', 'body', 'sha', 1, 1, now()),
  ('linked-b.md',   'Reference',   'B', 'body', 'sha', 1, 1, now()),
  ('isolated.md',   'ManualSection','I', 'body', 'sha', 1, 1, now())`)
	require.NoError(t, err)
	require.NoError(t, s.ReplaceEdges(ctx, "linked-a.md", []string{"linked-b.md"}))

	sparse, err := s.GraphSparsity(ctx)
	require.NoError(t, err)
	var ids []string
	for _, sc := range sparse {
		ids = append(ids, sc.ID)
	}
	assert.Contains(t, ids, "isolated.md", "concept with no edges must be flagged sparse")
	assert.NotContains(t, ids, "linked-a.md", "concept with an outgoing edge must not be flagged sparse")
	assert.NotContains(t, ids, "linked-b.md", "concept with only an incoming edge must not be flagged sparse")
}

// TestEmbeddingCoverage_CountsAndDetectsModelMix asserts on DELTAS, not
// absolute counts: this package's integration tests share one Postgres
// database with no truncation between tests (applyTestSchema only runs
// migrations), so other tests' concept/embedding rows are still present
// when this one runs. An absolute-count assertion is inherently flaky here;
// only "did OUR insert move the numbers by exactly what we expect" is safe.
func TestEmbeddingCoverage_CountsAndDetectsModelMix(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()

	before, err := s.EmbeddingCoverage(ctx)
	require.NoError(t, err)

	_, err = s.pool.Exec(ctx, `
INSERT INTO concept (id, type, title, body, content_sha, first_epoch, last_epoch, updated_at) VALUES
  ('embedded.md',   'Reference', 'E', 'body', 'sha', 1, 1, now()),
  ('unembedded.md', 'Reference', 'U', 'body', 'sha', 1, 1, now())`)
	require.NoError(t, err)
	// Unique per-run model name so this insert is guaranteed to be a
	// brand-new (model, dim) combination regardless of what other tests
	// left in this shared database.
	firstModel := fmt.Sprintf("test-model-a-%d", time.Now().UnixNano())
	require.NoError(t, s.UpsertEmbedding(ctx, "embedded.md", 1, firstModel, []float32{1, 0, 0}, time.Now()))

	cov, err := s.EmbeddingCoverage(ctx)
	require.NoError(t, err)
	assert.Equal(t, before.TotalConcepts+2, cov.TotalConcepts)
	assert.Equal(t, before.EmbeddedConcepts+1, cov.EmbeddedConcepts)
	assert.Equal(t, len(before.Models)+1, len(cov.Models), "one brand-new (model, dim) pair must grow the distinct-combinations count by exactly one")

	// A SECOND never-before-seen (model, dim) pair must grow the count by
	// one more, from THIS point (not the original baseline) — the
	// "model/dimension consistency" signal.
	secondModel := fmt.Sprintf("test-model-b-%d", time.Now().UnixNano())
	require.NoError(t, s.UpsertEmbedding(ctx, "unembedded.md", 1, secondModel, []float32{1, 0}, time.Now()))
	cov2, err := s.EmbeddingCoverage(ctx)
	require.NoError(t, err)
	assert.Equal(t, len(cov.Models)+1, len(cov2.Models), "a second brand-new (model, dim) pair must grow the distinct-combinations count by one more")
}
