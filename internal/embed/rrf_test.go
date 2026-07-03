package embed

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRRF_FusesAndSorts(t *testing.T) {
	fts := []string{"a", "b", "c"}
	vec := []string{"b", "a", "d"}
	got := RRF([][]string{fts, vec}, 60)
	// "a": 1/61 + 1/62 ; "b": 1/62 + 1/61 (== a) ; tie broken by first appearance order
	// "b" appears at rank0 in vec and rank1 in fts; "a" rank0 fts, rank1 vec.
	// Both share the same fused score; deterministic order required.
	require.Len(t, got, 4)
	assert.ElementsMatch(t, []string{"a", "b", "c", "d"}, got)
	// a and b outrank c and d (single-list-only items)
	assert.Contains(t, got[:2], "a")
	assert.Contains(t, got[:2], "b")
}

func TestRRF_HigherRankWins(t *testing.T) {
	l1 := []string{"x", "y"}
	l2 := []string{"x", "z"}
	got := RRF([][]string{l1, l2}, 60)
	// x is rank0 in both => clearly first
	require.NotEmpty(t, got)
	assert.Equal(t, "x", got[0])
}

func TestRRF_DefaultsKWhenNonPositive(t *testing.T) {
	got := RRF([][]string{{"a", "b"}}, 0)
	assert.Equal(t, []string{"a", "b"}, got)
}

func TestRRF_EmptyInput(t *testing.T) {
	assert.Empty(t, RRF(nil, 60))
	assert.Empty(t, RRF([][]string{}, 60))
	assert.Empty(t, RRF([][]string{{}, {}}, 60))
}

func TestRRF_DeterministicTieBreak(t *testing.T) {
	a := RRF([][]string{{"p", "q"}, {"q", "p"}}, 60)
	b := RRF([][]string{{"p", "q"}, {"q", "p"}}, 60)
	assert.Equal(t, a, b)
}
