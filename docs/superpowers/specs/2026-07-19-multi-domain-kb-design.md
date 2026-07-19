# pixkb v0.2 — Cross-Domain Regulatory Graph (Design)

**Status:** Approved (brainstorm, 2026-07-19) — ready for implementation planning.
**Author:** Dyam Marcano · **Date:** 2026-07-19
**Supersedes/extends:** the single-domain Pix/SPB KB shipped as `v0.1.0`.

> Point-in-time design record — no revision tag by convention.

## 1. Goal

Generalize pixkb from a single Pix/SPB knowledge base into a **multi-domain,
cross-domain regulatory knowledge graph** for the Brazilian regulatory space
(BACEN, government, banking, laws). The differentiator is **cross-domain
reasoning** — answering questions that span domains ("which BACEN resolution
grounds this Pix rule, and what does it require?") — not four isolated silos.

**Pilot (v0.2):** the existing Pix/SPB corpus **plus one new domain,
BACEN normatives** (Resoluções / Circulares / Instruções Normativas), linked to
Pix concepts. Two domains are the minimum needed to prove cross-domain; every
later domain (banking, laws, gov.br) follows the same pattern.

## 2. Non-goals (YAGNI — explicitly out of v0.2)

- More than two domains. No banking/gov/law corpus in this milestone.
- New FTS languages. Both pilot domains are Portuguese → the existing `pixpt`
  config is unchanged. (Multilingual is already half-supported via the per-row
  `concept.language` column; generalizing FTS-by-language is a later milestone.)
- Edge extraction beyond BACEN's canonical citation format.
- Per-domain epoch timelines, per-domain deployments/tenancy, or an auth model.
- HNSW / ANN index changes (exact-kNN stays; unaffected by this work).

## 3. Architecture decisions

### 3.1 One unified index, domain-tagged concepts (not silos)
A single Postgres index holds all domains; every concept carries a `domain`.
Search and RAG fuse **across** domains by default; a `--domain` filter narrows
when the user wants isolation. This is what makes cross-domain reasoning native.

### 3.2 Cross-domain links via the existing `edge` table
Reuse `edge(src, dst, kind)` as-is. Edges may span domains. New `kind` values:
`cites` (a concept references a normative), `implements` (reserved for a later
curated relation). The graph is the connective tissue; retrieval traversal over
these edges is the "reasoning" surface.

### 3.3 Two-tier linking: fusion floor + parsed edges (decided)
- **Floor (day one):** cross-domain retrieval fusion. `MultiHybrid` already fuses
  ranked arms; dropping the implicit single-corpus assumption makes every
  cross-domain concept pair reachable with zero new data.
- **Upgrade (incremental):** a deterministic, pure-Go citation parser materializes
  `cites` edges wherever BACEN's rigid reference format appears. Value exists
  before any edge is written; the graph deepens as edges accrue.

### 3.4 Global epoch timeline (decided — recommended)
The bitemporal epoch engine stays **global**: one epoch line, concepts tagged by
domain. Rationale: `diff`/`reindex`/`as-of` semantics are unchanged, a cross-domain
"as of epoch N" query is meaningful, and per-domain timelines add complexity with
no pilot payoff. (Per-domain epochs remain a future option if domains gain
separate custodians.)

### 3.5 Bitemporal fit (why this engine, not a rewrite)
The `concept_fact` table's valid-time / transaction-time model already fits
regulatory normatives: a resolution is *valid* from its vigência date and
*superseded/revoked* later, and `as-of` gives version history for free. The
pilot does not deepen legal versioning, but the model is chosen deliberately so
the laws domain (next milestone) needs no schema change.

## 4. Data model changes

### 4.1 `domain` field (additive, non-breaking)
- **OKF front-matter** (`internal/okf/frontmatter.go`): add `domain string
  \`yaml:"domain,omitempty"\``.
- **`Concept` struct** (`internal/okf/concept.go`): add `Domain string`.
- **Schema — migration `0007_concept_domain`:**
  `ALTER TABLE concept ADD COLUMN domain TEXT NOT NULL DEFAULT 'pix';`
  plus `CREATE INDEX concept_domain_idx ON concept(domain);`. The `DEFAULT 'pix'`
  back-fills all existing (275) concepts, so `v0.1` bundles read forward with no
  edit. Down-migration drops the column + index.
- **`concept_fact`**: no change required for the pilot (domain is derivable via
  join on `concept.id`); revisit only if as-of-by-domain becomes a query.

### 4.2 `Normative` concept type + canonical reference
- New `type: Normative`. Front-matter extension `norm_ref string
  \`yaml:"norm_ref,omitempty"\`` holds the canonical id (e.g. `RES-BCB-1-2020`),
  which is the stable target for `cites` edges and the citation parser's output key.

### 4.3 Bundle layout
Concepts move under a per-domain subtree: `bundle/<domain>/<concept>.md`
(`bundle/pix/…`, `bundle/bacen-normative/…`). Reserved files (index/log/README)
stay at bundle root. Each domain subtree is independently rebuildable; the
writer/reader resolve a concept's path from its `domain` + `id`.

## 5. Ingest: BACEN normatives (air-gap honest)

- **Sourcing:** normatives are downloaded **offline** into
  `mirrors/bcb/normatives/` (PDF/HTML), same shape as the current `mirrors/bcb`
  corpus and `scoutcrawl` flow. No live egress at ingest time (air-gap rule).
- **Pipeline:** reuse the existing PDF / scoutcrawl `ingest.Source` path; emitted
  concepts are tagged `domain: bacen-normative`, `type: Normative`, and carry
  `norm_ref` parsed from the document header (canonical BACEN numbering).
- **Citation-edge extractor** (`internal/link` or a curate route): a pure-Go,
  table-tested parser matching BACEN reference patterns —
  `Resolução (BCB|CMN)? nº N(, de …)?`, `Circular nº N`, `Instrução Normativa
  BCB nº N` — resolves each to a `norm_ref` and emits
  `edge(src=<citing concept id>, dst=<normative id>, kind='cites')`. Idempotent
  (re-runnable; upsert-by-(src,dst,kind)). Surfaced as `pixkb link` **and** wired
  as one more finding→edge route in the curator loop, so both a one-shot pass and
  the continuous fleet keep edges fresh.

## 6. Search & RAG

- **Search:** default fuses all domains; `--domain <name>` (repeatable) narrows.
  Output/`--format` gains a `domain` column; `HybridExplain` notes the domain of
  each contributing hit. The WHERE assembly already isolated in `buildFTSWhere`
  gets an optional `domain = ANY($n)` predicate.
- **Related-graph expansion:** the existing RAG grounding expansion follows
  `cites`/`implements` edges across domains (bounded by the existing expand/seed
  limits), so an answer about a Pix rule can pull in the normative that grounds it.
- **RAG citations:** each grounded chunk already carries concept id + source_uri;
  add `domain`. The answerer prompt is updated to acknowledge multi-domain
  grounding and to keep cite-or-refuse + PII/LGPD redaction unchanged.

## 7. Generalizing the three domain seams

1. **Vocabulary:** `internal/query/domain_vocabulary.yaml` →
   `internal/query/domains/<name>/vocabulary.yaml`; the loader reads all domain
   vocabularies and selects/merges by the active domain set. Existing Pix vocab
   moves under `domains/pix/` verbatim.
2. **Agent charter:** `internal/roster` gains a charter registry keyed by domain.
   The current BACEN/Pix charter serves both pilot domains (same issuer); the
   registry is the seam future domains plug into.
3. **FTS language:** no change for the pilot (both `pt`, `pixpt` serves both). The
   per-row `language` column is the future generalization point.

## 8. CLI / MCP surface

- `pixkb ingest` — unchanged entry; domain flows from source config.
- `pixkb search [--domain N ...]`, `pixkb ask [--domain N ...]` — optional filters.
- `pixkb link [--apply] [--limit N]` — run the citation-edge extractor.
- MCP: `kb_ask`/search verbs accept an optional `domain` array (server-gated,
  clamped like the existing `TopK`/`ExpandSeeds`).

## 9. Definition of Done (finite — the v0.2 finish line)

1. Migration 0007 applies; all existing concepts read forward as `domain=pix`;
   round-trip (bundle→store→bundle) preserves `domain` and `norm_ref`.
2. A sample set of BACEN normatives ingests into `domain=bacen-normative`.
3. A cross-domain query (`pixkb ask` / `search` with no `--domain`) returns fused
   Pix + normative hits, each labeled with its domain.
4. The citation extractor materializes at least one real `cites` edge from a Pix
   concept to a normative, and RAG grounding traverses it.
5. `--domain pix` reproduces the v0.1 single-domain behavior (no regression).
6. Full suite green (`-short -race`) DB-free; DB-gated paths green against the
   local throwaway DB (`task testdb:up`).

When DoD is met, cut `v0.2.0` and re-run `/project:horizon` at the raised tier to
redraw the line.

## 10. Risks & mitigations

- **Corpus quality of normatives** (PDF chaos, as with the Pix manual): reuse the
  TOC-suppression / hygiene machinery; treat body-quality cleanup as a curator-fleet
  job, not extractor heuristics (per ISSUES history).
- **Citation false positives:** the parser is allow-listed to BACEN's exact formats
  and table-tested against real reference strings; unmatched references simply
  produce no edge (fusion still covers them). Never guess an edge.
- **Bundle-path migration** of the 275 existing concepts into `bundle/pix/`: a
  one-time, git-committed move (recoverable), gated behind a reindex round-trip
  test before the old paths are removed.
- **Air-gap drift:** normative sourcing must be an explicit offline step; no ingest
  path may egress. Document the exception the same way the OpenAI embedder note does.
