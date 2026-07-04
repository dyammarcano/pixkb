package query

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseVocabulary_ParsesEmbeddedFile(t *testing.T) {
	t.Parallel()
	entries, err := parseVocabulary(vocabularyYAML)
	require.NoError(t, err)
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
