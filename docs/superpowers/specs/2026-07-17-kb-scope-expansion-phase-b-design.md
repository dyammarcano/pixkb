# Design: KB Scope Expansion — Phase B (Reforma Tributária legislation, LC 214/2025)

**Date:** 2026-07-17
**Status:** Draft (brainstormed; awaiting spec review → writing-plans)
**Milestone context:** Second phase of the BACEN + Receita Federal scope expansion
(BACKLOG "SCOPE EXPANSION" item). Phase A (unified domain model + tax OpenAPI
contract) shipped `bffae5b`. Phases C (BACEN split-payment specs) and D
(calculator source/schema) are out of scope here and each get their own
spec→plan cycle.

## Goal

Ingest the **full text of Lei Complementar 214/2025** (the Reforma Tributária
consumption-tax law: CBS, IBS, Imposto Seletivo, split payment) as **article-level
concepts**, tagged `domain:tax`, retrievable by the same hybrid FTS+vector RRF as
everything else. The driving use case remains cross-domain: a split-payment
question — how CBS/IBS are computed and split at Pix payment time — should resolve
to the exact **artigo** of the law *and* the tax-calc API endpoint (Phase A) that
implements it, both surfaced from one retrieval.

Success is demonstrable: after Phase B, `search "recolhimento na liquidação
financeira split payment"` surfaces the governing `LegalArticle` concept(s); a
`--type LegalArticle` filter cleanly isolates the statute; and the existing
`domain:tax` filter returns both the law and the tax endpoints together.

## Non-goals (Phase B)

- BACEN split-payment operational specs (the Pix-rail side) — Phase C.
- Modeling the calculator's computation logic from its Spring Boot source /
  Flyway schema — Phase D.
- **Automatic cross-links** from articles to tax endpoints or Pix charge flows.
  Speculative and hard to do reliably; the shared `domain:tax` tag + RRF already
  co-retrieve law and endpoints. Deferred under YAGNI; revisit only if retrieval
  proves it necessary.
- English translation of the statute (its own BACKLOG "KB standardized in
  English" item; the law is PT-native and stays PT here).
- Any live network dependency — the law is ingested from a local offline PDF,
  matching every other pixkb source.
- A schema migration — `LegalArticle` is a new `Type` string value, not a new
  column or frontmatter field (see Decision 1).

## Architecture

Phase B reuses pixkb's existing ingest → bundle → reindex pipeline and adds one
new source adapter plus one config key. It does **not** touch the store schema,
the embedder, or search.

### Decision 1 — `LegalArticle` as a new `Type` string, not a schema change

`okf.Concept.Type` is a free-form string column already carrying `ManualSection`,
`ApiEndpoint`, `Reference`, `PacsMessage`, `CamtMessage`, `WebPage`, `Repo`. Add
**`LegalArticle`** as one more value.

- **Why:** zero store/frontmatter/reader migration — exactly how `WebPage` and the
  ISO message types were added. It immediately gives a clean `--type LegalArticle`
  retrieval axis (and the future HQL `type` enum) with no new code in the store.
- **Alternative considered:** reuse `ManualSection`. Rejected — it would blur the
  statute against the BACEN Pix manual in every `--type` filter and lose the
  clean law-vs-manual query axis for no schema saving (both are just string
  values).

### Decision 2 — statute-aware sectioner, reusing the PDF text extractor

The existing `pdfSource` extracts text with `extractPDFText` (multi-page,
skip-malformed-page tolerant — keep it verbatim) and then splits with
`splitSections`, which keys on **manual** headings (`3.2 Título`, ALL-CAPS lines).
A statute's structure is entirely different (`Art. Nº`, `§`, incisos, alíneas,
`TÍTULO`/`CAPÍTULO`/`Seção`), so `splitSections` would mis-split it. Phase B adds a
**new sectioner** — it does not modify `splitSections` (the Pix manual still needs
it):

- **Structural context tracking.** As it scans lines, the sectioner maintains the
  current `LIVRO` / `TÍTULO` / `CAPÍTULO` / `Seção` / `Subseção` it is inside,
  updating on each such heading line. These become the article's structural tags.
