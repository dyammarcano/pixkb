package query

import (
	"context"
	"sort"

	"pixkb/internal/embed"
	"pixkb/internal/store/postgres"
)

// SubqueryMatch records that one expanded subquery's own Hybrid ranking
// surfaced a hit — the "which subquery matched / which arm / per-query rank"
// provenance docs/SEARCH-CAPABILITY-SPEC.md Feature 1 requires.
type SubqueryMatch struct {
	Query string
	Arm   string
	Rank  int
}

// MultiHit is a fused multi-query search hit: the final cross-subquery rank
// (embedded via Hit.Rank) plus the trail of subqueries that found it.
type MultiHit struct {
	postgres.Hit
	Subqueries []SubqueryMatch
}

// Hits strips provenance, returning plain hits for callers (CLI, MCP) that
// only need the id/title/type/rank shape shared with plain Hybrid results.
func Hits(mh []MultiHit) []postgres.Hit {
	out := make([]postgres.Hit, 0, len(mh))
	for _, m := range mh {
		out = append(out, m.Hit)
	}
	return out
}

// multiSubqueryLimit is the per-subquery result cap MultiHybrid requests
// internally, independent of the caller's final Filter.Limit — cross-subquery
// fusion needs headroom beyond the final page size, or a hit that ranks #3 in
// one subquery and #1 in another could be truncated before fusion ever sees
// it. The caller's Filter.Limit still governs the final, fused output size.
const multiSubqueryLimit = 20

// multiRRFK is MultiHybrid's OWN reciprocal-rank-fusion constant, deliberately
// decoupled from Hybrid's shared rrfK (hybrid.go) — ADR 0002 forbids
// re-tuning Hybrid's own ranking, but this second fusion pass over
// subqueries' ranks is MultiHybrid-only territory. A smaller K than the
// standard rrfK=60 sharpens the curve: a hit ranked well by ONE subquery
// (e.g. an entity-trigger subquery nailing an intent) scores closer to a hit
// that's merely mediocre-ranked across several subqueries, instead of being
// swamped by the sum of several weak ranks. See docs/BACKLOG.md's
// multi-intent partial-coverage case for the motivating example.
const multiRRFK = 5

// MultiHybrid expands q (via ExpandQuery) into a small deterministic set of
// subqueries, runs the existing, unmodified Hybrid search for each one, and
// fuses the per-subquery ranked lists with a second reciprocal-rank-fusion
// pass over their ranks — a hit surfaced by more subqueries (or ranked
// higher within them) scores higher. It never re-implements FTS/vector
// ranking: every subquery's ordering comes straight from Hybrid, honoring
// the spec's "must call the existing hybrid search path" constraint.
func MultiHybrid(ctx context.Context, s Searcher, emb embed.Embedder, q string, f postgres.Filter) ([]MultiHit, error) {
	subqueries := ExpandQuery(q)

	perSubFilter := f
	if perSubFilter.Limit <= 0 || perSubFilter.Limit < multiSubqueryLimit {
		perSubFilter.Limit = multiSubqueryLimit
	}

	scores := make(map[string]float64)
	firstSeen := make(map[string]int)
	hitByID := make(map[string]postgres.Hit)
	provenance := make(map[string][]SubqueryMatch)
	order := 0

	for _, sq := range subqueries {
		hits, err := Hybrid(ctx, s, emb, sq, perSubFilter)
		if err != nil {
			return nil, err
		}
		for _, h := range hits {
			scores[h.ID] += 1.0 / float64(multiRRFK+h.Rank)
			if _, ok := hitByID[h.ID]; !ok {
				hitByID[h.ID] = h
			}
			if _, ok := firstSeen[h.ID]; !ok {
				firstSeen[h.ID] = order
				order++
			}
			provenance[h.ID] = append(provenance[h.ID], SubqueryMatch{Query: sq, Arm: h.Arm, Rank: h.Rank})
		}
	}

	ids := make([]string, 0, len(scores))
	for id := range scores {
		ids = append(ids, id)
	}
	sort.SliceStable(ids, func(a, b int) bool {
		if scores[ids[a]] != scores[ids[b]] {
			return scores[ids[a]] > scores[ids[b]]
		}
		if firstSeen[ids[a]] != firstSeen[ids[b]] {
			return firstSeen[ids[a]] < firstSeen[ids[b]]
		}
		return ids[a] < ids[b]
	})

	limit := f.Limit
	if limit <= 0 {
		limit = multiSubqueryLimit
	}
	out := make([]MultiHit, 0, len(ids))
	for i, id := range ids {
		if i >= limit {
			break
		}
		h := hitByID[id]
		h.Rank = i + 1
		h.Score = scores[id]
		out = append(out, MultiHit{Hit: h, Subqueries: provenance[id]})
	}
	return out, nil
}
