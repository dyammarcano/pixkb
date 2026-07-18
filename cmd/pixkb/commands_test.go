package main

import (
	"bytes"
	"strings"
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

func TestBuildSources_IncludesOpenAPISpecsWhenConfigured(t *testing.T) {
	cfg := Config{OpenAPISpecs: []OpenAPISpecConf{{File: "mirror/openapi/x.json", Domain: "tax"}}}
	names := map[string]bool{}
	for _, s := range buildSources(cfg) {
		names[s.Name()] = true
	}
	assert.True(t, names["openapi"], "expected an openapi source when openapi_specs is set")
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

// TestFormatFlagWiring verifies every command that renders through
// internal/output exposes --format with the same name and "text" default
// (docs/BACKLOG.md's "CLI output formats"). The "text" default is the
// backwards-compat contract: an invocation with no --format keeps printing
// exactly what it printed before the flag existed.
func TestFormatFlagWiring(t *testing.T) {
	t.Parallel()
	for _, path := range [][]string{{"search"}, {"related"}, {"stats"}, {"ispb", "lookup"}} {
		t.Run(strings.Join(path, " "), func(t *testing.T) {
			t.Parallel()
			root := NewRootCmd()
			cmd, _, err := root.Find(path)
			require.NoError(t, err)

			format := cmd.Flags().Lookup("format")
			require.NotNilf(t, format, "%s missing --format flag", cmd.CommandPath())
			assert.Equal(t, "text", format.DefValue, "%s --format must default to text", cmd.CommandPath())
			assert.Equal(t, "output format: text|json|md|yaml", format.Usage)
		})
	}
}

// TestFormatFlag_UnknownValueErrors checks an unsupported --format is
// rejected. These commands all need a DSN, so the run fails before any
// rendering — the assertion is only that the flag parses and is accepted by
// cobra, with the format vocabulary itself covered by internal/output's tests.
func TestFormatFlag_UnknownValueErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PIXKB_CONFIG_DIR", t.TempDir())
	t.Setenv("PIXKB_DSN", "")

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"stats", "--format", "xml"})
	err := root.Execute()
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "unknown flag", "--format must be a registered flag on stats")
}

// TestSearchCmd_BadWhereFailsBeforeStore checks a malformed --where HQL
// predicate is rejected during parsing, before openStore is ever reached —
// no DSN/DB is needed for this test to pass.
func TestSearchCmd_BadWhereFailsBeforeStore(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PIXKB_CONFIG_DIR", t.TempDir())
	t.Setenv("PIXKB_DSN", "")

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"search", "x", "--where", "type = ="})
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse --where")
}
