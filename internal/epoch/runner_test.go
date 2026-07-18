package epoch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/embed"
	"pixkb/internal/okf"
	"pixkb/internal/store/postgres"
)

func testStore(t *testing.T) *postgres.Store {
	t.Helper()
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
	s, err := postgres.Open(ctx, dsn)
	require.NoError(t, err)
	require.NoError(t, s.Truncate(ctx))
	t.Cleanup(s.Close)
	return s
}

func mkConcept(id, title, body string) okf.Concept {
	return okf.Concept{
		ID: id, Type: "PacsMessage", Title: title, Body: body,
		ContentSHA: okf.ComputeSHA(body), Language: "en",
	}
}

func TestRunner_RunDiffReindex(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	bundle := t.TempDir()
	r := &Runner{Bundle: bundle, Store: st, Emb: embed.NewHashing(256), Git: NewGitCommitter(bundle)}

	c1 := mkConcept("messages/pacs.008.md", "Credit Transfer", "credit transfer body v1")
	c2 := mkConcept("messages/pacs.002.md", "Status Report", "payment status report body")

	res, err := r.Run(ctx, []okf.Concept{c1, c2}, "ingest")
	require.NoError(t, err)
	assert.Equal(t, 0, res.Epoch)
	assert.Equal(t, 2, res.Added)
	assert.Len(t, res.Commit, 40)

	// bundle files + indexes written
	_, err = os.Stat(filepath.Join(bundle, "messages", "pacs.008.md"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(bundle, "index.md"))
	require.NoError(t, err)

	// index is queryable
	hits, err := st.FTS(ctx, "transfer", postgres.Filter{})
	require.NoError(t, err)
	assert.NotEmpty(t, hits)

	// change c1 -> epoch 1, one changed
	c1b := mkConcept("messages/pacs.008.md", "Credit Transfer", "credit transfer body v2 CHANGED")
	res2, err := r.Run(ctx, []okf.Concept{c1b, c2}, "ingest")
	require.NoError(t, err)
	assert.Equal(t, 1, res2.Epoch)
	assert.Equal(t, 1, res2.Changed)

	d, err := r.Diff(ctx, 0, 1)
	require.NoError(t, err)
	assert.Contains(t, d.Changed, "messages/pacs.008.md")
	assert.NotContains(t, d.Changed, "messages/pacs.002.md")

	// reindex rebuilds from bundle
	require.NoError(t, r.Reindex(ctx))
	hits2, err := st.FTS(ctx, "status", postgres.Filter{})
	require.NoError(t, err)
	assert.NotEmpty(t, hits2)
}

// TestRunner_ConcurrentUpsertBatch confirms the write path is serialized: N
// goroutines each upserting one concept must all succeed with distinct epochs
// (no epoch PRIMARY KEY collision) and race-clean. Run with -race. DB-gated.
func TestRunner_ConcurrentUpsertBatch(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	bundle := t.TempDir()
	r := &Runner{Bundle: bundle, Store: st, Emb: embed.NewHashing(256), Git: NewGitCommitter(bundle)}

	const n = 8
	epochs := make([]int, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("messages/agent-%02d.md", i)
			c := mkConcept(id, "Agent Write", fmt.Sprintf("agent write-back body %d", i))
			res, err := r.UpsertBatch(ctx, []okf.Concept{c}, "agent")
			epochs[i], errs[i] = res.Epoch, err
		}(i)
	}
	wg.Wait()

	seen := map[int]bool{}
	for i := 0; i < n; i++ {
		require.NoErrorf(t, errs[i], "goroutine %d", i)
		require.Falsef(t, seen[epochs[i]], "epoch %d allocated twice — write path not serialized", epochs[i])
		seen[epochs[i]] = true
	}
}
