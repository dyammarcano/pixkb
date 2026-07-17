# pixkb Roadmap
<!-- rev:012 -->

Air-gap OKF (Open Knowledge Format) knowledge base for Brazil BCB Pix/SPB.
The OKF markdown bundle is the canonical source of truth; the Postgres+pgvector
index is a fully derived, rebuildable artifact.

## Phase 0 — Foundations [x]
- Go module `pixkb`, hexagonal layout (`cmd/`, `internal/`, `pkg/`).
- Config loading, structured logging (`log/slog`), Cobra CLI skeleton.

## Phase 1 — OKF Core [x]
- OKF markdown bundle format (canonical concepts, sections, front-matter).
- Concept model and stable concept IDs.
- Bundle read/write round-trip.

## Phase 2 — Postgres Store [x]
- Postgres + pgvector schema for the derived index.
- Bitemporal `concept_fact` table (valid-time vs transaction-time).
- Store read/write, migrations.

## Phase 3 — Embed + Search [x]
- Hashing embedder producing typed vectors.
- Full-text (FTS) index plus exact-cosine vector search.
- Hybrid retrieval fused via Reciprocal Rank Fusion (RRF).

## Phase 4 — Ingest Sources [x]
- ISO-20022 message ingest.
- PDF ingest (`ledongthuc/pdf`) → ManualSection concepts.
- Git mirror source.
- API-DICT source. Sources wired into ingest via config.

## Phase 5 — Epoch Engine [x]
- Epoch cut: snapshot the bundle and commit to git per epoch.
- Diff between epochs.
- Reindex of the derived store from the canonical bundle.

## Phase 6 — CLI + Ops [x]
- Commands: `ingest`, `search`, `reindex`, `diff`, `watch`, `serve`,
  `doctor`, `export-bundle`, `db`.
- `watch`: fsnotify debounce runner over the ingest drop-dir.

## Phase 7 — Hardening & Delivery [~]
IN PROGRESS. Production-readiness and distribution work.
- [x] Air-gap container image — builds AND runs end-to-end: `docker run` →
  initdb applies schema → `pixkb reindex` rebuilds 275 concepts from the baked
  `/kb` bundle. Internal-registry push path (`deploy/push-image.sh`) for corp nets.
- [x] Embedder is pure-Go hashing by default — ONNX/MiniLM removed (it needed a
  native runtime + vendored model, against the air-gap rule: subscription agents
  only, no metered API and no native model runtime). High-recall is to come from
  the agy agent fleet curating over pixdb, not a local model.
- [x] GitHub Actions CI (build, vet, lint, test) — `.github/workflows/ci.yml`.
- [x] Ops-command tests (`watch`, `serve`, `doctor`, `export-bundle`) — `cmd/pixkb/ops_test.go`.
- [x] Real BCB PDF ingest bundle — 188 concepts from the 2 BCB manuals, searchable (bundle git `73c5041`).

Also landed: code review (`docs/REVIEW.md`) with bitemporal valid-time + DB-clock tx fixes;
BSD-3 LICENSE; epoch ingest→diff→reindex integration test; internal-registry push
path (`deploy/push-image.sh`, `BASE_IMAGE` arg); prod-DB test guard + `pixkb_test`
provisioning (`deploy/sql/create-test-db.sql`).

Cycle 2 (KB quality + usability): stricter PDF heading detection (clean section
titles, no EMV/TLV junk); `pixkb stats`; VCS-aware `--version`; golangci-lint
clean (0 issues). Static `pixkb` binary built at `bin/pixkb.exe`.

Cycle 3 (KB content): ingested bacen pix-api + pix-dict-api mirrors; OpenAPI
source → 83 ApiEndpoint concepts enriched with parameters, request/response
schemas and status codes (searchable by schema name / status). Local throwaway
test DB (`deploy/local-testdb.sh`, `task testdb:up`) → full suite green.
KB now 275 concepts (181 manual, 83 API, 9 ISO).

## Phase 8 — Agent Fleet [x]
DONE. A subscription-coding-agent fleet that curates the KB, with pixkb
itself as the agents' self-contained tool surface.
- [x] `pkg/agents` host — Provider/Agent/Session/SessionPool/Agency + lazy
  provider registry; vendor-split packages **agy** (Antigravity, ConPTY),
  **codex** (OpenAI), **claude** (Anthropic), barrel `all`, installer `host`
  (`7da5828`, `64bd9bf`).
- [x] Long-running **warm sessions** to avoid cold-start/warmup (`14b1194`).
- [x] **MCP server** (`pixkb mcp serve`) — verbs as the agent's only tools;
  `mcp manifest` (`419dae0`).
- [x] **Full multi-host install** (claude/codex/agy) — `agents install|hosts`
  (`6bf1f83`).
