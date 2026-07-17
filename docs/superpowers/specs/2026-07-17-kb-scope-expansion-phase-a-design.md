# Design: KB Scope Expansion — Phase A (unified domain model + tax API contract)

**Date:** 2026-07-17
**Status:** Draft (brainstormed; awaiting spec review → writing-plans)
**Milestone context:** First phase of the BACEN + Receita Federal scope expansion
(BACKLOG "SCOPE EXPANSION" item). Phases B (Reforma Tributária legislation),
C (BACEN split-payment specs), and D (calculator source/schema) are out of scope
here and each get their own spec→plan cycle.

## Goal

Grow pixkb from a single-domain (BACEN Pix/SPB) knowledge base into a unified KB
that also carries **Receita Federal tax** content, starting with the smallest
real increment: a **domain model** over the existing concept set plus ingestion
of the **consumption-tax calculator's OpenAPI contract** (CBS / IBS / Imposto
Seletivo). The driving use case is **cross-domain answers** — e.g. how CBS/IBS
are computed and split at Pix payment time — so pix and tax concepts must live in
one store, be retrievable together, and be separable by domain.

Success is demonstrable: after Phase A, `search "cálculo CBS IBS"` surfaces the
tax endpoints ranked by the same hybrid RRF as everything else, and a domain
filter cleanly isolates each side.

## Non-goals (Phase A)

- Legislation / normative text (Reforma Tributária, LC 214/2025) — Phase B.
- BACEN split-payment specs — Phase C.
- Modeling the calculator's computation logic from its Spring Boot source /
  Flyway schema — Phase D.
- A first-class `Domain` struct field on `okf.Concept` (see decision below).
- Any live network dependency — everything is ingested from local offline files.

## Architecture

Phase A reuses pixkb's existing ingest → bundle → reindex pipeline and adds two
small pieces: a **domain tag** applied across all concepts, and a **standalone
OpenAPI spec source** for the tax contract.

### Decision 1 — domain as a namespaced TAG, not a schema field

`okf.Concept` already has `Tags []string` and no `Domain` field. Represent the
domain axis as a namespaced tag: **`domain:pix`** or **`domain:tax`** on every
concept.

- **Why a tag:** zero schema/frontmatter/store migration; the existing tag
  filter already queries it; the planned HQL `domain` field maps 1:1 to the
  `domain:` tag prefix; and it is human-readable in the bundle.
- **Why namespaced (`domain:pix`) not bare (`pix`):** keeps the domain axis
  distinct from content tags (`manual`, `api`, `iso20022`, …) so a filter on
  domain can never collide with a coincidental content tag, and gives HQL an
  unambiguous field→tag mapping.
- **Alternative considered:** a first-class `Domain` field on `okf.Concept`.
  Cleaner semantically and queryable without tag parsing, but it is a migration
  touching the OKF frontmatter, the Postgres schema, and every reader. Deferred
  under YAGNI; promotable later if the tag proves load-bearing (e.g. if domain
  needs its own index or column).

### Decision 2 — backfill existing concepts via a single post-gather pass

No current source emits a domain tag, so a per-source retag would touch every
adapter. Instead add ONE normalization step in the gather path
(`ingest.GatherAll` / `buildSources` seam): after all sources produce concepts,
a `tagDomain(concepts)` pass ensures each concept has exactly one `domain:*` tag —
concepts a source already tagged (`domain:tax` from the tax source) keep theirs;
everything else defaults to `domain:pix`. This backfills the existing 211 pix
concepts with no per-adapter edits and makes "pix unless explicitly tax" a single
well-tested rule.

### Decision 3 — standalone OpenAPI spec source with an attached domain

`NewOpenAPISource(files)` already parses OpenAPI/Swagger into one `ApiEndpoint`
concept per path+method (air-gap friendly; JSON parses via the YAML decoder).
Today it is only fed by `discoverOpenAPISpecs`, which globs specs inside staged
**repo mirrors**. Phase A adds a config-driven way to ingest a **standalone**
spec that isn't part of a repo mirror:

- New `pixkb.yaml` key `openapi_specs:` — a list of entries, each `{ file, api,
  domain }` (file path, API display/slug name, domain tag). The tax entry points
  at the calculator's `api-docs` spec placed in the per-user mirror dir.
