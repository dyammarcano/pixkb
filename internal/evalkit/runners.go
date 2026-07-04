package evalkit

import (
	"context"
	"fmt"

	"pixkb/internal/embed"
	"pixkb/internal/query"
	"pixkb/internal/store/postgres"
)

// CoverageResult is one case's outcome from RunMultiCoverage: how many of the
// case's required ids the fused multi-query result set actually covered.
type CoverageResult struct {
	Case  PairCase
	Found int
	Total int
}

// RunMultiCoverage runs query.MultiHybrid (unmodified — Feature 1's fusion,
// not a reimplementation) for each case and measures required-id coverage:
// did the fused result set surface evidence for EVERY intent in the
// combined query, not just the best-ranked one. Reports a number per case;
// does not fail on a low score (see plan's Global Constraints — this is a
// measurement tool, like eval/tophit.sh, not a CI gate).
func RunMultiCoverage(ctx context.Context, s query.Searcher, emb embed.Embedder, cases []PairCase, limit int) ([]CoverageResult, error) {
	out := make([]CoverageResult, 0, len(cases))
	for _, c := range cases {
		f := postgres.Filter{Limit: limit}
		mh, err := query.MultiHybrid(ctx, s, emb, c.Query, f)
		if err != nil {
			return nil, fmt.Errorf("multi-hybrid %q: %w", c.Query, err)
		}
		found, total := Coverage(query.Hits(mh), c.WantIDs)
		out = append(out, CoverageResult{Case: c, Found: found, Total: total})
	}
	return out, nil
}
