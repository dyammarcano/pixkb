package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pixkb/internal/okf"
)

func TestRecordFact_AppendsAndClosesPriorTx(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()
	truncateAll(t, s)

	id := "messages/pacs.008.md"
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	// First fact: epoch 0, open tx window.
	require.NoError(t, s.RecordFact(ctx,
		okf.Concept{ID: id, Type: "message", Title: "v1", ContentSHA: "sha-1", Epoch: 0}, t0, t0))

	// Exactly one open (current) tx window.
	var open int
	require.NoError(t, s.pool.QueryRow(ctx,
		"SELECT count(*) FROM concept_fact WHERE id=$1 AND upper_inf(tx)", id).Scan(&open))
	require.Equal(t, 1, open, "exactly one open tx window after first RecordFact")

	// Second fact: epoch 1 closes the prior open window and opens a new one.
	require.NoError(t, s.RecordFact(ctx,
		okf.Concept{ID: id, Type: "message", Title: "v2", ContentSHA: "sha-2", Epoch: 1}, t1, t1))

	// Still exactly one open tx window.
	require.NoError(t, s.pool.QueryRow(ctx,
		"SELECT count(*) FROM concept_fact WHERE id=$1 AND upper_inf(tx)", id).Scan(&open))
	require.Equal(t, 1, open, "only the latest fact may have an open tx window")

	// Total rows = 2.
	var total int
	require.NoError(t, s.pool.QueryRow(ctx,
		"SELECT count(*) FROM concept_fact WHERE id=$1", id).Scan(&total))
	require.Equal(t, 2, total, "two rows total for two RecordFact calls")

	// The open fact is the v2 row.
	var sha string
	require.NoError(t, s.pool.QueryRow(ctx,
		"SELECT content_sha FROM concept_fact WHERE id=$1 AND upper_inf(tx)", id).Scan(&sha))
	require.Equal(t, "sha-2", sha, "open row must be the latest fact")

	// Valid-time history: the prior row's valid range is closed at validFrom of
	// the second call (t1), so it is no longer "valid forever" (HIGH fix).
	var closedValidUpper time.Time
	require.NoError(t, s.pool.QueryRow(ctx,
		"SELECT upper(valid) FROM concept_fact WHERE id=$1 AND NOT upper_inf(tx)", id).Scan(&closedValidUpper))
	require.True(t, closedValidUpper.Equal(t1),
		"closed row valid upper must equal validFrom of second RecordFact; got %v want %v", closedValidUpper, t1)

	// Transaction-time is driven by the DB clock (clock_timestamp), not the
	// caller timestamp: the closed tx window must be non-empty and must not
	// overlap the new open window (EXCLUDE / empty-range safety).
	var closedTxUpper, openTxLower time.Time
	require.NoError(t, s.pool.QueryRow(ctx,
		"SELECT upper(tx) FROM concept_fact WHERE id=$1 AND NOT upper_inf(tx)", id).Scan(&closedTxUpper))
	require.NoError(t, s.pool.QueryRow(ctx,
		"SELECT lower(tx) FROM concept_fact WHERE id=$1 AND upper_inf(tx)", id).Scan(&openTxLower))
	require.True(t, closedTxUpper.After(t0), "closed tx upper must advance past first write")
	require.False(t, closedTxUpper.After(openTxLower),
		"closed tx upper must not overlap the new open window; closed=%v openLower=%v", closedTxUpper, openTxLower)

	// isCurrentTx predicate must also return exactly one row (mirrors FTS as-of logic).
	var currentCount int
	require.NoError(t, s.pool.QueryRow(ctx,
		"SELECT count(*) FROM concept_fact WHERE id=$1 AND "+currentTxPred("tx"), id).Scan(&currentCount))
	require.Equal(t, 1, currentCount, "isCurrentTx predicate must select exactly one row")
}
