package query

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/store/postgres"
)

// officialVecEmbedder returns one vector so hybridCore proceeds past the
// embed step (the vector arm returns nothing for these tests).
type officialVecEmbedder struct{}

func (officialVecEmbedder) Name() string { return "official-test" }
func (officialVecEmbedder) Dim() int     { return 8 }
func (officialVecEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return [][]float32{{1, 0, 0, 0, 0, 0, 0, 0}}, nil
}

// TestHybrid_OfficialBoost verifies the same hit scores exactly officialBoost×
// higher when it carries the trusted:official tag — rank-arithmetic-independent.
func TestHybrid_OfficialBoost(t *testing.T) {
	score := func(official bool) (float64, postgres.Hit, Explain) {
		s := &fakeSearcher{fts: []postgres.Hit{{ID: "x.md", Score: 1, Official: official}}}
		hits, ex, err := HybridExplain(context.Background(), s, officialVecEmbedder{}, "q", postgres.Filter{})
		require.NoError(t, err)
		require.Len(t, hits, 1)
		return hits[0].Score, hits[0], ex[0]
	}
	base, plain, plainEx := score(false)
	boosted, off, offEx := score(true)

	assert.InDelta(t, base*officialBoost, boosted, 1e-9, "official hit scored officialBoost×")
	assert.True(t, off.Official, "the Official flag propagates to the fused hit")
	assert.False(t, plain.Official)
	assert.InDelta(t, officialBoost, offEx.OfficialBoost, 1e-9, "explain records the boost")
	assert.InDelta(t, 1.0, plainEx.OfficialBoost, 1e-9, "non-official explain records no boost")
}
