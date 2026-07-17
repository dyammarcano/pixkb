# Multi-Query Retrieval Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement Feature 1 ("Multi-Query Retrieval") of `docs/SEARCH-CAPABILITY-SPEC.md`: expand one user query into a small, deterministic set of subqueries, run pixkb's existing hybrid search for each, and fuse the ranked lists into one result set with per-hit provenance (which subquery matched, which arm, per-query and final rank).

**Architecture:** A new `query.ExpandQuery(q string) []string` deterministically expands a query (original + a stopword/diacritic-folded rewrite + up to a few domain-entity subqueries from a small fixed keyword table). A new `query.MultiHybrid(...)` calls the existing, untouched `query.Hybrid(...)` once per subquery — never re-implementing FTS/vector ranking — and fuses the per-subquery ranked lists with a second RRF pass, carrying provenance on each `MultiHit`. `query.Hybrid` itself gains two additive fields on its output (`Hit.Score`, `Hit.Arm`) that `MultiHybrid` needs and that were previously computed internally but never surfaced (a small pre-existing gap: `Hit.Score` was always left at its zero value). CLI (`pixkb search --mode multi`) and MCP (`search` tool, `mode: "multi"`) get thin wiring on top; both already share the exact `postgres.Hit` shape multi-query returns via a `query.Hits()` unwrap helper.

**Tech Stack:** Go 1.25, `internal/query` (pure, DB-free via the `Searcher` interface + `fakeSearcher`/`queryAwareSearcher` test doubles), `internal/store/postgres` (Hit/Filter types), `internal/kbmcp` (MCP tool), `cmd/pixkb` (CLI), `eval/tophit.sh` + `eval/cases-{precise,fuzzy}-ids.tsv` (deterministic ranking regression gate).

## Global Constraints

