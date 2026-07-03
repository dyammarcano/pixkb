package query_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/embed"
	"pixkb/internal/epoch"
	"pixkb/internal/ingest"
	"pixkb/internal/query"
	"pixkb/internal/store/postgres"
)

// TestIngestThenHybridSearch is the end-to-end capstone: gather the ISO 20022
// Pix message set, run a full ingest pass (bundle + index + embeddings), then
// hybrid-search and confirm the right concept surfaces.
func TestIngestThenHybridSearch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping end-to-end integration test in -short mode")
	}
	dsn := os.Getenv("PIXKB_TEST_DSN")
	if dsn == "" {
		t.Skip("PIXKB_TEST_DSN not set")
	}
	// Refuse to truncate the live KB: this test is destructive.
	if prod := os.Getenv("PIXKB_DSN"); prod != "" && prod == dsn {
		t.Fatal("PIXKB_TEST_DSN equals PIXKB_DSN (prod KB) — use a throwaway database")
	}
	ctx := context.Background()

	st, err := postgres.Open(ctx, dsn)
	require.NoError(t, err)
	defer st.Close()
	require.NoError(t, st.Truncate(ctx))

	emb := embed.NewHashing(256)
	bundle := t.TempDir()
	r := &epoch.Runner{Bundle: bundle, Store: st, Emb: emb, Git: epoch.NewGitCommitter(bundle)}

	concepts, err := ingest.GatherAll(ctx, []ingest.Source{ingest.NewISOSpecSource(ingest.DefaultMsgDefs())})
	require.NoError(t, err)
	res, err := r.Run(ctx, concepts, "test")
	require.NoError(t, err)
	assert.Equal(t, 9, res.Added)

	hits, err := query.Hybrid(ctx, st, emb, "credit transfer", postgres.Filter{Limit: 5})
	require.NoError(t, err)
	require.NotEmpty(t, hits)

	found := false
	for _, h := range hits {
		if h.ID == "messages/pacs.008.md" {
			found = true
		}
	}
	assert.True(t, found, "a 'credit transfer' query should surface pacs.008")

	// FTS-only and vector-only paths also work.
	ftsHits, err := st.FTS(ctx, "cancellation", postgres.Filter{})
	require.NoError(t, err)
	assert.NotEmpty(t, ftsHits)
}
