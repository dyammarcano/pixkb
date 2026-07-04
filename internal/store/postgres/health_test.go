package postgres

import (
	"context"
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

func TestEmbeddingCoverage_CountsAndDetectsModelMix(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()

	_, err = s.pool.Exec(ctx, `
INSERT INTO concept (id, type, title, body, content_sha, first_epoch, last_epoch, updated_at) VALUES
  ('embedded.md',   'Reference', 'E', 'body', 'sha', 1, 1, now()),
  ('unembedded.md', 'Reference', 'U', 'body', 'sha', 1, 1, now())`)
	require.NoError(t, err)
	require.NoError(t, s.UpsertEmbedding(ctx, "embedded.md", 1, "hashing", []float32{1, 0, 0}, time.Now()))

	cov, err := s.EmbeddingCoverage(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, cov.TotalConcepts)
	assert.Equal(t, 1, cov.EmbeddedConcepts)
	assert.True(t, cov.Consistent(), "a single embed_model/dim combination must be reported consistent")

	require.NoError(t, s.UpsertEmbedding(ctx, "unembedded.md", 1, "other-model", []float32{1, 0}, time.Now()))
	cov2, err := s.EmbeddingCoverage(ctx)
	require.NoError(t, err)
	assert.False(t, cov2.Consistent(), "two distinct embed_model/dim combinations must be reported inconsistent")
}