- [x] **Real per-vendor usage APIs** — `agents usage --provider codex|claude|agy`
  calls each agent's own authenticated endpoint with its own credentials:
  codex `GET chatgpt.com/backend-api/wham/usage` (`6e503e0`), claude
  `GET api.anthropic.com/api/oauth/usage` (`8f21070`), agy
  `:retrieveUserQuotaSummary` (entitlement-gated; exact call MITM-captured,
  `271a8fa`/`52b4daa`). Codex + Claude verified live; agy 403s with the shared
  `~/.gemini` token (needs agy's own entitled token). `agents upstream [--check]`
  pins + drift-checks the openai/codex source. See `docs/agents-usage-signals.md`.
- [x] **Session-limit monitoring** — vendor-neutral `UsageReporter` capability +
  `LimitStatus`; the Agency gates every run on the provider's live limit window
  (`LimitThreshold`, default 98%) and returns `ErrRateLimited`, so a long-running
  fleet pauses before a hard subscription wall instead of failing mid-turn.
- [x] **BACEN domain charter** enforced on every agent; **diagram** agent
  (mermaid/drawio); LBP-coupled content purged, canonical docs ingested (`72fc5f8`).
- [x] **Control loop (the Curator)** — `internal/curate` closes the loop:
  scan → route each finding to its fix agent (deviation→deviation,
  junk/dup/link/sample→hygiene, stub→research) → deterministic gate (re-scan the
  PROPOSED concept with the SAME detector) → `concept_upsert` → reindex. The same
  hygiene engine is TRIGGER and write-back GATE, so an agent can never introduce a
  new deviation. `pixkb curate --plan|--apply [--limit N] [--provider P]`. Live
  sweep: 113 hygiene warnings → 0 (`86e9d03`, `5d63d16`, `8ef5867`, `40e5eb2`).
- [x] **Pix BR Code (EMV MPM) read/write** — `internal/brcode`: pure-Go TLV codec
  + CRC16 (0x29B1 conformance), static/dynamic, PNG render + QR image decode.
  Three surfaces: lib, `pixkb qr read|write`, MCP `qr_read`/`qr_write`/`qr_decode`
  (`d416173`, `a004beb`).
- [x] **RAG layer** — `pixkb ask` / `kb_ask` MCP: grounded, citation-backed answer
  synthesis. `internal/rag` retrieve+augment (hybrid top-k + related-graph + token
  budget, each chunk tagged with concept id + source_uri) → `answerer` agent
  (faithful, cite-or-refuse, BACEN-normative + LGPD) on the subscription fleet
  (air-gap). Guardrails: refuse empty/OOD without a turn, validate citations
  against the grounding, downgrade uncited/blank answers to refusals. RAG eval
  rubric (`eval/cases-rag.tsv`, `run-rag-judge.sh`) scores relevance, faithfulness,
  citation accuracy, correct-refusal (`7276aac`, `544473d`, `69fad4a`, `cabc7b4`).
- [x] **Migrated `pkg/agents` onto `github.com/inovacc/corral`** (2026-07-05) —
  confirmed via byte-level diff to be pixkb's own agent-runtime, generalized
  and published upstream. pixkb keeps only the BACEN-charter roster
  (`internal/roster`) and the pixkb-branded host installer
  (`internal/agenthost`); `corral` supplies Agency/Provider/Session/monitor +
  the claude/codex/agy vendor packages. Scope: 3 existing providers only
  (grok/kimi deferred). go.mod bumped to `go 1.26.3` for the dependency.
- [x] **Scraper wired** — render JS BACEN SPA pages into canonical concepts.
  `ingest.NewScoutCrawlSource` is registered from `cfg.ScoutCrawlDir`
  (`cmd/pixkb/commands.go`), the `scout_crawl_dir` key is live in `pixkb.yaml`,
  and 50 JS-rendered pages sit under `mirrors/bcb/knowledge/pages`. Residual:
  that crawl captured the general bcb.gov.br tree, not the Pix pages SOURCES.md
  still lists `pending` (#2,3,5,6,7,10) — re-targeting tracked in BACKLOG.

## Phase 9 — Search Capability Upgrade [x]
Implemented all 8 features of `docs/SEARCH-CAPABILITY-SPEC.md`: multi-query
retrieval (`internal/query.MultiHybrid`, domain-vocabulary-driven
`ExpandQuery`), concept similarity (`internal/similar`, `pixkb similar`),
search explanation (`query.HybridExplain`, `--explain`), rich filters/output
formats (`internal/output`, `--format`, as-of filters), RAG retrieval upgrade
(multi-query grounding, diversify, multi-seed expand, min-score refusal,
answer cache, deterministic PII/LGPD redaction), search evaluation expansion
(`internal/evalkit`, `pixkb eval {multi,similar,ood,explain,asof,rag-diversity}`),
domain-aware query understanding (`internal/query/domain_vocabulary.yaml`,
`pixkb vocab`), and search quality operations (`internal/searchhealth`,
`pixkb search-health`). New: SELIC/Dólar economic-index mappers
(`internal/econindex`, `pixkb econindex`), matching the ISPB mapper pattern.

Follow-up hardening pass (`/steps:next`, 2026-07-04): fixed 3 real test bugs
surfaced by finally running the full DSN-gated integration suite (a flaky
own-test, a stale ISO-message-count assertion, an MCP test hardcoding a
prod-only concept id); found and fixed 3 concepts that existed only in
Postgres, not the canonical bundle (one via an OKF reserved-filename
collision); `golangci-lint` clean; a targeted `curate --enrich --ids` pass
(new `--ids` flag) measurably lifted both precise and fuzzy recall; closed
the e2eid natural-language alias gap; `multiRRFK` fusion fix took
`pixkb eval multi` coverage to 100%. See `docs/BACKLOG.md` for full details
and remaining follow-ups.
- [x] Agent **e2e integration test** — live coding-agent CLI through the Agency
  with a conceptSchema returns a parseable concept (`pkg/agents/agency_e2e_test.go`,
  guarded by provider-on-PATH + `-short`); paired with the MCP
  concept_upsert→search round-trip, this covers agent → structured output →
  write-back → retrieve. Verified live on codex (21s).
