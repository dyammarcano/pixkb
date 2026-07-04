package searchhealth_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/okf"
	"pixkb/internal/searchhealth"
	"pixkb/internal/store/postgres"
)

// testDSN mirrors internal/store/postgres's own DSN-gating (skip under
// -short or when PIXKB_TEST_DSN is unset; refuse to run against the
// production KB DSN). Duplicated here rather than imported because it's
// intentionally unexported in the postgres package — see
// internal/query/hybrid_integration_test.go for the same pattern used by
// another package that needs a real Store.
func testDSN(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping postgres integration test under -short")
	}
	dsn := os.Getenv("PIXKB_TEST_DSN")
	if dsn == "" {
		t.Skip("PIXKB_TEST_DSN not set; skipping postgres integration test")
	}
	if prod := os.Getenv("PIXKB_DSN"); prod != "" && prod == dsn {
		t.Fatal("PIXKB_TEST_DSN equals PIXKB_DSN (the production KB) — refusing " +
			"to run destructive integration tests against it. Point PIXKB_TEST_DSN " +
			"at a separate throwaway database.")
	}
	return dsn
}

// TestBuildReport_FlagsBundleDrift asserts on a DELTA, not an absolute
// count: this package's integration tests share one uncleaned Postgres
// database, so only OUR unique-per-run concept id is checked.
func TestBuildReport_FlagsBundleDrift(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	s, err := postgres.Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()

	dbOnlyID := fmt.Sprintf("db-only-%d.md", time.Now().UnixNano())
	require.NoError(t, s.UpsertConcept(ctx, okf.Concept{
		ID:         dbOnlyID,
		Type:       "Reference",
		Title:      "DB only concept",
		Body:       "body",
		ContentSHA: "sha",
		Epoch:      1,
		Timestamp:  time.Now(),
	}))

	// The "bundle" passed to BuildReport intentionally omits dbOnlyID,
	// simulating the exact drift scenario this signal exists to catch: a
	// concept that ended up in Postgres without ever being in the
	// canonical OKF bundle. casePaths is left empty so no real embedder is
	// needed for the eval-regression signal.
	rep, err := searchhealth.BuildReport(ctx, nil, s, nil)
	require.NoError(t, err)

	assert.Contains(t, rep.BundleDrift, dbOnlyID, "a concept in the DB but absent from the bundle must be flagged as drift")

	var sawSignal bool
	for _, r := range rep.Recommendations {
		if r.ConceptID != dbOnlyID {
			continue
		}
		for _, sig := range r.Signals {
			if sig.Kind == searchhealth.KindBundleDrift {
				sawSignal = true
			}
		}
	}
	assert.True(t, sawSignal, "the drifted concept must carry a bundle-drift signal in the recommendation list")
}
