package postgres

import (
	"context"
	"fmt"

	"pixkb/internal/okf"
)

// AsOf returns the bitemporal point-in-time view of concepts as classified by
// concept_fact, joined to concept for type/title/body metadata.
//
// AsOfEpoch: transaction-time travel — for each id, returns the row with the
// greatest epoch <= N, regardless of whether its tx is open or closed.
// DISTINCT ON (cf.id) + ORDER BY cf.id, cf.epoch DESC picks the latest version
// per id at/before N, deduplicated. No upper_inf filter is applied.
//
// AsOfTime: valid-time as-of — facts whose valid range contains the instant and
// whose tx window is still open (current transaction time).
//
// default: all rows with open tx windows (current state).
//
// Type/Tag/Limit narrow the result. A zero/negative Limit means no limit.
// Populated fields: ID, Type, Title, ContentSHA, Epoch.
func (s *Store) AsOf(ctx context.Context, f Filter) ([]okf.Concept, error) {
	args := []any{}

	var innerWhere, innerOrder, outerOrder string
	switch {
	case f.AsOfEpoch != nil:
		args = append(args, *f.AsOfEpoch)
		innerWhere = fmt.Sprintf("cf.epoch <= $%d", len(args))
		innerOrder = "cf.id, cf.epoch DESC"
		outerOrder = "t.epoch DESC, t.id ASC"
	case f.AsOfTime != nil:
		args = append(args, *f.AsOfTime)
		innerWhere = fmt.Sprintf("cf.valid @> $%d::timestamptz AND %s", len(args), currentTxPred("cf.tx"))
		innerOrder = "cf.id, cf.epoch DESC"
		outerOrder = "t.epoch DESC, t.id ASC"
	default:
		innerWhere = currentTxPred("cf.tx")
		innerOrder = "cf.id, cf.epoch DESC"
		outerOrder = "t.epoch DESC, t.id ASC"
	}

	if f.Type != "" {
		args = append(args, f.Type)
		innerWhere += fmt.Sprintf(" AND cf.type = $%d", len(args))
	}
	if f.Tag != "" {
		args = append(args, f.Tag)
		innerWhere += fmt.Sprintf(" AND $%d = ANY(c.tags)", len(args))
	}

	limitClause := ""
	if f.Limit > 0 {
		args = append(args, f.Limit)
		limitClause = fmt.Sprintf(" LIMIT $%d", len(args))
	}

	q := fmt.Sprintf(`
SELECT id, type, title, content_sha, epoch FROM (
  SELECT DISTINCT ON (cf.id)
         cf.id, cf.type, coalesce(cf.title,'') AS title, cf.content_sha, cf.epoch
  FROM concept_fact cf
  JOIN concept c ON c.id = cf.id
  WHERE %s
  ORDER BY %s
) t
ORDER BY %s%s`, innerWhere, innerOrder, outerOrder, limitClause)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("asof query: %w", err)
	}
	defer rows.Close()

	var out []okf.Concept
	for rows.Next() {
		var c okf.Concept
		if err := rows.Scan(&c.ID, &c.Type, &c.Title, &c.ContentSHA, &c.Epoch); err != nil {
			return nil, fmt.Errorf("scan asof row: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate asof rows: %w", err)
	}
	return out, nil
}
