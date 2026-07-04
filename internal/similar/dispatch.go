package similar

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"pixkb/internal/embed"
	"pixkb/internal/okf"
	"pixkb/internal/store/postgres"
)

// fusionRRFK mirrors internal/query's rrfK (60) — kept as a separate local
// constant rather than importing query's unexported one; both packages
// independently choosing the conventional RRF k=60 is a deliberate, cheap
// duplication, not a coupling worth an exported cross-package constant.
const fusionRRFK = 60

// Options tune a Similar call. The zero value selects hybrid mode with graph
// neighbours excluded (IncludeGraph must be set explicitly true — see Task 6
// and 7 for the CLI/MCP default of true at the surface layer).
type Options struct {
	Mode         string // "semantic" | "graph" | "hybrid" | "more-like-this"
	IncludeGraph bool   // hybrid mode only: also fold in GraphSimilar's neighbours
	Filter       postgres.Filter
}

// Similar is the single entry point for concept-similarity search, selecting
// among four modes. CLI (Task 6) and MCP (Task 7) both call this directly —
// neither surface re-implements mode dispatch.
func Similar(ctx context.Context, s Store, emb embed.Embedder, bundleDir, id string, opts Options) ([]Hit, error) {
	switch opts.Mode {
	case "semantic":
		return SemanticSimilar(ctx, s, id, opts.Filter)
	case "graph":
		return GraphSimilar(ctx, s, id, opts.Filter.Limit)
	case "more-like-this":
		return MoreLikeThis(ctx, s, emb, bundleDir, id, opts.Filter)
	case "hybrid":
		return hybridSimilar(ctx, s, emb, bundleDir, id, opts)
	default:
		return nil, fmt.Errorf("similar: unknown mode %q (want semantic|graph|hybrid|more-like-this)", opts.Mode)
	}
}

// hybridSimilar fuses semantic + more-like-this + (if opts.IncludeGraph)
// graph signals with a reciprocal-rank-fusion pass over each signal's own
// rank, using the same three-tier deterministic tiebreak (score desc, first-
// seen order, id asc) query.Hybrid/query.MultiHybrid already use. Domain
// tagging is applied last, over the fused candidate set, using the queried
// concept's own type read from the bundle — a bundle-read failure at that
// point degrades to "no domain tags" rather than failing the whole request,
// since domain tagging is enrichment, not core retrieval.
func hybridSimilar(ctx context.Context, s Store, emb embed.Embedder, bundleDir, id string, opts Options) ([]Hit, error) {
	var lists [][]Hit

	sem, err := SemanticSimilar(ctx, s, id, opts.Filter)
	if err != nil {
		return nil, err
	}
	lists = append(lists, sem)

	mlt, err := MoreLikeThis(ctx, s, emb, bundleDir, id, opts.Filter)
	if err != nil {
		return nil, err
	}
	lists = append(lists, mlt)

	if opts.IncludeGraph {
		gr, err := GraphSimilar(ctx, s, id, opts.Filter.Limit)
		if err != nil {
			return nil, err
		}
		lists = append(lists, gr)
	}

	fused := fuse(lists)

	if c, err := okf.ReadConcept(filepath.Join(bundleDir, filepath.FromSlash(id)), bundleDir); err == nil {
		tagDomain(fused, c.Type)
	}

	limit := opts.Filter.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if len(fused) > limit {
		fused = fused[:limit]
	}
	return fused, nil
}

// fuse combines multiple signal-result lists into one ranked, deduplicated
// list via reciprocal-rank fusion over each list's own Rank, merging Why
// tags for any id that multiple signals surfaced. Determinism: score desc,
// then first-seen order (which list/position saw the id first), then id asc
// — the exact tiebreak pattern query.Hybrid/query.MultiHybrid use.
func fuse(lists [][]Hit) []Hit {
	scores := make(map[string]float64)
	firstSeen := make(map[string]int)
	byID := make(map[string]postgres.Hit)
	why := make(map[string]map[string]bool)
	order := 0

	for _, list := range lists {
		for _, h := range list {
			scores[h.ID] += 1.0 / float64(fusionRRFK+h.Rank)
			if _, ok := byID[h.ID]; !ok {
				byID[h.ID] = h.Hit
				firstSeen[h.ID] = order
				order++
			}
			if why[h.ID] == nil {
				why[h.ID] = make(map[string]bool)
			}
			for _, w := range h.Why {
				why[h.ID][w] = true
			}
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

	out := make([]Hit, 0, len(ids))
	for i, id := range ids {
		h := byID[id]
		h.Rank = i + 1
		h.Score = scores[id]
		whys := make([]string, 0, len(why[id]))
		for w := range why[id] {
			whys = append(whys, w)
		}
		sort.Strings(whys) // deterministic Why order regardless of map iteration
		out = append(out, Hit{Hit: h, Why: whys})
	}
	return out
}
