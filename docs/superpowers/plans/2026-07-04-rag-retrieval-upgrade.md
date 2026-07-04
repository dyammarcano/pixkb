# RAG Retrieval Upgrade Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development.

**Goal:** Feature 5 of `docs/SEARCH-CAPABILITY-SPEC.md`: upgrade `internal/rag.BuildGrounding` to optionally use multi-query retrieval, diversify grounding by concept type, expand graph neighbours from more than one seed hit, and refuse on weak evidence — all opt-in, all additive, zero change to today's default `Options{}` behavior.

**Architecture:** `rag.Hit` gains a `Type` field. A new `rag.MultiRetriever` optional interface (`RetrieveMulti(ctx, q, k) ([]Hit, error)`) lets `BuildGrounding` dispatch to `query.MultiHybrid` (via a type assertion, not an interface change — so every existing `Retriever` and test fake keeps compiling unmodified) when `Options.MultiQuery` is set; without a `MultiRetriever`, `MultiQuery` is a silent no-op fallback to the existing single-query `Retrieve`. `Options` gains `Diversify` (reorder hits so the first hit of each concept `Type` is promoted ahead of later same-type hits — a stable reshuffle, not a re-score), `ExpandSeeds` (how many top hits' graph neighbours to pull, default 1 = today's behavior), and `MinScore` (refuse — empty `Grounding`, no agent turn spent — when the top hit's score is below this; default 0 = disabled). `HybridRetriever` (the production adapter) implements `RetrieveMulti` via `query.MultiHybrid`, reusing its existing RRF fusion — no second ranking implementation.

**Tech Stack:** Go 1.25, `internal/rag`, `cmd/pixkb`, `internal/kbmcp`.

## Global Constraints

- Go 1.25.0, `CGO_ENABLED=0`, pure Go.
- **Zero default-behavior change.** `Options{}` (the zero value) must produce byte-identical `Grounding` output to today's `BuildGrounding` — all 5 existing tests in `internal/rag/rag_test.go` must pass UNMODIFIED after every task.
- New capabilities (`MultiQuery`, `Diversify`, `ExpandSeeds`, `MinScore`) are opt-in only.
- Retrieval must reuse `query.Hybrid`/`query.MultiHybrid` unchanged — no new ranking math (per spec's "Ranking Principles": "Use RRF or another rank-fusion method when combining independent retrieval lists" — already satisfied by `MultiHybrid`, not to be reimplemented here).
- RAG must still refuse (no agent turn spent) whenever the assembled `Grounding` ends up with zero `Chunks` — this is `Synthesize`'s existing behavior (`internal/rag/answer.go:40-42`) and must not be touched.

---

### Task 1: `rag.Hit.Type` + `MultiRetriever` interface + `HybridRetriever` wiring

**Files:**
- Modify: `internal/rag/rag.go`
- Modify: `internal/rag/adapters.go`

**Interfaces:**
- Produces: `Hit.Type string` (new field); `type MultiRetriever interface { RetrieveMulti(ctx context.Context, q string, k int) ([]Hit, error) }`; `func (h HybridRetriever) RetrieveMulti(ctx context.Context, q string, k int) ([]Hit, error)`.
- Consumes: `query.MultiHybrid` (`internal/query/multi.go:51`, signature `func MultiHybrid(ctx context.Context, s Searcher, emb embed.Embedder, q string, f postgres.Filter) ([]MultiHit, error)`), `postgres.Hit.Type` (already exists, `internal/store/postgres/search.go:22`).

- [ ] **Step 1:** In `internal/rag/rag.go`, add `Type` to the `Hit` struct:

```go
// Hit is a retrieved concept reference — the subset of a search hit the augment
// step needs (id + title + type + fused score), decoupled from the postgres type.
type Hit struct {
	ID    string
	Title string
	Type  string
	Score float64
}
```

- [ ] **Step 2:** In `internal/rag/rag.go`, add the `MultiRetriever` interface directly below the existing `Retriever` interface:

```go
// MultiRetriever is an optional capability a Retriever may implement: run
// query.MultiHybrid's multi-query expansion instead of single-query Hybrid.
// BuildGrounding type-asserts for it when Options.MultiQuery is set and falls
// back to Retrieve when the assertion fails — every existing Retriever
// (including every test fake) keeps compiling with no change, and a
// Retriever that doesn't support multi-query degrades to single-query
// search rather than erroring.
type MultiRetriever interface {
	RetrieveMulti(ctx context.Context, q string, k int) ([]Hit, error)
}
```

- [ ] **Step 3:** In `internal/rag/adapters.go`, update `HybridRetriever.Retrieve` to map `Type`, and add `RetrieveMulti`:

```go
// Retrieve runs the hybrid search and maps the top-k hits to rag.Hit.
func (h HybridRetriever) Retrieve(ctx context.Context, q string, k int) ([]Hit, error) {
	f := h.Filter
	f.Limit = k
	hits, err := query.Hybrid(ctx, h.Store, h.Emb, q, f)
	if err != nil {
		return nil, err
	}
	out := make([]Hit, 0, len(hits))
	for _, x := range hits {
		out = append(out, Hit{ID: x.ID, Title: x.Title, Type: x.Type, Score: x.Score})
	}
	return out, nil
}

// RetrieveMulti runs the multi-query expansion (query.MultiHybrid) instead of
// single-query Hybrid — same RRF fusion, same ranking math, just seeded from
// ExpandQuery's subqueries instead of one query string. Satisfies
// rag.MultiRetriever.
func (h HybridRetriever) RetrieveMulti(ctx context.Context, q string, k int) ([]Hit, error) {
	f := h.Filter
	f.Limit = k
	hits, err := query.MultiHybrid(ctx, h.Store, h.Emb, q, f)
	if err != nil {
		return nil, err
	}
	out := make([]Hit, 0, len(hits))
	for _, x := range hits {
		out = append(out, Hit{ID: x.ID, Title: x.Title, Type: x.Type, Score: x.Score})
	}
	return out, nil
}
```

- [ ] **Step 4:** No new test file. `adapters.go` wires real `postgres.Store`/`embed.Embedder` and has no existing unit test (`internal/rag/adapters_test.go` does not exist) — needs a live DB, matches this codebase's established convention for thin adapter wiring (e.g. `cmd/pixkb/ask.go` has never had a test). Task 2 unit-tests the logic this enables via a hand-written fake.
- [ ] **Step 5:** `go build ./...`, `go vet ./...`. Confirm `internal/rag`'s existing tests still pass: `go test ./internal/rag/... -v` — all pre-existing tests must be unaffected by adding a struct field and a new interface/method (pure addition, nothing removed or renamed).
- [ ] **Step 6:** Commit: `git add internal/rag/rag.go internal/rag/adapters.go && git commit -m "feat: add rag.Hit.Type and MultiRetriever for RAG multi-query support"`.

---

### Task 2: `BuildGrounding` — multi-query dispatch, diversity, multi-seed expansion, weak-evidence refusal

**Files:**
- Modify: `internal/rag/rag.go`
- Test: `internal/rag/rag_test.go`

**Interfaces:**
- Consumes: `Hit.Type` and `MultiRetriever` from Task 1.
- Produces: `Options.MultiQuery bool`, `Options.Diversify bool`, `Options.ExpandSeeds int`, `Options.MinScore float64`; unexported `func retrieve(ctx context.Context, r Retriever, q string, opts Options) ([]Hit, error)`; unexported `func diversify(hits []Hit) []Hit`; `func (o Options) expandSeeds() int`.

- [ ] **Step 1:** In `internal/rag/rag.go`, extend `Options`:

```go
// Options tune retrieval + assembly. The zero value is usable (defaults applied).
type Options struct {
	TopK          int     // hybrid hits to take (default defaultTopK)
	ExpandRelated bool    // also pull the graph neighbours of the seed hit(s)
	MaxChars      int     // grounding char budget, ~4 chars/token (default defaultMaxChars)
	MultiQuery    bool    // use the Retriever's MultiRetriever (multi-query expansion) when it implements one; silently falls back to single-query Retrieve otherwise
	Diversify     bool    // prefer the first hit of each concept Type before filling remaining slots by rank
	ExpandSeeds   int     // how many top hits' graph neighbours to pull when ExpandRelated (default 1, preserves the pre-upgrade single-seed behavior)
	MinScore      float64 // refuse (empty Grounding, no agent turn spent) when the top hit's score is below this (0 = disabled)
}
```

- [ ] **Step 2:** Add the `expandSeeds` accessor next to `topK()`/`maxChars()`:

```go
func (o Options) expandSeeds() int {
	if o.ExpandSeeds > 0 {
		return o.ExpandSeeds
	}
	return 1
}
```

- [ ] **Step 3:** Add the `retrieve` dispatch helper and `diversify` reshuffle, above `BuildGrounding`:

```go
// retrieve dispatches to the Retriever's multi-query path when the caller
// asked for one (Options.MultiQuery) AND the Retriever supports it — a type
// assertion, not an interface requirement, so every existing single-query
// Retriever (and every existing test fake) needs no change. Without a
// MultiRetriever, MultiQuery is silently a no-op fallback to single-query
// Hybrid, matching the spec's "a failed rewrite step must fall back to
// single-query hybrid search" constraint by construction.
func retrieve(ctx context.Context, r Retriever, q string, opts Options) ([]Hit, error) {
	if opts.MultiQuery {
		if mr, ok := r.(MultiRetriever); ok {
			return mr.RetrieveMulti(ctx, q, opts.topK())
		}
	}
	return r.Retrieve(ctx, q, opts.topK())
}

// diversify reorders hits so the first hit of each distinct Type is promoted
// ahead of any later hit of a Type already seen — a stable, rank-preserving
// reshuffle, not a re-score (ties within a group keep their relative order).
// Hits with Type == "" are never deduped against each other (each is treated
// as its own group), so untyped concepts are never silently dropped.
// Deterministic: the same input order always yields the same output order.
func diversify(hits []Hit) []Hit {
	seenType := map[string]bool{}
	var first, rest []Hit
	for _, h := range hits {
		if h.Type != "" && seenType[h.Type] {
			rest = append(rest, h)
			continue
		}
		if h.Type != "" {
			seenType[h.Type] = true
		}
		first = append(first, h)
	}
	return append(first, rest...)
}
```

- [ ] **Step 4:** In `BuildGrounding`, replace the retrieval + empty-check block:

```go
	g := Grounding{Query: q}
	hits, err := r.Retrieve(ctx, q, opts.topK())
	if err != nil {
		return g, fmt.Errorf("retrieve: %w", err)
	}
	if len(hits) == 0 {
		return g, nil // OOD / empty — caller refuses
	}
```

with:

```go
	g := Grounding{Query: q}
	hits, err := retrieve(ctx, r, q, opts)
	if err != nil {
		return g, fmt.Errorf("retrieve: %w", err)
	}
	if len(hits) == 0 {
		return g, nil // OOD / empty — caller refuses
	}
	if opts.MinScore > 0 && hits[0].Score < opts.MinScore {
		return g, nil // weak evidence — refuse without spending an agent turn
	}
	if opts.Diversify {
		hits = diversify(hits)
	}
```

- [ ] **Step 5:** In the same function, replace the single-seed `ExpandRelated` block:

```go
	if opts.ExpandRelated {
		// Neighbours of the single best hit — the most likely to share context.
		if nb, err := r.Related(ctx, hits[0].ID); err == nil {
			for _, id := range nb {
				add(id)
			}
		}
	}
```

with:

```go
	if opts.ExpandRelated {
		// Neighbours of the top N seed hits (N = ExpandSeeds, default 1 — the
		// pre-upgrade behavior of expanding only the single best hit).
		seeds := opts.expandSeeds()
		if seeds > len(hits) {
			seeds = len(hits)
		}
		for _, h := range hits[:seeds] {
			nb, err := r.Related(ctx, h.ID)
			if err != nil {
				continue
			}
			for _, id := range nb {
				add(id)
			}
		}
	}
```

- [ ] **Step 6:** Add tests to `internal/rag/rag_test.go`. First, a fake implementing `MultiRetriever` (place near the existing `fakeRetriever`):

```go
type fakeMultiRetriever struct {
	fakeRetriever
	multiHits []Hit
}

func (f *fakeMultiRetriever) RetrieveMulti(_ context.Context, _ string, _ int) ([]Hit, error) {
	return f.multiHits, nil
}
```

Then the new test cases:

```go
func TestBuildGrounding_MultiQueryUsesMultiRetriever(t *testing.T) {
	r := &fakeMultiRetriever{
		fakeRetriever: fakeRetriever{hits: []Hit{{ID: "single.md", Title: "Single"}}},
		multiHits:     []Hit{{ID: "multi.md", Title: "Multi"}},
	}
	cs := fakeSource{
		"single.md": concept("single.md", "Single", "body", "doc:single"),
		"multi.md":  concept("multi.md", "Multi", "body", "doc:multi"),
	}
	g, err := BuildGrounding(context.Background(), r, cs, "q", Options{MultiQuery: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Chunks) != 1 || g.Chunks[0].ID != "multi.md" {
		t.Fatalf("MultiQuery should use RetrieveMulti's hits, got %+v", g.Chunks)
	}
}

func TestBuildGrounding_MultiQueryFallsBackWithoutMultiRetriever(t *testing.T) {
	r := &fakeRetriever{hits: []Hit{{ID: "a.md", Title: "A"}}}
	cs := fakeSource{"a.md": concept("a.md", "A", "body", "doc:a")}
	g, err := BuildGrounding(context.Background(), r, cs, "q", Options{MultiQuery: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Chunks) != 1 || g.Chunks[0].ID != "a.md" {
		t.Fatalf("MultiQuery without a MultiRetriever must fall back to Retrieve, got %+v", g.Chunks)
	}
}

func TestBuildGrounding_DiversifyOrdersByTypeFirst(t *testing.T) {
	r := &fakeRetriever{hits: []Hit{
		{ID: "ref1.md", Type: "Reference"},
		{ID: "ref2.md", Type: "Reference"},
		{ID: "api1.md", Type: "ApiEndpoint"},
	}}
	cs := fakeSource{
		"ref1.md": concept("ref1.md", "R1", "body", "doc:r1"),
		"ref2.md": concept("ref2.md", "R2", "body", "doc:r2"),
		"api1.md": concept("api1.md", "A1", "body", "doc:a1"),
	}
	g, err := BuildGrounding(context.Background(), r, cs, "q", Options{Diversify: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Chunks) != 3 {
		t.Fatalf("expected all 3 chunks, got %+v", g.Chunks)
	}
	if g.Chunks[0].ID != "ref1.md" || g.Chunks[1].ID != "api1.md" || g.Chunks[2].ID != "ref2.md" {
		t.Fatalf("diversify should promote the first ApiEndpoint ahead of the second Reference, got order %v",
			[]string{g.Chunks[0].ID, g.Chunks[1].ID, g.Chunks[2].ID})
	}
}

func TestBuildGrounding_ExpandRelatedMultiSeed(t *testing.T) {
	r := &fakeRetriever{
		hits: []Hit{{ID: "a.md"}, {ID: "b.md"}},
		related: map[string][]string{
			"a.md": {"n1.md"},
			"b.md": {"n2.md"},
		},
	}
	cs := fakeSource{
		"a.md":  concept("a.md", "A", "body A", "doc:a"),
		"b.md":  concept("b.md", "B", "body B", "doc:b"),
		"n1.md": concept("n1.md", "N1", "body N1", "doc:n1"),
		"n2.md": concept("n2.md", "N2", "body N2", "doc:n2"),
	}
	g, err := BuildGrounding(context.Background(), r, cs, "q", Options{ExpandRelated: true, ExpandSeeds: 2})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(g.Chunks))
	for i, c := range g.Chunks {
		ids[i] = c.ID
	}
	want := []string{"a.md", "b.md", "n1.md", "n2.md"}
	if len(ids) != len(want) {
		t.Fatalf("ExpandSeeds:2 should pull neighbours of both seeds, got %v", ids)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ExpandSeeds:2 order = %v, want %v", ids, want)
		}
	}
}

func TestBuildGrounding_MinScoreRefuses(t *testing.T) {
	r := &fakeRetriever{hits: []Hit{{ID: "weak.md", Score: 0.1}}}
	cs := fakeSource{"weak.md": concept("weak.md", "Weak", "body", "doc:weak")}
	g, err := BuildGrounding(context.Background(), r, cs, "q", Options{MinScore: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Chunks) != 0 {
		t.Fatalf("a top hit below MinScore must refuse (empty grounding), got %+v", g.Chunks)
	}
}

func TestBuildGrounding_MinScorePassesStrongEvidence(t *testing.T) {
	r := &fakeRetriever{hits: []Hit{{ID: "strong.md", Score: 0.9}}}
	cs := fakeSource{"strong.md": concept("strong.md", "Strong", "body", "doc:strong")}
	g, err := BuildGrounding(context.Background(), r, cs, "q", Options{MinScore: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Chunks) != 1 || g.Chunks[0].ID != "strong.md" {
		t.Fatalf("a top hit at/above MinScore must proceed normally, got %+v", g.Chunks)
	}
}
```

- [ ] **Step 7:** Run `go test ./internal/rag/... -v` — every pre-existing test (`TestBuildGrounding_RanksAndTags`, `TestBuildGrounding_EmptyOnNoHits`, `TestBuildGrounding_BudgetKeepsFirst`, `TestBuildGrounding_SkipsMissingAndEmpty`, `TestBuildGrounding_ExpandRelated`) must still pass UNMODIFIED — this is the proof the upgrade is purely additive. All 7 new tests must also pass.
- [ ] **Step 8:** `go build ./...`, `go vet ./...`.
- [ ] **Step 9:** Commit: `git add internal/rag/rag.go internal/rag/rag_test.go && git commit -m "feat: add multi-query, diversify, multi-seed expand, min-score refusal to BuildGrounding"`.

---

### Task 3: CLI `pixkb ask` + MCP `kb_ask` — expose the new options

**Files:**
- Modify: `cmd/pixkb/ask.go`
- Modify: `internal/kbmcp/ask.go`

**Interfaces:**
- Consumes: `rag.Options.MultiQuery/Diversify/ExpandSeeds/MinScore` from Task 2.

- [ ] **Step 1:** In `cmd/pixkb/ask.go`, add flag variables and flags, and wire them into `rag.Options`:

Change the var block:

```go
		var dsn, provider, typ string
		var topK, expandSeeds int
		var expand, asJSON, multi, diversify bool
		var minScore float64
```

Change the `rag.Ask` call's `rag.Options{...}` argument:

```go
				rag.Options{
					TopK:          topK,
					ExpandRelated: expand,
					MultiQuery:    multi,
					Diversify:     diversify,
					ExpandSeeds:   expandSeeds,
					MinScore:      minScore,
				},
```

Add the new flags alongside the existing `cmd.Flags()` calls:

```go
	cmd.Flags().BoolVar(&multi, "multi", false, "expand the question into multiple subqueries before retrieving (query.MultiHybrid)")
	cmd.Flags().BoolVar(&diversify, "diversify", false, "prefer one concept per type before filling remaining grounding slots by rank")
	cmd.Flags().IntVar(&expandSeeds, "expand-seeds", 0, "graph-neighbour seed hits to expand when --expand is set (0 = default 1)")
	cmd.Flags().Float64Var(&minScore, "min-score", 0, "refuse when the top retrieved hit's score is below this (0 = disabled)")
```

- [ ] **Step 2:** In `internal/kbmcp/ask.go`, add fields to `askIn` and wire them:

```go
type askIn struct {
	Question    string  `json:"question" jsonschema:"natural-language question to answer from the KB"`
	Type        string  `json:"type,omitempty" jsonschema:"optional concept-type filter for retrieval"`
	TopK        int     `json:"top_k,omitempty" jsonschema:"concepts to ground on (default 6)"`
	Expand      bool    `json:"expand,omitempty" jsonschema:"also ground on the seed hit(s)' graph neighbours"`
	Multi       bool    `json:"multi,omitempty" jsonschema:"expand the question into multiple subqueries before retrieving"`
	Diversify   bool    `json:"diversify,omitempty" jsonschema:"prefer one concept per type before filling remaining grounding slots by rank"`
	ExpandSeeds int     `json:"expand_seeds,omitempty" jsonschema:"graph-neighbour seed hits to expand when expand is set (0 = default 1)"`
	MinScore    float64 `json:"min_score,omitempty" jsonschema:"refuse when the top retrieved hit's score is below this (0 = disabled)"`
}
```

And the `rag.Options{...}` literal inside `registerAsk`:

```go
			rag.Options{
				TopK:          in.TopK,
				ExpandRelated: in.Expand,
				MultiQuery:    in.Multi,
				Diversify:     in.Diversify,
				ExpandSeeds:   in.ExpandSeeds,
				MinScore:      in.MinScore,
			},
```

- [ ] **Step 3:** No new test. Neither `cmd/pixkb/ask.go` nor `internal/kbmcp/ask.go` has an existing test (both need a live DB + a live agent fleet to exercise end-to-end) — matches the established convention already used for this exact command/tool pair. Task 2's unit tests are what cover the new logic.
- [ ] **Step 4:** `go build ./...`, `go vet ./...`.
- [ ] **Step 5:** Commit: `git add cmd/pixkb/ask.go internal/kbmcp/ask.go && git commit -m "feat: expose multi-query, diversify, expand-seeds, min-score on pixkb ask and kb_ask"`.

---

### Task 4: Full verification + backlog

**Files:**
- Modify: `docs/BACKLOG.md`

- [ ] **Step 1:** Run the full suite: `go build ./...`, `go vet ./...`, `go test ./... -short` — all packages must pass, including every pre-existing test (this plan touches no ranking code, only `internal/rag` + thin CLI/MCP wiring).
- [ ] **Step 2:** Run the deterministic ranking gates to confirm zero regression (this plan doesn't touch `query.Hybrid`/`query.MultiHybrid`, so these must be unchanged from before Task 1 — run to prove it, not to tune it): `bash eval/tophit.sh eval/cases-precise-ids.tsv` and `bash eval/tophit.sh eval/cases-fuzzy-ids.tsv` (or the equivalent invocation documented at the top of `eval/tophit.sh`). If no DSN is reachable, note the skip in the task report — do not treat it as a failure, matching this plan's established convention for DB-gated checks.
- [ ] **Step 3:** If a DSN and an agent provider are both reachable, run `ANSWERER=claude bash eval/run-rag-judge.sh` per the spec's Feature 5 acceptance criterion ("`eval/run-rag-judge.sh` must pass or improve before changing default RAG retrieval behavior") — note this plan changes no *default* (`Options{}`) behavior, so this run is a confirmation, not a gate on new code paths the judge doesn't yet exercise. If unreachable, note the skip.
- [ ] **Step 4:** Backlog (P2) in `docs/BACKLOG.md`:
  - Partial-chunk budget trimming: `BuildGrounding`'s char budget is still all-or-nothing per whole concept body (a chunk either fits or is dropped); truncating the last admitted chunk to fill the remaining budget would pack denser context but risks cutting a citation mid-thought — deliberately deferred.
  - `Diversify`'s "one per type first" is a simple promotion, not a per-type quota (e.g. "at most 2 ApiEndpoint, at least 1 Reference") — the spec's retrieval-policy example (one reference + one endpoint + one ISO message + one manual section) is a stronger diversity contract than this task implements; revisit once Feature 6 (eval expansion) has a diversity metric to measure against.
  - Feature 6 (Search Evaluation Expansion), Feature 7 (Domain-Aware Query Understanding), Feature 8 (Search Quality Operations) remain unimplemented from `docs/SEARCH-CAPABILITY-SPEC.md` — each needs its own scoped plan.
- [ ] **Step 5:** Commit: `git add docs/BACKLOG.md && git commit -m "docs: backlog RAG diversity quota, partial-chunk budget trimming, remaining Feature 6-8 scope"`.
