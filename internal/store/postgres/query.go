package postgres

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"pixkb/internal/okf"
)

// conceptColumns is the full concept-row column list, in scan order, shared
// by QueryConcepts. Kept in one place so the SELECT list and scanConcept
// stay in lockstep.
const conceptColumns = "id, type, coalesce(title,''), coalesce(description,''), coalesce(resource,''), tags, language, body, " +
	"content_sha, coalesce(source_uri,''), first_epoch, last_epoch, updated_at, coalesce(intent_terms,''), domain, coalesce(norm_ref,'')"

// scanConcept scans one concept row selected via conceptColumns into an
// okf.Concept. first_epoch is read but discarded onto Epoch alongside
// last_epoch (Epoch tracks last_epoch, matching AsOf/UpsertConcept usage
// elsewhere in this package).
func scanConcept(rows pgx.Rows) (okf.Concept, error) {
	var c okf.Concept
	var firstEpoch int
	if err := rows.Scan(
		&c.ID, &c.Type, &c.Title, &c.Description, &c.Resource, &c.Tags, &c.Language, &c.Body,
		&c.ContentSHA, &c.SourceURI, &firstEpoch, &c.Epoch, &c.Timestamp, &c.IntentTerms, &c.Domain, &c.NormRef,
	); err != nil {
		return okf.Concept{}, fmt.Errorf("scan concept row: %w", err)
	}
	return c, nil
}

// QueryConcepts runs a structured HQL-compiled filter over the concept
// table. where/order are already-parameterized SQL fragments produced by
// hql.Query.ToSQL (no leading WHERE/ORDER BY keyword); args are their $N
// placeholder values. where is omitted from the query entirely when empty,
// as is order; limit <= 0 means no LIMIT clause.
func (s *Store) QueryConcepts(ctx context.Context, where string, args []any, order string, limit int) ([]okf.Concept, error) {
	q := "SELECT " + conceptColumns + " FROM concept"
	if where != "" {
		q += " WHERE " + where
	}
	if order != "" {
		q += " ORDER BY " + order
	}
	if limit > 0 {
		q += " LIMIT " + strconv.Itoa(limit)
	}

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query concepts: %w", err)
	}
	defer rows.Close()

	var out []okf.Concept
	for rows.Next() {
		c, err := scanConcept(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate concept rows: %w", err)
	}
	return out, nil
}
