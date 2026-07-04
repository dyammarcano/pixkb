package similar

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/embed"
	"pixkb/internal/store/postgres"
)

func TestSimilar_UnknownModeErrors(t *testing.T) {
	t.Parallel()
	s := &queryAwareStore{}
	_, err := Similar(context.Background(), s, embed.NewHashing(8), t.TempDir(), "a.md", Options{Mode: "not-a-real-mode"})
	require.Error(t, err)
}

func TestSimilar_SemanticModeDelegates(t *testing.T) {
	t.Parallel()
	s := &fakeStore{
		embeddings: map[string][]float32{"a.md": {1, 0, 0}},
		vecResults: []postgres.Hit{{ID: "a.md"}, {ID: "b.md"}},
	}
	got, err := Similar(context.Background(), s, embed.NewHashing(8), t.TempDir(), "a.md", Options{Mode: "semantic"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "b.md", got[0].ID)
}

func TestSimilar_GraphModeDelegates(t *testing.T) {
	t.Parallel()
	s := &fakeStore{related: map[string][]postgres.RelatedConcept{
		"a.md": {{ID: "b.md", Direction: "out"}},
	}}
	got, err := Similar(context.Background(), s, embed.NewHashing(8), t.TempDir(), "a.md", Options{Mode: "graph"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "b.md", got[0].ID)
}

func TestSimilar_HybridMode_FusesSemanticAndMoreLikeThisAndTagsDomain(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestConcept(t, dir, "a.md", "Refund Concept", "estorno", "Pix refund details.")
	mltQuery := "Refund Concept estorno Pix refund details."

	s := &hybridFakeStore{
		embeddings: map[string][]float32{"a.md": {1, 0, 0}},
		vecResults: []postgres.Hit{{ID: "a.md"}, {ID: "endpoint.md", Type: "ApiEndpoint"}},
		fts:        map[string][]postgres.Hit{mltQuery: {{ID: "endpoint.md", Type: "ApiEndpoint"}}},
		related:    map[string][]postgres.RelatedConcept{"a.md": {{ID: "neighbour.md", Type: "Reference", Direction: "out"}}},
	}

	got, err := Similar(context.Background(), s, embed.NewHashing(8), dir, "a.md", Options{
		Mode: "hybrid", IncludeGraph: true, Filter: postgres.Filter{Limit: 10},
	})
	require.NoError(t, err)
	require.NotEmpty(t, got)

	byID := map[string]Hit{}
	for _, h := range got {
		byID[h.ID] = h
	}
	require.Contains(t, byID, "endpoint.md")
	// endpoint.md was found by BOTH semantic (Vector) and lexical (FTS via
	// MoreLikeThis) -> multiple Why entries, PLUS domain: writeTestConcept
	// always writes "type: Reference" in its frontmatter, and ApiEndpoint IS
	// domain-adjacent to Reference per domainAdjacency (domain.go).
	assert.Contains(t, byID["endpoint.md"].Why, SignalDomain)
	require.Contains(t, byID, "neighbour.md")
	assert.Contains(t, byID["neighbour.md"].Why, SignalGraph)
	assert.NotContains(t, byID, "a.md", "queried concept excluded from hybrid results too")
}

// hybridFakeStore combines fakeStore's embedding/vector/related behavior with
// queryAwareStore's query-string-keyed FTS, since hybrid mode exercises all
// three signal paths in one call.
type hybridFakeStore struct {
	embeddings map[string][]float32
	vecResults []postgres.Hit
	fts        map[string][]postgres.Hit
	related    map[string][]postgres.RelatedConcept
}

func (h *hybridFakeStore) FTS(_ context.Context, q string, _ postgres.Filter) ([]postgres.Hit, error) {
	return h.fts[q], nil
}
func (h *hybridFakeStore) Vector(_ context.Context, _ []float32, _ postgres.Filter) ([]postgres.Hit, error) {
	return h.vecResults, nil
}
func (h *hybridFakeStore) GetEmbedding(_ context.Context, id string) ([]float32, error) {
	return h.embeddings[id], nil
}
func (h *hybridFakeStore) Related(_ context.Context, id string) ([]postgres.RelatedConcept, error) {
	return h.related[id], nil
}

func TestSimilar_Deterministic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestConcept(t, dir, "a.md", "X", "", "body text")
	s := &hybridFakeStore{
		embeddings: map[string][]float32{"a.md": {1, 0, 0}},
		vecResults: []postgres.Hit{{ID: "a.md"}, {ID: "b.md"}},
		fts:        map[string][]postgres.Hit{"X body text": {{ID: "b.md"}}},
	}
	opts := Options{Mode: "hybrid", Filter: postgres.Filter{Limit: 10}}
	got1, err := Similar(context.Background(), s, embed.NewHashing(8), dir, "a.md", opts)
	require.NoError(t, err)
	got2, err := Similar(context.Background(), s, embed.NewHashing(8), dir, "a.md", opts)
	require.NoError(t, err)
	require.Equal(t, len(got1), len(got2))
	for i := range got1 {
		assert.Equal(t, got1[i].ID, got2[i].ID, "index %d", i)
		assert.Equal(t, got1[i].Rank, got2[i].Rank, "index %d", i)
	}
}
