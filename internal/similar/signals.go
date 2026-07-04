package similar

import (
	"context"
	"fmt"

	"pixkb/internal/store/postgres"
)

// SemanticSimilar returns concepts nearest to id's own stored embedding by
// cosine similarity, excluding id itself. Reuses Store.Vector unmodified —
// Vector doesn't care whether its query vector came from freshly embedding
// text or from GetEmbedding's stored vector for an existing concept.
func SemanticSimilar(ctx context.Context, s Store, id string, f postgres.Filter) ([]Hit, error) {
	vec, err := s.GetEmbedding(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("similar: get embedding for %q: %w", id, err)
	}
	hits, err := s.Vector(ctx, vec, withHeadroom(f))
	if err != nil {
		return nil, fmt.Errorf("similar: vector search for %q: %w", id, err)
	}
	return tagAndExclude(hits, id, SignalSemantic, f.Limit), nil
}

// GraphSimilar returns id's direct link-graph neighbours (both directions),
// tagged with the graph signal, optionally narrowed to f.Type. Related()
// edges should never be self-loops, but a defensive exclude-self is cheap
// and correct regardless of that invariant holding. Note: f.Tag is NOT
// applied here — RelatedConcept carries no tag data (Related's SQL never
// selects it), so tag filtering is a real, documented limitation of graph
// mode, not a silent gap; semantic/lexical/more-like-this DO honor Tag via
// Store.Vector/query.Hybrid.
func GraphSimilar(ctx context.Context, s Store, id string, f postgres.Filter) ([]Hit, error) {
	rel, err := s.Related(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("similar: related for %q: %w", id, err)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	out := make([]Hit, 0, len(rel))
	for _, r := range rel {
		if r.ID == id {
			continue
		}
		if f.Type != "" && r.Type != f.Type {
			continue
		}
		out = append(out, Hit{
			Hit: postgres.Hit{ID: r.ID, Title: r.Title, Type: r.Type},
			Why: []string{SignalGraph},
		})
		if len(out) >= limit {
			break
		}
	}
	for i := range out {
		out[i].Rank = i + 1
	}
	return out, nil
}
