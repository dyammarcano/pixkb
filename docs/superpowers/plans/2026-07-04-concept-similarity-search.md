# Concept Similarity Search Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement Feature 2 ("Concept Similarity Search") of `docs/SEARCH-CAPABILITY-SPEC.md`: given a concept id, return ranked "similar" concepts using multiple signals (semantic/embedding, lexical, graph, domain type-adjacency), each result tagged with why it matched, across four modes (`semantic`, `graph`, `hybrid`, `more-like-this`).

**Architecture:** A new `internal/similar` package (mirroring `internal/query`/`internal/rag`) with one function per signal — `SemanticSimilar` (fetches the concept's own stored embedding, reuses the existing `Store.Vector` unmodified), `GraphSimilar` (wraps the existing `Store.Related`), `MoreLikeThis` (builds a synthetic query from the concept's title/intent_terms/body and runs it through the existing, unmodified `query.Hybrid` — reusing Task-1-of-the-prior-plan's `Hit.Arm` field to label each hit `semantic`/`lexical`) — plus a small fixed type-pair `domainAdjacency` table that re-tags candidates already found by the other signals (not an independent corpus scan). A `Similar()` dispatcher selects by mode; `hybrid` mode fuses semantic + more-like-this + (optionally) graph with the same RRF-style, three-tier-deterministic-tiebreak pattern already used by `query.Hybrid`/`query.MultiHybrid`. Two small, additive Postgres-layer changes are needed first: a `GetEmbedding` accessor (doesn't exist yet) and a determinism fix (missing id-tiebreak on `Vector`'s `ORDER BY`, found during this plan's research — the same class of gap `query.Hybrid` already guards against).

**Tech Stack:** Go 1.25, `internal/store/postgres` (Filter/Hit/RelatedConcept types, pgx, pgvector-go v0.4.0), `internal/query` (`Searcher` interface, `Hybrid`, `rrfK`), `internal/okf` (`Concept`, `ReadConcept`), `internal/embed` (`Embedder`), `cmd/pixkb`, `internal/kbmcp`.

## Global Constraints

