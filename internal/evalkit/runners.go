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

// ExplainIssue is one structural-consistency problem found in a --explain
// response: the rank order (Hit.Rank) must agree with the score order
// (Explain.FinalScore descending) — if rank 2 has a higher FinalScore than
// rank 1, the explanation is lying about why something ranked where it did,
// which is exactly what Feature 3 exists to prevent silently breaking.
type ExplainIssue struct {
	Query  string
	Detail string
}

// RunExplainConsistency runs query.HybridExplain (unmodified) for each case
// and checks two invariants that must always hold if the explanation is
// telling the truth about the ranking it describes: (1) FinalScore is
// non-increasing across the hits in rank order; (2) explains[i].FinalScore
// equals hits[i].Score for every i (the same invariant
// TestHybridExplain_MatchesHits already unit-tests with a fake — this reruns
// it against the live index as a "search explanation consistency" gate, per
// docs/SEARCH-CAPABILITY-SPEC.md Feature 6).
func RunExplainConsistency(ctx context.Context, s query.Searcher, emb embed.Embedder, cases []PairCase) ([]ExplainIssue, error) {
	var issues []ExplainIssue
	for _, c := range cases {
		hits, explains, err := query.HybridExplain(ctx, s, emb, c.Query, postgres.Filter{})
		if err != nil {
			return nil, fmt.Errorf("hybrid-explain %q: %w", c.Query, err)
		}
		if len(hits) != len(explains) {
			issues = append(issues, ExplainIssue{Query: c.Query, Detail: fmt.Sprintf("len(hits)=%d != len(explains)=%d", len(hits), len(explains))})
			continue
		}
		for i := range hits {
			if hits[i].Score != explains[i].FinalScore {
				issues = append(issues, ExplainIssue{Query: c.Query, Detail: fmt.Sprintf("hit[%d].Score=%v != explain[%d].FinalScore=%v", i, hits[i].Score, i, explains[i].FinalScore)})
			}
			if i > 0 && explains[i].FinalScore > explains[i-1].FinalScore {
				issues = append(issues, ExplainIssue{Query: c.Query, Detail: fmt.Sprintf("explain[%d].FinalScore=%v > explain[%d].FinalScore=%v (rank order violated)", i, explains[i].FinalScore, i-1, explains[i-1].FinalScore)})
			}
		}
	}
	return issues, nil
}

// AsOfIssue is one as-of-filtering invariant violation: querying at the
// current latest epoch must return exactly the same result as an unfiltered
// query, since "as of the latest state" and "no time-travel filter" describe
// the same state by construction. This is the deterministic gate
// docs/SEARCH-CAPABILITY-SPEC.md Feature 4's own acceptance criterion asks
// for ("As-of filtering is test-covered at the public surface") — reusing
// the live index instead of authoring historical fixtures (a concept's
// epoch history is environment-specific and would make a hardcoded
// before/after fixture fragile across KB instances).
type AsOfIssue struct {
	Query  string
	Detail string
}

// RunAsOfInvariant runs query.Hybrid (unmodified) twice per case — once
// unfiltered, once with Filter.AsOfEpoch pinned to the current latest
// epoch — and checks the two id sequences are identical.
func RunAsOfInvariant(ctx context.Context, s query.Searcher, emb embed.Embedder, cases []PairCase, latestEpoch int) ([]AsOfIssue, error) {
	var issues []AsOfIssue
	for _, c := range cases {
		unfiltered, err := query.Hybrid(ctx, s, emb, c.Query, postgres.Filter{})
		if err != nil {
			return nil, fmt.Errorf("hybrid %q: %w", c.Query, err)
		}
		epoch := latestEpoch
		asOf, err := query.Hybrid(ctx, s, emb, c.Query, postgres.Filter{AsOfEpoch: &epoch})
		if err != nil {
			return nil, fmt.Errorf("hybrid --as-of-epoch %d %q: %w", epoch, c.Query, err)
		}
		if len(unfiltered) != len(asOf) {
			issues = append(issues, AsOfIssue{Query: c.Query, Detail: fmt.Sprintf("len(unfiltered)=%d != len(as-of)=%d", len(unfiltered), len(asOf))})
			continue
		}
		for i := range unfiltered {
			if unfiltered[i].ID != asOf[i].ID {
				issues = append(issues, AsOfIssue{Query: c.Query, Detail: fmt.Sprintf("position %d: unfiltered=%s as-of=%s", i, unfiltered[i].ID, asOf[i].ID)})
				break
			}
		}
	}
	return issues, nil
}
