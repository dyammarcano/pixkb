# Multi-Domain Foundation (v0.2 part 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a first-class `domain` dimension to pixkb (model → store → search) plus the pure-Go citation-edge parser and the two generalized domain seams (vocabulary, charter) — the buildable, corpus-independent foundation of the v0.2 cross-domain regulatory graph.

**Architecture:** Additive, non-breaking. A `domain` field flows OKF front-matter → `Concept` → `concept` table (migration 0007, `DEFAULT 'pix'` back-fills the existing 275 concepts) → an optional search facet. Cross-domain `cites` edges are produced by a deterministic parser over BACEN's canonical citation format. Vocabulary and agent-charter become per-domain registries.

**Tech Stack:** Go 1.26, pgx v5, Postgres+pgvector, cobra, yaml.v3, testify.

## Global Constraints

- **Non-breaking:** every existing `v0.1` bundle and DB must read forward unchanged. `domain` defaults to `pix` everywhere it is absent.
- **Air-gap:** no task adds network egress. No corpus files are fetched here.
- **BRL = decimal, never float64** (unaffected here, but the rule stands).
- **Line endings LF; stage explicit paths only (never `git add -A`); conventional commits, no AI attribution.**
- **DB-gated tests** run against the local throwaway DB only (`task testdb:up`, `PIXKB_TEST_DSN=…:5433/pixkb`), never prod. DB-free tests must pass under `go test -short -race`.
- Follow existing package patterns; do not restructure unrelated code.

---

### Task 1: `domain` + `norm_ref` on the OKF concept model

**Files:**
- Modify: `internal/okf/concept.go` (Concept struct), `internal/okf/frontmatter.go` (frontmatter struct + marshal/unmarshal), `internal/okf/reader.go` (map fm→Concept)
- Test: `internal/okf/frontmatter_test.go`, `internal/okf/reader_test.go`

**Interfaces:**
- Produces: `Concept.Domain string`, `Concept.NormRef string`; front-matter keys `domain`, `norm_ref` (both `,omitempty`).

- [ ] **Step 1: Write the failing round-trip test** in `frontmatter_test.go`: build a `Concept{Domain:"bacen-normative", NormRef:"RES-BCB-1-2020", …}`, marshal, unmarshal, assert both fields survive. Add an assertion to the existing reader round-trip test that a concept with no `domain` in front-matter unmarshals to `Domain == ""` (caller defaults later).
- [ ] **Step 2: Run it, confirm FAIL** (`go test ./internal/okf/ -run Domain -v`).
- [ ] **Step 3: Add `Domain`/`NormRef` to `Concept` and `frontmatter`** with `yaml:"domain,omitempty"` / `yaml:"norm_ref,omitempty"`; wire them through `marshalFrontmatter`, `unmarshalFrontmatter`, and the `reader.go` fm→Concept mapping.
- [ ] **Step 4: Run tests, confirm PASS** (`go test ./internal/okf/ -race`).
- [ ] **Step 5: Commit** (`feat(okf): add domain + norm_ref to concept model`).

### Task 2: Migration 0007 — `concept.domain` column

**Files:**
- Create: `internal/store/postgres/schema/0007_concept_domain.up.sql`, `…0007_concept_domain.down.sql`
- Test: `internal/store/postgres/migrations_test.go`

**Interfaces:**
- Produces: `concept.domain TEXT NOT NULL DEFAULT 'pix'` + `concept_domain_idx`.

- [ ] **Step 1:** up: `ALTER TABLE concept ADD COLUMN IF NOT EXISTS domain TEXT NOT NULL DEFAULT 'pix';` then `CREATE INDEX IF NOT EXISTS concept_domain_idx ON concept(domain);`. down: drop index then column.
- [ ] **Step 2:** Follow the existing `migrations_test.go` pattern to assert the new migration is discovered and the file pair parses (DB-free assertion of the embedded set count/order). If the suite has a DB-gated apply/rollback test, extend it to assert the column exists after up and is gone after down.
- [ ] **Step 3:** Run `go test ./internal/store/postgres/ -run Migrations -short` (DB-free) green; note the apply test as DB-gated.
- [ ] **Step 4: Commit** (`feat(store): migration 0007 adds concept.domain`).

