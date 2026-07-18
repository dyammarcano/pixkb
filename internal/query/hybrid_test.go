package query

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/embed"
	"pixkb/internal/store/postgres"
)

type fakeSearcher struct {
	fts []postgres.Hit
	vec []postgres.Hit
}

func (f *fakeSearcher) FTS(_ context.Context, _ string, _ postgres.Filter) ([]postgres.Hit, error) {
	return f.fts, nil
}
func (f *fakeSearcher) Vector(_ context.Context, _ []float32, _ postgres.Filter) ([]postgres.Hit, error) {
	return f.vec, nil
}

// emptyEmbedder returns no vectors — models an Embedder that yields fewer
// vectors than inputs (the interface permits it).
type emptyEmbedder struct{}

func (emptyEmbedder) Name() string                                         { return "empty" }
func (emptyEmbedder) Dim() int                                             { return 8 }
func (emptyEmbedder) Embed(context.Context, []string) ([][]float32, error) { return nil, nil }

// TestHybrid_EmptyEmbeddingErrors confirms an embedder returning no vector for
// the query yields an error rather than an index-out-of-range panic.
func TestHybrid_EmptyEmbeddingErrors(t *testing.T) {
	s := &fakeSearcher{fts: []postgres.Hit{{ID: "a.md", Score: 1}}}
	_, err := Hybrid(context.Background(), s, emptyEmbedder{}, "q", postgres.Filter{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no vector")
}

func TestHybrid_FusesAndHydrates(t *testing.T) {
	t.Parallel()
	s := &fakeSearcher{
		fts: []postgres.Hit{{ID: "a", Title: "Alpha"}, {ID: "b", Title: "Bravo"}},
		vec: []postgres.Hit{{ID: "b", Title: "Bravo", Score: 0.9}, {ID: "c", Title: "Charlie", Score: 0.8}},
	}
	got, err := Hybrid(context.Background(), s, embed.NewHashing(8), "q", postgres.Filter{})
	require.NoError(t, err)

	// b appears in both arms (rank 0+1) -> highest fused score -> first.
	require.NotEmpty(t, got)
	assert.Equal(t, "b", got[0].ID)
	assert.Equal(t, "Bravo", got[0].Title)
	assert.Equal(t, 1, got[0].Rank)

	ids := map[string]bool{}
	for _, h := range got {
		ids[h.ID] = true
	}
	assert.True(t, ids["a"] && ids["b"] && ids["c"], "all three ids present")
}

// TestHybrid_TitleBoostWinsOverNoisyFragment: two concepts tie on RRF rank, but
// one's title covers the query tokens and the other's is an unrelated OCR sample
// name. The title-intent match must rank first.
func TestHybrid_TitleBoostWinsOverNoisyFragment(t *testing.T) {
	t.Parallel()
	s := &fakeSearcher{
		// noisy fragment first in FTS (higher term-frequency), canonical second.
		fts: []postgres.Hit{
			{ID: "secao-73", Title: "FULANO DE TAL EIRELI", Type: "ManualSection"},
			{ID: "qr", Title: "QR Code Estático Pix (BR Code / EMV MPM)", Type: "Reference"},
		},
	}
	got, err := Hybrid(context.Background(), s, embed.NewHashing(8), "QR code estático BR EMV", postgres.Filter{})
	require.NoError(t, err)
	require.NotEmpty(t, got)
	assert.Equal(t, "qr", got[0].ID, "title-intent match must outrank the noisy fragment")
}

func TestTitleBoost(t *testing.T) {
	t.Parallel()
	// full coverage -> max boost; zero coverage -> none; accents/stopwords folded.
	assert.InDelta(t, 1.5, titleBoost("QR code estático", "QR Code Estático Pix"), 1e-9)
	assert.InDelta(t, 1.0, titleBoost("QR code estático", "FULANO DE TAL EIRELI"), 1e-9)
	assert.InDelta(t, 1.25, titleBoost("liquidação reservas", "Liquidação no SPI"), 1e-9) // 1 of 2 tokens
}

func TestHybrid_RespectsLimit(t *testing.T) {
	t.Parallel()
	s := &fakeSearcher{
		fts: []postgres.Hit{{ID: "a"}, {ID: "b"}, {ID: "c"}},
		vec: []postgres.Hit{{ID: "d", Score: 0.9}},
	}
	got, err := Hybrid(context.Background(), s, embed.NewHashing(8), "q", postgres.Filter{Limit: 2})
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

// TestHybrid_VectorFloorDropsOOD: an out-of-domain query yields no FTS hits and
// only near-zero-cosine vector hits; the floor must drop them so the result is
// empty (no unrelated-concept noise).
func TestHybrid_VectorFloorDropsOOD(t *testing.T) {
	t.Parallel()
	s := &fakeSearcher{
		fts: nil,
		vec: []postgres.Hit{{ID: "x", Score: 0.01}, {ID: "y", Score: 0.0}},
	}
	got, err := Hybrid(context.Background(), s, embed.NewHashing(8), "previsão do tempo", postgres.Filter{})
	require.NoError(t, err)
	assert.Empty(t, got, "sub-floor vector-only hits dropped -> empty result")
}

// TestHybrid_VectorFloorKeepsRealHit: a real vector hit above the floor survives
// even with no FTS hits (in-domain conceptual query).
func TestHybrid_VectorFloorKeepsRealHit(t *testing.T) {
	t.Parallel()
	s := &fakeSearcher{
		fts: nil,
		vec: []postgres.Hit{{ID: "good", Title: "Good", Score: 0.42}, {ID: "noise", Score: 0.02}},
	}
	got, err := Hybrid(context.Background(), s, embed.NewHashing(8), "q", postgres.Filter{})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "good", got[0].ID)
}

func TestHybridExplain_ParallelToHits(t *testing.T) {
	t.Parallel()
	s := &fakeSearcher{
		fts: []postgres.Hit{{ID: "a", Title: "Alpha"}, {ID: "b", Title: "Bravo"}},
		vec: []postgres.Hit{{ID: "b", Title: "Bravo", Score: 0.9}, {ID: "c", Title: "Charlie", Score: 0.8}},
	}
	hits, explains, err := HybridExplain(context.Background(), s, embed.NewHashing(8), "q", postgres.Filter{})
	require.NoError(t, err)
	require.Len(t, hits, 3)
	require.Len(t, explains, len(hits))

	for i := range hits {
		assert.Equal(t, hits[i].Arm, explains[i].Arm, "explains[%d].Arm must match hits[%d].Arm", i, i)
		assert.Equal(t, hits[i].Score, explains[i].FinalScore, "explains[%d].FinalScore must equal hits[%d].Score", i, i)
	}

	byID := map[string]int{}
	for i, h := range hits {
		byID[h.ID] = i
	}
	// b is in both arms -> both ranks populated (nonzero).
	bIdx := byID["b"]
	assert.Equal(t, "both", hits[bIdx].Arm)
	assert.Positive(t, explains[bIdx].FTSRank, "both-arm hit must have a nonzero FTS rank")
	assert.Positive(t, explains[bIdx].VecRank, "both-arm hit must have a nonzero vector rank")

	// a is FTS-only -> vector rank is the "not present" sentinel (0).
	aIdx := byID["a"]
	assert.Equal(t, "fts", hits[aIdx].Arm)
	assert.Positive(t, explains[aIdx].FTSRank, "fts-only hit must have a nonzero FTS rank")
	assert.Equal(t, 0, explains[aIdx].VecRank, "fts-only hit must have vector rank sentinel 0")
}

func TestHybrid_SetsScoreAndArm(t *testing.T) {
	t.Parallel()
	s := &fakeSearcher{
		fts: []postgres.Hit{{ID: "a", Title: "Alpha"}, {ID: "b", Title: "Bravo"}},
		vec: []postgres.Hit{{ID: "b", Title: "Bravo", Score: 0.9}, {ID: "c", Title: "Charlie", Score: 0.8}},
	}
	got, err := Hybrid(context.Background(), s, embed.NewHashing(8), "q", postgres.Filter{})
	require.NoError(t, err)
	require.Len(t, got, 3)

	byID := map[string]postgres.Hit{}
	for _, h := range got {
		byID[h.ID] = h
	}
	assert.Equal(t, "both", byID["b"].Arm, "b appears in both arms")
	assert.Equal(t, "fts", byID["a"].Arm, "a appears only in the FTS arm")
	assert.Equal(t, "vector", byID["c"].Arm, "c appears only in the vector arm")

	assert.Positive(t, byID["a"].Score, "fused score must be populated, not left at zero")
	assert.Positive(t, byID["b"].Score)
	assert.Positive(t, byID["c"].Score)
	// b is in both arms so its RRF contribution is strictly greater than either
	// single-arm hit's.
	assert.Greater(t, byID["b"].Score, byID["a"].Score)
	assert.Greater(t, byID["b"].Score, byID["c"].Score)
}
