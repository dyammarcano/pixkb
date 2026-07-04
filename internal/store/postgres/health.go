package postgres

import (
	"context"
	"fmt"
	"sort"
)

// SparseConcept is a concept with no graph edges in either direction — a
// candidate for the "empty or unusually sparse graph links" signal Feature 8
// of docs/SEARCH-CAPABILITY-SPEC.md ("Search Quality Operations") asks for.
type SparseConcept struct {
	ID    string
	Type  string
	Title string
}

// GraphSparsity returns every concept with zero edges (neither an outgoing
// nor an incoming link), ordered by id for deterministic output. A concept
// with no graph links isn't necessarily wrong — some concept types are
// naturally leaf nodes — but it is a signal worth surfacing for triage.
func (s *Store) GraphSparsity(ctx context.Context) ([]SparseConcept, error) {
	const q = `
SELECT c.id, c.type, coalesce(c.title,'')
  FROM concept c
 WHERE NOT EXISTS (SELECT 1 FROM edge e WHERE e.src = c.id OR e.dst = c.id)
 ORDER BY c.id`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("graph sparsity query: %w", err)
	}
	defer rows.Close()

	var out []SparseConcept
	for rows.Next() {
		var sc SparseConcept
		if err := rows.Scan(&sc.ID, &sc.Type, &sc.Title); err != nil {
			return nil, fmt.Errorf("scan sparse concept row: %w", err)
		}
		out = append(out, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sparse concept rows: %w", err)
	}
	return out, nil
}

// EmbeddingModelDim is one distinct (embed_model, dim) combination found in
// the embedding table. More than one combination is a consistency hazard:
// cosine similarity between vectors from different models or dimensions is
// not meaningful, so a mixed index silently degrades vector search quality.
type EmbeddingModelDim struct {
	Model string
	Dim   int
}

// EmbeddingCoverage is the "reporting embedding coverage and model/dimension
// consistency" signal Feature 8 asks for: how many concepts have at least one
// stored embedding, out of the total, and which (model, dim) combinations are
// present.
type EmbeddingCoverage struct {
	TotalConcepts    int
	EmbeddedConcepts int
	Models           []EmbeddingModelDim
}

// EmbeddingCoverage reports how many concepts have a stored embedding and
// which embedding model/dimension combinations exist in the index today.
func (s *Store) EmbeddingCoverage(ctx context.Context) (EmbeddingCoverage, error) {
	var cov EmbeddingCoverage
	const countQ = `
SELECT
  (SELECT count(*) FROM concept) AS total,
  (SELECT count(DISTINCT id) FROM embedding) AS embedded`
	if err := s.pool.QueryRow(ctx, countQ).Scan(&cov.TotalConcepts, &cov.EmbeddedConcepts); err != nil {
		return cov, fmt.Errorf("embedding coverage counts: %w", err)
	}

	const modelsQ = `SELECT DISTINCT embed_model, dim FROM embedding ORDER BY embed_model, dim`
	rows, err := s.pool.Query(ctx, modelsQ)
	if err != nil {
		return cov, fmt.Errorf("embedding model/dim query: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var m EmbeddingModelDim
		if err := rows.Scan(&m.Model, &m.Dim); err != nil {
			return cov, fmt.Errorf("scan embedding model/dim row: %w", err)
		}
		cov.Models = append(cov.Models, m)
	}
	if err := rows.Err(); err != nil {
		return cov, fmt.Errorf("iterate embedding model/dim rows: %w", err)
	}
	return cov, nil
}

// Consistent reports whether the index has at most one (model, dim)
// combination — the "model/dimension consistency" half of EmbeddingCoverage.
func (c EmbeddingCoverage) Consistent() bool { return len(c.Models) <= 1 }

// BundleDrift compares the given set of canonical bundle concept ids against
// what's actually in the DB, and returns the ids that exist in the DB but
// are NOT in the bundle set, sorted for deterministic output. This is the
// "search-health should diff bundle-vs-DB concept counts itself" signal
// docs/BACKLOG.md asks for: a concept can end up in Postgres without ever
// being (or no longer being) in the canonical OKF bundle — e.g. a stale row
// surviving a rename, or, as happened once, an accidental filename
// collision with an OKF reserved nav filename — and that's a real
// correctness problem, since the bundle is supposed to be the single source
// of truth.
//
// We only report DB-only drift here, not the reverse (bundle-only: "in the
// bundle but not yet in the DB"). A concept present in the bundle but absent
// from the DB is the normal, transient state of "not yet (re)indexed" —
// that's what the regular reindex flow exists to fix, not a health problem
// — whereas a concept in the DB with no bundle backing can only be
// explained by drift or a bug, so it is worth flagging unconditionally.
func (s *Store) BundleDrift(ctx context.Context, bundleIDs []string) (dbOnly []string, err error) {
	const q = `SELECT id FROM concept`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("bundle drift concept id query: %w", err)
	}
	defer rows.Close()

	inBundle := make(map[string]struct{}, len(bundleIDs))
	for _, id := range bundleIDs {
		inBundle[id] = struct{}{}
	}

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan concept id row: %w", err)
		}
		if _, ok := inBundle[id]; !ok {
			dbOnly = append(dbOnly, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate concept id rows: %w", err)
	}
	sort.Strings(dbOnly)
	return dbOnly, nil
}