### Task 3: Store reads/writes `domain`

**Files:**
- Modify: `internal/store/postgres/concept.go` (INSERT column list + args, SELECT column list + row scan)
- Test: `internal/store/postgres/concept_test.go` (DB-gated)

**Interfaces:**
- Consumes: `okf.Concept.Domain`. Produces: round-tripped `domain` through the store.

- [ ] **Step 1:** Add `domain` to the concept upsert (column, `$N`, value `coalesce to 'pix' when empty`) and to every SELECT that hydrates a `Concept`/`Hit` (add the scan target).
- [ ] **Step 2:** DB-gated test: insert a concept with `Domain:"bacen-normative"`, read it back, assert domain preserved; insert one with empty domain, assert it reads back `pix`.
- [ ] **Step 3:** Run against local DB (`task testdb:up`; `PIXKB_TEST_DSN=…`; `go test ./internal/store/postgres/ -run Concept -p 1`).
- [ ] **Step 4: Commit** (`feat(store): persist concept.domain with pix default`).

### Task 4: Search domain facet — `buildFTSWhere` + `--domain` flag

**Files:**
- Modify: `internal/store/postgres/search.go` (`buildFTSWhere` — optional domain predicate), the search entry that assembles filters
- Modify: `cmd/pixkb/` search command (add repeatable `--domain` flag), output/`--format` to include a domain column
- Test: `internal/store/postgres/search_where_test.go` (DB-free — this is why buildFTSWhere was extracted)

**Interfaces:**
- Consumes: a `domains []string` filter. Produces: `... AND domain = ANY($n)` appended only when the filter is non-empty; empty filter = all domains (v0.1 behavior preserved).

- [ ] **Step 1:** DB-free test in `search_where_test.go`: assert `buildFTSWhere` with no domains yields the current WHERE (regression guard); with `["pix","bacen-normative"]` appends `domain = ANY($k)` and the arg is the string slice at the right position.
- [ ] **Step 2: Run, confirm FAIL.**
- [ ] **Step 3:** Thread an optional `domains []string` into the WHERE assembly; wire the repeatable `--domain` cobra flag → filter; add `domain` to the output rows/`--format`.
- [ ] **Step 4: Run tests, confirm PASS** (`go test ./internal/store/postgres/ -run Where -race`).
- [ ] **Step 5: Commit** (`feat(search): optional --domain facet, all-domain default`).

### Task 5: Citation-edge parser + `pixkb link`

**Files:**
- Create: `internal/link/citation.go` (pure parser), `internal/link/citation_test.go` (table tests)
- Create/modify: `cmd/pixkb/link.go` (`pixkb link [--apply] [--limit N]`)
- Test: `internal/link/citation_test.go` (DB-free); command wiring smoke test

**Interfaces:**
- Produces: `func ParseCitations(body string) []NormRef` where `NormRef` is the canonical id string (e.g. `RES-BCB-1-2020`); `func Edges(conceptID string, body string) []Edge{Src,Dst,Kind:"cites"}`.

- [ ] **Step 1:** Table test covering real BACEN reference strings → expected canonical `norm_ref`: `Resolução BCB nº 1, de 12 de agosto de 2020` → `RES-BCB-1-2020`; `Resolução CMN nº 4.893` → `RES-CMN-4893`; `Circular nº 3.978` → `CIR-3978`; `Instrução Normativa BCB nº 300` → `IN-BCB-300`; plus negative cases (prose "resolução do problema" → none) so it never guesses.
- [ ] **Step 2: Run, confirm FAIL.**
- [ ] **Step 3:** Implement allow-listed regexes (anchored to the instrument keywords + `nº`), normalize number (strip thousands `.`), map instrument→prefix. Unmatched → no edge.
- [ ] **Step 4: Run tests, confirm PASS** (`go test ./internal/link/ -race`).
- [ ] **Step 5:** Wire `pixkb link`: scan concepts, emit `cites` edges to the concept whose `norm_ref` matches (idempotent upsert by `(src,dst,kind)`); `--apply` writes, default dry-run prints. DB-gated write path; the parser is the DB-free core.
- [ ] **Step 6: Commit** (`feat(link): BACEN citation-edge parser + pixkb link`).

