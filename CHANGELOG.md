# Changelog

All notable changes to pixkb are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