- The source applies the entry's `domain` tag (`domain:tax`) plus its existing
  `["api", slug]` tags to every emitted endpoint concept. (Concretely: either a
  thin `NewOpenAPISourceWithDomain(files, domain)` constructor, or the
  `tagDomain` pass from Decision 2 keyed off the source — the plan picks one; the
  simplest is to let the tax entry seed `domain:tax` and `tagDomain` leave it.)

## Data flow

1. Operator places the tax OpenAPI spec (from the offline `calculadora.zip`,
   endpoint `…/api/api-docs`) at a relative path under the per-user mirror dir
   (`%LocalAppData%\PixKB\mirror\openapi\tributos-consumo.json` on Windows),
   mirroring the manual-PDF convention established in `08b7ec0`.
2. `pixkb.yaml` gains an `openapi_specs:` entry: `{ file:
   mirror/openapi/tributos-consumo.json, api: tributos-consumo, domain: tax }`.
3. `pixkb ingest` → `GatherAll` runs all sources; the tax spec yields
   `ApiEndpoint` concepts tagged `domain:tax`, `api`, `tributos-consumo`; the
   `tagDomain` pass stamps `domain:pix` on the existing concepts → new epoch.
4. Reindex/embeddings happen as part of the epoch commit; tax endpoints are
   embedded and searchable alongside pix concepts in the one store.
5. Retrieval: `search "cálculo CBS IBS"` returns tax endpoints via the same
   hybrid FTS+vector RRF; `search --tag domain:tax` isolates the tax domain;
   `--tag domain:pix` isolates pix.

## Components (units of work)

| Unit | What | Depends on |
|------|------|-----------|
| `tagDomain` pass | Post-gather normalization: every concept gets exactly one `domain:*` tag, defaulting to `domain:pix`. | `ingest.GatherAll` seam |
| `openapi_specs` config | New `pixkb.yaml` key + `Config` field + `applyConfigFile` merge, mirroring `api_docs`/`scout_crawl_*`. | `cmd/pixkb/config.go` |
| standalone OpenAPI wiring | `buildSources` feeds `openapi_specs` files into the OpenAPI source with the entry's domain. | `NewOpenAPISource`, config |
| domain-aware search surfacing | Confirm `--tag domain:tax|domain:pix` filters correctly (should need no new code — existing tag filter). | existing search |

## Error handling

- Missing spec file: consistent with the existing all-or-nothing gather — a
  listed `openapi_specs` file that is absent aborts ingest with a clear error
  (same contract as `pdfs:`; the graceful-skip improvement is a tracked P3, not
  Phase A).
- Malformed/JSON-vs-YAML spec: `NewOpenAPISource` already best-effort decodes;
  a spec with no `paths` yields zero concepts (warn, don't fail).
- A concept arriving with two `domain:*` tags is a bug in a source — `tagDomain`
  asserts at most one and is unit-tested for it.

## Testing

- **Unit:** `tagDomain` table tests (untagged→`domain:pix`; pre-tagged
  `domain:tax` preserved; idempotent). `openapi_specs` config load test mirroring
  the `scout_crawl_*` tests. OpenAPI-source-with-domain test over a tiny inline
  spec fixture — asserts endpoint concepts carry `domain:tax` (no network).
- **Integration (local DB, the pattern used all session):** ingest with the tax
  spec against the throwaway pgvector; assert (a) tax `ApiEndpoint` concepts
  present and tagged `domain:tax`, (b) existing concepts now `domain:pix`,
  (c) `search --tag domain:tax` returns only tax, (d) a cross-domain query
  surfaces a tax endpoint. Diff concept counts before/after to prove no pix
  concept is lost.
- **No prod mutation in the plan** — prod re-baseline is a separate, explicit
  decision after local validation, consistent with this session's practice.

## Open questions to settle in the plan

1. Domain tag application: a `NewOpenAPISourceWithDomain` constructor vs. relying
   solely on the `tagDomain` default. Lean: keep `NewOpenAPISource` unchanged and
   pass domain through the `openapi_specs` entry, applied in `buildSources`.
2. Whether `tagDomain` also normalizes the tax source's slug tag, or leaves
   content tags untouched (recommended: only touches `domain:*`).
