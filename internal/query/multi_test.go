package query

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/embed"
	"pixkb/internal/store/postgres"
)

// queryAwareSearcher, unlike fakeSearcher in hybrid_test.go, returns different
// FTS hits depending on the query string — needed to prove MultiHybrid runs
// each expanded subquery independently through Hybrid.
type queryAwareSearcher struct {
	fts map[string][]postgres.Hit
}

func (f *queryAwareSearcher) FTS(_ context.Context, q string, _ postgres.Filter) ([]postgres.Hit, error) {
	return f.fts[q], nil
}
func (f *queryAwareSearcher) Vector(_ context.Context, _ []float32, _ postgres.Filter) ([]postgres.Hit, error) {
	return nil, nil
}

func TestMultiHybrid_HitFoundByMultipleSubqueriesRanksHigher(t *testing.T) {
	t.Parallel()
	q := "notificar via webhook pix"
	subqueries := ExpandQuery(q)
	require.Len(t, subqueries, 2, "expected original + the webhook entity subquery")

	s := &queryAwareSearcher{fts: map[string][]postgres.Hit{
		subqueries[0]: {{ID: "x", Title: "X"}, {ID: "y", Title: "Y"}},
		subqueries[1]: {{ID: "x", Title: "X"}},
	}}
	got, err := MultiHybrid(context.Background(), s, embed.NewHashing(8), q, postgres.Filter{})
	require.NoError(t, err)
	require.NotEmpty(t, got)

	assert.Equal(t, "x", got[0].ID, "hit surfaced by both subqueries must rank first")
	assert.GreaterOrEqual(t, len(got[0].Subqueries), 2, "x's provenance must list both subqueries")

	ids := map[string]bool{}
	for _, h := range got {
		ids[h.ID] = true
	}
	assert.True(t, ids["y"], "single-subquery hit still present")
}

func TestMultiHybrid_ProvenanceRecordsQueryAndArm(t *testing.T) {
	t.Parallel()
	q := "notificar via webhook pix"
	subqueries := ExpandQuery(q)
	require.Len(t, subqueries, 2)

	s := &queryAwareSearcher{fts: map[string][]postgres.Hit{
		subqueries[0]: {{ID: "x", Title: "X"}},
	}}
	got, err := MultiHybrid(context.Background(), s, embed.NewHashing(8), q, postgres.Filter{})
	require.NoError(t, err)
	require.NotEmpty(t, got)
	require.Len(t, got[0].Subqueries, 1)
	assert.Equal(t, subqueries[0], got[0].Subqueries[0].Query)
	assert.Equal(t, "fts", got[0].Subqueries[0].Arm)
	assert.Equal(t, 1, got[0].Subqueries[0].Rank)
}

func TestMultiHybrid_RespectsLimit(t *testing.T) {
	t.Parallel()
	q := "prazos de implementação"
	s := &queryAwareSearcher{fts: map[string][]postgres.Hit{
		q: {{ID: "a"}, {ID: "b"}, {ID: "c"}},
	}}
	got, err := MultiHybrid(context.Background(), s, embed.NewHashing(8), q, postgres.Filter{Limit: 2})
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestMultiHybrid_NoHits_ReturnsEmptyNotError(t *testing.T) {
	t.Parallel()
	s := &queryAwareSearcher{fts: map[string][]postgres.Hit{}}
	got, err := MultiHybrid(context.Background(), s, embed.NewHashing(8), "previsão do tempo amanhã", postgres.Filter{})
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestHits_StripsProvenance(t *testing.T) {
	t.Parallel()
	mh := []MultiHit{
		{Hit: postgres.Hit{ID: "a", Title: "A", Rank: 1}, Subqueries: []SubqueryMatch{{Query: "q", Arm: "fts", Rank: 1}}},
	}
	got := Hits(mh)
	require.Len(t, got, 1)
	assert.Equal(t, postgres.Hit{ID: "a", Title: "A", Rank: 1}, got[0])
}