- Go 1.25.0, module `pixkb`, `CGO_ENABLED=0` — pure Go only (air-gapped project).
- **Do not modify FTS or vector ranking math itself.** ADR `docs/adr/0002-recall-tuning-findings.md` records that every OR/coverage rewrite of the recall SQL was measured and reverted — re-tuning that lever is out of scope and forbidden by that ADR. Multi-query retrieval is a *different*, additive lever (query expansion), not a ranking-formula change.
- **Multi-query retrieval must call the existing hybrid search path (`query.Hybrid`) rather than creating a second ranking implementation** (spec constraint, `docs/SEARCH-CAPABILITY-SPEC.md` Feature 1).
- **Expansion must be deterministic with no agent/LLM call** — the MVP in this plan has no agent-rewrite path at all (agent-generated rewrites are explicitly optional per spec and are BACKLOG, not built here).
- Default expansion count: small, 3–5 subqueries (spec constraint) — this plan caps at 5 (`maxSubqueries`).
- **Exact lookup quality must not regress:** `eval/cases-precise-ids.tsv` top@5 must remain 100% via `eval/tophit.sh` after this change (spec acceptance criterion; verified in Task 6).
- Bilingual query variants are explicitly optional in the spec — **not built in this plan** (note in Task 6 as a backlog follow-up, do not add scope).
- **Out of scope for this plan** (belong to later features per the spec's own "Recommended Implementation Order"): surfacing provenance in CLI/MCP JSON output (Feature 3, "Search Explanation"), richer CLI/MCP filters and output formats (Feature 4), and wiring multi-query into RAG's `Retriever` (Feature 5, explicitly listed as *after* multi-query is measured). This plan produces the reusable primitives (`MultiHybrid`, per-hit provenance) those later features consume — it does not wire them in.
- Follow existing code conventions: doc comments explaining *why*, not just *what* (see `internal/query/hybrid.go` for the house style); `gofmt` clean; table-driven tests with `testify`'s `assert`/`require`, `t.Parallel()` on pure unit tests.
- Commit messages: short, conventional, imperative (see `git log --oneline` in this repo for style — e.g. `feat: ...`, `fix: ...`).

---

### Task 1: `query.Hybrid` — surface `Score` and `Arm` on each returned `Hit`

**Files:**
- Modify: `internal/store/postgres/search.go` (the `Hit` struct, ~line 19)
- Modify: `internal/query/hybrid.go` (the `Hybrid` function, ~lines 167–221)
- Test: `internal/query/hybrid_test.go`

**Interfaces:**
- Consumes: nothing new — reads the existing `scores map[string]float64` and per-arm hit lists (`ftsHits`, `vecHits`) that `Hybrid` already builds internally.
- Produces: `postgres.Hit` gains a new field `Arm string` (values: `"fts"`, `"vector"`, `"both"`, or `""` on raw non-fused `FTS()`/`Vector()` results where it's never set). `Hit.Score` — already declared but previously always left at its Go zero value in `Hybrid`'s output — is now populated with the hit's final fused score (`scores[id]`, after the type-weight and title-boost multipliers are applied). Task 3 (`MultiHybrid`) reads both fields directly off each `postgres.Hit` it gets back from `Hybrid`.

This is an **additive** change: no existing field changes meaning, no sort order changes, no existing caller (`cmd/pixkb` search command, `internal/kbmcp` search tool, `internal/rag/adapters.go`'s `HybridRetriever`) needs to change. `Hit.Score` was read by `HybridRetriever.Retrieve` (`internal/rag/adapters.go:33`, `Hit{..., Score: x.Score}`) but was silently always zero before this fix — a latent gap this task also closes as a side effect of touching the same lines.

- [ ] **Step 1: Write the failing test**

Add to `internal/query/hybrid_test.go` (same file, same `fakeSearcher` already defined there):

```go
func TestHybrid_SetsScoreAndArm(t *testing.T) {
	t.Parallel()
	s := &fakeSearcher{
		fts: []postgres.Hit{{ID: "a", Title: "Alpha"}, {ID: "b", Title: "Bravo"}},
		vec: []postgres.Hit{{ID: "b", Title: "Bravo", Score: 0.9}, {ID: "c", Title: "Charlie", Score: 0.8}},
	}
	got, err := Hybrid(context.Background(), s, embed.NewHashing(8), "q", postgres.Filter{})
	require.NoError(t, err)
	require.Len(t, got, 3)

	byID := map[string]postgres.Hit{}
	for _, h := range got {
		byID[h.ID] = h
	}
	assert.Equal(t, "both", byID["b"].Arm, "b appears in both arms")
	assert.Equal(t, "fts", byID["a"].Arm, "a appears only in the FTS arm")
	assert.Equal(t, "vector", byID["c"].Arm, "c appears only in the vector arm")

	assert.Positive(t, byID["a"].Score, "fused score must be populated, not left at zero")
	assert.Positive(t, byID["b"].Score)
	assert.Positive(t, byID["c"].Score)
	// b is in both arms so its RRF contribution is strictly greater than either
	// single-arm hit's.
	assert.Greater(t, byID["b"].Score, byID["a"].Score)
	assert.Greater(t, byID["b"].Score, byID["c"].Score)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/query/... -run TestHybrid_SetsScoreAndArm -v`
Expected: FAIL — `byID["b"].Arm` is `""` (field doesn't exist yet: compile error `unknown field Arm in struct literal` is also acceptable/expected at this point since the field isn't added yet).

- [ ] **Step 3: Add the `Arm` field and implement**

In `internal/store/postgres/search.go`, add the field to `Hit`:

```go
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
```

In `internal/query/hybrid.go`, track arm membership alongside the existing `note` closure and populate both new fields on the final output. Replace this block:

```go
	// Build a title + type lookup (FTS title wins) and per-arm rank scores.
	titles := make(map[string]string)
	types := make(map[string]string)
	scores := make(map[string]float64)
	firstSeen := make(map[string]int)
	order := 0
	note := func(h postgres.Hit, rank int, weight float64) {
		if _, ok := titles[h.ID]; !ok {
			titles[h.ID] = h.Title
		}
		if h.Type != "" {
			types[h.ID] = h.Type
		}
		scores[h.ID] += weight / float64(rrfK+rank)
		if _, ok := firstSeen[h.ID]; !ok {
			firstSeen[h.ID] = order
			order++
		}
	}
	for i, h := range ftsHits {
		note(h, i, ftsArmWeight)
	}
	for i, h := range vecHits {
		note(h, i, vecArmWeight)
	}
```

with:

```go
	// Build a title + type lookup (FTS title wins) and per-arm rank scores.
	titles := make(map[string]string)
	types := make(map[string]string)
	scores := make(map[string]float64)
	firstSeen := make(map[string]int)
	fromFTS := make(map[string]bool)
	fromVec := make(map[string]bool)
	order := 0
	note := func(h postgres.Hit, rank int, weight float64) {
		if _, ok := titles[h.ID]; !ok {
			titles[h.ID] = h.Title
		}
		if h.Type != "" {
			types[h.ID] = h.Type
		}
		scores[h.ID] += weight / float64(rrfK+rank)
		if _, ok := firstSeen[h.ID]; !ok {
			firstSeen[h.ID] = order
			order++
		}
	}
	for i, h := range ftsHits {
		note(h, i, ftsArmWeight)
		fromFTS[h.ID] = true
	}
	for i, h := range vecHits {
		note(h, i, vecArmWeight)
		fromVec[h.ID] = true
	}
```

Then replace the final output construction:

```go
	out := make([]postgres.Hit, 0, len(ids))
	for i, id := range ids {
		if i >= limit {
			break
		}
		out = append(out, postgres.Hit{ID: id, Title: titles[id], Type: types[id], Rank: i + 1})
	}
	return out, nil
}
```

with:

```go
	out := make([]postgres.Hit, 0, len(ids))
	for i, id := range ids {
		if i >= limit {
			break
		}
		out = append(out, postgres.Hit{
			ID: id, Title: titles[id], Type: types[id], Rank: i + 1,
			Score: scores[id],
			Arm:   armLabel(fromFTS[id], fromVec[id]),
		})
	}
	return out, nil
}

// armLabel names which retrieval arm(s) surfaced a hit, for provenance
// (used directly by query.MultiHybrid, and by any future search-explanation
// surface).
func armLabel(fts, vec bool) string {
	switch {
	case fts && vec:
		return "both"
	case fts:
		return "fts"
	case vec:
		return "vector"
	default:
		return ""
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/query/... -v`
Expected: PASS, including `TestHybrid_SetsScoreAndArm` and every pre-existing test in the package (`TestHybrid_FusesAndHydrates`, `TestHybrid_TitleBoostWinsOverNoisyFragment`, `TestTitleBoost`, `TestHybrid_RespectsLimit`, `TestHybrid_VectorFloorDropsOOD`, `TestHybrid_VectorFloorKeepsRealHit`) — none of them assert on `Score`/`Arm` today, so none should change behavior or break.

- [ ] **Step 5: Commit**

```bash
git add internal/store/postgres/search.go internal/query/hybrid.go internal/query/hybrid_test.go
git commit -m "feat: surface Score and Arm provenance on query.Hybrid's output hits"
```

---

### Task 2: `query.ExpandQuery` — deterministic query expansion

**Files:**
- Create: `internal/query/expand.go`
- Test: `internal/query/expand_test.go`

**Interfaces:**
- Consumes: the unexported `foldTokens(s string) []string` already defined in `internal/query/hybrid.go` (same package — no new export needed; lowercases, strips Portuguese diacritics, splits on non-alphanumeric runs, drops stopwords and single characters).
- Produces: `func ExpandQuery(q string) []string` — Task 3 (`MultiHybrid`) calls this to get the subquery list to run through `Hybrid`.

- [ ] **Step 1: Write the failing test**

Create `internal/query/expand_test.go`:

```go
package query

import "testing"

func TestExpandQuery_OriginalAlwaysFirstAndUnmodified(t *testing.T) {
	t.Parallel()
	q := "consultar cobrança por txid"
	out := ExpandQuery(q)
	if len(out) == 0 || out[0] != q {
		t.Fatalf("expected original query first and unmodified, got %v", out)
	}
}

func TestExpandQuery_NoEntityMatch_OnlyOriginalAndRewrite(t *testing.T) {
	t.Parallel()
	// No recognized domain entity here -> at most [original, folded rewrite].
	out := ExpandQuery("prazos de implementação")
	if len(out) > 2 {
		t.Fatalf("expected at most 2 subqueries (original + rewrite), got %v", out)
	}
}

func TestExpandQuery_MatchesRefundEntityOnRealEvalCase(t *testing.T) {
	t.Parallel()
	// Exact case from eval/cases-fuzzy-ids.tsv — this is the query the spec's
	// worked example (Pix refund) names explicitly.
	out := ExpandQuery("como estornar um pix que recebi por engano")
	found := false
	for _, sq := range out {
		if sq == "devolução pix refund" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the refund entity subquery, got %v", out)
	}
}

func TestExpandQuery_MatchesWebhookEntity(t *testing.T) {
	t.Parallel()
	out := ExpandQuery("notificar via webhook pix")
	if len(out) != 2 {
		t.Fatalf("expected exactly [original, webhook subquery], got %v", out)
	}
	if out[1] != "webhook notificação pix" {
		t.Fatalf("expected the webhook entity subquery second, got %v", out)
	}
}

func TestExpandQuery_CapsAtMaxSubqueries(t *testing.T) {
	t.Parallel()
	// Hits many entity stems at once (estorno, webhook, chave, api, pacs,
	// certificado, qr, liquidacao) -> must still cap at maxSubqueries.
	out := ExpandQuery("estorno webhook chave api pacs certificado qr liquidacao")
	if len(out) > maxSubqueries {
		t.Fatalf("expected at most %d subqueries, got %d: %v", maxSubqueries, len(out), out)
	}
}

func TestExpandQuery_Deterministic(t *testing.T) {
	t.Parallel()
	q := "como estornar um pix que recebi por engano"
	a := ExpandQuery(q)
	b := ExpandQuery(q)
	if len(a) != len(b) {
		t.Fatalf("non-deterministic length: %v vs %v", a, b)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("non-deterministic at index %d: %v vs %v", i, a, b)
		}
	}
}

func TestExpandQuery_NoDuplicateSubqueries(t *testing.T) {
	t.Parallel()
	out := ExpandQuery("estorno de devolução via refund")
	seen := map[string]bool{}
	for _, sq := range out {
		key := sq
		if seen[key] {
			t.Fatalf("duplicate subquery %q in %v", sq, out)
		}
		seen[key] = true
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/query/... -run TestExpandQuery -v`
Expected: FAIL with `undefined: ExpandQuery` (function doesn't exist yet).

- [ ] **Step 3: Write the implementation**

Create `internal/query/expand.go`:

```go
// Package query: deterministic query expansion for multi-query retrieval
// (docs/SEARCH-CAPABILITY-SPEC.md Feature 1). No agent/LLM call — this is
// the spec's mandatory non-agent fallback; an agent-generated rewrite layer
// is explicitly optional per spec and is not built here.
package query

import "strings"

// maxSubqueries bounds ExpandQuery's output. Spec: "Default expansion count
// should be small, preferably 3 to 5 queries."
const maxSubqueries = 5

// entityTriggers maps a fixed, ordered set of Portuguese word-stems to a
// canonical subquery that steers retrieval toward that domain entity's
// concepts. Stems (not whole words) so common inflections match ("estornar",
// "estornado", "estorno" all start with "estorn"). Order is fixed so
// expansion is reproducible and reviewable. This list mirrors the exact
// entity examples named in Feature 1's spec text (Pix refund, webhook, DICT
// key, API endpoint, pacs/camt message, certificate, QR code, settlement) —
// it is intentionally small and separate from the larger, versioned
// vocabulary Feature 7 ("Domain-Aware Query Understanding") will add later;
// that feature should supersede/extend this table, not duplicate it.
var entityTriggers = []struct {
	stems    []string
	subquery string
}{
	{[]string{"estorn", "devolu", "refund"}, "devolução pix refund"},
	{[]string{"webhook", "notific", "avis"}, "webhook notificação pix"},
	{[]string{"chave", "dict", "evp"}, "chave DICT pix"},
	{[]string{"endpoint", "api"}, "endpoint API"},
	{[]string{"pacs", "camt", "mensagem"}, "mensagem ISO 20022 pacs camt"},
	{[]string{"certific", "mtls", "icp"}, "certificado mTLS ICP-Brasil"},
	{[]string{"qr", "qrcode"}, "QR Code Pix BR Code"},
	{[]string{"liquida", "settlement", "spi"}, "liquidação SPI settlement"},
}

// ExpandQuery deterministically expands q into up to maxSubqueries retrieval
// queries: the original query verbatim, a concise domain-term rewrite
// (diacritics folded, stopwords stripped, via the same foldTokens used for
// title-boost matching), and one subquery per recognized domain entity the
// query mentions. Duplicate subqueries (case-insensitive) are dropped. The
// original query is always present and always first, so MultiHybrid always
// has at least the equivalent of a plain single-query hybrid search to fall
// back on.
func ExpandQuery(q string) []string {
	out := []string{q}
	seen := map[string]bool{strings.ToLower(strings.TrimSpace(q)): true}
	add := func(s string) bool {
		s = strings.TrimSpace(s)
		key := strings.ToLower(s)
		if s == "" || seen[key] {
			return false
		}
		seen[key] = true
		out = append(out, s)
		return len(out) >= maxSubqueries
	}

	tokens := foldTokens(q)
	tokenSet := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		tokenSet[t] = true
	}

	if add(strings.Join(tokens, " ")) {
		return out
	}
	for _, trig := range entityTriggers {
		matched := false
		for token := range tokenSet {
			for _, stem := range trig.stems {
				if strings.HasPrefix(token, stem) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if matched && add(trig.subquery) {
			return out
		}
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/query/... -run TestExpandQuery -v`
Expected: PASS for all `TestExpandQuery_*` cases.

- [ ] **Step 5: Commit**

```bash
git add internal/query/expand.go internal/query/expand_test.go
git commit -m "feat: add deterministic query expansion for multi-query retrieval"
```

---

### Task 3: `query.MultiHybrid` — fuse per-subquery hybrid results with provenance

**Files:**
- Create: `internal/query/multi.go`
- Modify: `internal/query/doc.go` (one-line package doc update)
- Test: `internal/query/multi_test.go`

**Interfaces:**
- Consumes: `ExpandQuery` (Task 2), `Hybrid` + its `Searcher` interface + the `rrfK` constant (Task 1's `hybrid.go`, unchanged), `postgres.Hit` (now with `Score`/`Arm` from Task 1), `embed.Embedder`, `postgres.Filter`.
- Produces:
  - `type SubqueryMatch struct { Query string; Arm string; Rank int }`
  - `type MultiHit struct { postgres.Hit; Subqueries []SubqueryMatch }`
  - `func MultiHybrid(ctx context.Context, s Searcher, emb embed.Embedder, q string, f postgres.Filter) ([]MultiHit, error)`
  - `func Hits(mh []MultiHit) []postgres.Hit` — strips provenance for callers (CLI/MCP, Tasks 4–5) that only need the plain hit shape.

- [ ] **Step 1: Write the failing test**

Create `internal/query/multi_test.go`:

```go
package query

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pixkb/internal/embed"
	"pixkb/internal/store/postgres"
)

// queryAwareSearcher, unlike fakeSearcher in hybrid_test.go, returns different
// FTS hits depending on the query string — needed to prove MultiHybrid runs
// each expanded subquery independently through Hybrid.
type queryAwareSearcher struct {
	fts map[string][]postgres.Hit
}

func (f *queryAwareSearcher) FTS(_ context.Context, q string, _ postgres.Filter) ([]postgres.Hit, error) {
	return f.fts[q], nil
}
func (f *queryAwareSearcher) Vector(_ context.Context, _ []float32, _ postgres.Filter) ([]postgres.Hit, error) {
	return nil, nil
}

func TestMultiHybrid_HitFoundByMultipleSubqueriesRanksHigher(t *testing.T) {
	t.Parallel()
	q := "notificar via webhook pix"
	subqueries := ExpandQuery(q)
	require.Len(t, subqueries, 2, "expected original + the webhook entity subquery")

	s := &queryAwareSearcher{fts: map[string][]postgres.Hit{
		subqueries[0]: {{ID: "x", Title: "X"}, {ID: "y", Title: "Y"}},
		subqueries[1]: {{ID: "x", Title: "X"}},
	}}
	got, err := MultiHybrid(context.Background(), s, embed.NewHashing(8), q, postgres.Filter{})
	require.NoError(t, err)
	require.NotEmpty(t, got)

	assert.Equal(t, "x", got[0].ID, "hit surfaced by both subqueries must rank first")
	assert.GreaterOrEqual(t, len(got[0].Subqueries), 2, "x's provenance must list both subqueries")

	ids := map[string]bool{}
	for _, h := range got {
		ids[h.ID] = true
	}
	assert.True(t, ids["y"], "single-subquery hit still present")
}

func TestMultiHybrid_ProvenanceRecordsQueryAndArm(t *testing.T) {
	t.Parallel()
	q := "notificar via webhook pix"
	subqueries := ExpandQuery(q)
	require.Len(t, subqueries, 2)

	s := &queryAwareSearcher{fts: map[string][]postgres.Hit{
		subqueries[0]: {{ID: "x", Title: "X"}},
	}}
	got, err := MultiHybrid(context.Background(), s, embed.NewHashing(8), q, postgres.Filter{})
	require.NoError(t, err)
	require.NotEmpty(t, got)
	require.Len(t, got[0].Subqueries, 1)
	assert.Equal(t, subqueries[0], got[0].Subqueries[0].Query)
	assert.Equal(t, "fts", got[0].Subqueries[0].Arm)
	assert.Equal(t, 1, got[0].Subqueries[0].Rank)
}

func TestMultiHybrid_RespectsLimit(t *testing.T) {
	t.Parallel()
	q := "prazos de implementação"
	s := &queryAwareSearcher{fts: map[string][]postgres.Hit{
		q: {{ID: "a"}, {ID: "b"}, {ID: "c"}},
	}}
	got, err := MultiHybrid(context.Background(), s, embed.NewHashing(8), q, postgres.Filter{Limit: 2})
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestMultiHybrid_NoHits_ReturnsEmptyNotError(t *testing.T) {
	t.Parallel()
	s := &queryAwareSearcher{fts: map[string][]postgres.Hit{}}
	got, err := MultiHybrid(context.Background(), s, embed.NewHashing(8), "previsão do tempo amanhã", postgres.Filter{})
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestHits_StripsProvenance(t *testing.T) {
	t.Parallel()
	mh := []MultiHit{
		{Hit: postgres.Hit{ID: "a", Title: "A", Rank: 1}, Subqueries: []SubqueryMatch{{Query: "q", Arm: "fts", Rank: 1}}},
	}
	got := Hits(mh)
	require.Len(t, got, 1)
	assert.Equal(t, postgres.Hit{ID: "a", Title: "A", Rank: 1}, got[0])
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/query/... -run 'TestMultiHybrid|TestHits' -v`
Expected: FAIL with `undefined: MultiHybrid` / `undefined: MultiHit` / `undefined: Hits` (nothing exists yet).

- [ ] **Step 3: Write the implementation**

Create `internal/query/multi.go`:

```go
package query

import (
	"context"
	"sort"

	"pixkb/internal/embed"
	"pixkb/internal/store/postgres"
)

// SubqueryMatch records that one expanded subquery's own Hybrid ranking
// surfaced a hit — the "which subquery matched / which arm / per-query rank"
// provenance docs/SEARCH-CAPABILITY-SPEC.md Feature 1 requires.
type SubqueryMatch struct {
	Query string
	Arm   string
	Rank  int
}

// MultiHit is a fused multi-query search hit: the final cross-subquery rank
// (embedded via Hit.Rank) plus the trail of subqueries that found it.
type MultiHit struct {
	postgres.Hit
	Subqueries []SubqueryMatch
}

// Hits strips provenance, returning plain hits for callers (CLI, MCP) that
// only need the id/title/type/rank shape shared with plain Hybrid results.
func Hits(mh []MultiHit) []postgres.Hit {
	out := make([]postgres.Hit, 0, len(mh))
	for _, m := range mh {
		out = append(out, m.Hit)
	}
	return out
}

// multiSubqueryLimit is the per-subquery result cap MultiHybrid requests
// internally, independent of the caller's final Filter.Limit — cross-subquery
// fusion needs headroom beyond the final page size, or a hit that ranks #3 in
// one subquery and #1 in another could be truncated before fusion ever sees
// it. The caller's Filter.Limit still governs the final, fused output size.
const multiSubqueryLimit = 20

// MultiHybrid expands q (via ExpandQuery) into a small deterministic set of
// subqueries, runs the existing, unmodified Hybrid search for each one, and
// fuses the per-subquery ranked lists with a second reciprocal-rank-fusion
// pass over their ranks — a hit surfaced by more subqueries (or ranked
// higher within them) scores higher. It never re-implements FTS/vector
// ranking: every subquery's ordering comes straight from Hybrid, honoring
// the spec's "must call the existing hybrid search path" constraint.
func MultiHybrid(ctx context.Context, s Searcher, emb embed.Embedder, q string, f postgres.Filter) ([]MultiHit, error) {
	subqueries := ExpandQuery(q)

	perSubFilter := f
	if perSubFilter.Limit <= 0 || perSubFilter.Limit < multiSubqueryLimit {
		perSubFilter.Limit = multiSubqueryLimit
	}

	scores := make(map[string]float64)
	firstSeen := make(map[string]int)
	hitByID := make(map[string]postgres.Hit)
	provenance := make(map[string][]SubqueryMatch)
	order := 0

	for _, sq := range subqueries {
		hits, err := Hybrid(ctx, s, emb, sq, perSubFilter)
		if err != nil {
			return nil, err
		}
		for _, h := range hits {
			scores[h.ID] += 1.0 / float64(rrfK+h.Rank)
			if _, ok := hitByID[h.ID]; !ok {
				hitByID[h.ID] = h
			}
			if _, ok := firstSeen[h.ID]; !ok {
				firstSeen[h.ID] = order
				order++
			}
			provenance[h.ID] = append(provenance[h.ID], SubqueryMatch{Query: sq, Arm: h.Arm, Rank: h.Rank})
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

	limit := f.Limit
	if limit <= 0 {
		limit = multiSubqueryLimit
	}
	out := make([]MultiHit, 0, len(ids))
	for i, id := range ids {
		if i >= limit {
			break
		}
		h := hitByID[id]
		h.Rank = i + 1
		out = append(out, MultiHit{Hit: h, Subqueries: provenance[id]})
	}
	return out, nil
}
```

Update `internal/query/doc.go`:

```go
// Package query exposes the hybrid FTS-plus-vector search entrypoint, plus
// deterministic multi-query expansion and fusion on top of it.
package query
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/query/... -v`
Expected: PASS for every test in the package, old and new (Tasks 1–3 combined).

- [ ] **Step 5: Commit**

```bash
git add internal/query/multi.go internal/query/multi_test.go internal/query/doc.go
git commit -m "feat: fuse per-subquery hybrid results into MultiHybrid with provenance"
```

---

### Task 4: CLI — `pixkb search --mode multi`

**Files:**
- Modify: `cmd/pixkb/commands.go` (`newSearchCmd`, ~lines 113–165)

**Interfaces:**
- Consumes: `query.MultiHybrid`, `query.Hits` (Task 3).
- Produces: nothing new consumed elsewhere — this is a leaf CLI wire-up.

No new automated test: `newSearchCmd` opens a live `*postgres.Store` via `openStore(ctx, cfg)` (see `cmd/pixkb/commands.go:126`), same as the pre-existing `fts`/`vector`/`hybrid` modes — none of those have a CLI-level unit test either (they need a real DSN; `cmd/pixkb/commands_test.go` only tests DB-free helpers like `buildSources`). Correctness of the `multi` mode's actual ranking is already covered by Task 3's `MultiHybrid` unit tests; correctness of this exact CLI code path against the live KB is verified in Task 6's eval run, which shells out to the built `pixkb search ... --mode multi` binary.

- [ ] **Step 1: Wire the new mode**

In `cmd/pixkb/commands.go`, inside `newSearchCmd`'s `RunE`, replace:

```go
			switch mode {
			case "fts":
				hits, err = st.FTS(ctx, q, f)
			case "vector":
				var vs [][]float32
				if vs, err = emb.Embed(ctx, []string{q}); err == nil {
					hits, err = st.Vector(ctx, vs[0], f)
				}
			default:
				hits, err = query.Hybrid(ctx, st, emb, q, f)
			}
```

with:

```go
			switch mode {
			case "fts":
				hits, err = st.FTS(ctx, q, f)
			case "vector":
				var vs [][]float32
				if vs, err = emb.Embed(ctx, []string{q}); err == nil {
					hits, err = st.Vector(ctx, vs[0], f)
				}
			case "multi":
				var mh []query.MultiHit
				if mh, err = query.MultiHybrid(ctx, st, emb, q, f); err == nil {
					hits = query.Hits(mh)
				}
			default:
				hits, err = query.Hybrid(ctx, st, emb, q, f)
			}
```

And update the flag's help text:

```go
	cmd.Flags().StringVar(&mode, "mode", "hybrid", "search mode: hybrid|fts|vector|multi")
```

- [ ] **Step 2: Build and smoke-test manually**

Run: `go build ./... ` then, against your dev DB (`PIXKB_DSN`/config file already set per project convention — never a `--dsn` flag on *new* commands, but note `search` predates that convention and keeps its existing `--dsn` flag; do not remove it, out of scope here):

```bash
pixkb search "como estornar um pix que recebi por engano" --mode multi
pixkb search "como estornar um pix que recebi por engano" --mode hybrid
```//
Expected: both commands run without error and print ranked `rank / id / title` lines; `--mode multi` output may reorder/add results relative to `--mode hybrid` but must not error.

- [ ] **Step 3: Commit**

```bash
git add cmd/pixkb/commands.go
git commit -m "feat: add pixkb search --mode multi (multi-query retrieval)"
```

---

### Task 5: MCP — `search` tool gains `mode: "multi"`

**Files:**
- Modify: `internal/kbmcp/server.go` (`searchIn` struct and `registerSearch`, ~lines 69–96)
- Test: `internal/kbmcp/server_test.go`

**Interfaces:**
- Consumes: `query.MultiHybrid`, `query.Hits` (Task 3).
- Produces: nothing new consumed elsewhere — `searchOut`/`hitOut` are unchanged (provenance is not exposed via MCP JSON in this plan — that's Feature 3's job, see Global Constraints).

- [ ] **Step 1: Write the failing test**

Add to `internal/kbmcp/server_test.go`, mirroring `TestServerReadTools`'s exact DSN/skip boilerplate:

```go
// TestServerSearch_MultiMode exercises search's mode="multi" over an
// in-memory MCP transport against a live KB. Skipped without a DSN or under
// -short, same as TestServerReadTools (read-only, no Runner needed).
func TestServerSearch_MultiMode(t *testing.T) {
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

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search",
		Arguments: map[string]any{"query": "como estornar um pix que recebi por engano", "mode": "multi", "limit": 5},
	})
	if err != nil || res.IsError {
		t.Fatalf("search (multi) call: err=%v isErr=%v", err, res.IsError)
	}
	if len(res.Content) == 0 {
		t.Fatal("search (multi) returned no content")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (with `PIXKB_TEST_DSN` or `PIXKB_DSN` set to a reachable pixkb Postgres instance): `go test ./internal/kbmcp/... -run TestServerSearch_MultiMode -v`
Expected: FAIL — the tool call succeeds today (unknown fields in a JSON args map are ignored by default), but this only proves `mode` isn't yet wired: add a temporary stronger assertion isn't necessary if you confirm by reading the current `registerSearch` body — it always calls `query.Hybrid` regardless of `in.Mode`, so this test doesn't distinguish behavior yet. Treat Step 2 as a compile check instead: without `Mode` on `searchIn`, this test still compiles and passes today, which is why Step 1 alone doesn't prove much — proceed to Step 3, then re-run in Step 4 to confirm no regression, and rely on Task 3's dedicated `MultiHybrid` unit tests (which DO fail-then-pass meaningfully) for the actual TDD signal on ranking behavior. This step exists to prevent a wiring regression (e.g., a typo causing `mode: "multi"` to error), not to test fusion correctness.

- [ ] **Step 3: Wire the `mode` field**

In `internal/kbmcp/server.go`, replace:

```go
type searchIn struct {
	Query string `json:"query" jsonschema:"natural-language or lexical query"`
	Type  string `json:"type,omitempty" jsonschema:"optional concept-type filter (ApiEndpoint, ManualSection, PacsMessage, ...)"`
	Limit int    `json:"limit,omitempty" jsonschema:"max hits (default 10)"`
}
type searchOut struct {
	Hits []hitOut `json:"hits"`
}

func registerSearch(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "search",
		Description: "Hybrid (lexical + vector) search over the Pix/SPB knowledge base. Returns ranked concept hits.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, searchOut, error) {
		limit := in.Limit
		if limit <= 0 {
			limit = 10
		}
		hits, err := query.Hybrid(ctx, d.Store, d.Emb, in.Query, postgres.Filter{Type: in.Type, Limit: limit})
		if err != nil {
			return nil, searchOut{}, err
		}
```

with:

```go
type searchIn struct {
	Query string `json:"query" jsonschema:"natural-language or lexical query"`
	Type  string `json:"type,omitempty" jsonschema:"optional concept-type filter (ApiEndpoint, ManualSection, PacsMessage, ...)"`
	Limit int    `json:"limit,omitempty" jsonschema:"max hits (default 10)"`
	Mode  string `json:"mode,omitempty" jsonschema:"retrieval mode: hybrid (default) or multi (expands the query into several deterministic subqueries for broader recall)"`
}
type searchOut struct {
	Hits []hitOut `json:"hits"`
}

func registerSearch(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "search",
		Description: "Hybrid (lexical + vector) search over the Pix/SPB knowledge base. Returns ranked concept hits. mode=multi broadens recall via deterministic query expansion.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, searchOut, error) {
		limit := in.Limit
		if limit <= 0 {
			limit = 10
		}
		f := postgres.Filter{Type: in.Type, Limit: limit}
		var hits []postgres.Hit
		var err error
		if in.Mode == "multi" {
			var mh []query.MultiHit
			if mh, err = query.MultiHybrid(ctx, d.Store, d.Emb, in.Query, f); err == nil {
				hits = query.Hits(mh)
			}
		} else {
			hits, err = query.Hybrid(ctx, d.Store, d.Emb, in.Query, f)
		}
		if err != nil {
			return nil, searchOut{}, err
		}
```

The rest of the function body (building `searchOut` from `hits`) is unchanged.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/kbmcp/... -run 'TestServerReadTools|TestServerSearch_MultiMode' -v`
Expected: PASS for both (with a DSN set; both skip identically without one).

- [ ] **Step 5: Commit**

```bash
git add internal/kbmcp/server.go internal/kbmcp/server_test.go
git commit -m "feat: add mode=multi to the search MCP tool"
```

---

### Task 6: Verify against the deterministic eval gates

**Files:** none modified — this task runs existing tooling and is a merge gate, not a code change.

**Interfaces:**
- Consumes: `eval/tophit.sh`, `eval/cases-precise-ids.tsv`, `eval/cases-fuzzy-ids.tsv`, the `pixkb search --mode multi` CLI path from Task 4.
- Produces: a pass/fail verdict for this plan's acceptance criteria (spec: "Exact lookup quality must not regress on `eval/cases-precise-ids.tsv`"; "Fuzzy recall should be measured against `eval/cases-fuzzy-ids.tsv`; any improvement must not reduce precise top@5").

- [ ] **Step 1: Build the eval binary**

```bash
go build -o bin/pixkb.exe ./cmd/pixkb
```

- [ ] **Step 2: Record the baseline (mode=hybrid) numbers**

```bash
bash eval/tophit.sh eval/cases-precise-ids.tsv --mode hybrid
bash eval/tophit.sh eval/cases-fuzzy-ids.tsv --mode hybrid
```

Note the `top@1`, `top@5`, and `MRR` lines from each run — this is the pre-change baseline (should match ADR 0002's last recorded numbers: precise MRR 0.821/100%, fuzzy top@5 53%, modulo any KB content drift since that ADR was written).

- [ ] **Step 3: Run the same two eval sets with mode=multi**

```bash
bash eval/tophit.sh eval/cases-precise-ids.tsv --mode multi
bash eval/tophit.sh eval/cases-fuzzy-ids.tsv --mode multi
```

- [ ] **Step 4: Compare and gate**

- **Precise set:** `top@5` from Step 3 must equal 100% (same as Step 2's baseline). If it regressed, do not proceed — this plan's Task 3 fusion logic or Task 2's entity table needs a fix before merging (most likely cause: an entity subquery is dragging in an off-topic concept that outranks the correct precise hit; narrow the offending `entityTriggers` stem or its `subquery` text).
- **Fuzzy set:** report `top@5`/MRR whether it improves, stays flat, or regresses relative to baseline. Per spec, an improvement is desired but not required to merge Feature 1 — the acceptance criterion is precise-set non-regression plus having the multi-query capability measurably in place. Record the before/after numbers in the commit message or PR description for this task so the next person (or the Feature 5 RAG-upgrade work) has real data instead of having to re-derive it.
- If precise top@5 holds and fuzzy is flat-or-better: mark this task (and the plan) complete.
- If fuzzy regresses: still acceptable to merge (precise is the hard gate, per spec), but flag it in `docs/BACKLOG.md` as a known limitation of the initial `entityTriggers` table for follow-up tuning.

- [ ] **Step 5: Record backlog follow-ups this plan intentionally deferred**

Add to `docs/BACKLOG.md` (do not build now):
- Agent-generated query rewrites and bilingual (PT/EN ISO-message) subqueries for `ExpandQuery` (spec: both explicitly optional).
- Surfacing `MultiHit.Subqueries` provenance in CLI/MCP JSON output (spec Feature 3, "Search Explanation").
- Wiring `MultiHybrid` into `rag.Retriever`/`HybridRetriever` for RAG grounding diversity (spec Feature 5 — explicitly sequenced *after* this plan's measurement step).

- [ ] **Step 6: Commit the eval report + backlog note**

```bash
git add docs/BACKLOG.md
git commit -m "docs: record multi-query eval results and defer Feature 1's optional extensions"
```