### Task 6: Per-domain vocabulary registry

**Files:**
- Move: `internal/query/domain_vocabulary.yaml` → `internal/query/domains/pix/vocabulary.yaml`
- Modify: the vocabulary loader in `internal/query/` (load all `domains/*/vocabulary.yaml`, select/merge by active domain set)
- Test: `internal/query/` loader test (DB-free)

**Interfaces:**
- Consumes: active `domains []string` (empty = all). Produces: merged expansion terms; Pix behavior identical when domain set is empty or `["pix"]`.

- [ ] **Step 1:** DB-free test: loader over a temp `domains/` with two files returns pix terms for `["pix"]` and the union for empty/both; a bacen file's terms don't leak into a `["pix"]`-scoped expansion.
- [ ] **Step 2: Run, confirm FAIL.**
- [ ] **Step 3:** Move the existing yaml verbatim under `domains/pix/`; change the loader (likely `go:embed domains/*/vocabulary.yaml`) to key by directory name; select/merge by active set.
- [ ] **Step 4: Run tests + full `go test ./internal/query/... -race`** (guards no regression in ExpandQuery/MultiHybrid).
- [ ] **Step 5: Commit** (`refactor(query): per-domain vocabulary registry`).

### Task 7: Per-domain agent charter registry

**Files:**
- Modify: `internal/roster/roster.go` (charter keyed by domain; current BACEN/Pix charter registered for `pix` and `bacen-normative`)
- Test: `internal/roster/roster_test.go`

**Interfaces:**
- Produces: `func CharterFor(domain string) string` (or equivalent) returning the domain's system-prompt charter; unknown domain falls back to the BACEN charter with a logged note.

- [ ] **Step 1:** Test: `CharterFor("pix")` and `CharterFor("bacen-normative")` return the BACEN charter; an unknown domain returns the fallback (non-empty) — registry has both pilot domains.
- [ ] **Step 2: Run, confirm FAIL.**
- [ ] **Step 3:** Introduce the small keyed registry; register the existing charter for both pilot domains; keep the corral registration blank-import behavior intact.
- [ ] **Step 4: Run `go test ./internal/roster/ -race`** (note the live-fleet e2e stays `-short`-gated).
- [ ] **Step 5: Commit** (`refactor(roster): per-domain charter registry`).

---

## Build order & dependencies

1 → 2 → 3 (model → migration → store; 3 depends on 1+2). 4 depends on 3. 5 depends on 1 (needs `norm_ref`) and the store for `--apply` (parser itself independent). 6 and 7 are independent of the DB chain — parallelizable, but run through SDD sequentially since 6 touches `internal/query` broadly.

Suggested execution: **1, 2, 3, 4, 5, 6, 7.** After 7: full suite green (`-short -race` DB-free; DB-gated green on local throwaway DB), then `/docs:update` and mark ROADMAP Phase 10 (multi-domain) in progress.

## Out of scope (v0.2 part 2 — separate plan, corpus/operator-gated)

- Ingesting the actual BACEN-normative corpus (needs offline files under `mirrors/bcb/normatives/`).
- Cross-domain RAG citation-provenance + edge traversal in grounding.
- MCP `domain` array on `kb_ask`/search verbs.
- The physical `bundle/pix/` subtree move of existing concepts (gated behind a reindex round-trip on the live bundle).

## Self-review

- Spec coverage: model (§4.1/4.2 → T1/T2/T3), search facet (§6 → T4), citation edges (§5 → T5), vocab + charter seams (§7 → T6/T7). Corpus ingest, RAG traversal, MCP flags, bundle move = explicitly deferred to part 2. ✅
- Placeholder scan: none. ✅
- Type consistency: `Domain`/`NormRef` names used identically across T1/T3/T5; `domain = ANY($n)` filter shape consistent T4/T6. ✅