- **Article splitting.** It splits on `Art. Nº` markers — `Art. 1º`, `Art. 22.`,
  `Art. 22-A` (letter-suffixed articles inserted by amendment) — accumulating each
  article's following `§`, inciso (`I -`, `II -`), and alínea (`a)`, `b)`) lines
  into that article's body until the next `Art.` marker.
- **Preamble / ementa.** Everything before `Art. 1º` (the ementa and promulgation
  clause) becomes one leading concept (`art-0000-ementa`), so nothing is dropped.
- **Anexos.** LC 214/2025's annexes are rate/goods **tables**, which do not fit the
  article model. Each Anexo becomes one `Reference` concept (`anexo-<n>`), tagged
  `domain:tax`, `lei:lc-214-2025`, `anexo` — coarse but honest; table modeling is
  explicitly not attempted here.

### Decision 3 — structural context as namespaced TAGS, not new fields

Following Phase A's domain-as-tag decision, the article's structural position is
carried as namespaced tags on the concept, not new `okf.Concept` fields:

- `domain:tax` (the domain axis, consistent with Phase A)
- `lei:lc-214-2025` (which statute — lets one KB later hold multiple laws)
- `livro:<slug>`, `titulo:<slug>`, `capitulo:<slug>`, `secao:<slug>` (structural
  position; empty levels are simply omitted)
- `legislacao` (a coarse content tag, mirroring `manual`/`api`)

Rationale is Phase A's, verbatim in spirit: no migration; the existing tag filter
queries it; and the planned HQL `livro`/`titulo`/`lei` fields map 1:1 to these tag
prefixes. Promotable to real fields later if a structural axis proves load-bearing.

### Decision 4 — config-driven, offline, mirror-dir source

Mirrors Phase A's `openapi_specs:` wiring exactly:

- New `pixkb.yaml` key `legislation:` — a list of `{ file, lei, domain }` entries.
- New `Config.Legislation []LegislationConf` field + guarded merge in
  `applyConfigFile` (`if len(fromFile.Legislation) > 0 { … }`), identical in shape
  to Phase A's `OpenAPISpecs` merge.
- `buildSources` appends `NewLegislationSource(files, lei, domain)` for each entry.
- The operator places the PDF at `%LocalAppData%\PixKB\mirror\legislation\
  LC214-2025.pdf` (Windows; macOS/Linux equivalents per the existing mirror-dir
  convention documented in `pixkb.yaml.example`). The PDF is **not** vendored into
  the repo — same rule as the Pix manual and the tax OpenAPI spec.

## Data flow

1. Operator places `LC214-2025.pdf` under the per-user mirror dir
   (`…/PixKB/mirror/legislation/LC214-2025.pdf`).
2. `pixkb.yaml` gains a `legislation:` entry:
   `{ file: mirror/legislation/LC214-2025.pdf, lei: lc-214-2025, domain: tax }`.
3. `pixkb ingest` → `GatherAll` runs all sources; the legislation source yields one
   `LegalArticle` concept per artigo (plus the ementa concept and one `Reference`
   per Anexo), each tagged `domain:tax`, `lei:lc-214-2025`, `legislacao`, and its
   structural `livro:/titulo:/capitulo:/secao:` tags. The Phase A `tagDomain` pass
   sees `domain:tax` already present and leaves it untouched → new epoch.
4. Reindex/embeddings happen as part of the epoch commit; articles are embedded and
   searchable alongside the tax endpoints and the pix corpus in the one store.
5. Retrieval: `search "recolhimento na liquidação financeira"` returns the
   governing articles via hybrid RRF; `--type LegalArticle` isolates the statute;
   `--tag domain:tax` returns law + endpoints together; `--tag titulo:<x>` narrows
   to one título.

## Components (units of work)

