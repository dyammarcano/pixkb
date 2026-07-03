package postgres

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmbeddedSchemaFiles(t *testing.T) {
	t.Parallel()

	entries, err := fs.ReadDir(SchemaFS, "schema")
	require.NoError(t, err)

	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[e.Name()] = true
	}
	assert.True(t, names["0001_init.up.sql"], "missing up migration")
	assert.True(t, names["0001_init.down.sql"], "missing down migration")

	up, err := fs.ReadFile(SchemaFS, "schema/0001_init.up.sql")
	require.NoError(t, err)
	upSQL := string(up)

	for _, want := range []string{
		"CREATE EXTENSION IF NOT EXISTS vector",
		"CREATE TABLE IF NOT EXISTS concept",
		"GENERATED ALWAYS AS",
		"USING GIN (fts)",
		"USING GIN (tags)",
		"CREATE TABLE IF NOT EXISTS epoch",
		"CREATE TABLE IF NOT EXISTS concept_fact",
		"EXCLUDE USING gist",
		"CREATE TABLE IF NOT EXISTS edge",
	} {
		assert.Truef(t, strings.Contains(upSQL, want), "up migration missing %q", want)
	}

	assert.Regexp(t, regexp.MustCompile(`vec\s+vector`), upSQL, "embedding.vec column should be untyped vector")

	down, err := fs.ReadFile(SchemaFS, "schema/0001_init.down.sql")
	require.NoError(t, err)
	downSQL := string(down)
	for _, want := range []string{
		"DROP TABLE IF EXISTS edge",
		"DROP TABLE IF EXISTS concept_fact",
		"DROP TABLE IF EXISTS epoch",
		"DROP TABLE IF EXISTS embedding",
		"DROP TABLE IF EXISTS concept",
	} {
		assert.Truef(t, strings.Contains(downSQL, want), "down migration missing %q", want)
	}
}

func TestMigration0002IntentTerms(t *testing.T) {
	t.Parallel()

	entries, err := fs.ReadDir(SchemaFS, "schema")
	require.NoError(t, err)
	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[e.Name()] = true
	}
	require.True(t, names["0002_intent_terms.up.sql"], "missing 0002 up migration")
	require.True(t, names["0002_intent_terms.down.sql"], "missing 0002 down migration")

	up, err := fs.ReadFile(SchemaFS, "schema/0002_intent_terms.up.sql")
	require.NoError(t, err)
	upSQL := string(up)
	for _, want := range []string{
		"ADD COLUMN IF NOT EXISTS intent_terms text",
		"DROP COLUMN IF EXISTS fts",                  // redefining a generated col needs drop+add
		"coalesce(intent_terms, '')",                 // intent_terms woven into the fts expression
		"GENERATED ALWAYS AS",
		"USING GIN (fts)",
	} {
		assert.Truef(t, strings.Contains(upSQL, want), "0002 up missing %q", want)
	}

	down, err := fs.ReadFile(SchemaFS, "schema/0002_intent_terms.down.sql")
	require.NoError(t, err)
	downSQL := string(down)
	// The down must restore the title+body-only fts and drop intent_terms.
	assert.Contains(t, downSQL, "DROP COLUMN IF EXISTS intent_terms")
	assert.Contains(t, downSQL, "GENERATED ALWAYS AS")
	assert.NotContains(t, downSQL, "intent_terms, ''", "down fts must not reference intent_terms")
}

func TestMigration0003PtStopwords(t *testing.T) {
	t.Parallel()

	entries, err := fs.ReadDir(SchemaFS, "schema")
	require.NoError(t, err)
	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[e.Name()] = true
	}
	require.True(t, names["0003_fts_pt_stopwords.up.sql"], "missing 0003 up migration")
	require.True(t, names["0003_fts_pt_stopwords.down.sql"], "missing 0003 down migration")

	up, err := fs.ReadFile(SchemaFS, "schema/0003_fts_pt_stopwords.up.sql")
	require.NoError(t, err)
	upSQL := string(up)
	for _, want := range []string{
		"TEMPLATE = pg_catalog.simple", // simple tokenizer (no stemmer)
		"STOPWORDS = portuguese",       // drop Portuguese stopwords
		"CONFIGURATION pixpt",
		"to_tsvector('pixpt'",          // generated fts uses the custom config
		"GENERATED ALWAYS AS",
	} {
		assert.Truef(t, strings.Contains(upSQL, want), "0003 up missing %q", want)
	}
	// Must NOT use the snowball 'portuguese' config (that stems and regressed).
	assert.NotContains(t, upSQL, "to_tsvector('portuguese'", "0003 must not stem with 'portuguese'")

	down, err := fs.ReadFile(SchemaFS, "schema/0003_fts_pt_stopwords.down.sql")
	require.NoError(t, err)
	downSQL := string(down)
	assert.Contains(t, downSQL, "to_tsvector('simple'", "down restores the 'simple' fts")
	assert.Contains(t, downSQL, "DROP TEXT SEARCH CONFIGURATION IF EXISTS pixpt")
	assert.Contains(t, downSQL, "DROP TEXT SEARCH DICTIONARY IF EXISTS pt_simple_nostem")
}
