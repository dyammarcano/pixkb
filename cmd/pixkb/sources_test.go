package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func sourceNames(cfg Config) []string {
	var names []string
	for _, s := range buildSources(cfg) {
		names = append(names, s.Name())
	}
	return names
}

// TestBuildSources_AlwaysHasISOSpec confirms the ISO-20022 message set is always
// present, even for an empty config.
func TestBuildSources_AlwaysHasISOSpec(t *testing.T) {
	require.Equal(t, []string{"iso-spec"}, sourceNames(Config{}))
}

// TestBuildSources_WiresConfiguredSources confirms each configured input adds its
// source (and only its source) in the expected shape.
func TestBuildSources_WiresConfiguredSources(t *testing.T) {
	cfg := Config{
		PDFs:         []string{"a.pdf"},
		Markdown:     []string{"m.md"},
		Docx:         []string{"d.docx"},
		Xlsx:         []string{"x.xlsx"},
		APIDocs:      []string{"api.html"},
		OpenAPISpecs: []OpenAPISpecConf{{File: "spec.json", Domain: "tax"}},
		Legislation:  []LegislationConf{{File: "lc.pdf", Lei: "lc-214-2025", Domain: "tax"}},
	}
	names := sourceNames(cfg)
	require.Subset(t, names, []string{"iso-spec", "pdf", "markdown", "docx", "xlsx", "api-doc", "openapi", "legislation"})
	// No git/scout-crawl source when neither repos nor a crawl dir is configured.
	require.NotContains(t, names, "git")
	require.NotContains(t, names, "scout-crawl")
}

// TestBuildSources_ScoutCrawlOptIn confirms the scout-crawl source appears only
// when a crawl dir is set.
func TestBuildSources_ScoutCrawlOptIn(t *testing.T) {
	require.Contains(t, sourceNames(Config{ScoutCrawlDir: "crawl"}), "scout-crawl")
}
