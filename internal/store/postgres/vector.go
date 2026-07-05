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
// results identically to FTS (shared asOfConceptPredicate), as do
// IncludeTypes/ExcludeIDs (shared combinedTypes). MinVecScore is honored
// here directly: hits scoring below it are dropped before Rank is assigned,
// so the returned Rank stays a contiguous 1-based sequence over the kept
// hits (may be fewer than Limit when low-scoring hits are dropped). The
// embedding subquery exposes its id as `eid`, so the unqualified `id` in the
// as-of predicate resolves unambiguously to concept.id.
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
	if types := combinedTypes(f); len(types) > 0 {
		args = append(args, types)
		add(fmt.Sprintf("c.type = ANY($%d)", len(args)))
	}
	if f.Tag != "" {
		args = append(args, f.Tag)
		add(fmt.Sprintf("c.tags @> ARRAY[$%d]::text[]", len(args)))
	}
	if len(f.ExcludeIDs) > 0 {
		args = append(args, f.ExcludeIDs)
		add(fmt.Sprintf("c.id != ALL($%d)", len(args)))
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
ORDER BY e.vec <=> $1 ASC, c.id ASC
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
		// MinVecScore is applied here rather than in SQL: score is a computed
		// SELECT-list expression, not a real column, so it cannot be
		// referenced from a WHERE clause without repeating the <=> expression.
		// Skipping before Rank is assigned keeps Rank a contiguous 1-based
		// sequence over the hits that are actually returned.
		if f.MinVecScore > 0 && h.Score < f.MinVecScore {
			continue
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
