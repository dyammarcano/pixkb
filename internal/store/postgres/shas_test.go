package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/okf"
)

func TestCurrentSHAs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertConcept(ctx, okf.Concept{ID: "a.md", Type: "X", Body: "b1", ContentSHA: "sha-a", Epoch: 0}))
	require.NoError(t, s.UpsertConcept(ctx, okf.Concept{ID: "b.md", Type: "X", Body: "b2", ContentSHA: "sha-b", Epoch: 0}))

	got, err := s.CurrentSHAs(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"a.md": "sha-a", "b.md": "sha-b"}, got)
}
