# pixkb Backlog
<!-- rev:033 -->

Prioritized future work. P1 = highest. Promote items into the active phase
(see `docs/ROADMAP.md` Phase 7) as they are scheduled.

## P1
- _(none open — the RAG layer shipped; see Shipped. Promote a P2 item here when
  scheduled.)_
- **RAG follow-ups (optional polish).** The core RAG layer is shipped; these are
  nice-to-haves, not blockers: (a) multi-query rewriting before retrieval; (b) an
  answer cache keyed by (question-hash, KB-epoch) to avoid re-spending a turn on a
  repeated question; (c) a deterministic PII/LGPD post-filter on the answer (today
  it is prompt-level only). Gate any change on `eval/run-rag-judge.sh`.

## P2
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
