package similar

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/store/postgres"
)

// fakeStore is a minimal in-memory Store double for pure unit tests — no DB.
type fakeStore struct {
	embeddings map[string][]float32
	vecResults []postgres.Hit
	related    map[string][]postgres.RelatedConcept
}

func (f *fakeStore) FTS(_ context.Context, _ string, _ postgres.Filter) ([]postgres.Hit, error) {
	return nil, nil
}
func (f *fakeStore) Vector(_ context.Context, _ []float32, _ postgres.Filter) ([]postgres.Hit, error) {
	return f.vecResults, nil
}
func (f *fakeStore) GetEmbedding(_ context.Context, id string) ([]float32, error) {
	if v, ok := f.embeddings[id]; ok {
		return v, nil
	}
	return nil, errors.New("not found")
}
func (f *fakeStore) Related(_ context.Context, id string) ([]postgres.RelatedConcept, error) {
	return f.related[id], nil
}

func TestSemanticSimilar_ExcludesSelfAndTagsSignal(t *testing.T) {
	t.Parallel()
	s := &fakeStore{
		embeddings: map[string][]float32{"a.md": {1, 0, 0}},
		vecResults: []postgres.Hit{
			{ID: "a.md", Title: "A", Rank: 1}, // the queried concept itself — cosine 1.0, must be excluded
			{ID: "b.md", Title: "B", Rank: 2},
			{ID: "c.md", Title: "C", Rank: 3},
		},
	}
	got, err := SemanticSimilar(context.Background(), s, "a.md", postgres.Filter{Limit: 2})
	require.NoError(t, err)
	require.Len(t, got, 2, "self excluded, 2 of the remaining returned")
	assert.Equal(t, "b.md", got[0].ID)
	assert.Equal(t, 1, got[0].Rank, "rank renumbered after exclusion")
	assert.Equal(t, []string{SignalSemantic}, got[0].Why)
	assert.Equal(t, "c.md", got[1].ID)
	assert.Equal(t, 2, got[1].Rank)
}

func TestSemanticSimilar_PropagatesGetEmbeddingError(t *testing.T) {
	t.Parallel()
	s := &fakeStore{embeddings: map[string][]float32{}}
	_, err := SemanticSimilar(context.Background(), s, "missing.md", postgres.Filter{})
	require.Error(t, err)
}

func TestGraphSimilar_TagsSignalAndExcludesSelf(t *testing.T) {
	t.Parallel()
	s := &fakeStore{related: map[string][]postgres.RelatedConcept{
		"a.md": {
			{ID: "a.md", Title: "A (self-loop, must be excluded)", Direction: "out"},
			{ID: "b.md", Title: "B", Type: "ApiEndpoint", Direction: "out"},
		},
	}}
	got, err := GraphSimilar(context.Background(), s, "a.md", 20)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "b.md", got[0].ID)
	assert.Equal(t, "ApiEndpoint", got[0].Type)
	assert.Equal(t, []string{SignalGraph}, got[0].Why)
}

func TestGraphSimilar_RespectsLimit(t *testing.T) {
	t.Parallel()
	s := &fakeStore{related: map[string][]postgres.RelatedConcept{
		"a.md": {
			{ID: "b.md", Direction: "out"},
			{ID: "c.md", Direction: "out"},
			{ID: "d.md", Direction: "out"},
		},
	}}
	got, err := GraphSimilar(context.Background(), s, "a.md", 2)
	require.NoError(t, err)
	assert.Len(t, got, 2)
}
