package postgres

import (
	"context"
	"fmt"

	"github.com/pgvector/pgvector-go"
)

// Vector runs an exact cosine k-NN search over the latest embedding per
// concept. embedding.vec is an untyped pgvector `vector` (no HNSW index —
// pgvector cannot index an untyped column), so this is an exact scan using the
// `<=>` cosine-distance operator: sub-millisecond at KB scale, perfect recall.
// Score is cosine similarity (1 - distance). Type/Tag/Limit and --as-of narrow
// results identically to FTS (shared asOfConceptPredicate). The embedding
// subquery exposes its id as `eid`, so the unqualified `id` in the as-of
// predicate resolves unambiguously to concept.id.
func (s *Store) Vector(ctx context.Context, vec []float32, f Filter) ([]Hit, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = defaultLimit
	}

	args := []any{pgvector.NewVector(vec)}
	where := ""
	add := func(cond string) {
		if where == "" {
			where = "WHERE " + cond
		} else {
			where += " AND " + cond
		}
	}
	if f.Type != "" {
		args = append(args, f.Type)
		add(fmt.Sprintf("c.type = $%d", len(args)))
	}
	if f.Tag != "" {
		args = append(args, f.Tag)
		add(fmt.Sprintf("c.tags @> ARRAY[$%d]::text[]", len(args)))
	}
	if pred, ok := asOfConceptPredicate(&args, f); ok {
		add(pred) // "id IN (...)" — resolves to concept.id
	}
	args = append(args, limit)

	query := fmt.Sprintf(`
SELECT c.id, coalesce(c.title,''), c.type, 1 - (e.vec <=> $1) AS score
FROM (SELECT DISTINCT ON (id) id AS eid, vec FROM embedding ORDER BY id, epoch DESC) e
JOIN concept c ON c.id = e.eid
%s
ORDER BY e.vec <=> $1 ASC
LIMIT $%d`, where, len(args))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("vector query: %w", err)
	}
	defer func() { rows.Close() }()

	var hits []Hit
	rank := 0
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.ID, &h.Title, &h.Type, &h.Score); err != nil {
			return nil, fmt.Errorf("scan vector hit: %w", err)
		}
		rank++
		h.Rank = rank
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate vector rows: %w", err)
	}
	return hits, nil
}
