package postgres

import (
	"context"
	"fmt"
	"time"
)

// Filter narrows search/AsOf queries.
//
// Type and IncludeTypes combine with OR when both are set: a concept
// matches the type predicate if its type equals Type OR appears in
// IncludeTypes. When only one of the two is set, that one alone applies;
// when neither is set, no type predicate is emitted at all.
type Filter struct {
	Type      string
	Tag       string
	AsOfEpoch *int
	AsOfTime  *time.Time
	Limit     int

	// IncludeTypes, if non-empty, restricts results to concepts whose type is
	// in this list. See the Filter doc comment above for how it combines
	// with Type.
	IncludeTypes []string
	// ExcludeIDs, if non-empty, excludes these concept ids from results.
	ExcludeIDs []string
	// MinVecScore, if > 0, drops Vector() hits whose cosine score is below
	// this threshold (0 = disabled, matching the "0 = disabled" convention
	// used elsewhere in this codebase, e.g. rag.Options.MinScore). It has no
	// effect on FTS — cosine score only exists on the vector arm. This is
	// distinct from, and additive to, query.hybridCore's internal
	// vecScoreFloor: that unexported constant filters the vector arm before
	// RRF fusion on every Hybrid/HybridExplain call, while MinVecScore is a
	// caller-configurable floor honored directly by Store.Vector itself,
	// independent of whether the caller goes through Hybrid at all.
	MinVecScore float64

	// HQLWhere, when non-nil, contributes an additional parameterized WHERE
	// fragment (from internal/hql's ToSQLAt) AND-ed into FTS/Vector. It receives
	// the count of args already bound and must number its own placeholders from
	// startArg+1; it returns the fragment (no leading AND) + its arg values. The
	// store never imports hql — the caller supplies this closure.
	HQLWhere func(startArg int) (where string, args []any, err error)
}

// Hit is a single ranked search result.
type Hit struct {
	ID    string
	Title string
	Type  string
	Score float64
	Rank  int
	// Arm is "fts", "vector", or "both" — which retrieval arm(s) surfaced this
	// hit. Set only by query.Hybrid's fused output; empty ("") on raw
	// FTS()/Vector() results before fusion.
	Arm string
}

const defaultLimit = 20

// currentTxPred returns the SQL predicate matching a concept_fact row whose
// transaction window is still open, for the given column expression (e.g. "tx" or "cf.tx").
func currentTxPred(col string) string {
	return fmt.Sprintf("(upper_inf(%s) OR upper(%s) = 'infinity'::timestamptz)", col, col)
}

// isCurrentTx matches a concept_fact row whose transaction window is still open.
// Deprecated: use currentTxPred("tx") or currentTxPred("cf.tx") instead.
const isCurrentTx = `(upper_inf(tx) OR upper(tx) = 'infinity'::timestamptz)`

