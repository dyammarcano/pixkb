# Changelog

All notable changes to pixkb are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added — web UI, dump/ingest, and official sources

- **Ask web UI** (`serve --ask`): a self-contained browser page (`GET /`) with a
  grounded/cited ask tab (`POST /ask`, reusing the `pixkb ask` pipeline) and a
  per-browser question history in `localStorage`.
- **Answer feedback**: 👍/👎 + optional note per answer, appended to
  `<ingest_dir>/feedback.jsonl` (`POST`/`GET /feedback`) for later quality analysis.
- **Dump/Ingest UI + inbox**: drag-drop files and paste URLs into a staging inbox
  (`ingest.NewInboxSource`, wired into `buildSources`), review the queue, and
  **Ingest now** (`POST /inbox/ingest`). Files route by extension to the existing
  extractors or become searchable `Attachment` concepts; ingested items are
  archived out of the visible queue but still gathered (Run is a full-corpus
  snapshot).
- **URL handling**: fetched URLs route by Content-Type (PDF stays binary and is
  PDF-parsed; HTML→UTF-8-sanitized markdown; other binary kept as attachment) —
  fixes an invalid-UTF-8 upsert crash on PDF links. Pasted URLs are normalized and
  hashed so re-adds dedup without re-fetching.
- **Official sources**: `official_sources` config (hosts/gather_every/issues);
  `ingest.TagOfficial` stamps `trusted:official` on concepts from BACEN hosts;
  `serve --gather-every <dur>` runs a periodic gather daemon (off by default —
  needs network). Foundation of a 5-increment subsystem (issues source, rank boost,
  change tracking, gate relaxation to follow).
- **ISPB brand/trade-name search**: `ispb search` resolves brand names the BACEN
  registry lists under legal names (Nubank→NU PAGAMENTOS, banco→BCO rewrite,
  modal→Genial, bancoob→Sicoob, banese/banpara). Accent-insensitive matching
  already existed (`unaccent`).

### Fixed — config-driven DSN

- `db` subcommands (`db up`/`down`) now resolve the DSN from the config file, not
  only `--dsn`/`PIXKB_DSN`, matching the rest of the CLI.

### Added — cross-domain regulatory graph foundation (Phase 10 part 1)

- First-class `domain` on concepts: OKF model + front-matter → `concept.domain`
  column (migration 0007). Reconciled as the source of truth with the pre-existing
  `domain:*` tag convention — `tagDomain` sets the column, migration 0010 backfills
  existing rows (the Receita `domain:tax` corpus becomes `domain='tax'`); the tag
  stays a compatibility alias.
- `norm_ref` field + column (migration 0008) — stable citation-edge target.
- Optional `--domain` search facet (empty = all-domain, byte-for-byte v0.1);
  filters FTS and vector arms.
- `internal/link` BACEN citation-edge parser + `pixkb link` — deterministic,
  date-independent base-key matching materialises cross-domain `cites` edges
  (`edge` unique index, migration 0009).
- Per-domain vocabulary registry (`internal/query/domains/<name>/vocabulary.yaml`)
  and per-domain agent-charter registry (`roster.CharterFor`).

### Fixed

- Concept hydrator crashed on a NULL `resource` column (`coalesce`) — pre-existing;
  surfaced by the `pixkb link` full scan.

## [0.1.0] - 2026-07-18

First tagged release. pixkb is an air-gapped Open Knowledge Format (OKF)
knowledge base for the Brazilian BCB Pix/SPB + Receita Federal tax domain. The
OKF markdown bundle is the canonical source of truth; the Postgres + pgvector
index is a fully derived, rebuildable artifact.

### Added

- **OKF core** — canonical concept model with stable IDs, front-matter, and a
  bundle read/write round-trip.
- **Postgres + pgvector store** — bitemporal `concept_fact` table (valid-time vs
  transaction-time) with embedded, versioned migrations.
- **Hybrid search** — pure-Go hashing embedder, full-text (`pixpt` config) and
  exact-cosine vector arms fused via Reciprocal Rank Fusion, with a
  title-overlap boost.
- **Ingest sources** — ISO-20022, PDF (with TOC-junk suppression), git mirror,
  API-DICT/OpenAPI, docx, and xlsx → typed concepts.
- **Epoch engine** — per-epoch bundle snapshot committed to git, epoch diff, and
  `reindex` that rebuilds the derived store from the canonical bundle
  (read-and-validate before truncate; the index can never be left empty on a
  failed rebuild).
- **CLI + ops** — `ingest`, `search`, `similar`, `ask`, `reindex`, `diff`,
  `watch`, `serve`, `doctor`, `export-bundle`, `db`, `stats`, `qr`, `vocab`,
  `search-health`, `econindex`.
- **Agent fleet** — subscription-coding-agent fleet (on `github.com/inovacc/corral`)
  that curates the KB, an MCP server exposing pixkb verbs as the agents' tool
  surface, and a curator control loop where the hygiene engine is both trigger
  and write-back gate.
- **RAG** — grounded, citation-backed `pixkb ask` / `kb_ask` with cite-or-refuse
  guardrails, answer cache, and deterministic PII/LGPD redaction.
- **Pix BR Code (EMV MPM)** — pure-Go TLV codec + CRC16, static/dynamic payloads,
  PNG render and QR decode.
- **Air-gap delivery** — all-in-one pgvector container image that applies the
  schema and reindexes from the baked bundle end-to-end; internal-registry push
  path for corporate networks.

### Notes

- Air-gap by design: no metered embedding API and no native model runtime;
  recall improvements come from the agent fleet curating over the index, not a
  learned local model.
- Currency values use decimal types, never `float64`, per BCB/LGPD context.

[0.1.0]: https://github.com/dyammarcano/pixkb/releases/tag/v0.1.0