| Unit | What | Depends on |
|------|------|-----------|
| `LegalArticle` type | New `Type` string value used by the legislation source; no store change. | `okf.Concept` (none) |
| statute sectioner | Pure function: statute plain text → `[]article` with number, title, body, and structural context. Table-tested. | none |
| `legislation` source | `NewLegislationSource(files, lei, domain)`: `extractPDFText` (reused) → sectioner → `LegalArticle` concepts + ementa + Anexo `Reference`s, tagged per Decision 3. | `extractPDFText`, sectioner |
| `legislation` config | New `pixkb.yaml` key + `Config.Legislation` field + `applyConfigFile` merge, mirroring `openapi_specs`. | `cmd/pixkb/config.go` |
| `buildSources` wiring | Feed `legislation:` entries into `NewLegislationSource`. | config, source |
| retrieval surfacing | Confirm `--type LegalArticle` and `--tag lei:*|livro:*` filter correctly (should need no new code — existing type/tag filters). | existing search |

## Error handling

- **Missing PDF:** consistent with the existing all-or-nothing gather — a listed
  `legislation` file that is absent aborts ingest with a clear error (same contract
  as `pdfs:`/`openapi_specs:`; the graceful-skip improvement remains the tracked P3).
- **No `Art.` markers found** (wrong PDF, or extraction produced no recognizable
  article structure): the source emits zero `LegalArticle` concepts and logs a
  warning rather than fabricating one giant concept — fail loud, not silent. A unit
  test covers the empty-input case.
- **Malformed page:** already handled by `extractPDFText` (skips the page, warns) —
  inherited unchanged.
- **Duplicate article numbers** (e.g. a badly extracted `Art. 22` appearing twice):
  the second gets a disambiguating id suffix; the sectioner is unit-tested to never
  emit two concepts with the same ID.

## Testing

- **Unit (no network, no real PDF):** table tests for the sectioner over a small
  inline statute fixture exercising: article boundaries (`Art. 1º` … `Art. 3º`),
  §/inciso/alínea accumulation into the right article body, structural-context
  tracking (a `CAPÍTULO` line changes the `capitulo:` tag of subsequent articles),
  the `Art. 22-A` letter-suffix edge case, ementa capture, Anexo → `Reference`,
  empty/no-`Art.` input → zero articles + warning, and duplicate-number
  disambiguation. Config-load test for `legislation:` mirroring the
  `openapi_specs`/`scout_crawl_*` tests.
- **Integration (local throwaway DB — the pattern used all session):** ingest a
  small real or representative statute PDF against the throwaway pgvector; assert
  (a) `LegalArticle` concepts present and tagged `domain:tax` + `lei:lc-214-2025`,
  (b) a split-payment free-text query surfaces a governing article, (c) `--type
  LegalArticle` returns only articles, (d) a `domain:tax` query returns both an
  article and a Phase A tax endpoint (the cross-domain proof). Diff concept counts
  before/after to prove no pix/tax concept from earlier phases is lost.
- **No prod mutation in the plan** — prod re-baseline is a separate, explicit
  decision after local validation, consistent with this session's practice.

## Open questions to settle in the plan

1. **Article-number normalization for IDs.** Zero-pad to a fixed width
   (`art-0022.md`) so lexical sort matches numeric order; decide the width (4 digits
   covers LC 214's ~500 articles). Letter suffixes (`22-A`) sort after `22`.
2. **Where structural-heading recognition draws the line.** `Art.`, `§`, inciso,
   alínea, and the five structure keywords are unambiguous; decide whether
   `Parágrafo único` is folded into its article body (recommended: yes, it is part
   of the article) and how `Seção`/`Subseção` slugs collide (namespace the tag with
   the parent capítulo if needed).
3. **Anexo granularity.** One `Reference` per Anexo (recommended) vs. per Anexo
   sub-table. Default to per-Anexo; revisit only if a rate lookup needs finer grain.
4. **Integration-test fixture.** Whether to commit a small hand-authored statute
   PDF as a testdata fixture (deterministic, in-repo) or drive the integration test
   from the operator-supplied LC 214 PDF in the mirror dir (real, but not present on
   a fresh checkout). Lean: hand-authored testdata fixture for determinism, plus a
   documented manual validation step against the real PDF.
