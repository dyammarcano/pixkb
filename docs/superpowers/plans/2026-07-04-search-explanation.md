# Search Explanation and Debug Output Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement Feature 3 of `docs/SEARCH-CAPABILITY-SPEC.md`: an optional, disabled-by-default explanation of why each search hit ranked where it did — FTS rank/score, vector rank/cosine, title-boost and type-authority multipliers, final fused score, retrieval arm, and (for multi-query mode) subquery attribution — surfaced via CLI `--explain` and MCP `explain: true`, without touching ranking itself or default output.

**Architecture:** `query.Hybrid`'s body is split into an unexported `hybridCore` that computes and returns BOTH the final `[]postgres.Hit` AND a parallel `[]Explain` (the per-id components it already computes internally today but currently discards — FTS rank, vector rank+cosine score, type-weight multiplier, title-boost multiplier). `Hybrid` becomes a thin wrapper (`hits, _ := hybridCore(...); return hits, err`) — byte-identical behavior, zero ranking change. A new `HybridExplain` wrapper returns both. `MultiHybrid` already carries `MultiHit.Subqueries` (from the prior plan) — Feature 3's "subquery attribution" requirement is just surfacing that existing field, no new computation.

**Tech Stack:** Go 1.25, `internal/query`, `cmd/pixkb`, `internal/kbmcp`.

## Global Constraints

- Go 1.25.0, `CGO_ENABLED=0`, pure Go.
- **Zero ranking change.** `hybridCore`'s extraction must be a pure refactor — `Hybrid`'s existing tests must pass UNMODIFIED (same inputs, same outputs, same order) after Task 1, before any Explain logic is added in Task 2.
- Default output stays compact — explanation is opt-in only (`--explain` flag / `explain: true` param), never on by default.
- No hidden prompt/agent internals exposed (n/a here — this is pure search-ranking data, not agent prompts).
- Explanation must be valid, machine-readable JSON.
- Follow existing conventions: doc comments explain *why*; `testify`; `t.Parallel()`.

---

### Task 1: Extract `hybridCore` — pure refactor, zero behavior change

**Files:** Modify `internal/query/hybrid.go`.

**Interfaces:** Produces `type Explain struct { FTSRank int; VecRank int; VecScore float64; TypeWeight float64; TitleBoost float64; FinalScore float64; Arm string }` and unexported `func hybridCore(ctx, s Searcher, emb embed.Embedder, q string, f postgres.Filter) ([]postgres.Hit, []Explain, error)`. `Hybrid` becomes `func Hybrid(...) ([]postgres.Hit, error) { hits, _, err := hybridCore(...); return hits, err }`.

- [ ] Rename `Hybrid`'s current body to `hybridCore`, changing its signature to also build and return an `[]Explain` slice, one entry per output hit, populated from data `hybridCore` ALREADY computes (`ftsHits`/`vecHits` ranks — track a `ftsRank[id]`/`vecRank[id]` map same way `fromFTS`/`fromVec` are already tracked; `vecHits`' `.Score` for `VecScore`; `typeWeight(types[id])` for `TypeWeight`; `titleBoost(q, titles[id])` for `TitleBoost`; the final `scores[id]` for `FinalScore`; `armLabel(...)` for `Arm`). Build the `Explain` slice in the SAME final loop that builds `out []postgres.Hit`, same order, same length, same `id` correspondence (index `i` in both slices refers to the same hit).
- [ ] Add `func Hybrid(ctx context.Context, s Searcher, emb embed.Embedder, q string, f postgres.Filter) ([]postgres.Hit, error) { hits, _, err := hybridCore(ctx, s, emb, q, f); return hits, err }` — the exact original public signature, unchanged.
- [ ] Run `go test ./internal/query/... -v` — every existing `TestHybrid_*` test must pass with ZERO changes to the test file. This is the proof the refactor didn't alter ranking.
- [ ] `go build ./...`, `go vet ./...`.
- [ ] Commit: `git add internal/query/hybrid.go && git commit -m "refactor: extract hybridCore, add Explain struct — zero ranking change"`.

---

### Task 2: `HybridExplain` public entrypoint + tests

**Files:** Modify `internal/query/hybrid.go` (add the function); Test: `internal/query/hybrid_test.go` (add cases).

**Interfaces:** Produces `func HybridExplain(ctx, s Searcher, emb embed.Embedder, q string, f postgres.Filter) ([]postgres.Hit, []Explain, error) { return hybridCore(ctx, s, emb, q, f) }` — trivial wrapper, same signature `hybridCore` already has.