// ReplaceEdges replaces the entire outbound edge set for src by deleting any
// existing rows and inserting the new links in a single transaction.
func (s *Store) ReplaceEdges(ctx context.Context, src string, links []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin edges tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "DELETE FROM edge WHERE src=$1", src); err != nil {
		return fmt.Errorf("delete edges for %q: %w", src, err)
	}
	for _, dst := range links {
		if _, err := tx.Exec(ctx,
			"INSERT INTO edge (src, dst, kind) VALUES ($1, $2, 'link')", src, dst); err != nil {
			return fmt.Errorf("insert edge %q->%q: %w", src, dst, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit edges tx: %w", err)
	}
	return nil
}

// FTS runs a full-text search. The generated `fts` column (built with the custom
// 'pixpt' config — Portuguese stopwords removed, no stemming) is used for recall
// via @@, but ranking is computed at query time with the per-concept language
// config so a Portuguese term ranks a pt concept correctly: ts_rank_cd over
// to_tsvector(<lang cfg>, title||' '||intent_terms||' '||body).
// When the Filter carries an as-of point, results are restricted to concept IDs
// whose bitemporal fact is valid at that point.
func (s *Store) FTS(ctx context.Context, q string, f Filter) ([]Hit, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = defaultLimit
	}

	args := []any{q}
	// Recall uses the same custom 'pixpt' config as the generated fts column
	// (migration 0003): Portuguese stopwords dropped, NO stemming — so query and
	// index tokenize identically and natural-language stopwords are not required
	// AND-terms. Ranking (below) still stems per-language for ranking quality.
	//
	// NOTE: this is websearch's implicit AND-of-all-words. A naive OR-recall
	// rewrite (' & ' -> ' | ') was tried to let intent_terms fire on fuzzy queries
	// and MEASURED WORSE on the deterministic top-hit harness (fuzzy MRR 0.285 ->
	// 0.162, top@5 41% -> 24%; precise flat): OR floods the FTS arm with every
	// 'pix' doc and length-normalized ts_rank_cd floats short junk, which RRF then
	// dilutes the real target down. The real lever is QUORUM/coverage ranking (rank
	// by distinct query-terms matched, not length-normalized density) — see BACKLOG.
	where := "WHERE fts @@ websearch_to_tsquery('pixpt', $1)"
	if types := combinedTypes(f); len(types) > 0 {
		args = append(args, types)
		where += fmt.Sprintf(" AND type = ANY($%d)", len(args))
	}
	if f.Tag != "" {
		args = append(args, f.Tag)
		where += fmt.Sprintf(" AND tags @> ARRAY[$%d]::text[]", len(args))
	}
	if len(f.ExcludeIDs) > 0 {
		args = append(args, f.ExcludeIDs)
		where += fmt.Sprintf(" AND id != ALL($%d)", len(args))
	}
	if pred, ok := asOfConceptPredicate(&args, f); ok {
		where += " AND " + pred
	}
	if f.HQLWhere != nil {
		hw, ha, err := f.HQLWhere(len(args))
		if err != nil {
			return nil, fmt.Errorf("hql filter: %w", err)
		}
		if hw != "" {
			where += " AND (" + hw + ")"
			args = append(args, ha...)
		}
	}
	args = append(args, limit)

	// Per-concept language config drives ranking: pt rows rank Portuguese terms,
	// en rows rank English terms. Recall comes from the 'pixpt' fts column.
	// The CASE expression must be cast to regconfig for to_tsvector/websearch_to_tsquery.
	// Title terms get weight 'A' and body terms 'D', so ts_rank_cd's default
	// weight ramp (A=1.0 vs D=0.1) makes a section whose *title* matches the
	// query outrank one that only mentions it in passing in the body.
	// Normalization flag 1 divides the rank by 1+log(document length): without
	// it, ts_rank_cd rewards sheer term count, so a huge manual annex ("ANEXO
	// IV") outranks the short, exact API endpoint for common-word queries like
	// "consultar cobrança por txid". Length normalization fixes that bias.
	query := fmt.Sprintf(`
SELECT id, coalesce(title,''), type,
       ts_rank_cd(
         setweight(to_tsvector(
           (CASE WHEN language = 'en' THEN 'english' ELSE 'portuguese' END)::regconfig,
           coalesce(title,'')), 'A')
         || setweight(to_tsvector(
           (CASE WHEN language = 'en' THEN 'english' ELSE 'portuguese' END)::regconfig,
           coalesce(intent_terms,'')), 'B')
         || setweight(to_tsvector(
           (CASE WHEN language = 'en' THEN 'english' ELSE 'portuguese' END)::regconfig,
           body), 'D'),
         websearch_to_tsquery(
           (CASE WHEN language = 'en' THEN 'english' ELSE 'portuguese' END)::regconfig, $1),
         1
       ) AS score
FROM concept
%s
ORDER BY score DESC, id ASC
LIMIT $%d`, where, len(args))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("fts query: %w", err)
	}
	defer func() { rows.Close() }()

	var hits []Hit
	rank := 0
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.ID, &h.Title, &h.Type, &h.Score); err != nil {
			return nil, fmt.Errorf("scan fts hit: %w", err)
		}
		rank++
		h.Rank = rank
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fts rows: %w", err)
	}
	return hits, nil
}

// combinedTypes returns the set of type values FTS/Vector's type predicate
// should match, applying Filter's Type/IncludeTypes OR interaction (see the
// Filter doc comment): both set -> union of the two; only one set -> that one
// alone; neither set -> nil, meaning the caller should emit no type predicate.
func combinedTypes(f Filter) []string {
	switch {
	case f.Type == "" && len(f.IncludeTypes) == 0:
		return nil
	case f.Type == "":
		return f.IncludeTypes
	default:
		out := make([]string, 0, len(f.IncludeTypes)+1)
		out = append(out, f.Type)
		out = append(out, f.IncludeTypes...)
		return out
	}
}

// asOfConceptPredicate appends an as-of bound parameter (epoch or timestamp)
// and returns a SQL predicate restricting `id` to concepts whose bitemporal
// fact in concept_fact is current at that point. It returns ok=false when the
// Filter carries no as-of bound. Shared by FTS and Vector so --as-of narrows
// both search paths identically.
func asOfConceptPredicate(args *[]any, f Filter) (string, bool) {
	switch {
	case f.AsOfEpoch != nil:
		*args = append(*args, *f.AsOfEpoch)
		return fmt.Sprintf(
			"id IN (SELECT DISTINCT ON (id) id FROM concept_fact WHERE epoch <= $%d ORDER BY id, epoch DESC)",
			len(*args)), true
	case f.AsOfTime != nil:
		*args = append(*args, *f.AsOfTime)
		return fmt.Sprintf(
			"id IN (SELECT id FROM concept_fact WHERE valid @> $%d::timestamptz AND %s)",
			len(*args), currentTxPred("tx")), true
	default:
		return "", false
	}
}
