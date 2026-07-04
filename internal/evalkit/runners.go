package evalkit

import (
	"context"
	"fmt"

	"pixkb/internal/embed"
	"pixkb/internal/query"
	"pixkb/internal/similar"
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

// RankResult is one case's outcome from a rank-based runner (similarity,
// explain-consistency): the best rank among acceptable ids, or 0 if none
// appeared within the requested limit.
type RankResult struct {
	Label string
	Rank  int
}

// RunSimilarFamily runs similar.Similar (unmodified — Feature 2's dispatch,
// not a reimplementation) for each case and reports the best rank among the
// case's acceptable neighbour ids — the "expected-neighbor test per major
// concept family" docs/SEARCH-CAPABILITY-SPEC.md Feature 6 asks for
// ("API endpoint, ISO message, reference concept, manual section").
func RunSimilarFamily(ctx context.Context, s similar.Store, emb embed.Embedder, bundleDir string, cases []SimilarCase, limit int) ([]RankResult, error) {
	out := make([]RankResult, 0, len(cases))
	for _, c := range cases {
		opts := similar.Options{Mode: c.Mode, IncludeGraph: true, Filter: postgres.Filter{Limit: limit}}
		hits, err := similar.Similar(ctx, s, emb, bundleDir, c.ConceptID, opts)
		if err != nil {
			return nil, fmt.Errorf("similar %q (%s): %w", c.ConceptID, c.Mode, err)
		}
		plain := make([]postgres.Hit, len(hits))
		for i, h := range hits {
			plain[i] = h.Hit
		}
		out = append(out, RankResult{Label: c.ConceptID, Rank: BestRank(plain, c.WantIDs)})
	}
	return out, nil
}

// OODResult is one out-of-domain case's outcome: which (if any) forbidden
// normative ids leaked into the result set for a query that should not match
// any of them.
type OODResult struct {
	Query  string
	Leaked []string
}

// RunOOD runs query.Hybrid (unmodified) for each out-of-domain query and
// checks that none of the forbidden ids (normally the union of every
// precise/fuzzy suite's expected ids — see ForbiddenIDs) leaked into the
// result. This is the "forbidden-id absence for out-of-domain or noisy
// cases" metric docs/SEARCH-CAPABILITY-SPEC.md Feature 6 names — it
// tolerates generic institutional filler in the results (verified live: OOD
// queries here return web/acessoinformacao-*.md noise, not empty results —
// the vector floor does not fully zero these out today) but treats a
// confidently-returned NORMATIVE Pix procedure as a real failure, per the
// Ranking Principles: "Treat out-of-domain silence as better than confident
// noise."
func RunOOD(ctx context.Context, s query.Searcher, emb embed.Embedder, queries []string, forbidden map[string]bool, limit int) ([]OODResult, error) {
	out := make([]OODResult, 0, len(queries))
	for _, q := range queries {
		hits, err := query.Hybrid(ctx, s, emb, q, postgres.Filter{Limit: limit})
		if err != nil {
			return nil, fmt.Errorf("hybrid %q: %w", q, err)
		}
		out = append(out, OODResult{Query: q, Leaked: ForbiddenPresent(hits, forbidden)})
	}
	return out, nil
}