- [ ] Add the one-line wrapper.
- [ ] Add test using the existing `fakeSearcher` (same file): call `HybridExplain` with the same fixture `TestHybrid_FusesAndHydrates` uses, assert `len(hits) == len(explains)`, assert `explains[i].Arm` matches `hits[i].Arm`, assert `explains[i].FinalScore == hits[i].Score` (Explain's FinalScore must equal the Hit's own Score field — same value, not a different one), assert the both-arm hit's `FTSRank`/`VecRank` are both nonzero (>0) while a single-arm hit has the other at 0 (sentinel for "not in this arm").
- [ ] `go test ./internal/query/... -v`, `go build ./...`, `go vet ./...`.
- [ ] Commit: `git add internal/query/hybrid.go internal/query/hybrid_test.go && git commit -m "feat: add HybridExplain entrypoint"`.

---

### Task 3: CLI — `pixkb search --explain`

**Files:** Modify `cmd/pixkb/commands.go` (`newSearchCmd`).

- [ ] Add an `explain bool` flag (`--explain`, default false). In `RunE`, when `explain` is true and `mode` is `hybrid` or unset (default), call `query.HybridExplain` instead of `query.Hybrid`, and print a JSON array (one object per hit: `id`, `title`, `rank`, plus the full `Explain` struct fields) via `json.NewEncoder(out).SetIndent("", "  ")` instead of the normal plain-text loop. When `explain` is true but `mode` is `fts`/`vector`/`multi`, return an error `"--explain is only supported with --mode hybrid (or the default)"` — do not silently ignore the flag.
- [ ] No new automated test (needs a live DB, matches `newSearchCmd`'s existing untested convention — see the multi-query-retrieval plan's Task 4 for the identical rationale already established in this codebase).
- [ ] `go build ./...`, `go vet ./...`. Manual smoke test against live DB if available: `pixkb search "criar cobrança imediata" --explain` — confirm valid JSON, confirm default (no `--explain`) output is unchanged plain text.
- [ ] Commit: `git add cmd/pixkb/commands.go && git commit -m "feat: add pixkb search --explain"`.

---

### Task 4: MCP — `search` tool `explain: true`

**Files:** Modify `internal/kbmcp/server.go` (`searchIn`/`searchOut`/`registerSearch`); Test: `internal/kbmcp/server_test.go`.

- [ ] Add `Explain bool` to `searchIn` (`json:"explain,omitempty"`). Add `type explainOut struct { FTSRank int; VecRank int; VecScore float64; TypeWeight float64; TitleBoost float64; FinalScore float64; Arm string }` and add `Explain *explainOut `json:"explain,omitempty"`` to `hitOut` (pointer so it's omitted from JSON entirely when not requested — default output stays exactly as compact as before). In `registerSearch`, when `in.Explain` and mode is hybrid, call `query.HybridExplain` and populate each `hitOut.Explain`; otherwise (explain requested with `mode=multi`/`fts`/`vector`), leave `Explain` nil and do not error (MCP callers get best-effort: no explanation for modes that don't support it yet, rather than a hard failure — less strict than the CLI's explicit error, since an agent caller is more likely to probe capabilities than a human reading `--help`).
- [ ] Add `TestServerSearch_ExplainMode` mirroring `TestServerSearch_MultiMode`'s exact DSN/skip boilerplate (same file) — call `search` with `{"query": "criar cobrança imediata", "explain": true, "limit": 3}`, assert no error, assert response content non-empty.
- [ ] `go test ./internal/kbmcp/... -run 'TestServerReadTools|TestServerSearch_ExplainMode' -v` (PASS or SKIP consistently — no DSN expected in this environment, that's fine), `go build ./...`, `go vet ./...`.
- [ ] Commit: `git add internal/kbmcp/server.go internal/kbmcp/server_test.go && git commit -m "feat: add explain=true to the search MCP tool"`.

---

### Task 5: Surface multi-query subquery attribution + live verification + backlog

**Files:** Modify `cmd/pixkb/commands.go` (the `--explain` + `--mode multi` combination), `docs/BACKLOG.md`.

- [ ] Relax Task 3's restriction: when `--explain` is combined with `--mode multi`, print each hit's `query.MultiHit.Subqueries` (already existing field from the prior plan) as JSON instead of erroring — this is Feature 3's "subquery attribution for multi-query search" requirement, and it's free (the data already exists, just wasn't being printed).
- [ ] Build the binary, run `pixkb search "..." --explain` and `pixkb search "..." --explain --mode multi` against the live KB if a DSN is available; confirm valid JSON, confirm default (no `--explain`) output is byte-identical to before this whole plan (the single most important regression check — explanation must be purely additive).
- [ ] Backlog (P2): matched-query-token highlighting and matched-field-category breakdown (2 of Feature 3's 7 required explain fields not built here — extracting them needs either a Postgres `ts_headline`-style query or client-side re-tokenization, a bigger unit of work); a `/explain` HTTP endpoint / `explain=true` query param for `pixkb serve` (not touched by this plan).
- [ ] Commit: `git add cmd/pixkb/commands.go docs/BACKLOG.md && git commit -m "feat: surface multi-query subquery attribution in --explain; backlog remaining explain fields"`.
