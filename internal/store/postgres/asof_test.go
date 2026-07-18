package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/okf"
)

func TestAsOf(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateAll(t, s)

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Seed two concepts with facts at different epochs (neither is updated, so
	// both keep open tx windows — matches the brief's intended test scenario).
	seedConcept(t, s, "messages/pacs.008.md", "message", "Credit Transfer", "credit body", []string{"pix"}, 0)
	seedConcept(t, s, "messages/pacs.004.md", "message", "Payment Return", "return body", []string{"pix"}, 5)

	require.NoError(t, s.RecordFact(ctx,
		okf.Concept{ID: "messages/pacs.008.md", Type: "message", Title: "Credit Transfer",
			ContentSHA: "sha-008", Epoch: 0}, t0, t0))
	require.NoError(t, s.RecordFact(ctx,
		okf.Concept{ID: "messages/pacs.004.md", Type: "message", Title: "Payment Return",
			ContentSHA: "sha-004", Epoch: 5}, t0, t0))

	t.Run("AsOfEpoch=0 returns only epoch-0 fact", func(t *testing.T) {
		ep := 0
		got, err := s.AsOf(ctx, Filter{AsOfEpoch: &ep})
		require.NoError(t, err)
		require.Len(t, got, 1, "only pacs.008 has epoch<=0")
		assert.Equal(t, "messages/pacs.008.md", got[0].ID)
		assert.Equal(t, "sha-008", got[0].ContentSHA)
		assert.Equal(t, 0, got[0].Epoch)
	})

	t.Run("AsOfEpoch=5 returns both facts", func(t *testing.T) {
		ep := 5
		got, err := s.AsOf(ctx, Filter{AsOfEpoch: &ep, Limit: 100})
		require.NoError(t, err)
		require.Len(t, got, 2, "both concepts have epoch<=5")
	})

	t.Run("AsOfEpoch=5 no duplicate rows per id", func(t *testing.T) {
		ep := 5
		got, err := s.AsOf(ctx, Filter{AsOfEpoch: &ep, Limit: 100})
		require.NoError(t, err)
		seen := map[string]int{}
		for _, c := range got {
			seen[c.ID]++
		}
		for cid, count := range seen {
			assert.Equal(t, 1, count, "duplicate rows for id %q", cid)
		}
	})

	t.Run("AsOfTime=now returns current state", func(t *testing.T) {
		now := time.Now().UTC()
		got, err := s.AsOf(ctx, Filter{AsOfTime: &now})
		require.NoError(t, err)
		// Both rows are valid at now() (valid=[t0, ∞)).
		require.Len(t, got, 2)
	})

	t.Run("AsOfTime=before t0 returns nothing", func(t *testing.T) {
		before := t0.Add(-time.Second)
		got, err := s.AsOf(ctx, Filter{AsOfTime: &before})
		require.NoError(t, err)
		assert.Len(t, got, 0, "no fact is valid before t0")
	})

	t.Run("Type filter narrows to message type", func(t *testing.T) {
		ep := 5
		got, err := s.AsOf(ctx, Filter{AsOfEpoch: &ep, Type: "message"})
		require.NoError(t, err)
		require.Len(t, got, 2)
		for _, c := range got {
			assert.Equal(t, "message", c.Type)
		}
	})

	t.Run("Tag filter narrows to pix tag", func(t *testing.T) {
		ep := 5
		got, err := s.AsOf(ctx, Filter{AsOfEpoch: &ep, Tag: "pix"})
		require.NoError(t, err)
		require.Len(t, got, 2)
	})

	t.Run("Limit=1 returns exactly one row", func(t *testing.T) {
		ep := 5
		got, err := s.AsOf(ctx, Filter{AsOfEpoch: &ep, Limit: 1})
		require.NoError(t, err)
		assert.Len(t, got, 1)
	})

	t.Run("default no AsOf returns all current rows", func(t *testing.T) {
		got, err := s.AsOf(ctx, Filter{})
		require.NoError(t, err)
		require.Len(t, got, 2)
	})

	// Historical AsOfEpoch snapshot after UPDATE: verify that AsOf{epoch:0}
	// returns the epoch-0 row even after a later RecordFact closes its tx.
	// This is the core bitemporal correctness test — previously failing.
	t.Run("AsOfEpoch historical snapshot survives tx close", func(t *testing.T) {
		// Add a second concept so we can confirm only the right id/epoch is returned.
		require.NoError(t, s.UpsertConcept(ctx, okf.Concept{
			ID: "messages/pacs.008.md", Type: "message", Title: "Credit Transfer v2",
			ContentSHA: "sha-008-v2", Tags: []string{"pix"}, Epoch: 1,
		}))
		t1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		// RecordFact epoch=1 closes the epoch-0 tx row for pacs.008.
		require.NoError(t, s.RecordFact(ctx,
			okf.Concept{ID: "messages/pacs.008.md", Type: "message", Title: "Credit Transfer v2",
				ContentSHA: "sha-008-v2", Epoch: 1}, t1, t1))

		// AsOf epoch=0: must still find pacs.008 with sha-008 (epoch-0 snapshot).
		ep0 := 0
		got0, err := s.AsOf(ctx, Filter{AsOfEpoch: &ep0})
		require.NoError(t, err)
		require.Len(t, got0, 1, "only pacs.008 had epoch<=0")
		assert.Equal(t, "messages/pacs.008.md", got0[0].ID)
		assert.Equal(t, "sha-008", got0[0].ContentSHA, "historical snapshot: epoch-0 content_sha")
		assert.Equal(t, 0, got0[0].Epoch)

		// AsOf epoch=1: pacs.008 returns the epoch-1 version.
		ep1 := 1
		got1, err := s.AsOf(ctx, Filter{AsOfEpoch: &ep1, Limit: 100})
		require.NoError(t, err)
		var pacs1 *okf.Concept
		for i := range got1 {
			if got1[i].ID == "messages/pacs.008.md" {
				pacs1 = &got1[i]
			}
		}
		require.NotNil(t, pacs1, "pacs.008 must appear in epoch<=1 result")
		assert.Equal(t, "sha-008-v2", pacs1.ContentSHA, "epoch-1 snapshot: updated content_sha")
		assert.Equal(t, 1, pacs1.Epoch)

		// No duplicate ids in either result.
		for _, got := range [][]okf.Concept{got0, got1} {
			seen := map[string]int{}
			for _, c := range got {
				seen[c.ID]++
			}
			for cid, count := range seen {
				assert.Equal(t, 1, count, "duplicate id %q in result", cid)
			}
		}
	})

	// Verify epoch-1 update scenario: RecordFact epoch=1 on pacs.008 closes
	// the epoch-0 tx; current filter returns epoch-1 row.
	t.Run("after epoch-1 update default returns epoch-1 row", func(t *testing.T) {
		t1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		require.NoError(t, s.UpsertConcept(ctx, okf.Concept{
			ID: "messages/pacs.008.md", Type: "message", Title: "Credit Transfer v2",
			ContentSHA: "sha-008-v2", Tags: []string{"pix"}, Epoch: 1,
		}))
		require.NoError(t, s.RecordFact(ctx,
			okf.Concept{ID: "messages/pacs.008.md", Type: "message", Title: "Credit Transfer v2",
				ContentSHA: "sha-008-v2", Epoch: 1}, t1, t1))

		got, err := s.AsOf(ctx, Filter{})
		require.NoError(t, err)

		var pacs okf.Concept
		for _, c := range got {
			if c.ID == "messages/pacs.008.md" {
				pacs = c
			}
		}
		assert.Equal(t, "sha-008-v2", pacs.ContentSHA, "current view reflects epoch-1 update")
		assert.Equal(t, 1, pacs.Epoch)

		// Only one current tx row for pacs.008.
		var open int
		require.NoError(t, s.pool.QueryRow(ctx,
			"SELECT count(*) FROM concept_fact WHERE id=$1 AND "+currentTxPred("tx"),
			"messages/pacs.008.md").Scan(&open))
		assert.Equal(t, 1, open, "only one open tx window after update")
	})
}
