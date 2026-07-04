package postgres

import (
	"context"
	"fmt"
)

// RelatedConcept is a neighbour of a concept in the OKF link graph.
type RelatedConcept struct {
	ID        string
	Title     string
	Type      string // neighbour's concept type; "" if the neighbour row has no concept (dangling edge)
	Direction string // "out" = this concept links to it; "in" = it links here
}

// Related returns the concept's graph neighbours in both directions: outgoing
// links (concepts it references) and incoming links (concepts that reference
// it). It is read-only.
func (s *Store) Related(ctx context.Context, id string) ([]RelatedConcept, error) {
	const q = `
SELECT e.dst AS rid, COALESCE(c.title, ''), COALESCE(c.type, ''), 'out'
  FROM edge e LEFT JOIN concept c ON c.id = e.dst
 WHERE e.src = $1
UNION
SELECT e.src AS rid, COALESCE(c.title, ''), COALESCE(c.type, ''), 'in'
  FROM edge e LEFT JOIN concept c ON c.id = e.src
 WHERE e.dst = $1
 ORDER BY 4, 1`
	rows, err := s.pool.Query(ctx, q, id)
	if err != nil {
		return nil, fmt.Errorf("related query %q: %w", id, err)
	}
	defer rows.Close()

	var out []RelatedConcept
	for rows.Next() {
		var r RelatedConcept
		if err := rows.Scan(&r.ID, &r.Title, &r.Type, &r.Direction); err != nil {
			return nil, fmt.Errorf("scan related row: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate related rows: %w", err)
	}
	return out, nil
}
