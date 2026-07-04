// Package similar implements concept-to-concept similarity search
// (docs/SEARCH-CAPABILITY-SPEC.md Feature 2): given a known concept id,
// surface nearby concepts using multiple independent signals — semantic
// (embedding similarity), lexical (shared terms via the existing hybrid
// search), graph (direct link-graph neighbours), and domain (type-pair
// adjacency) — each result tagged with which signal(s) found it. Every
// signal reuses an existing, unmodified retrieval primitive
// (postgres.Store.Vector, query.Hybrid, postgres.Store.Related); this
// package only adds self-exclusion, signal tagging, and cross-signal fusion.
package similar

import (
	"context"

	"pixkb/internal/query"
	"pixkb/internal/store/postgres"
)

// Signal names why a concept was surfaced as similar to the queried one.
const (
	SignalSemantic = "semantic"
	SignalLexical  = "lexical"
	SignalGraph    = "graph"
	SignalDomain   = "domain"
)

// Hit is one similarity result: the underlying concept plus which signal(s)
// surfaced it. Rank always reflects THIS package's own re-ranking after
// self-exclusion (or, for hybrid mode, the cross-signal fused rank) — never
// a raw Vector/Hybrid rank taken verbatim.
type Hit struct {
	postgres.Hit
	Why []string
}

// Store is the subset of *postgres.Store similar needs — query.Searcher
// (FTS, Vector) plus the two new accessors from Task 1. An interface so this
// package is unit-testable with a fake, matching internal/query.Searcher's
// pattern; *postgres.Store satisfies it directly.
type Store interface {
	query.Searcher
	GetEmbedding(ctx context.Context, id string) ([]float32, error)
	Related(ctx context.Context, id string) ([]postgres.RelatedConcept, error)
}

// defaultLimit mirrors postgres.defaultLimit / query.Hybrid's fallback.
const defaultLimit = 20

// withHeadroom returns a copy of f with Limit bumped by one extra slot — the
// queried concept itself is typically the #1 raw hit (cosine similarity ~1.0
// against its own embedding, or a top lexical match against its own title),
// so fetching one extra result before self-exclusion keeps the final count
// at the caller's requested Limit instead of silently returning one short.
func withHeadroom(f postgres.Filter) postgres.Filter {
	out := f
	if out.Limit <= 0 {
		out.Limit = defaultLimit
	}
	out.Limit++
	return out
}

// tagAndExclude drops excludeID from hits, tags every remaining hit with why,
// truncates to limit, and renumbers Rank 1..N over the surviving hits (never
// the raw pre-exclusion rank, which would have a gap where excludeID was).
func tagAndExclude(hits []postgres.Hit, excludeID, why string, limit int) []Hit {
	if limit <= 0 {
		limit = defaultLimit
	}
	out := make([]Hit, 0, len(hits))
	for _, h := range hits {
		if h.ID == excludeID {
			continue
		}
		out = append(out, Hit{Hit: h, Why: []string{why}})
		if len(out) >= limit {
			break
		}
	}
	for i := range out {
		out[i].Rank = i + 1
	}
	return out
}
