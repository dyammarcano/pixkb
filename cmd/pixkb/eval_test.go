package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/evalkit"
)

// TestEvalFailUnderFlagWiring verifies every `pixkb eval` subcommand exposes
// an opt-in --fail-under flag defaulting to 0 (never fail) — the "unset ==
// unchanged behavior" contract from docs/BACKLOG.md's search-eval follow-up.
func TestEvalFailUnderFlagWiring(t *testing.T) {
	t.Parallel()
	root := NewRootCmd()
	for _, name := range []string{"multi", "similar", "ood", "explain", "asof", "rag-diversity"} {
		cmd, _, err := root.Find([]string{"eval", name})
		require.NoError(t, err, "Find eval %q", name)
		assert.Equal(t, name, cmd.Name())
		f := cmd.Flags().Lookup("fail-under")
		require.NotNil(t, f, "eval %s missing --fail-under flag", name)
		assert.Equal(t, "0", f.DefValue, "eval %s --fail-under default", name)
	}
}

// TestCheckFailUnder covers the shared gating helper directly: unset (<=0)
// never fails, an undefined metric (-1, meaning no cases ran) never fails
// even when a threshold is set, and a set threshold above the metric fails.
func TestCheckFailUnder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		metric    float64
		failUnder float64
		wantErr   bool
	}{
		{"unset flag never fails on bad metric", 10, 0, false},
		{"unset flag (negative) never fails", 10, -5, false},
		{"undefined metric never fails even with threshold", -1, 90, false},
		{"metric above threshold passes", 80, 50, false},
		{"metric equal to threshold passes", 50, 50, false},
		{"metric below threshold fails", 40, 50, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := checkFailUnder("label", tt.metric, tt.failUnder)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestPrintCoverageReport_Metric proves printCoverageReport (multi's
// headline) computes required-id coverage on a 0-100 scale, and reports -1
// (undefined) when there is nothing to measure.
func TestPrintCoverageReport_Metric(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	coverage, err := printCoverageReport(&buf, []evalkit.CoverageResult{
		{Case: evalkit.PairCase{Query: "q1"}, Found: 1, Total: 2},
		{Case: evalkit.PairCase{Query: "q2"}, Found: 1, Total: 2},
	})
	require.NoError(t, err)
	assert.InDelta(t, 50.0, coverage, 0.001)

	buf.Reset()
	coverage, err = printCoverageReport(&buf, nil)
	require.NoError(t, err)
	assert.Equal(t, -1.0, coverage)
}

// TestPrintRankReport_Metric proves printRankReport (similar's headline)
// reports top@5 on a 0-100 scale.
func TestPrintRankReport_Metric(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	top5, err := printRankReport(&buf, []evalkit.RankResult{
		{Label: "a", Rank: 1},
		{Label: "b", Rank: 4},
		{Label: "c", Rank: 0}, // not found
		{Label: "d", Rank: 20},
	})
	require.NoError(t, err)
	assert.InDelta(t, 50.0, top5, 0.001) // 2 of 4 within top@5

	buf.Reset()
	top5, err = printRankReport(&buf, nil)
	require.NoError(t, err)
	assert.Equal(t, -1.0, top5)
}

// TestPrintOODReport_Metric proves printOODReport reports the clean rate
// (non-leaked fraction) and preserves the original per-line output format.
func TestPrintOODReport_Metric(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	cleanRate := printOODReport(&buf, []evalkit.OODResult{
		{Query: "clean one"},
		{Query: "leaky one", Leaked: []string{"forbidden-id"}},
	})
	assert.InDelta(t, 50.0, cleanRate, 0.001)
	out := buf.String()
	assert.Contains(t, out, "clean clean one")
	assert.Contains(t, out, "LEAK  leaky one")
	assert.Contains(t, out, "cases=2  clean=1  leaked=1")

	buf.Reset()
	assert.Equal(t, -1.0, printOODReport(&buf, nil))
}

// TestPrintExplainReport_Metric proves printExplainReport reports the
// structural-consistency rate as the percentage of distinct cases (by query)
// with zero issues.
func TestPrintExplainReport_Metric(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	cases := []evalkit.PairCase{{Query: "q1"}, {Query: "q2"}}
	// Two issues both attributed to q1 (e.g. score mismatch + rank-order
	// violation on the same case) must still count as ONE faulty case.
	consistency := printExplainReport(&buf, cases, []evalkit.ExplainIssue{
		{Query: "q1", Detail: "d1"},
		{Query: "q1", Detail: "d2"},
	})
	assert.InDelta(t, 50.0, consistency, 0.001)

	buf.Reset()
	assert.Equal(t, -1.0, printExplainReport(&buf, nil, nil))
}

// TestPrintAsOfReport_Metric proves printAsOfReport reports the as-of
// invariant pass rate.
func TestPrintAsOfReport_Metric(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	cases := []evalkit.PairCase{{Query: "q1"}, {Query: "q2"}, {Query: "q3"}, {Query: "q4"}}
	passRate := printAsOfReport(&buf, cases, 7, []evalkit.AsOfIssue{{Query: "q1", Detail: "mismatch"}})
	assert.InDelta(t, 75.0, passRate, 0.001)
	assert.Contains(t, buf.String(), "latest-epoch=7")

	buf.Reset()
	assert.Equal(t, -1.0, printAsOfReport(&buf, nil, 0, nil))
}

// TestPrintRAGDiversityReport_Metric proves printRAGDiversityReport reports
// the min-types pass rate.
func TestPrintRAGDiversityReport_Metric(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	passRate := printRAGDiversityReport(&buf, []evalkit.DiversityResult{
		{ID: "c1", Types: []string{"api", "message"}, MinTypes: 2},
		{ID: "c2", Types: []string{"api"}, MinTypes: 2},
	})
	assert.InDelta(t, 50.0, passRate, 0.001)
	assert.Contains(t, buf.String(), "BELOW MIN")

	buf.Reset()
	assert.Equal(t, -1.0, printRAGDiversityReport(&buf, nil))
}
