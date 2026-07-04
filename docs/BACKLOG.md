# pixkb Backlog
<!-- rev:047 -->

Prioritized future work. P1 = highest. Promote items into the active phase
(see `docs/ROADMAP.md` Phase 7) as they are scheduled.

## P1
- _(none open — the RAG layer shipped; see Shipped. Promote a P2 item here when
  scheduled.)_
- **RAG follow-ups (optional polish).** The core RAG layer is shipped; these are
  nice-to-haves, not blockers: (a) wire the now-shipped `query.MultiHybrid`
  (below) into `rag.Retriever`/`HybridRetriever` for grounding diversity on
  broad questions — the primitive exists, RAG just doesn't call it yet; (b) an
  answer cache keyed by (question-hash, KB-epoch) to avoid re-spending a turn on a
  repeated question; (c) a deterministic PII/LGPD post-filter on the answer (today
  it is prompt-level only). Gate any change on `eval/run-rag-judge.sh`.

## P2
- **Search evaluation expansion follow-ups (Feature 6 of
  `docs/SEARCH-CAPABILITY-SPEC.md` shipped; these are deliberately deferred).**
  `internal/evalkit` (case loaders + pure metrics) and `pixkb eval
  {multi,similar,ood,explain,asof,rag-diversity}` ship deterministic,
  in-process measurement gates over Features 1-5's retrieval entry points
  (`query.MultiHybrid`, `similar.Similar`, `query.Hybrid`,
  `query.HybridExplain`, `rag.Ask`) — no new ranking math, no CI-failing exit
  codes, per this feature's own plan. Remaining, explicitly out of scope for
  that plan:
  - **`eval multi`'s known partial-coverage case** (refund+webhook query,
    1/2 today per `eval/cases-multi-ids.tsv`'s documented baseline) —
    fusion-weighting work to improve cross-subquery coverage without
    regressing precise top@5, deferred per ADR 0002's re-tuning caution.
  - **`eval rag-diversity`'s first live run (2026-07-04, `--provider claude`,
    against the live KB) came back BELOW MIN for both cases** —
    `diversity-devolucao-fluxo` and `diversity-cobranca-fluxo` both reported
    `types=[]` (zero valid citations), not merely below `min-types=2`. The
    implementer's earlier sandbox run couldn't reach the DSN at all; this run
    could, and the answerer returned an answer/refusal whose citations didn't
    resolve to any grounding chunk id (see `rag.validCitations` /
    `Synthesize`'s uncited-answer-downgrades-to-refusal path), so
    `RunRAGDiversity`'s `typeByID` lookup found nothing to count. Needs
    follow-up: run `pixkb ask --json --provider claude "<question>"`
    directly for one of the two questions and inspect whether the agent
    refused outright (plausible — both questions are broader/more compound
    than `eval/cases-rag.tsv`'s existing single-concept questions) or
    answered with citations that just didn't match. Per this plan's own
    guidance: revisit the two questions' wording (they may be reading as
    unanswerable-as-phrased rather than as the intended broad-but-answerable
    Pix flow questions) or lower `min-types` explicitly — do not treat this
    silently as a passing case.
  - Feature 8 (Search Quality Operations) has since shipped — see its own
    backlog block below.
  - **`pixkb eval`'s six subcommands report numbers but never fail the
    process** (`os.Exit(1)` on a bad number) — intentional per the plan's
    Global Constraints (these are measurement tools, like `eval/tophit.sh`,
    not CI gates), but a future CI-gating use case would need a
    `--fail-under`-style flag; not built here.
- **Domain-aware query understanding follow-ups (Feature 7 of
  `docs/SEARCH-CAPABILITY-SPEC.md` shipped; these are deliberately
  deferred).** `internal/query/domain_vocabulary.yaml` + `vocab.go` replace
  the old hardcoded `entityTriggers` table with a versioned, auditable
  12-entry vocabulary (9 enabled, 3 disabled-with-reason); `ExpandQuery`
  matches only enabled entries; `PIXKB_DISABLE_DOMAIN_VOCAB` and
  `pixkb vocab [--reasons]` cover the spec's inspect/disable acceptance
  criterion. All 7 migrated mappings verified byte-identical to the old
  table (every pre-existing `expand_test.go` test passes unmodified);
  precise top@5 unchanged at 96%. Remaining, explicitly out of scope for
  that plan:
  - **The `e2eid` entry's natural-language form doesn't trigger.** Its
    stems (`e2eid`, `endtoend`) only match the literal jargon, not the
    spec's own alias "identificador fim a fim" — a `fim` stem would catch
    it, but `fim` (Portuguese for "end") is too common to add without a
    dedicated precise/fuzzy regression check, which this plan didn't run.
    Needs: add `fim` (or a longer, safer multi-word variant) and re-measure
    `eval/tophit.sh` on both suites before enabling.
  - **The `txid` entry's real-world lift is modest, not dramatic.** Full-
    pipeline measurement (2026-07-04): the fuzzy case "ver os dados de uma
    cobrança pelo identificador da transação" went from api/openapi/get-
    cob-txid.md being ABSENT in `--mode multi --limit 20` to rank 17 — a
    real improvement, but not a top@5 win. `eval/cases-vocab-ids.tsv` only
    measures the standalone subquery's own ranking (rank 1), not this
    full-pipeline fusion result, because `eval/tophit.sh` doesn't exercise
    `--mode multi`. If multi-query coverage ever gets its own deterministic
    harness beyond `pixkb eval multi`'s hand-curated cases, this is a good
    regression case to add.
  - The `pacs`/`camt` disabled entries' stem ambiguity (a bare `pacs` stem
    can't distinguish pacs.008 from any other pacs.NNN message) — a future
    re-attempt at this class of mapping needs per-message-code
    disambiguation, not a bare family-name stem.
  - `endpoint`/`api` stem entry was migrated as-is without re-evaluating
    whether it still earns its place — not evaluated in this plan.
- **Search quality operations follow-ups (Feature 8 of
  `docs/SEARCH-CAPABILITY-SPEC.md` shipped; these are deliberately
  deferred).** `internal/store/postgres.GraphSparsity`/`EmbeddingCoverage`
  + `internal/searchhealth` (reuses `hygiene.Scan`/`MissingIntentTerms` for
  the content-quality signals, `query.Hybrid` + `evalkit.BestRank` for the
  eval-regression signal — no new ranking or hygiene detection logic,
  purely synthesis) + `pixkb search-health [--json]` give the "one command"
  health report the spec asks for. Live run against the production KB
  (2026-07-04): 252 bundle concepts, ALL 252 missing `intent_terms`, 56
  noisy titles, 123 sparse-graph concepts, 255/255 DB concepts embedded
  (single model/dim, consistent), 10/43 precise+fuzzy eval cases regressed
  (no acceptable hit). Remaining, explicitly out of scope for that plan:
  - ~~**Bundle vs. DB concept-count mismatch found by this very run**~~
    RESOLVED (2026-07-04, `/steps:next` item 2). Root cause: two concepts
    (`reference/api/lote-cobv.md`, `reference/spi/liquidacao-spi.md`) were
    upserted straight to Postgres at some point (`first_epoch=0`,
    `last_epoch=0` — never went through a real `pixkb ingest` epoch), and a
    third (a real scraped WebPage, "Banco Central do Brasil" homepage) had
    its id `web/index.md` collide with OKF's reserved
    auto-generated-navigation filename (`internal/okf/bundle.go`'s
    `isNonConcept`), so `ReadBundle` silently skipped it on every read —
    permanently invisible to the bundle regardless of any write attempt
    under that id. All three were DB-only orphans that a `pixkb reindex`
    would have silently deleted (the bundle is supposed to be the source of
    truth). Fixed: wrote the first two back to the bundle verbatim; renamed
    the third to `web/portal-index.md` (no edges referenced the old id) and
    migrated its DB rows + embedding under the new id; regenerated
    `okf.WriteIndexes` nav files (this also restored `web/index.md` itself,
    which the fix's first pass accidentally deleted — it turned out to
    double as the real per-directory nav index, not just a colliding
    concept id). Committed in `kb-data`'s own git history (bundle is a
    separate repo from `pixkb`'s source). `pixkb search-health` now reports
    255/255 bundle-vs-DB consistent. Still worth doing as a follow-up:
    `search-health` should diff bundle-vs-DB concept counts itself as a
    dedicated signal, so a future drift is caught automatically instead of
    requiring a manual investigation like this one.
  - **"Missing intent_terms" only detects absence, not staleness** — the
    spec's own wording is "missing OR stale". `okf.Concept` has no
    last-enriched timestamp to compare against `updated_at`/content
    changes, so staleness (an intent_terms list that predates a body edit)
    isn't detectable without adding one. Not built here.
  - **100% of concepts missing `intent_terms` is itself worth investigating**,
    not just enriching one by one — it suggests the enrich loop (BACKLOG's
    "Eval-driven KB quality loop" / hygiene curate loop) either hasn't run
    against `intent_terms` specifically, or `intent_terms` isn't being
    written back to the canonical bundle files even when generated
    elsewhere. Worth a `search-health`-driven enrichment pass as a
    follow-up action, not just a report.
  - `pixkb search-health` reports counts/recommendations but never fails
    the process (`os.Exit(1)`) — intentional, matching `pixkb eval`'s own
    "measurement tool, not a CI gate" convention from Feature 6.
- **Concept similarity search follow-ups (Feature 2 of
  `docs/SEARCH-CAPABILITY-SPEC.md` shipped; these are deliberately deferred).**
  `internal/similar`'s `Similar()` (semantic/graph/hybrid/more-like-this modes)
  shipped via `pixkb similar <id>` and the MCP `similar` tool. Live spot-check
  against the production KB: self-exclusion holds, all 4 modes give visibly
  distinct result sets for the same concept, hybrid mode genuinely fuses
  signals (multi-Why hits observed, e.g. `[graph semantic]`), and domain
  tagging fires correctly only for type-adjacent candidates already in the
  fused set (verified against both the `Reference`->{ApiEndpoint,
  ManualSection} and `PacsMessage`->{ApiEndpoint, CamtMessage} rows of
  `domainAdjacency`). Remaining, explicitly out of scope for that plan:
  - **No curated regression gate.** Unlike multi-query retrieval's
    `eval/tophit.sh` + `eval/cases-{precise,fuzzy}-ids.tsv`, this plan's Task 8
    was a one-time manual spot-check, not a standing harness. Build a
    `eval/cases-similar-ids.tsv` (concept id -> expected similar id(s)) gold
    set plus a `tophit.sh`-equivalent so future `domainAdjacency`/fusion
    changes are measured, not spot-checked.
  - **Domain signal is type-pair-only.** The spec's finer-grained examples
    ("DICT endpoints near key concepts" specifically) need a topic-specific
    rule layer beyond `internal/similar/domain.go`'s 5-entry type table.
  - **Surfacing `Hit.Why`/per-signal scores in a dedicated explanation view**
    (spec Feature 3) and **richer CLI/MCP filters/output formats** (Feature 4)
    — same deferral already recorded for multi-query retrieval's provenance.
  - **Wiring `similar.Similar` into RAG grounding** (spec Feature 5) as an
    additional evidence-diversification source, alongside the already-
    backlogged `query.MultiHybrid` RAG wiring above.
  - **Semantic-mode noise from the hashing embedder** (e.g. unrelated WebPage
    hits surfacing in `--mode semantic`/`more-like-this` results) is the same
    documented, air-gap-bounded limitation ADR 0002 already recorded for
    search generally — not a defect introduced by this feature, and not
    separately actionable without a learned embedder (forbidden by the
    air-gap rule).
- **Multi-query retrieval follow-ups (Feature 1 of `docs/SEARCH-CAPABILITY-SPEC.md`
  shipped; these are the pieces deliberately deferred).** `query.ExpandQuery` +
  `query.MultiHybrid` (`internal/query/expand.go`, `multi.go`) ship deterministic
  query expansion + fusion, wired to `pixkb search --mode multi` and the MCP
  `search` tool's `mode: "multi"`. Measured on `eval/tophit.sh` against the live
  KB: precise top@5 held exactly at baseline (96%, unchanged — two harmful
  `entityTriggers` entries were removed during the verification gate: the
  pacs/camt-message trigger and the bare English "settlement" stem, both of
  which only ever fired on already-precise queries and never helped a fuzzy
  one — see `internal/query/expand.go`'s `entityTriggers` doc comment); fuzzy
  improved (top@1 6%→12%, MRR 0.131→0.216, top@5 flat at 29%). Remaining,
  explicitly out of scope for that plan:
  - Agent-generated query rewrites and bilingual (PT/EN ISO-message) subqueries
    for `ExpandQuery` — both optional per the spec.
  - Surfacing `MultiHit.Subqueries` provenance (which subquery/arm/rank found a
    hit) in CLI/MCP JSON output — spec Feature 3, "Search Explanation".
  - Wiring `MultiHybrid` into RAG grounding (see the RAG follow-up above,
    P1) — spec Feature 5, sequenced after this measurement.
  - The `entityTriggers` table is a small, 7-entry seed (only the entities
    Feature 1's spec text names directly) — Feature 7 ("Domain-Aware Query
    Understanding") will add a larger, versioned vocabulary; don't duplicate
    entries here when that lands, extend/supersede this table instead. Given
    two of the original 8 seed entities turned out net-harmful once measured,
    treat any FUTURE entity addition here as needing the same
    `eval/tophit.sh` precise-regression check before it's kept, not just
    "does it look right in a spot-check".
- **Search explanation follow-ups (Feature 3 of `docs/SEARCH-CAPABILITY-SPEC.md`
  shipped; these are deliberately deferred).** `query.HybridExplain` (FTS/vector
  rank, vector cosine, type-weight/title-boost multipliers, final fused score,
  retrieval arm) ships via `pixkb search --explain` and the MCP `search` tool's
  `explain: true`; multi-query mode's subquery attribution (`MultiHit.Subqueries`)
  is now surfaced too via `pixkb search --explain --mode multi`. Remaining,
  explicitly out of scope for that plan:
  - **Matched-query-token highlighting and matched-field-category breakdown** —
    2 of the spec's 7 required explain fields, not built here. Extracting them
    needs either a Postgres `ts_headline`-style query or client-side
    re-tokenization — a bigger unit of work than the rest of Feature 3.
  - **An HTTP `/explain` endpoint / `explain=true` query param for `pixkb
    serve`** — not touched by this plan; only the CLI and MCP surfaces got
    `--explain`/`explain: true`.
- **Rich search filters/formats follow-ups (Feature 4 of
  `docs/SEARCH-CAPABILITY-SPEC.md`, as-of subset shipped; these are deliberately
  deferred).** `internal/output` (text/json/md/yaml rendering) plus
  `AsOfEpoch`/`AsOfTime` shipped via `pixkb search --format`/`--as-of-epoch`/
  `--as-of-time` and the MCP `search` tool's `as_of_epoch`/`as_of_time` fields.
  Remaining, explicitly out of scope for that plan:
  - **Include/exclude concept-id and concept-type list filters, and a
    minimum-vector-score filter.** None of these exist on `postgres.Filter`
    today; adding them needs new SQL predicates in `FTS`/`Vector`/`Hybrid`/
    `MultiHybrid` — a bigger unit of work deliberately deferred here.
  - **An HTTP `/search` format query param for `pixkb serve`** — this plan
    only touched the CLI (`--format`) and MCP surfaces, not `pixkb serve`.
- **RAG retrieval upgrade follow-ups (Feature 5 of `docs/SEARCH-CAPABILITY-SPEC.md`
  shipped; these are deliberately deferred).** `rag.BuildGrounding` gained opt-in
  `MultiQuery` (dispatches to `query.MultiHybrid` via the `MultiRetriever`
  type-assertion, falling back to single-query `Retrieve` when unsupported),
  `Diversify` (promotes the first hit of each concept `Type` ahead of later
  same-type hits), `ExpandSeeds` (graph-neighbour expansion from more than one
  seed hit, default 1 = pre-upgrade behavior), and `MinScore` (refuse — empty
  `Grounding`, no agent turn spent — on weak top-hit evidence) — all
  zero-default-behavior-change (`Options{}` unchanged), exposed via
  `pixkb ask --multi/--diversify/--expand-seeds/--min-score` and the MCP
  `kb_ask` tool's matching fields. Remaining, explicitly out of scope for that
  plan:
  - **Partial-chunk budget trimming.** `BuildGrounding`'s char budget is still
    all-or-nothing per whole concept body (a chunk either fits or is
    dropped); truncating the last admitted chunk to fill the remaining
    budget would pack denser context but risks cutting a citation
    mid-thought — deliberately deferred.
  - **`Diversify`'s "one per type first" is a promotion, not a per-type
    quota** (e.g. "at most 2 ApiEndpoint, at least 1 Reference") — the
    spec's retrieval-policy example (one reference + one endpoint + one ISO
    message + one manual section) is a stronger diversity contract than this
    task implements; revisit once Feature 6 (eval expansion) has a diversity
    metric to measure against.
  - Features 6, 7, and 8 have since shipped (see their own backlog blocks
    above for what each deliberately deferred). All eight
    `docs/SEARCH-CAPABILITY-SPEC.md` features are now implemented; remaining
    work is the follow-ups itemized per-feature, plus the spec's own
    "Default-mode promotion" step (promoting multi-query or domain expansion
    to the default only after precise/fuzzy/OOD/RAG evals all support it —
    not evaluated, not scheduled).
- **KB standardized in English — translate agent-written content + ingested
  sources.** The KB is currently mostly Portuguese (BACEN source material is
  PT-native: PDFs, scout-crawled bcb.gov.br pages, markdown references, git
  mirrors). Standard going forward: canonical concept bodies should be
  English. Three surfaces, one now done:
  - ~~**Agent SYSTEM PROMPTS audited/translated to English**~~ DONE
    (2026-07-04, `agents: add English-commentary language note to
    content-producing prompts`, `pkg/agents/roster.go`). Audited every
    roster agent's `System` prompt string for Portuguese instructional
    text — none was found; all were already English prose wrapping
    verbatim BACEN normative terms. Added one explicit line to each of the
    four content-producing agents (enrich, hygiene, deviation, research):
    their own commentary/notes/critique must be written in English, but
    canonical BACEN/Pix concept body, title, and domain vocabulary must
    never be force-translated out of their source language. This closes
    only the *instructional-prompt* surface — it does not translate what
    those agents read or write. (a), (b), and the existing corpus below
    remain open.
  (a) **Agent-written/rewritten content — still open.** The `enrich` roster
  agent (intent_terms) and `curate`'s fix agents (hygiene, deviation,
  research — `internal/curate`) currently read/write concept bodies in
  whatever language the source was; they should translate to English as
  part of their fix/write step, gated the same way hygiene fixes already
  are (agent proposes → re-scanned by the same detector → rejected if it
  still trips an error).
  (b) **New ingestion — still open.** Every `ingest.Source` (PDF, Markdown,
  git-mirror/OpenAPI, scout-crawl) currently stores extracted text as-is; a
  translation pass (agent-driven, since this project has no offline MT
  model and adding one would violate the air-gap/no-native-runtime rule)
  needs to run before a new concept is written, not after, so nothing
  PT-only ever lands in the canonical bundle.
  **Open questions to resolve at brainstorm time before planning this** (do
  not build directly from this backlog line):
  - **Existing ~255 concept corpus is PT (bodies + titles) — still open,
    future work.** Is this a one-time bulk re-translation batch (like the
    `intent_terms` enrichment rollout — `curate --enrich`-style loop) or
    translate-on-touch (only when an agent already needs to rewrite that
    concept for another reason)?
  - **FTS is tuned for Portuguese today** — migration 0003's `pixpt` config
    (simple tokenizer + PT stopwords, no stemmer) exists specifically because
    the corpus and queries are PT. Translating bodies to English without
    retuning FTS/search would break the exact recall mechanism ADR 0001/0002
    measured and shipped. This needs its own before/after eval run on
    `eval/tophit.sh`, not just a content change.
  - **Users query in Portuguese** (all of `eval/cases-*.tsv` are PT queries —
    this is a Brazilian-financial/BACEN domain KB). If bodies become English,
    does `intent_terms` become the PT recall bridge deliberately (bilingual by
    design: EN body + PT intent_terms), or does everything become bilingual?
    This is the same tension ADR 0002 already found for pacs/camt (EN message
    concepts vs PT queries) — decide it as one policy, not per-concept.
  - Scope this as its own brainstorm → spec → plan → SDD pipeline (same as the
    ISPB mapper and multi-query retrieval work) once the questions above have
    answers; it touches ingest, curate, enrich, and search ranking, so it is
    NOT a small task.
- **CLI output formats — markdown, YAML, JSON.** Commands that currently print
  fixed plain-text tables (`ispb lookup`, `stats`, `search`, `related`, etc.)
  should accept a shared `--format` flag (`text` default, `md`, `yaml`,
  `json`) so output is both scriptable (json/yaml) and shareable as a
  rendered doc (md). Likely a small shared `internal/output` helper each
  command's print step routes through, rather than each command hand-rolling
  its own formatter.
- **SELIC and Dólar (USD/BRL) mappers — current + historical series.** Same
  pattern as the ISPB mapper (`internal/ispb`): a new `internal/econindex` (or
  similar) package sourcing BACEN's public SGS time-series API
  (`https://api.bcb.gov.br/dados/serie/bcdata.sgs.<codigo>/dados`), no auth.
  Needs, per indicator: (a) the "real" (latest/current) value — a lightweight
  single-point fetch — and (b) full history — the same endpoint with
  `dataInicial`/`dataFinal` params, paginated for long ranges. Known series
  codes to verify at brainstorm time: Selic (daily rate, series 11; target/meta
  rate, series 432) and Dólar comercial (PTAX venda, series 1) — BCB's Olinda
  OData API (`olinda.bcb.gov.br/olinda/servico/PTAX`) may be a better fit
  specifically for PTAX's official daily quote vs. the SGS series. New table(s)
  + migration, `pixkb` CLI subcommands (`fetch`/`load`/`sync`/`lookup` or
  similar, matching the air-gap online/offline split), following the exact
  brainstorm → spec → plan → SDD pipeline used for ISPB.
- **Scraper wired to a headless renderer.** Render the JS-rendered BACEN SPA
  pages into BACEN-canonical concepts via the scraper agent. BLOCKED on two
  prerequisites: (1) Scout MCP browser must be connected (it was down this run),
  and (2) a catalogued list of target BACEN SPA URLs (`SOURCES.md`, pending). The
  scraper agent + conceptSchema already exist; this is wiring + inputs, not new code.
- **Fuzzy recall ceiling is the VECTOR arm + en/pt config, NOT FTS (re-scoped).**
  UPDATE (2026-07-04, `/steps:next` item 4): lever (b) — targeted
  `curate --enrich --apply --ids ...` against exactly the 10 concepts
  `pixkb search-health` flagged as sparse-terms+eval-regression — CONFIRMED
  the lift: precise top@1 62%→69% (top@5 held 96%, MRR 0.751→0.806), fuzzy
  top@1 6%→12%/top@5 29%→35% (MRR 0.131→0.198), eval regressions 10/43→8/43.
  Both axes improved together, matching this section's own prediction.
  `--ids` targeting (`internal/curate`) was added to make this a small,
  quota-bounded pass instead of a full ~253-concept sweep. 8 regressions
  remain — same lever, more concepts, is the obvious next pass.
  A measured chain on the deterministic harness (`eval/tophit.sh`) settled where
  the recall ceiling actually is:
  - AND-recall baseline: precise MRR 0.698 (top@5 100%) / fuzzy 0.285 (top@5 41%).
  - naive OR-recall (` & `→` | `): fuzzy REGRESSED to 0.162 — OR floods the FTS arm
    and length-normalized `ts_rank_cd` floats short junk. Reverted.
  - COVERAGE-ranked OR (primary sort = # distinct query lexemes matched, stemmed,
    length-independent; ts_rank_cd secondary): fixed the FTS arm — fts-only fuzzy
    rose to 0.313 and pacs.008/camt.054/certificado hit #1 in the FTS arm. But
    HYBRID fuzzy stayed 0.274 (top@5 still 41%).
  - coverage + FTS arm-weight ×2 in RRF: precise IMPROVED to 0.745 (top@5 100%),
    fuzzy still 0.284 (top@5 41%).
  CONCLUSION: fuzzy top@5 is pinned at 41% across EVERY FTS config — the misses
  (pacs.004, post-loc, webhook) are NOT FTS-recall failures. Causes: (1) coverage
  uses each doc's OWN language config, so a pt query vs an EN message concept
  (pacs.004/camt) tokenizes differently and misses; (2) the hashing-vector arm is
  too noisy to pull a target into top-5 (ISSUES — air-gap forbids a learned
  embedder); (3) a few concepts still lack the layperson term. ALL reverted to the
  committed AND-recall — a core-ranking change that only moved PRECISE (not the
  fuzzy goal) and contradicts a prior broad-judge warning must NOT ship on 33
  curated cases without the full 51-case judge. NEXT LEVERS, in order: (a) make
  coverage/FTS tokenize the QUERY and the DOC with the SAME config (pin to
  `portuguese` or the query's detected language, not the doc's) so en message
  concepts are reachable from pt queries — cheap, measure on the harness; (b) fill
  the residual term gaps (webhook/post-loc) via another `curate --enrich` pass;
  (c) only then revisit the precise-improving coverage+FTS×2 combo behind the full
  judge. Harness + id sets (`eval/cases-{precise,fuzzy}-ids.tsv`) are committed for
  all of this.
  - FOLLOW-UP MEASURED (lever a — pin coverage to one `portuguese` config for query
    AND doc, so pt queries reach EN pacs/camt concepts): on the ORIGINAL 16-case
    precise set it looked like a clean win (precise 0.698→0.719, fuzzy 0.285→0.321).
    BROADENING the precise guard to 26 cases EXPOSED a precise REGRESSION the small
    set hid: baseline 0.788 (top@1 65%) → pinned 0.753 (top@1 58%). So coverage
    (per-doc OR pinned) consistently TRADES precise for fuzzy — fails measure-then-
    keep. Reverted. LESSON: the 16-case set was overfit; the broadened 26-case set
    is the new precise guard (committed). CONCLUSION (firm): FTS-recall ranking is
    EXHAUSTED as a fuzzy lever — every variant regresses one axis and fuzzy top@5
    stays 41%. The remaining fuzzy gains live OUTSIDE the FTS query: (b) fill the
    residual layperson term gaps via `curate --enrich` (e.g. pacs.004 lacks
    `devolver`/`instituições`; webhook/post-loc thin), measured on the harness; and
    a better vector arm — which the air-gap forbids (no learned embedder). Net: the
    intent_terms rollout helps PRECISE + the vector arm; fuzzy is vector-bound.
  Side-finding (NOT recall): the static-QR manual section has a mangled OCR title
  (`ODE ESTÁTICO PARA PACS`) — a junk-title hygiene target, fix via `curate`.

## P3
- **HNSW-on-typed-vector revisit.** Current vector search is exact-cosine.
  Revisit an HNSW (approximate) index on the typed vector column only if the
  corpus grows enough that exact search latency becomes a problem.

## Shipped
- **RAG layer — grounded, cited answer synthesis (`pixkb ask` / `kb_ask`).**
  `internal/rag`: retrieve+augment (`BuildGrounding` — hybrid top-k + optional
  related-graph expansion + token-budget assembly, each chunk tagged with concept
  id + source_uri) → `Synthesize` over the `answerer` roster agent (KindAnswerer,
  strict faithful-or-refuse prompt, runs on the subscription fleet — air-gap).
  Guardrails: refuse empty/OOD WITHOUT spending a turn, validate citations against
  the grounding (drop hallucinated ids), downgrade uncited/blank answers to
  refusals. Surfaces: `pixkb ask <q> [--json --top-k --expand --type --provider]`,
  `kb_ask` MCP tool (`pixkb mcp serve --answerer <backend>`), `rag.Ask` lib entry.
  RAG eval rubric: `eval/cases-rag.tsv` (11 cases incl. 3 OOD-refuse) +
  `rag-judge-schema.json` (relevance/faithfulness/citation/correct-refusal) +
  `run-rag-judge.sh`. DB-free unit tests for grounding, citation validation, and
  refusal paths (`7276aac`, `544473d`, `69fad4a`, `cabc7b4`).
- **`curate --reenrich` — re-tune existing intent_terms.** The enrich loop only
  flagged EMPTY intent_terms; `--reenrich` routes ALL concepts so the 207
  already-filled ones can be regenerated (e.g. after the embed-text change). Shared
  `enrichCandidates(concepts, reenrich)` selector; default unchanged (`3c2f015`).
- **intent_terms folded into the vector embedding — lifts BOTH axes.** The vector
  embedder fed `Title + Body` only (`epoch/runner.go`), so the 207 enriched
  intent_terms never reached the vector arm — the fuzzy bottleneck. Adding them to
  the embedded text (`Title + intent_terms + Body`) makes the hashing bag-of-words
  cosine match paraphrase queries against the recall synonyms. Measured on the
  broadened deterministic harness: precise MRR 0.788→0.821 (top@5 held 100%), fuzzy
  top@5 41%→53% / MRR 0.285→0.303 — the FIRST change that improves BOTH axes
  (every FTS-ranking variant traded one for the other). Low-risk: touches only the
  down-weighted/floored vector arm, not the FTS/precise-ranking SQL. Prod reindexed.
  This is the air-gap-compliant payoff of the intent_terms rollout.
- **intent_terms rollout COMPLETE — 207/207 concepts enriched in prod.** Nine
  `curate --enrich --apply` batches (8+25×7+24), 0 rejected by the charter gate,
  ~16–41 terms/concept, DB written + reindexed each batch. The KB now carries
  recall synonyms everywhere. HONEST OUTCOME: the fuzzy judge stayed FLAT (rel
  1.95→2.05, prec 1.13→1.00, 0 pass) — NOT because the terms are bad (proven: an
  in-vocab query ranks the target #1 via intent_terms) but because the FTS recall
  query ANDs every word and `pixpt` doesn't stem, so natural-language queries match
  nothing (see ISSUES + the OR/quorum lever in P1/P2). The terms are a prerequisite
  now satisfied; the query-semantics fix is what converts them into measured lift.
- **intent_terms enrichment loop — enrich agent + `curate --enrich`.** Closes the
  recall mechanism: `hygiene.MissingIntentTerms` detector (kept OUT of default Scan
  — enrichment is an opportunity, not a defect) + `IntentTermsDeviations` charter
  gate; a dedicated `enrich` roster agent (KindEnrich, minimal `enrichSchema`
  `{id,intent_terms}`) that returns ONLY recall terms so it can never mangle/wipe
  the body; `curate.Enrich` loop (scan-missing → enrich agent → MERGE terms onto
  the concept → gate charter-clean body+terms, non-empty → upsert+reindex); CLI
  `pixkb curate --enrich` (`--plan/--apply/--limit`). Validated live: dry-run +
  8-concept prod `--apply` batch, 0 rejected, ~16–41 terms/concept. DB-free tests
  for detector, gate, merge, limit, parser. The 199-concept fill is IN PROGRESS (P2).
- **Fuzzy-query eval set (`eval/cases-fuzzy.tsv`, 22 cases).** Layperson / synonym /
  sigla-expanded / cross-language paraphrases that avoid each concept's title words,
  reusing verified expected ids from `cases.tsv` so phrasing is the only variable —
  the A/B that measures the intent_terms recall lift (the precise suite cannot).
  `run-judge.sh` takes a `CASES` env var to select the set.
- **Custom `pixpt` FTS config (migration 0003) — stopword fix, no stemming.**
  `pg_catalog.simple` template + Portuguese STOPWORDS dictionary + the `simple`
  tokenizer (NO stemmer); the generated `fts` column and the search query both use
  it. Removes the `'simple'`-config stopword blocker (natural queries no longer
  carry required stopword AND-terms) WITHOUT the precision loss of the reverted
  `'portuguese'` stemmer. Applied to prod (version 3). Verified: 12/12 judge-query
  top-hits unchanged (deterministic — the codex judge score is too noisy at 12
  cases to detect the change), stopword-heavy natural queries now recall, codes/
  identifiers (pacs.008/camt/txid) hold. DB-free migration 0003 up/down test added.
- **Recall pilot run on prod (migration 0002 applied).** Migration 0002 applied
  to the live KB (207 concepts intact, fts now indexes intent_terms). A 10-query
  paraphrase A/B with hand-crafted `intent_terms` validated the lever (FTS-arm
  flips for synonym queries; `débito recorrente` → Pix Automático in hybrid) and
  surfaced the `'simple'`-config stopword bug (now ISSUES + migration 0003 in P2).
  Pilot enrichments reverted so prod DB stays == bundle; the column remains, NULL.
- **Recall `intent_terms` mechanism (ADR 0001).** `okf.Concept.IntentTerms`
  (frontmatter, kept out of the body) + migration 0002 (column + generated `fts`
  redefined to index title+intent_terms+body) + `UpsertConcept`/search ranking
  (weight B) + optional `intent_terms` on the `concept_upsert` MCP tool and CLI.
  DB-free tests: okf round-trip + 0002 up/down validation. The fleet can now write
  recall terms; apply+pilot pending a database (`258be40`).
- **Intent-terms storage decided (ADR 0001) + recall mechanism scoped.** The FTS
  recall column is a generated `fts` tsvector (title+body), so intent terms must
  live in a dedicated `intent_terms` field woven into FTS at weight B/C — not in
  tags (unindexed) or body (pollutes canonical content). See
  `docs/adr/0001-intent-terms-storage.md`. The recall PILOT is deferred until the
  mechanism (a generated-column migration + `okf.Concept.IntentTerms` + FTS weave)
  lands as its own focused unit — rushing a generated-column migration was judged
  unsafe at session tail.
- **Ingestion sources catalogued.** `docs/SOURCES.md` lists 10 authoritative
  BACEN/gov URLs with adapter + status + the SPA-render blocker; the scraper item
  is now purely the headless-render wiring (Scout MCP), not the inputs.
- **Curate real-fixer e2e.** `internal/curate/e2e_test.go` drives the Curator with
  the live AgencyFixer over a junk-titled concept → hygiene agent rewrite → gate →
  proposed, no DB. Guarded by provider-on-PATH + `-short`. Codex live: 31s pass.
- **Session lift quantified (codex judge).** Judge over the 12 cases this session
  touched: **mean relevance 4.92 / precision 3.92, 0 fails** (11 pass, 1 weak).
  `liquidacao-spi` and `lote-cobv` went rel2 FAIL → rel5 PASS (gaps closed); the 4
  enriched concepts all score rel5; `qr-estatico` top-hits the canonical concept
  now `secao-73` is gone. Only `prazos` weak (RRF precision residual). Also fixed
  `run-judge.sh` on Windows (codex couldn't resolve bash forward-slash schema/out
  paths → every case FAILED; `winpath`/cygpath fix). Evidence:
  `eval/report-session-lift.md` (gitignored), `eval/out/<id>.json`.
- **Agent e2e integration test.** Live coding-agent CLI through the Agency with a
  conceptSchema returns a parseable concept (`pkg/agents/agency_e2e_test.go`),
  guarded by provider-on-PATH + `-short`. With the MCP upsert→search round-trip,
  covers agent → structured output → write-back → retrieve. Codex live: 21s pass.
- **Pix QR image decode.** `internal/brcode` DecodeImage/ParseImage via gozxing
  (pure-Go, no cgo); `pixkb qr read --image`, MCP `qr_decode`. Full encode→PNG→
  decode→parse round-trip verified (`a004beb`).
- **secao-73 dropped + no siblings.** The lone `sample-data` hit removed via
  `pixkb concept rm`; a full rescan shows 0 sample-data findings (no other OCR
  example fragments). KB 207 concepts, hygiene 0/0.
- **Pix BR Code (EMV MPM) read/write — library + CLI + MCP.** `internal/brcode`:
  pure-Go EMV TLV codec + CRC16-CCITT (passes the 0x29B1 conformance value),
  static-key and dynamic-URL forms, amount as a DECIMAL STRING (never float),
  lossless round-trip, CRC verify on parse, PNG render via skip2/go-qrcode. CLI
  `pixkb qr read|write` (--json, --png); MCP tools `qr_read`/`qr_write` for the
  agent fleet. (`d416173`.)
- **`pixkb concept rm` + secao-73 dropped.** Removal path (delete bundle file →
  rebuild indexes → commit → reindex from the now-smaller bundle). Dropped the
  OCR junk fragment so `QR code estático` top-hits the canonical concept. (`9665fb8`.)
- **Magnitude-aware fusion — title-overlap boost.** `internal/query/hybrid.go`
  `titleBoost` multiplies a concept's fused RRF score by the fraction of distinct
  query tokens its title covers (accent-folded, stopwords dropped), bounded to
  [1, 1.5], never penalising. Fixes pure-RRF's rank-only blindness so a title-
  intent match wins. Live: `lote de cobranças com vencimento` flipped to the
  canonical LoteCobV concept; liquidacao-spi/mTLS/txid/prazos unchanged-correct.
  (`90bebdd`.) Note: the QR-vs-`secao-73` case is bound by junk-removal, not
  ranking — the OCR fragment dominates beyond any bounded boost.
- **`sample-data` hygiene check.** `internal/hygiene` flags OCR worked-example
  fragments (placeholder names FULANO/BELTRANO/DE TAL, sample CNPJ/CPF literals)
  that pass junk-title/stub yet pollute search. Routed to the hygiene agent in
  `curate`. Real KB: 1 hit (`secao-73`), no false positives. (`bfaacce`.)
- **4 thin concepts enriched (research agent).** key types / static QR / mTLS /
  webhook redelivery expanded ~3-4× (bodies 1.6-2KB → 6-8KB) with 6-8 BACEN-
  normative cross-links each, written via `concept upsert` + reindex (kb-data
  epoch `3744eab`). KB stays 208 / 0 hygiene errors+warnings (all links resolve);
  the `related` graph — previously empty for these — is now populated (concept 01
  links in/out to mTLS, webhook, pix-in, DICT, cobrança). 3/4 top-hit their topic;
  static-QR is outranked by an OCR sample fragment (`secao-73`, now tracked P2).
- **BACEN content gaps closed (research agent).** The two claude-sonnet-judge
  fails — `liquidacao-spi` (SPI settlement / LBTR in reserves) and `lote-cobv`
  (batch cobrança-com-vencimento) — filled by the **research** agent: two
  canonical BACEN Reference concepts (3.5KB + 6.3KB, sourced to bcb.gov.br /
  bacen/pix-api), written via `concept upsert` + reindex. KB 206 → 208, still
  0 hygiene errors/warnings. `liquidação no spi reservas` now top-hits
  `reference/spi/liquidacao-spi.md`; `lote de cobranças com vencimento` top-hits
  the `/lotecobv` endpoints (the eval case's expected hit). kb-data epoch
  `cfeda1c`.
- **Whole-KB hygiene sweep applied.** Ran the Curator (`curate --apply`, claude-
  sonnet) over the live KB in batches: **113 hygiene warnings → 0** (~40 concepts
  retitled from mangled OCR fragments like "ERVIÇO DE"/"ANEXO IV" to canonical
  BACEN titles), every fix gate-checked, 0 deviations. Each batch = a kb-data
  epoch commit. Surfaced + fixed 3 latent provider bugs (see below).
- **Curator orchestrator (the control loop closes).** `internal/curate` runs
  scan → route → fix-agent → **gate** → upsert → reindex. `hygiene.Scan` groups
  fixable findings per concept and routes each to its repair agent by priority
  (deviation > hygiene > research); the agent's proposed concept is re-scanned by
  the SAME air-gap-pure detector (swapped into the full set) and rejected if it
  still trips an error — so an agent can never write a new deviation. Fixer is an
  interface (AgencyFixer drives the fleet; routing + gate are pure/tested offline).
  Exposed as `pixkb curate`: `--plan` (offline routing preview), default dry-run
  (run agents + gate, no write), `--apply` (upsert gated-clean fixes + reindex).
  Real bundle: 206 concepts → 65 routed (all hygiene; 0 deviations). The detection,
  agent, AND orchestration loop are now complete.
- **KB hygiene + deviation engine** — `internal/hygiene` deterministic,
  air-gap-pure detector: BACEN-charter DEVIATIONS (implementation-specific
  content) + mechanical issues (junk titles, stubs, duplicates, broken links,
  missing provenance). Exposed as `pixkb hygiene` (CLI), the `hygiene_scan` MCP
  tool (the agents' eyes), and the **hygiene** + **deviation** roster agents.
  Same detector doubles as the write-back governance gate. Real KB: 206 concepts,
  0 deviations (charter clean), 113 fixable warnings. The detection + agent half
  of the control loop.
- **OOD vector-score floor** — `internal/query/hybrid.go` drops sub-floor
  (cosine < 0.05) vector hits before RRF so an out-of-domain query returns
  nothing instead of hashing-vector noise. Pure-Go, no model. `ood-control` now
  passes; the 4 content-gap concepts top-hit correctly (`76b79b6`).
- **Claude-sonnet judge variant** — `eval/run-judge-claude.sh` (embeds schema in
  prompt, claude runs the searches via Bash tool). Confirmed the content-gap +
  floor fixes hold under a second, stricter judge.
- **Air-gap embedder rule enforced** — ONNX/MiniLM removed (native runtime +
  vendored model violated "no metered API / no native model runtime");
  `onnxruntime_go` dep dropped. `OpenAIEmbedder` demoted to opt-in dev-only with
  the doc corrected (it's a metered-API violation, not the recall path). Default
  is pure-Go hashing; stronger recall is now the agent-driven path (`31c6705`).
- **BACEN content gaps closed** — `docs/diagrams/bacen-pix-concepts.md`
  (key types, static QR, mTLS security, webhook redelivery) ingested as 4
  Reference concepts (`560657b`, bundle `99648d9`). Codex judge iter7→iter8:
  rel 3.52→**3.88**, prec 2.56→**2.72**, fails 4→**1** (the 1 is the OOD
  vector-noise control, not a gap). Evidence: `eval/out-iter8/`.
- **Agent fleet (Phase 8)** — `pkg/agents` host (vendor-split agy/codex/claude),
  warm sessions, MCP server, multi-host install, Codex usage + upstream drift
  tracking, BACEN domain charter + diagram agent. See ROADMAP Phase 8.
- **Eval-driven KB quality loop** — Codex-as-judge over `eval/cases.tsv`
  (`eval/run-judge.sh`) drove: junk PDF-title segmentation gate + bundle
  reconcile (commits `69b3ea4`, `fd2634d`), bilingual ISO intent terms +
  camt.052/.053/.054 (`ba2043e`), title-weighted FTS + vector-arm down-weight.
- **GitHub Actions CI** — build/vet/lint/test pipeline (commit `22b7eb1`,
  `.github/workflows/ci.yml`).

## DEPRECATION
_None._
