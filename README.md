# pixkb — Air-Gap OKF Knowledge Base for Pix / SPB
<!-- rev:004 -->

`pixkb` ingests Brazilian instant-payment (Pix / SPB / SPI) and consumption-tax
(Receita Federal / Reforma Tributária) knowledge — ISO 20022 PACS/CAMT message
specs, BCB manuals and statutes (PDF), Office documents (docx / xlsx), OpenAPI
contracts, curated Markdown, and staged git repo mirrors — into a canonical
**Open Knowledge Format (OKF)** bundle, and indexes it for **full-text + vector
search** in Postgres + pgvector. It runs fully air-gapped after a one-time online
gather and evolves over **epochs** with bitemporal history.

## Architecture

- **OKF markdown bundle = source of truth.** One concept per file, YAML
  frontmatter + markdown body, cross-linked into a graph. Portable, git-versioned.
- **Postgres + pgvector = derived, rebuildable index.** Full-text (`tsvector`,
  bilingual pt/en), vector (exact cosine KNN over `pgvector`), bitemporal facts
  (`tstzrange` valid/tx → query "as of epoch N"), and the link graph. Rebuild it
  any time from the bundle with `pixkb reindex` — no lock-in.
- **Hybrid search** fuses FTS + vector via reciprocal-rank fusion (RRF).

## Ingest sources

Every source reads **local offline files** (air-gap: no network at ingest) and
emits OKF concepts, wired via `pixkb.yaml` (see [`pixkb.yaml.example`](pixkb.yaml.example)):

| Source | Input | Concepts |
|--------|-------|----------|
| ISO 20022 | built-in PACS/CAMT message set | `PacsMessage` / `CamtMessage` |
| `pdfs:` | BCB manuals (PDF) | `ManualSection` (TOC-junk suppressed) |
| `legislation:` | statute PDFs (e.g. LC 214/2025) | `LegalArticle` per *artigo* + Anexos |
| `docx:` | Word documents | `Reference` (heading-split, tables flattened) |
| `xlsx:` | Excel workbooks | `Reference` (one per sheet, Markdown table) |
| `markdown:` | curated reference docs | `Reference` (one per H2) |
| `openapi_specs:` | OpenAPI/Swagger contracts | `ApiEndpoint` per operation |
| `api_docs:` | local API-DICT HTML | `ApiEndpoint` |
| `repos:` | staged git mirrors | `Repo` + bundled OpenAPI |
| `scout_crawl_dir:` | offline web capture | `WebPage` |

Concepts carry a `domain:pix` / `domain:tax` tag (backfilled to `domain:pix` when
unset) so search can isolate the payments rail from the tax domain.

## Quickstart

```bash
# 1. Postgres + pgvector (local container) ...
docker compose -f deploy/docker-compose.yml up -d
export PIXKB_DSN='postgres://pixkb:pixkb@localhost:5432/pixkb?sslmode=disable'
#    ... or point at any Postgres where a superuser has run:
#    CREATE EXTENSION vector; CREATE EXTENSION btree_gist;

# 2. Apply schema
go run ./cmd/pixkb db up

# 3. Ingest the ISO 20022 Pix message set (+ PDFs/repos if configured)
go run ./cmd/pixkb ingest

# 4. Search
go run ./cmd/pixkb search "devolucao refund"           # hybrid (default)
go run ./cmd/pixkb search --mode fts "credit transfer"
go run ./cmd/pixkb search --mode vector "cancel payment"
```

## Commands

| Command | Purpose |
|---------|---------|
| `db up\|down` | Apply / roll back the embedded pgvector schema migration |
| `ingest` | Gather sources → OKF bundle + index, cutting a new epoch |
| `search <q> [--mode hybrid\|fts\|vector] [--type T] [--tag G] [--limit N]` | Search the KB |
| `reindex` | Rebuild the Postgres index from the canonical bundle |
| `diff <n> <m>` | Concept-level diff between two epochs (bitemporal) |
| `watch [--debounce-ms]` | Offline daemon: re-ingest when artifacts land in the drop-dir |
| `serve [--addr]` | Read-only HTTP JSON search API (`GET /search?q=...`) |
| `export-bundle [--out]` | Package the OKF bundle as a portable tar.gz |
| `doctor` | Air-gap readiness checks (db, pgvector, embedder, bundle) |
| `hygiene [--json] [--check C] [--errors]` | Deterministic KB health scan (deviations + mechanical issues) |
| `curate [--plan\|--apply] [--limit N] [--provider P]` | Curation loop: scan → route to fix agents → gate → upsert+reindex |
| `concept get\|upsert\|rm <id>` | Read / write-back / remove a single concept |
| `qr read <brcode>\|--image f` / `qr write --key --name --city [--amount --txid --png f]` | Pix BR Code (EMV MPM / "Copia e Cola") parse (string or QR image) / build |

## Configuration

Settings resolve as defaults < `pixkb.yaml` < env (`PIXKB_*`); `--dsn` overrides
the DSN. See [`pixkb.yaml.example`](pixkb.yaml.example).

| Env | Default | Meaning |
|-----|---------|---------|
| `PIXKB_DSN` | — | Postgres DSN |
| `PIXKB_BUNDLE` | `kb` | OKF bundle directory (canonical) |
| `PIXKB_INGEST` | `ingest` | drop-dir watched by `watch` |
| `PIXKB_EMBEDDER` | `hashing` | `hashing` (pure-Go); `openai` is opt-in dev-only (metered, not air-gap) |

## Build variants

- **Default** (`CGO_ENABLED=0`): pure-Go hashing embedder, no native deps —
  cross-compiles cleanly into the air gap. This is the only build.
- No learned-model embedder: ONNX/MiniLM was removed (a native runtime + vendored
  model violates the air-gap rule — subscription agents only, no metered API, no
  native model runtime). Stronger recall is to come from the agy agent fleet.

## Air-gap packaging

Ship the `pixkb` binary + a pre-staged `pgvector/pgvector` container image
(`docker save` → `docker load`) + the seed OKF bundle. No network at runtime.
