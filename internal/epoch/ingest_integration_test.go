package epoch_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/embed"
	"pixkb/internal/epoch"
	"pixkb/internal/ingest"
	"pixkb/internal/okf"
	"pixkb/internal/store/postgres"
)

// memSource is an in-memory ingest.Source whose concept set can change between
// passes, used to drive a realistic gather -> Run -> Diff -> Reindex cycle.
type memSource struct {
	name string
	cs   []okf.Concept
}

func (s memSource) Name() string { return s.name }
func (s memSource) Fetch(_ context.Context) ([]okf.Concept, error) {
	return s.cs, nil
}

func memConcept(id, title, body string) okf.Concept {
	return okf.Concept{
		ID: id, Type: "PacsMessage", Title: title, Body: body,
		ContentSHA: okf.ComputeSHA(body), Language: "en",
	}
}

// TestIngestSource_RunDiffReindex is an end-to-end epoch integration test: it
// gathers from an in-memory source twice (with one concept changed between
// passes), asserts Diff reports the changed concept, then asserts Reindex
// rebuilds the index rows from the canonical bundle.
func TestIngestSource_RunDiffReindex(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping postgres integration test in -short mode")
	}
	dsn := os.Getenv("PIXKB_TEST_DSN")
	if dsn == "" {
		t.Skip("PIXKB_TEST_DSN not set")
	}
	// Refuse to truncate the live KB: these tests are destructive.
	if prod := os.Getenv("PIXKB_DSN"); prod != "" && prod == dsn {
		t.Fatal("PIXKB_TEST_DSN equals PIXKB_DSN (prod KB) — use a throwaway database")
	}
	ctx := context.Background()

	st, err := postgres.Open(ctx, dsn)
	require.NoError(t, err)
	defer st.Close()
	require.NoError(t, st.Truncate(ctx))

	bundle := t.TempDir()
	r := &epoch.Runner{Bundle: bundle, Store: st, Emb: embed.NewHashing(256), Git: epoch.NewGitCommitter(bundle)}

	// Pass 1 (epoch 0): two concepts gathered through ingest.GatherAll.
	src1 := memSource{name: "mem", cs: []okf.Concept{
		memConcept("messages/pacs.008.md", "Credit Transfer", "credit transfer body v1"),
		memConcept("messages/pacs.002.md", "Status Report", "payment status report body"),
	}}
	concepts1, err := ingest.GatherAll(ctx, []ingest.Source{src1})
	require.NoError(t, err)
	res1, err := r.Run(ctx, concepts1, "ingest")
	require.NoError(t, err)
	assert.Equal(t, 0, res1.Epoch)
	assert.Equal(t, 2, res1.Added)

	// Pass 2 (epoch 1): pacs.008 body changes, pacs.002 is unchanged.
	src2 := memSource{name: "mem", cs: []okf.Concept{
		memConcept("messages/pacs.008.md", "Credit Transfer", "credit transfer body v2 CHANGED"),
		memConcept("messages/pacs.002.md", "Status Report", "payment status report body"),
	}}
	concepts2, err := ingest.GatherAll(ctx, []ingest.Source{src2})
	require.NoError(t, err)
	res2, err := r.Run(ctx, concepts2, "ingest")
	require.NoError(t, err)
	assert.Equal(t, 1, res2.Epoch)
	assert.Equal(t, 1, res2.Changed)
	assert.Equal(t, 0, res2.Added)

	// Diff between the two epochs reports exactly the changed concept.
	d, err := r.Diff(ctx, 0, 1)
	require.NoError(t, err)
	assert.Contains(t, d.Changed, "messages/pacs.008.md")
	assert.NotContains(t, d.Changed, "messages/pacs.002.md")

	// Reindex truncates and rebuilds the index from the on-disk bundle.
	require.NoError(t, r.Reindex(ctx))
	_, err = os.Stat(filepath.Join(bundle, "messages", "pacs.008.md"))
	require.NoError(t, err)
	hits, err := st.FTS(ctx, "transfer", postgres.Filter{})
	require.NoError(t, err)
	assert.NotEmpty(t, hits, "reindex should rebuild searchable index rows from the bundle")
}