- Go 1.25.0, module `pixkb`, `CGO_ENABLED=0` — pure Go only (air-gapped project).
- **Must call existing ranking primitives, never reimplement them.** `SemanticSimilar` calls `Store.Vector` unmodified; `MoreLikeThis` calls `query.Hybrid` unmodified. Only the NEW glue (self-exclusion, signal tagging, cross-signal RRF fusion) is new logic — same discipline the prior multi-query-retrieval plan established and its ADR (`docs/adr/0002-recall-tuning-findings.md`) demands: do not re-tune FTS/vector ranking math itself.
- **Determinism is a hard acceptance criterion** (spec: "Results are stable and deterministic for the same index state"). Every new SQL query needs an explicit id tiebreaker; every new fusion/sort needs the same three-tier pattern `query.Hybrid`/`query.MultiHybrid` already use: score desc → first-seen-order → id asc.
- **The queried concept must be excluded from its own results by default** (spec acceptance criterion) — every signal function is responsible for its own self-exclusion; do not rely on a single filter applied once at the end (a signal added later must not reintroduce the concept).
- **Domain signal is v1-scoped to a small, fixed type-pair table**, re-tagging candidates already surfaced by another signal — NOT an independent full-corpus scan and NOT the finer-grained topic-specific rules the spec's prose examples suggest ("DICT endpoints near key concepts" specifically, vs. just "ApiEndpoint near Reference" generally). The finer-grained version is out of scope here — see Task 8's backlog note. This mirrors how the prior plan kept `entityTriggers` small and measured before trusting it.
- **Out of scope for this plan** (explicitly, per the spec's own later features): CLI/MCP JSON explanation of WHY each signal fired beyond the simple `Why []string` tag (Feature 3, "Search Explanation"); richer output formats beyond plain text + the existing JSON hit shape (Feature 4); wiring concept-similarity into RAG grounding (Feature 5); a full curated `eval/cases-similar-ids.tsv` gold-set regression harness (Task 8 does a live manual spot-check instead — building a proper gold set needs hand curation and is backlogged).
- Follow existing code conventions: doc comments explain *why*; `gofmt` clean; `testify`'s `assert`/`require`; `t.Parallel()` on pure unit tests; DB-touching tests are `PIXKB_TEST_DSN`-gated and skip under `-short`, matching `internal/kbmcp/server_test.go`'s and `internal/query/hybrid_integration_test.go`'s existing pattern — do not invent new test infrastructure.
- Commit messages: short, conventional, imperative (see `git log --oneline`).

---

### Task 1: Postgres layer — `GetEmbedding`, `Vector` determinism fix, `RelatedConcept.Type`

**Files:**
- Modify: `internal/store/postgres/embedding.go`
- Modify: `internal/store/postgres/vector.go`
- Modify: `internal/store/postgres/related.go`
- Test: `internal/store/postgres/embedding_test.go` (new), `internal/store/postgres/related_test.go` (new) — both `PIXKB_TEST_DSN`-gated, matching existing store test conventions in this package.

**Interfaces:**
- Produces: `func (s *Store) GetEmbedding(ctx context.Context, id string) ([]float32, error)`; `Vector`'s SQL gains a deterministic tiebreak (no signature change); `RelatedConcept` gains a `Type string` field (no method signature change, existing callers unaffected — `Type` is simply "" for anyone not reading it).
- Consumes: nothing new.

- [ ] **Step 1: Add `GetEmbedding`**

In `internal/store/postgres/embedding.go`, add below `UpsertEmbedding`:

```go
// GetEmbedding fetches a concept's own latest stored embedding vector by id —
// the "what does THIS concept's vector look like" accessor UpsertEmbedding's
// write path has no counterpart for until now. Used by similar.SemanticSimilar
// to find a concept's nearest neighbours (as opposed to Vector, which embeds
// fresh query TEXT). Picks the latest epoch if a concept has been re-embedded.
func (s *Store) GetEmbedding(ctx context.Context, id string) ([]float32, error) {
	const q = `SELECT vec FROM embedding WHERE id = $1 ORDER BY epoch DESC LIMIT 1`
	var v pgvector.Vector
	if err := s.pool.QueryRow(ctx, q, id).Scan(&v); err != nil {
		return nil, fmt.Errorf("get embedding %q: %w", id, err)
	}
	return v.Slice(), nil
}
```

- [ ] **Step 2: Write the failing test for `GetEmbedding`**

Create `internal/store/postgres/embedding_test.go` (seeds a real `concept` row first, since `embedding.id` has a foreign key to `concept.id`):

```go
package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetEmbedding_ReturnsLatestEpoch(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()

	_, err = s.pool.Exec(ctx, `
INSERT INTO concept (id, type, title, body, content_sha, first_epoch, last_epoch, updated_at)
VALUES ('x.md', 'Reference', 'X', 'body', 'sha', 1, 1, now())`)
	require.NoError(t, err)

	require.NoError(t, s.UpsertEmbedding(ctx, "x.md", 1, "hashing", []float32{1, 0, 0}, time.Now()))
	require.NoError(t, s.UpsertEmbedding(ctx, "x.md", 2, "hashing", []float32{0, 1, 0}, time.Now()))

	got, err := s.GetEmbedding(ctx, "x.md")
	require.NoError(t, err)
	assert.Equal(t, []float32{0, 1, 0}, got, "must return the LATEST epoch's vector, not the first")
}

func TestGetEmbedding_NotFound(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()

	_, err = s.GetEmbedding(ctx, "does-not-exist.md")
	require.Error(t, err)
}
```

(Check `internal/store/postgres/*_test.go` for the exact `testDSN(t)` / `applyTestSchema(t, dsn)` helper signatures already in this package — e.g. `ispb_test.go` uses both; reuse them verbatim, do not redefine.)

- [ ] **Step 3: Run test to verify it fails, then run again after implementing**

Run: `go test ./internal/store/postgres/... -run TestGetEmbedding -v` (needs `PIXKB_TEST_DSN`; skips otherwise — that's fine, note in your report whether a DSN was available, matching the prior plan's Task 5 convention).
Expected before Step 1's code exists: FAIL with `s.GetEmbedding undefined`. After: PASS (or SKIP if no DSN — do not treat SKIP as failure, but do not skip WRITING the test).

- [ ] **Step 4: Fix `Vector`'s missing determinism tiebreak**

In `internal/store/postgres/vector.go`, the query template currently ends:

```go
	query := fmt.Sprintf(`
SELECT c.id, coalesce(c.title,''), c.type, 1 - (e.vec <=> $1) AS score
FROM (SELECT DISTINCT ON (id) id AS eid, vec FROM embedding ORDER BY id, epoch DESC) e
JOIN concept c ON c.id = e.eid
%s
ORDER BY e.vec <=> $1 ASC
LIMIT $%d`, where, len(args))
```

Replace the `ORDER BY` line to add an id tiebreak, matching `FTS`'s `ORDER BY score DESC, id ASC` pattern in the same package (`search.go:135`):

```go
	query := fmt.Sprintf(`
SELECT c.id, coalesce(c.title,''), c.type, 1 - (e.vec <=> $1) AS score
FROM (SELECT DISTINCT ON (id) id AS eid, vec FROM embedding ORDER BY id, epoch DESC) e
JOIN concept c ON c.id = e.eid
%s
ORDER BY e.vec <=> $1 ASC, c.id ASC
LIMIT $%d`, where, len(args))
```

This is a pure determinism fix (tie-break only; it cannot change any non-tied ordering) — no existing test should need to change, but read `internal/store/postgres/vector_test.go` (if it exists) or any `TestVector*`/`TestHybrid_*` test that asserts exact ordering to confirm none of them depended on tie order being unspecified (they shouldn't, since a real assertion on unspecified order would itself be a latent test bug — if you find one, flag it, don't silently "fix" the test).

- [ ] **Step 5: Add `Type` to `RelatedConcept`**

In `internal/store/postgres/related.go`, replace:

```go
// RelatedConcept is a neighbour of a concept in the OKF link graph.
type RelatedConcept struct {
	ID        string
	Title     string
	Direction string // "out" = this concept links to it; "in" = it links here
}

// Related returns the concept's graph neighbours in both directions: outgoing
// links (concepts it references) and incoming links (concepts that reference
// it). It is read-only.
func (s *Store) Related(ctx context.Context, id string) ([]RelatedConcept, error) {
	const q = `
SELECT e.dst AS rid, COALESCE(c.title, ''), 'out'
  FROM edge e LEFT JOIN concept c ON c.id = e.dst
 WHERE e.src = $1
UNION
SELECT e.src AS rid, COALESCE(c.title, ''), 'in'
  FROM edge e LEFT JOIN concept c ON c.id = e.src
 WHERE e.dst = $1
 ORDER BY 3, 1`
	rows, err := s.pool.Query(ctx, q, id)
	if err != nil {
		return nil, fmt.Errorf("related query %q: %w", id, err)
	}
	defer rows.Close()

	var out []RelatedConcept
	for rows.Next() {
		var r RelatedConcept
		if err := rows.Scan(&r.ID, &r.Title, &r.Direction); err != nil {
			return nil, fmt.Errorf("scan related row: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate related rows: %w", err)
	}
	return out, nil
}
```

with:

```go
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
```

(Note the `ORDER BY 3, 1` → `ORDER BY 4, 1` change: `Type` was inserted as the 3rd selected column, pushing `Direction` to 4th — the ordering INTENT is unchanged, "order by direction then id", just the column position shifted.)

- [ ] **Step 6: Write the failing test for `Related`'s new `Type` field**

Create `internal/store/postgres/related_test.go`:

```go
package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelated_IncludesNeighbourType(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	applyTestSchema(t, dsn)
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	defer s.Close()

	_, err = s.pool.Exec(ctx, `
INSERT INTO concept (id, type, title, body, content_sha, first_epoch, last_epoch, updated_at) VALUES
  ('a.md', 'Reference',    'A', 'body', 'sha', 1, 1, now()),
  ('b.md', 'ApiEndpoint',  'B', 'body', 'sha', 1, 1, now())`)
	require.NoError(t, err)
	require.NoError(t, s.ReplaceEdges(ctx, "a.md", []string{"b.md"}))

	rel, err := s.Related(ctx, "a.md")
	require.NoError(t, err)
	require.Len(t, rel, 1)
	assert.Equal(t, "b.md", rel[0].ID)
	assert.Equal(t, "ApiEndpoint", rel[0].Type, "neighbour's type must be populated")
	assert.Equal(t, "out", rel[0].Direction)
}
```

- [ ] **Step 7: Run the full package test suite**

Run: `go test ./internal/store/postgres/... -v` (with `PIXKB_TEST_DSN` if available; without one, most of this package's tests skip — that's expected and matches existing convention, note it in your report).
Expected: PASS (or SKIP, consistently) — including every pre-existing test in the package (confirm `TestUpsertSTR_ThenGet`, `TestSearchISPB_*`, etc. are unaffected).

- [ ] **Step 8: Run `go build ./...` and `go vet ./...`**

- [ ] **Step 9: Commit**

```bash
git add internal/store/postgres/embedding.go internal/store/postgres/embedding_test.go internal/store/postgres/vector.go internal/store/postgres/related.go internal/store/postgres/related_test.go
git commit -m "feat: add GetEmbedding, fix Vector determinism, add RelatedConcept.Type"
```

---

### Task 2: `internal/similar` package — `SemanticSimilar` and `GraphSimilar`

**Files:**
- Create: `internal/similar/similar.go` (package doc + shared types + shared helpers)
- Create: `internal/similar/signals.go` (`SemanticSimilar`, `GraphSimilar`)
- Test: `internal/similar/signals_test.go`

**Interfaces:**
- Consumes: `query.Searcher` (embedded into a new `Store` interface), `postgres.Filter`/`Hit`/`RelatedConcept`, `Store.GetEmbedding`/`Store.Related` (Task 1).
- Produces: `type Hit struct { postgres.Hit; Why []string }`; `const SignalSemantic/SignalLexical/SignalGraph/SignalDomain`; `type Store interface { query.Searcher; GetEmbedding(...); Related(...) }`; `func SemanticSimilar(ctx, s Store, id string, f postgres.Filter) ([]Hit, error)`; `func GraphSimilar(ctx, s Store, id string, limit int) ([]Hit, error)`; `func withHeadroom(f postgres.Filter) postgres.Filter`; `func tagAndExclude(hits []postgres.Hit, excludeID, why string, limit int) []Hit` — Tasks 3-5 depend on `Hit`, `Store`, `withHeadroom`, `tagAndExclude`, and the `Signal*` constants.

- [ ] **Step 1: Write the failing tests**

Create `internal/similar/signals_test.go`:

```go
package similar

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/store/postgres"
)

// fakeStore is a minimal in-memory Store double for pure unit tests — no DB.
type fakeStore struct {
	embeddings map[string][]float32
	vecResults []postgres.Hit
	related    map[string][]postgres.RelatedConcept
}

func (f *fakeStore) FTS(_ context.Context, _ string, _ postgres.Filter) ([]postgres.Hit, error) {
	return nil, nil
}
func (f *fakeStore) Vector(_ context.Context, _ []float32, _ postgres.Filter) ([]postgres.Hit, error) {
	return f.vecResults, nil
}
func (f *fakeStore) GetEmbedding(_ context.Context, id string) ([]float32, error) {
	if v, ok := f.embeddings[id]; ok {
		return v, nil
	}
	return nil, errors.New("not found")
}
func (f *fakeStore) Related(_ context.Context, id string) ([]postgres.RelatedConcept, error) {
	return f.related[id], nil
}

func TestSemanticSimilar_ExcludesSelfAndTagsSignal(t *testing.T) {
	t.Parallel()
	s := &fakeStore{
		embeddings: map[string][]float32{"a.md": {1, 0, 0}},
		vecResults: []postgres.Hit{
			{ID: "a.md", Title: "A", Rank: 1}, // the queried concept itself — cosine 1.0, must be excluded
			{ID: "b.md", Title: "B", Rank: 2},
			{ID: "c.md", Title: "C", Rank: 3},
		},
	}
	got, err := SemanticSimilar(context.Background(), s, "a.md", postgres.Filter{Limit: 2})
	require.NoError(t, err)
	require.Len(t, got, 2, "self excluded, 2 of the remaining returned")
	assert.Equal(t, "b.md", got[0].ID)
	assert.Equal(t, 1, got[0].Rank, "rank renumbered after exclusion")
	assert.Equal(t, []string{SignalSemantic}, got[0].Why)
	assert.Equal(t, "c.md", got[1].ID)
	assert.Equal(t, 2, got[1].Rank)
}

func TestSemanticSimilar_PropagatesGetEmbeddingError(t *testing.T) {
	t.Parallel()
	s := &fakeStore{embeddings: map[string][]float32{}}
	_, err := SemanticSimilar(context.Background(), s, "missing.md", postgres.Filter{})
	require.Error(t, err)
}

func TestGraphSimilar_TagsSignalAndExcludesSelf(t *testing.T) {
	t.Parallel()
	s := &fakeStore{related: map[string][]postgres.RelatedConcept{
		"a.md": {
			{ID: "a.md", Title: "A (self-loop, must be excluded)", Direction: "out"},
			{ID: "b.md", Title: "B", Type: "ApiEndpoint", Direction: "out"},
		},
	}}
	got, err := GraphSimilar(context.Background(), s, "a.md", 20)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "b.md", got[0].ID)
	assert.Equal(t, "ApiEndpoint", got[0].Type)
	assert.Equal(t, []string{SignalGraph}, got[0].Why)
}

func TestGraphSimilar_RespectsLimit(t *testing.T) {
	t.Parallel()
	s := &fakeStore{related: map[string][]postgres.RelatedConcept{
		"a.md": {
			{ID: "b.md", Direction: "out"},
			{ID: "c.md", Direction: "out"},
			{ID: "d.md", Direction: "out"},
		},
	}}
	got, err := GraphSimilar(context.Background(), s, "a.md", 2)
	require.NoError(t, err)
	assert.Len(t, got, 2)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/similar/... -v`
Expected: FAIL — package `internal/similar` doesn't exist yet (build error: no such package / undefined symbols).

- [ ] **Step 3: Write the implementation**

Create `internal/similar/similar.go`:

```go
// Package similar implements concept-to-concept similarity search
// (docs/SEARCH-CAPABILITY-SPEC.md Feature 2): given a known concept id,
// surface nearby concepts using multiple independent signals — semantic
// (embedding similarity), lexical (shared terms via the existing hybrid
// search), graph (direct link-graph neighbours), and domain (type-pair
// adjacency) — each result tagged with which signal(s) found it. Every
// signal reuses an existing, unmodified retrieval primitive
// (postgres.Store.Vector, query.Hybrid, postgres.Store.Related); this
// package only adds self-exclusion, signal tagging, and cross-signal fusion.
package similar

import (
	"context"

	"pixkb/internal/query"
	"pixkb/internal/store/postgres"
)

// Signal names why a concept was surfaced as similar to the queried one.
const (
	SignalSemantic = "semantic"
	SignalLexical  = "lexical"
	SignalGraph    = "graph"
	SignalDomain   = "domain"
)

// Hit is one similarity result: the underlying concept plus which signal(s)
// surfaced it. Rank always reflects THIS package's own re-ranking after
// self-exclusion (or, for hybrid mode, the cross-signal fused rank) — never
// a raw Vector/Hybrid rank taken verbatim.
type Hit struct {
	postgres.Hit
	Why []string
}

// Store is the subset of *postgres.Store similar needs — query.Searcher
// (FTS, Vector) plus the two new accessors from Task 1. An interface so this
// package is unit-testable with a fake, matching internal/query.Searcher's
// pattern; *postgres.Store satisfies it directly.
type Store interface {
	query.Searcher
	GetEmbedding(ctx context.Context, id string) ([]float32, error)
	Related(ctx context.Context, id string) ([]postgres.RelatedConcept, error)
}

// defaultLimit mirrors postgres.defaultLimit / query.Hybrid's fallback.
const defaultLimit = 20

// withHeadroom returns a copy of f with Limit bumped by one extra slot — the
// queried concept itself is typically the #1 raw hit (cosine similarity ~1.0
// against its own embedding, or a top lexical match against its own title),
// so fetching one extra result before self-exclusion keeps the final count
// at the caller's requested Limit instead of silently returning one short.
func withHeadroom(f postgres.Filter) postgres.Filter {
	out := f
	if out.Limit <= 0 {
		out.Limit = defaultLimit
	}
	out.Limit++
	return out
}

// tagAndExclude drops excludeID from hits, tags every remaining hit with why,
// truncates to limit, and renumbers Rank 1..N over the surviving hits (never
// the raw pre-exclusion rank, which would have a gap where excludeID was).
func tagAndExclude(hits []postgres.Hit, excludeID, why string, limit int) []Hit {
	if limit <= 0 {
		limit = defaultLimit
	}
	out := make([]Hit, 0, len(hits))
	for _, h := range hits {
		if h.ID == excludeID {
			continue
		}
		out = append(out, Hit{Hit: h, Why: []string{why}})
		if len(out) >= limit {
			break
		}
	}
	for i := range out {
		out[i].Rank = i + 1
	}
	return out
}
```

Create `internal/similar/signals.go`:

```go
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
// tagged with the graph signal. Related() edges should never be self-loops,
// but a defensive exclude-self is cheap and correct regardless of that
// invariant holding.
func GraphSimilar(ctx context.Context, s Store, id string, limit int) ([]Hit, error) {
	rel, err := s.Related(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("similar: related for %q: %w", id, err)
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	out := make([]Hit, 0, len(rel))
	for _, r := range rel {
		if r.ID == id {
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/similar/... -v`
Expected: PASS for all 4 tests.

- [ ] **Step 5: Run `go build ./...` and `go vet ./...`**

- [ ] **Step 6: Commit**

```bash
git add internal/similar/similar.go internal/similar/signals.go internal/similar/signals_test.go
git commit -m "feat: add internal/similar package with SemanticSimilar and GraphSimilar"
```

---

### Task 3: `MoreLikeThis` signal

**Files:**
- Create: `internal/similar/morelikethis.go`
- Test: `internal/similar/morelikethis_test.go`

**Interfaces:**
- Consumes: `Hit`, `Store`, `withHeadroom`, `tagAndExclude`, `Signal*` (Task 2); `query.Hybrid` (unmodified); `okf.Concept`, `okf.ReadConcept` (existing); `embed.Embedder` (existing).
- Produces: `func MoreLikeThis(ctx context.Context, s Store, emb embed.Embedder, bundleDir, id string, f postgres.Filter) ([]Hit, error)` — Task 5 (hybrid dispatcher) calls this directly.

- [ ] **Step 1: Write the failing test**

Create `internal/similar/morelikethis_test.go`:

```go
package similar

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/embed"
	"pixkb/internal/store/postgres"
)

// queryAwareStore differs its FTS result by the exact query string it
// receives — needed to prove MoreLikeThis builds its synthetic query from
// the concept's own fields rather than passing the concept id through as-is.
type queryAwareStore struct {
	fts map[string][]postgres.Hit
}

func (q *queryAwareStore) FTS(_ context.Context, query string, _ postgres.Filter) ([]postgres.Hit, error) {
	return q.fts[query], nil
}
func (q *queryAwareStore) Vector(_ context.Context, _ []float32, _ postgres.Filter) ([]postgres.Hit, error) {
	return nil, nil
}
func (q *queryAwareStore) GetEmbedding(_ context.Context, _ string) ([]float32, error) {
	return nil, nil
}
func (q *queryAwareStore) Related(_ context.Context, _ string) ([]postgres.RelatedConcept, error) {
	return nil, nil
}

func writeTestConcept(t *testing.T, dir, id, title, intentTerms, body string) {
	t.Helper()
	full := filepath.Join(dir, id)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	fm := "---\ntype: Reference\ntitle: " + title + "\n"
	if intentTerms != "" {
		fm += "intent_terms: " + intentTerms + "\n"
	}
	fm += "---\n"
	require.NoError(t, os.WriteFile(full, []byte(fm+body), 0o644))
}

func TestMoreLikeThis_BuildsSyntheticQueryFromConceptFields(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestConcept(t, dir, "a.md", "Refund Concept", "estorno devolucao", "Explains how Pix refunds work end to end.")

	wantQuery := "Refund Concept estorno devolucao Explains how Pix refunds work end to end."
	s := &queryAwareStore{fts: map[string][]postgres.Hit{
		wantQuery: {
			{ID: "a.md", Title: "Refund Concept"}, // self — must be excluded
			{ID: "b.md", Title: "Related Endpoint", Arm: "fts"},
		},
	}}
	got, err := MoreLikeThis(context.Background(), s, embed.NewHashing(8), dir, "a.md", postgres.Filter{})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "b.md", got[0].ID)
	assert.Equal(t, []string{SignalLexical}, got[0].Why, "Arm=fts must map to lexical")
}

func TestMoreLikeThis_MapsArmToWhy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		arm  string
		want []string
	}{
		{"fts", []string{SignalLexical}},
		{"vector", []string{SignalSemantic}},
		{"both", []string{SignalSemantic, SignalLexical}},
		{"", nil},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, whyFromArm(c.arm), "arm=%q", c.arm)
	}
}

func TestMoreLikeThis_ReadConceptErrorPropagates(t *testing.T) {
	t.Parallel()
	s := &queryAwareStore{}
	_, err := MoreLikeThis(context.Background(), s, embed.NewHashing(8), t.TempDir(), "does-not-exist.md", postgres.Filter{})
	require.Error(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/similar/... -run TestMoreLikeThis -v`
Expected: FAIL — `MoreLikeThis`/`whyFromArm` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/similar/morelikethis.go`:

```go
package similar

import (
	"context"
	"fmt"
	"path/filepath"

	"pixkb/internal/embed"
	"pixkb/internal/okf"
	"pixkb/internal/query"
	"pixkb/internal/store/postgres"
)

// mltBodyChars caps how much of a concept's body feeds the synthetic
// more-like-this query — the goal is topical signal for retrieval, not a
// full-body echo (which would just re-run search on near-arbitrary length
// text and cost more without adding recall quality).
const mltBodyChars = 500

// MoreLikeThis builds a synthetic query from id's own title, intent_terms,
// and a body excerpt, then runs it through the existing, unmodified
// query.Hybrid — reusing precise/fuzzy-safe ranking rather than inventing a
// second one. Each hit's Why is derived from Hybrid's own Arm field
// (fts->lexical, vector->semantic, both->both), so more-like-this directly
// reuses the multi-query-retrieval plan's Score/Arm provenance work.
func MoreLikeThis(ctx context.Context, s Store, emb embed.Embedder, bundleDir, id string, f postgres.Filter) ([]Hit, error) {
	c, err := okf.ReadConcept(filepath.Join(bundleDir, filepath.FromSlash(id)), bundleDir)
	if err != nil {
		return nil, fmt.Errorf("similar: read concept %q: %w", id, err)
	}

	q := c.Title
	if c.IntentTerms != "" {
		q += " " + c.IntentTerms
	}
	q += " " + truncate(c.Body, mltBodyChars)

	hits, err := query.Hybrid(ctx, s, emb, q, withHeadroom(f))
	if err != nil {
		return nil, fmt.Errorf("similar: more-like-this hybrid search for %q: %w", id, err)
	}

	limit := f.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	out := make([]Hit, 0, len(hits))
	for _, h := range hits {
		if h.ID == id {
			continue
		}
		out = append(out, Hit{Hit: h, Why: whyFromArm(h.Arm)})
		if len(out) >= limit {
			break
		}
	}
	for i := range out {
		out[i].Rank = i + 1
	}
	return out, nil
}

// truncate returns body's first n bytes, or the whole string if shorter.
func truncate(body string, n int) string {
	if len(body) <= n {
		return body
	}
	return body[:n]
}

// whyFromArm maps query.Hybrid's Arm field ("fts"/"vector"/"both"/"") onto
// this package's Signal constants.
func whyFromArm(arm string) []string {
	switch arm {
	case "both":
		return []string{SignalSemantic, SignalLexical}
	case "vector":
		return []string{SignalSemantic}
	case "fts":
		return []string{SignalLexical}
	default:
		return nil
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/similar/... -v`
Expected: PASS for all tests in the package (Task 2's + Task 3's).

- [ ] **Step 5: Run `go build ./...` and `go vet ./...`**

- [ ] **Step 6: Commit**

```bash
git add internal/similar/morelikethis.go internal/similar/morelikethis_test.go
git commit -m "feat: add MoreLikeThis similarity signal reusing query.Hybrid"
```

---

### Task 4: Domain signal — type-pair adjacency tagging

**Files:**
- Create: `internal/similar/domain.go`
- Test: `internal/similar/domain_test.go`

**Interfaces:**
- Consumes: `Hit`, `Signal*` (Task 2).
- Produces: `func tagDomain(hits []Hit, queryType string)` (mutates `hits` in place, appending `SignalDomain` to `Why` where applicable) — Task 5 calls this after fusing the other signals.

- [ ] **Step 1: Write the failing test**

Create `internal/similar/domain_test.go`:

```go
package similar

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"pixkb/internal/store/postgres"
)

func TestTagDomain_AppendsDomainSignalForAdjacentTypes(t *testing.T) {
	t.Parallel()
	hits := []Hit{
		{Hit: postgres.Hit{ID: "a", Type: "PacsMessage"}, Why: []string{SignalSemantic}},
		{Hit: postgres.Hit{ID: "b", Type: "ManualSection"}, Why: []string{SignalLexical}},
	}
	tagDomain(hits, "ApiEndpoint") // querying from an ApiEndpoint concept

	assert.Equal(t, []string{SignalSemantic, SignalDomain}, hits[0].Why, "PacsMessage is domain-adjacent to ApiEndpoint")
	assert.Equal(t, []string{SignalLexical}, hits[1].Why, "ManualSection is NOT domain-adjacent to ApiEndpoint")
}

func TestTagDomain_UnknownQueryTypeIsNoOp(t *testing.T) {
	t.Parallel()
	hits := []Hit{{Hit: postgres.Hit{ID: "a", Type: "PacsMessage"}, Why: []string{SignalSemantic}}}
	tagDomain(hits, "SomeUnmappedType")
	assert.Equal(t, []string{SignalSemantic}, hits[0].Why, "unmapped query type must not panic or mutate Why")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/similar/... -run TestTagDomain -v`
Expected: FAIL — `tagDomain` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/similar/domain.go`:

```go
package similar

// domainAdjacency maps a concept type to the OTHER types considered
// domain-adjacent to it for Feature 2's "domain" signal — e.g. an API
// endpoint is domain-adjacent to the ISO messages and reference docs that
// describe the same Pix workflow. This is intentionally a SMALL, FIXED v1
// table at type-pair granularity only — it re-tags candidates already
// surfaced by another signal (semantic/lexical/graph), it does not run an
// independent corpus scan. The spec's prose examples are finer-grained than
// type pairs ("DICT endpoints near key concepts" specifically, not just
// "ApiEndpoint near Reference" generally) — that level of topic-specific
// domain rule is out of scope for this v1 and is backlogged (see
// docs/BACKLOG.md); adding entries here should be measured the same way the
// multi-query-retrieval plan's entityTriggers table was (a rule that looks
// right in a spot-check is not the same as one that holds up measured).
var domainAdjacency = map[string][]string{
	"ApiEndpoint":   {"PacsMessage", "CamtMessage", "Reference", "ManualSection"},
	"PacsMessage":   {"ApiEndpoint", "CamtMessage"},
	"CamtMessage":   {"ApiEndpoint", "PacsMessage"},
	"Reference":     {"ApiEndpoint", "ManualSection"},
	"ManualSection": {"ApiEndpoint", "Reference"},
}

// tagDomain appends SignalDomain to the Why of every hit in hits whose Type
// is domain-adjacent to queryType, mutating hits in place. A queryType with
// no table entry is a no-op (not an error) — most concept types (e.g.
// "WebPage") simply have no domain-adjacency rule yet.
func tagDomain(hits []Hit, queryType string) {
	adj := domainAdjacency[queryType]
	if len(adj) == 0 {
		return
	}
	adjSet := make(map[string]bool, len(adj))
	for _, t := range adj {
		adjSet[t] = true
	}
	for i := range hits {
		if adjSet[hits[i].Type] {
			hits[i].Why = append(hits[i].Why, SignalDomain)
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/similar/... -v`
Expected: PASS for all tests in the package.

- [ ] **Step 5: Run `go build ./...` and `go vet ./...`**

- [ ] **Step 6: Commit**

```bash
git add internal/similar/domain.go internal/similar/domain_test.go
git commit -m "feat: add type-pair domain-adjacency signal tagging"
```

---

### Task 5: `Similar()` dispatcher — 4 modes, hybrid fusion

**Files:**
- Create: `internal/similar/dispatch.go`
- Test: `internal/similar/dispatch_test.go`

**Interfaces:**
- Consumes: `SemanticSimilar`, `GraphSimilar` (Task 2), `MoreLikeThis` (Task 3), `tagDomain` (Task 4), `Hit`, `Store`, `Signal*` (Task 2), `okf.ReadConcept`, `query.rrfK`-equivalent (define locally, do not import an unexported constant across packages — see Step 3).
- Produces: `type Options struct { Mode string; IncludeGraph bool; Filter postgres.Filter }`; `func Similar(ctx context.Context, s Store, emb embed.Embedder, bundleDir, id string, opts Options) ([]Hit, error)` — Tasks 6 (CLI) and 7 (MCP) call this directly as the single entry point.

- [ ] **Step 1: Write the failing test**

Create `internal/similar/dispatch_test.go`:

```go
package similar

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/embed"
	"pixkb/internal/store/postgres"
)

func TestSimilar_UnknownModeErrors(t *testing.T) {
	t.Parallel()
	s := &queryAwareStore{}
	_, err := Similar(context.Background(), s, embed.NewHashing(8), t.TempDir(), "a.md", Options{Mode: "not-a-real-mode"})
	require.Error(t, err)
}

func TestSimilar_SemanticModeDelegates(t *testing.T) {
	t.Parallel()
	s := &fakeStore{
		embeddings: map[string][]float32{"a.md": {1, 0, 0}},
		vecResults: []postgres.Hit{{ID: "a.md"}, {ID: "b.md"}},
	}
	got, err := Similar(context.Background(), s, embed.NewHashing(8), t.TempDir(), "a.md", Options{Mode: "semantic"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "b.md", got[0].ID)
}

func TestSimilar_GraphModeDelegates(t *testing.T) {
	t.Parallel()
	s := &fakeStore{related: map[string][]postgres.RelatedConcept{
		"a.md": {{ID: "b.md", Direction: "out"}},
	}}
	got, err := Similar(context.Background(), s, embed.NewHashing(8), t.TempDir(), "a.md", Options{Mode: "graph"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "b.md", got[0].ID)
}

func TestSimilar_HybridMode_FusesSemanticAndMoreLikeThisAndTagsDomain(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestConcept(t, dir, "a.md", "Refund Concept", "estorno", "Pix refund details.")
	mltQuery := "Refund Concept estorno Pix refund details."

	s := &hybridFakeStore{
		embeddings: map[string][]float32{"a.md": {1, 0, 0}},
		vecResults: []postgres.Hit{{ID: "a.md"}, {ID: "endpoint.md", Type: "ApiEndpoint"}},
		fts:        map[string][]postgres.Hit{mltQuery: {{ID: "endpoint.md", Type: "ApiEndpoint"}}},
		related:    map[string][]postgres.RelatedConcept{"a.md": {{ID: "neighbour.md", Type: "Reference", Direction: "out"}}},
	}

	got, err := Similar(context.Background(), s, embed.NewHashing(8), dir, "a.md", Options{
		Mode: "hybrid", IncludeGraph: true, Filter: postgres.Filter{Limit: 10},
	})
	require.NoError(t, err)
	require.NotEmpty(t, got)

	byID := map[string]Hit{}
	for _, h := range got {
		byID[h.ID] = h
	}
	require.Contains(t, byID, "endpoint.md")
	// endpoint.md was found by BOTH semantic (Vector) and lexical (FTS via
	// MoreLikeThis) -> multiple Why entries, PLUS domain: writeTestConcept
	// always writes "type: Reference" in its frontmatter, and ApiEndpoint IS
	// domain-adjacent to Reference per domainAdjacency (domain.go).
	assert.Contains(t, byID["endpoint.md"].Why, SignalDomain)
	require.Contains(t, byID, "neighbour.md")
	assert.Contains(t, byID["neighbour.md"].Why, SignalGraph)
	assert.NotContains(t, byID, "a.md", "queried concept excluded from hybrid results too")
}

// hybridFakeStore combines fakeStore's embedding/vector/related behavior with
// queryAwareStore's query-string-keyed FTS, since hybrid mode exercises all
// three signal paths in one call.
type hybridFakeStore struct {
	embeddings map[string][]float32
	vecResults []postgres.Hit
	fts        map[string][]postgres.Hit
	related    map[string][]postgres.RelatedConcept
}

func (h *hybridFakeStore) FTS(_ context.Context, q string, _ postgres.Filter) ([]postgres.Hit, error) {
	return h.fts[q], nil
}
func (h *hybridFakeStore) Vector(_ context.Context, _ []float32, _ postgres.Filter) ([]postgres.Hit, error) {
	return h.vecResults, nil
}
func (h *hybridFakeStore) GetEmbedding(_ context.Context, id string) ([]float32, error) {
	return h.embeddings[id], nil
}
func (h *hybridFakeStore) Related(_ context.Context, id string) ([]postgres.RelatedConcept, error) {
	return h.related[id], nil
}

func TestSimilar_Deterministic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestConcept(t, dir, "a.md", "X", "", "body text")
	s := &hybridFakeStore{
		embeddings: map[string][]float32{"a.md": {1, 0, 0}},
		vecResults: []postgres.Hit{{ID: "a.md"}, {ID: "b.md"}},
		fts:        map[string][]postgres.Hit{"X body text": {{ID: "b.md"}}},
	}
	opts := Options{Mode: "hybrid", Filter: postgres.Filter{Limit: 10}}
	got1, err := Similar(context.Background(), s, embed.NewHashing(8), dir, "a.md", opts)
	require.NoError(t, err)
	got2, err := Similar(context.Background(), s, embed.NewHashing(8), dir, "a.md", opts)
	require.NoError(t, err)
	require.Equal(t, len(got1), len(got2))
	for i := range got1 {
		assert.Equal(t, got1[i].ID, got2[i].ID, "index %d", i)
		assert.Equal(t, got1[i].Rank, got2[i].Rank, "index %d", i)
	}
}
```

(`writeTestConcept` is defined in `morelikethis_test.go`, Task 3 — same package, reused directly. `fakeStore` is defined in `signals_test.go`, Task 2 — same package, reused directly.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/similar/... -run TestSimilar -v`
Expected: FAIL — `Similar`/`Options` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/similar/dispatch.go`:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/similar/... -v`
Expected: PASS for every test in the package (Tasks 2-5 combined).

- [ ] **Step 5: Run `go build ./...` and `go vet ./...`**

- [ ] **Step 6: Commit**

```bash
git add internal/similar/dispatch.go internal/similar/dispatch_test.go
git commit -m "feat: add Similar() dispatcher with hybrid signal fusion"
```

---

### Task 6: CLI — `pixkb similar <concept-id>`

**Files:**
- Modify: `cmd/pixkb/commands.go` (add `newSimilarCmd`, register it in `attachCommands`)

**Interfaces:**
- Consumes: `similar.Similar`, `similar.Options` (Task 5).
- Produces: nothing new consumed elsewhere — leaf CLI command.

- [ ] **Step 1: Add the command**

In `cmd/pixkb/commands.go`, add the import `"pixkb/internal/similar"` to the existing import block, then add this function (place it near `newRelatedCmd` for locality):

```go
func newSimilarCmd() *cobra.Command {
	var dsn, mode, typ, tag string
	var limit int
	var includeGraph bool
	cmd := &cobra.Command{
		Use:   "similar <concept-id>",
		Short: "Find concepts similar to a known concept (semantic, graph, hybrid, or more-like-this)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			if dsn != "" {
				cfg.DSN = dsn
			}
			ctx := cmd.Context()
			st, err := openStore(ctx, cfg)
			if err != nil {
				return err
			}
			defer st.Close()
			emb, err := newEmbedder(cfg)
			if err != nil {
				return err
			}

			opts := similar.Options{
				Mode:         mode,
				IncludeGraph: includeGraph,
				Filter:       postgres.Filter{Type: typ, Tag: tag, Limit: limit},
			}
			hits, err := similar.Similar(ctx, st, emb, cfg.BundleDir, args[0], opts)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, h := range hits {
				_, _ = fmt.Fprintf(out, "%2d  %-34s  %-14s  %v\n", h.Rank, h.ID, h.Type, h.Why)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN")
	cmd.Flags().StringVar(&mode, "mode", "hybrid", "similarity mode: hybrid|semantic|graph|more-like-this")
	cmd.Flags().StringVar(&typ, "type", "", "filter results by concept type")
	cmd.Flags().StringVar(&tag, "tag", "", "filter results by tag")
	cmd.Flags().IntVar(&limit, "limit", 20, "max results")
	cmd.Flags().BoolVar(&includeGraph, "include-graph", true, "hybrid mode: also fold in direct graph neighbours")
	return cmd
}
```

(`cfg.BundleDir` is confirmed correct — `cmd/pixkb/config.go:25`, `yaml:"bundle_dir"`, already used the same way at `config.go:192`'s `epoch.Runner{Bundle: cfg.BundleDir, ...}`.)

Update `attachCommands`:

```go
func attachCommands(root *cobra.Command) {
	root.AddCommand(newIngestCmd(), newSearchCmd(), newReindexCmd(), newDiffCmd(), newStatsCmd(), newRelatedCmd(), newSimilarCmd(), newAgentsCmd(), newConceptCmd(), newMCPCmd(), newHygieneCmd(), newCurateCmd(), newQRCmd(), newAskCmd(), newISPBCmd())
}
```

(Only `newSimilarCmd()` is added to the existing list — do not reorder or remove any other command.)

- [ ] **Step 2: Build and smoke-test manually**

No new automated test — `newSimilarCmd` needs a live Postgres store, same as `newSearchCmd`/`newRelatedCmd`, neither of which have CLI-level unit tests either (see the prior plan's Task 4 for the identical, already-established rationale in this codebase).

Run: `go build ./...` then, against your dev DB:

```bash
pixkb similar "reference/bacen-pix-concepts/03-requisitos-de-seguran-a-pix-mtls-e-certificados.md" --mode hybrid
pixkb similar "reference/bacen-pix-concepts/03-requisitos-de-seguran-a-pix-mtls-e-certificados.md" --mode semantic
pixkb similar "reference/bacen-pix-concepts/03-requisitos-de-seguran-a-pix-mtls-e-certificados.md" --mode graph
```

Expected: all three run without error; hybrid/semantic never include the queried concept's own id in the output.

- [ ] **Step 3: Commit**

```bash
git add cmd/pixkb/commands.go
git commit -m "feat: add pixkb similar <concept-id> CLI command"
```

---

### Task 7: MCP — `similar` tool

**Files:**
- Modify: `internal/kbmcp/server.go` (add `similarIn`/`similarOut`/`registerSimilar`, register it in `NewServer`)

**Interfaces:**
- Consumes: `similar.Similar`, `similar.Options` (Task 5).
- Produces: nothing new consumed elsewhere — leaf MCP tool.

- [ ] **Step 1: Add the tool**

In `internal/kbmcp/server.go`, add the import `"pixkb/internal/similar"` to the existing import block, then add (place it near `registerRelated`):

```go
type similarIn struct {
	ID           string `json:"id" jsonschema:"concept id (bundle-relative path) to find similar concepts for"`
	Mode         string `json:"mode,omitempty" jsonschema:"semantic|graph|hybrid (default)|more-like-this"`
	Type         string `json:"type,omitempty" jsonschema:"optional concept-type filter on results"`
	Limit        int    `json:"limit,omitempty" jsonschema:"max hits (default 20)"`
	// ExcludeGraph, not IncludeGraph: plain JSON bools can't distinguish
	// "omitted" from "explicitly false", so the field is named/phrased so its
	// zero value (false, or omitted entirely) IS the desired default — hybrid
	// mode includes graph neighbours unless a caller explicitly opts out.
	ExcludeGraph bool `json:"exclude_graph,omitempty" jsonschema:"hybrid mode: set true to exclude direct graph neighbours (default: graph included)"`
}
type similarHitOut struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Type  string   `json:"type"`
	Score float64  `json:"score"`
	Rank  int      `json:"rank"`
	Why   []string `json:"why"`
}
type similarOut struct {
	Hits []similarHitOut `json:"hits"`
}

// registerSimilar exposes concept-to-concept similarity: given a known
// concept id, ranked nearby concepts tagged with why each one matched
// (semantic/lexical/graph/domain). Mirrors registerSearch's shape.
func registerSimilar(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "similar",
		Description: "Find concepts similar to a known concept id, tagged with why each result matched (semantic, lexical, graph, domain). Modes: semantic, graph, hybrid (default), more-like-this.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in similarIn) (*mcp.CallToolResult, similarOut, error) {
		mode := in.Mode
		if mode == "" {
			mode = "hybrid"
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 20
		}
		opts := similar.Options{
			Mode:         mode,
			IncludeGraph: !in.ExcludeGraph,
			Filter:       postgres.Filter{Type: in.Type, Limit: limit},
		}
		hits, err := similar.Similar(ctx, d.Store, d.Emb, d.Bundle, in.ID, opts)
		if err != nil {
			return nil, similarOut{}, err
		}
		out := similarOut{Hits: make([]similarHitOut, 0, len(hits))}
		for _, h := range hits {
			out.Hits = append(out.Hits, similarHitOut{ID: h.ID, Title: h.Title, Type: h.Type, Score: h.Score, Rank: h.Rank, Why: h.Why})
		}
		return textResult(fmt.Sprintf("%d concepts similar to %s", len(out.Hits), in.ID)), out, nil
	})
}
```

Register it in `NewServer` (in the same function that already has `registerSearch(s, d)`, `registerRelated(s, d)`, etc.):

```go
	registerSearch(s, d)
	registerRelated(s, d)
	registerSimilar(s, d)
	registerStats(s, d)
```

(Insert `registerSimilar(s, d)` right after `registerRelated(s, d)` — do not reorder or remove any other registration.)

- [ ] **Step 2: Write the test**

Add to `internal/kbmcp/server_test.go`, mirroring the exact DSN/skip boilerplate already used by `TestServerReadTools`/`TestServerSearch_MultiMode` in the same file:

```go
// TestServerSimilar_HybridMode exercises the similar tool over an in-memory
// MCP transport against a live KB. Skipped without a DSN or under -short,
// same pattern as TestServerReadTools.
func TestServerSimilar_HybridMode(t *testing.T) {
	if testing.Short() {
		t.Skip("needs a live Postgres KB")
	}
	dsn := os.Getenv("PIXKB_TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("PIXKB_DSN")
	}
	if dsn == "" {
		t.Skip("no PIXKB_TEST_DSN/PIXKB_DSN set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	st, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	srv := NewServer(Deps{Store: st, Emb: embed.NewHashing(256), Bundle: "../../kb-data"})
	serverTr, clientTr := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, serverTr, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientTr, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer func() { _ = cs.Close() }()

	// Search first to get a real concept id to query similarity against.
	sres, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "search", Arguments: map[string]any{"query": "criar cobrança imediata", "limit": 1}})
	if err != nil || sres.IsError {
		t.Fatalf("search call: err=%v isErr=%v", err, sres.IsError)
	}

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "similar",
		Arguments: map[string]any{"id": "api/openapi/post-cob.md", "mode": "hybrid", "limit": 5},
	})
	if err != nil || res.IsError {
		t.Fatalf("similar call: err=%v isErr=%v", err, res.IsError)
	}
	if len(res.Content) == 0 {
		t.Fatal("similar returned no content")
	}
}
```

Check the exact `Deps` field names (`Store`, `Emb`, `Bundle`) against `internal/kbmcp/server.go`'s current `Deps` struct before using them — they should already match `TestServerReadTools`'s existing usage in this same file; copy from there if in doubt.

- [ ] **Step 3: Run tests, build, vet**

Run: `go test ./internal/kbmcp/... -run 'TestServerReadTools|TestServerSimilar_HybridMode' -v` (PASS or SKIP consistently, matching the prior plan's Task 5 convention), then `go build ./...`, `go vet ./...`.

- [ ] **Step 4: Commit**

```bash
git add internal/kbmcp/server.go internal/kbmcp/server_test.go
git commit -m "feat: add similar MCP tool"
```

---

### Task 8: Manual verification against the live KB + backlog follow-ups

**Files:** none modified — this task runs the built binary against the live KB and records backlog items. Same shape as the prior plan's Task 6 (eval verification gate), scaled down: there is no existing curated "expected similar concepts" gold set to gate against (unlike `eval/cases-precise-ids.tsv` for search), so this task is a live spot-check, not an automated regression gate.

- [ ] **Step 1: Build and spot-check**

```bash
go build -o bin/pixkb.exe ./cmd/pixkb
pixkb similar "reference/bacen-pix-concepts/03-requisitos-de-seguran-a-pix-mtls-e-certificados.md" --mode hybrid
pixkb similar "api/openapi/put-pix-e2eid-devolucao-id.md" --mode hybrid
pixkb similar "messages/pacs.008.md" --mode hybrid
```

For each, manually judge (this is a human/agent judgment call, not a scripted pass/fail):
- Is the queried concept itself absent from its own results? (Hard requirement — if present, Task 5's self-exclusion has a bug, stop and fix before proceeding.)
- Do the top few results look genuinely related (not random)?
- Does at least one case show a result tagged with more than one `Why` (proving fusion is actually combining signals, not just concatenating one signal's list)?
- Does at least one case show `domain` in `Why` for a type-adjacent result (e.g. the mTLS reference concept should surface API endpoints or manual sections with `domain` in their `Why`)?

- [ ] **Step 2: Compare modes on the same concept**

```bash
pixkb similar "messages/pacs.008.md" --mode semantic
pixkb similar "messages/pacs.008.md" --mode graph
pixkb similar "messages/pacs.008.md" --mode more-like-this
```

Confirm the four modes give visibly different (not identical) result sets for the same input — if `semantic` and `hybrid` return byte-identical output every time, something in the fusion is likely only picking up one signal; investigate before marking this task complete.

- [ ] **Step 3: Record backlog follow-ups**

Add to `docs/BACKLOG.md` (P2, do not build now):
- A proper curated `eval/cases-similar-ids.tsv` gold set (concept id → expected similar id(s)) plus a `tophit.sh`-equivalent harness for concept-to-concept similarity, so future changes to `domainAdjacency`/fusion weights are measured, not spot-checked. This plan's Task 8 is a one-time manual check, not a standing regression gate — a real gap versus how the multi-query-retrieval plan is gated.
- Finer-grained, topic-specific domain rules (e.g. "DICT endpoints near key concepts specifically," not just "ApiEndpoint near Reference generally") — the spec's fuller vision for the domain signal; this plan's `domainAdjacency` table is intentionally type-pair-only.
- Surfacing `Hit.Why` and per-signal scores in a dedicated CLI `--json`/explanation view (Feature 3, "Search Explanation" — same deferral the multi-query-retrieval plan already recorded for its own provenance).
- Wiring `similar.Similar` into RAG grounding as an additional evidence-diversification source (Feature 5), alongside the already-backlogged multi-query-retrieval wiring.

- [ ] **Step 4: Commit the backlog note**

```bash
git add docs/BACKLOG.md
git commit -m "docs: record concept-similarity spot-check results and backlog follow-ups"
```
