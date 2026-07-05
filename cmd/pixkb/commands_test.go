package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSources_IncludesScoutCrawlWhenConfigured(t *testing.T) {
	t.Parallel()
	cfg := Config{ScoutCrawlDir: "mirrors/bcb/knowledge/pages"}
	srcs := buildSources(cfg)

	names := map[string]bool{}
	for _, s := range srcs {
		names[s.Name()] = true
	}
	assert.True(t, names["scout-crawl"], "expected scout-crawl source when ScoutCrawlDir is set")
}

func TestBuildSources_OmitsScoutCrawlWhenNotConfigured(t *testing.T) {
	t.Parallel()
	cfg := Config{}
	srcs := buildSources(cfg)

	for _, s := range srcs {
		assert.NotEqual(t, "scout-crawl", s.Name())
	}
}

// TestSearchCmd_RichFilterFlagsWiring verifies `pixkb search` registers the
// three rich-filter flags (docs/BACKLOG.md's "Include/exclude concept-id and
// concept-type list filters, and a minimum-vector-score filter" follow-up)
// with the documented zero-value defaults (unset == unchanged behavior).
func TestSearchCmd_RichFilterFlagsWiring(t *testing.T) {
	t.Parallel()
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"search"})
	require.NoError(t, err)
	assert.Equal(t, "search", cmd.Name())

	includeType := cmd.Flags().Lookup("include-type")
	require.NotNil(t, includeType, "search missing --include-type flag")
	assert.Equal(t, "[]", includeType.DefValue)

	excludeID := cmd.Flags().Lookup("exclude-id")
	require.NotNil(t, excludeID, "search missing --exclude-id flag")
	assert.Equal(t, "[]", excludeID.DefValue)

	minVectorScore := cmd.Flags().Lookup("min-vector-score")
	require.NotNil(t, minVectorScore, "search missing --min-vector-score flag")
	assert.Equal(t, "0", minVectorScore.DefValue)
}
