package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelated_IncludesNeighbourType(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()

	_, err = s.pool.Exec(ctx, `
INSERT INTO concept (id, type, title, body, content_sha, first_epoch, last_epoch, updated_at) VALUES
  ('a.md', 'Reference',    'A', 'body', 'sha', 1, 1, now()),
  ('b.md', 'ApiEndpoint',  'B', 'body', 'sha', 1, 1, now())`)
	require.NoError(t, err)
	require.NoError(t, s.ReplaceEdges(ctx, "a.md", []string{"b.md"}))

	rel, err := s.Related(ctx, "a.md")
	require.NoError(t, err)
	require.Len(t, rel, 1)
	assert.Equal(t, "b.md", rel[0].ID)
	assert.Equal(t, "ApiEndpoint", rel[0].Type, "neighbour's type must be populated")
	assert.Equal(t, "out", rel[0].Direction)
}
