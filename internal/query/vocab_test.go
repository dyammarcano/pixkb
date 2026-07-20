package query

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseVocabulary_ParsesEmbeddedFile(t *testing.T) {
	t.Parallel()
	entries := vocabularies["pix"]
	require.NotEmpty(t, entries)
	for _, e := range entries {
		assert.NotEmpty(t, e.Stems, "every entry must have at least one stem")
		assert.NotEmpty(t, e.Subquery, "every entry must have a subquery")
		assert.NotEmpty(t, e.Reason, "every entry must document a reason")
	}
}

func TestActiveVocabulary_FiltersDisabled(t *testing.T) {
	t.Parallel()
	entries := []VocabEntry{
		{Stems: []string{"a"}, Subquery: "A", Enabled: true, Reason: "r"},
		{Stems: []string{"b"}, Subquery: "B", Enabled: false, Reason: "r"},
	}
	active := activeVocabulary(entries)
	require.Len(t, active, 1)
	assert.Equal(t, "A", active[0].Subquery)
}

func TestVocabRegistry_LoadsPixDomain(t *testing.T) {
	t.Parallel()
	// The embedded registry must key the moved yaml under the "pix" domain.
	require.Contains(t, vocabularies, "pix")
	require.NotEmpty(t, vocabularies["pix"])
}

func TestVocabRegistry_PixIdenticalForEmptyAndPixSet(t *testing.T) {
	t.Parallel()
	// Hard regression guard: pix expansion must be identical whether the
	// active domain set is empty (merge all) or exactly ["pix"].
	all := VocabularyFor(nil)
	pixOnly := VocabularyFor([]string{"pix"})
	require.Equal(t, all, pixOnly, "pix is the only real domain: empty set == [pix]")
	require.Equal(t, vocabularies["pix"], pixOnly)
	// Vocabulary() (all-merge) must equal the pix-only view too, today.
	require.Equal(t, Vocabulary(), all)
}

func TestVocabRegistry_SelectNoLeakAcrossDomains(t *testing.T) {
	t.Parallel()
	reg := map[string][]VocabEntry{
		"pix": {
			{Stems: []string{"pix"}, Subquery: "PIX", Enabled: true, Reason: "r"},
		},
		"bacen": {
			{Stems: []string{"circ"}, Subquery: "CIRCULAR", Enabled: true, Reason: "r"},
		},
	}
	subqueries := func(entries []VocabEntry) []string {
		out := make([]string, 0, len(entries))
		for _, e := range entries {
			out = append(out, e.Subquery)
		}
		return out
	}

	// Empty set merges ALL domains (deterministic, domain-sorted order).
	assert.Equal(t, []string{"CIRCULAR", "PIX"}, subqueries(selectVocabulary(reg, nil)))
	// A ["pix"]-scoped selection must NOT leak the bacen term.
	assert.Equal(t, []string{"PIX"}, subqueries(selectVocabulary(reg, []string{"pix"})))
	assert.NotContains(t, subqueries(selectVocabulary(reg, []string{"pix"})), "CIRCULAR")
	// A ["bacen"]-scoped selection returns only bacen.
	assert.Equal(t, []string{"CIRCULAR"}, subqueries(selectVocabulary(reg, []string{"bacen"})))
	// Both explicitly = the union.
	assert.Equal(t, []string{"CIRCULAR", "PIX"}, subqueries(selectVocabulary(reg, []string{"pix", "bacen"})))
	// Unknown domain contributes nothing.
	assert.Empty(t, selectVocabulary(reg, []string{"nope"}))
}

func TestVocabulary_PacsCamtEntriesAreDisabledWithReason(t *testing.T) {
	t.Parallel()
	// Guards against silently flipping these back on without addressing the
	// documented regression history (commit 2e3b722).
	for _, want := range []string{"pacs.008", "pacs.004", "camt.054"} {
		found := false
		for _, e := range Vocabulary() {
			if !strings.Contains(e.Subquery, want) {
				continue
			}
			found = true
			assert.False(t, e.Enabled, "%s entry must stay disabled", want)
			assert.Contains(t, e.Reason, "2e3b722", "%s entry must cite the regression commit", want)
		}
		assert.True(t, found, "expected a vocabulary entry mentioning %s", want)
	}
}
