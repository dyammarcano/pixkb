package evalkit

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"pixkb/internal/store/postgres"
)

func TestCoverage_CountsPresentIDs(t *testing.T) {
	hits := []postgres.Hit{{ID: "a.md"}, {ID: "b.md"}}
	found, total := Coverage(hits, []string{"a.md", "z.md"})
	assert.Equal(t, 1, found)
	assert.Equal(t, 2, total)
}

func TestBestRank_ReturnsLowestMatchingRank(t *testing.T) {
	hits := []postgres.Hit{{ID: "a.md", Rank: 3}, {ID: "b.md", Rank: 1}}
	assert.Equal(t, 1, BestRank(hits, []string{"a.md", "b.md"}))
	assert.Equal(t, 0, BestRank(hits, []string{"z.md"}))
}

func TestForbiddenPresent_ReturnsOnlyForbiddenHits(t *testing.T) {
	hits := []postgres.Hit{{ID: "a.md"}, {ID: "b.md"}}
	forbidden := map[string]bool{"a.md": true}
	assert.Equal(t, []string{"a.md"}, ForbiddenPresent(hits, forbidden))
}
