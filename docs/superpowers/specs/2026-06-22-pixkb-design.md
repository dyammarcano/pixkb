# pixkb — Air-Gap OKF Knowledge Base for Pix / SPB

**Date:** 2026-06-22
**Status:** Draft for review (rev 2 — Postgres/pgvector pivot)
**Module:** `pixkb` (scaffolded into `C:\Users\dyamm\My Drive\dell\bacen`)

## 1. Summary

`pixkb` is an offline-first Go CLI that ingests Brazilian instant-payment
(Pix / SPB / SPI) knowledge — bacen open-source repositories, ISO 20022
PACS/CAMT message specifications, BCB manuals (PDF), and DICT/API docs —
into an **Open Knowledge Format (OKF) v0.1** bundle. The **OKF markdown
bundle is the canonical source of truth** (portable, git-versionable,
"just files" — air-gap native). **Postgres + pgvector** is a **derived,
rebuildable index** providing full-text search, vector (semantic) search,
bitemporal history, and graph queries in **one ACID engine**. Because the
bundle is canonical, the entire Postgres index can be rebuilt anywhere with
`pixkb reindex` — no lock-in. Everything runs fully air-gapped after a
one-time online gather.

## 2. Goals / Non-Goals

### Goals
- **OKF bundle = canonical** knowledge (markdown + YAML frontmatter,
  concept-per-file, markdown links = graph). Portable, git-versionable.
- **Postgres + pgvector = derived index**, rebuildable from the bundle:
  full-text + vector + bitemporal + graph in one transactional store.
- **Full-text** (`tsvector`/GIN, pt + en) **and** **vector search**
  (pgvector HNSW) with a **hybrid** ranker. Every embedded doc carries a
  **datetime** + **epoch**.
- **Historical evolution:** git commit + OKF `log.md` per epoch (human);
  Postgres **bitemporal** (`tstzrange` valid/tx) for queryable "as-of".
- **Continuous update:** online gather once, then an offline watcher
  processes artifacts dropped into `ingest/` (sneakernet / diode), cutting
  a new epoch.
- Air-gap delivery via a **pre-staged `pgvector/pgvector` container image**.

### Non-Goals
- No cloud services, no runtime calls to BCB/GitHub after the gather.
- Not a Pix transaction processor — a *knowledge* base, not an SPI
  participant. It documents the messages; it does not send them.
- No multi-user auth in v1 (optional read-only `serve`).

## 3. Locked Decisions (from brainstorming)

| Decision | Choice |
|----------|--------|
| KB scope | Repos + specs + PDFs (unified OKF graph) |
| Canonical store | **OKF markdown bundle** (source of truth) |
| Index engine | **Postgres + pgvector** (derived, rebuildable) |
| PG delivery | **Pre-staged container image** (`pgvector/pgvector`, pinned) |
| History model | git + `log.md` + **Postgres bitemporal** (`tstzrange`) |
| Ingress + update trigger | Online gather once, then watch `ingest/` drop-dir |
| Embedder | Interface: vendored ONNX MiniLM default + pure-Go hashing fallback |
| Extra reqs | Per-doc datetime; full vector search; full-text search |
| Name / location | `pixkb` in current dir |
| Superseded | ~~modernc.org/sqlite + duckdb-go/v2~~ (replaced by Postgres) |

> DuckDB can return later as an *optional* read-only analytical attach over
> the bundle for heavy epoch-diff scans, but is out of scope for v1.

## 4. Sources → OKF Concepts

One file = one concept; the file path is the concept identity. Concepts
cross-link with normal markdown links → graph.

```
kb/                                  # the OKF bundle (CANONICAL source of truth)
  index.md                           # root progressive-disclosure index
  repos/
    index.md
    pix-api.md                       # repo overview concept
    pix-api/<endpoint|struct>.md     # extracted code/API concepts
    pix-dict-api.md
    pix-dict-quickstart.md
    pix-api-recebimentos.md
  messages/
    index.md
    pacs.008.md  pacs.002.md  pacs.004.md  pacs.028.md  pacs.007.md
    pacs.009.md  pacs.003.md  pacs.010.md
    camt.056.md  camt.029.md
  manuals/
    index.md
    iniciacao-pix/<section>.md       # II Manual de Padrões p/ Iniciação do Pix (PDF → chunks)
    bcb-case-study/<section>.md      # RedHat BCB case study (PDF → chunks)
  apis/
    index.md
    dict-api/<tag>.md                # DICT / API-DICT docs
  log.md                             # bundle-level chronological epoch log
```

Sources to gather online (one-time):
- Repos: `bacen/pix-api`, `bacen/pix-dict-api`, `bacen/pix-dict-quickstart`,
  `bacen/pix-api-recebimentos`.
- PDFs (already local): `II_ManualdePadroesparaIniciacaodoPix.pdf`,
  `rh-central-bank-of-brazil-case-study-...pdf` (in Downloads).
- Specs: ISO 20022 PACS/CAMT definitions for the Pix-relevant set.
- API docs: BCB API-DICT HTML.

### Frontmatter schema (OKF-required + extensions)
```yaml
---
type: <PacsMessage|CamtMessage|Repo|CodeSymbol|ApiEndpoint|ManualSection|...>  # OKF: only required field
title: <string>
description: <string>
resource: <source URL or local path>
tags: [pix, pacs008, devolucao, ...]
timestamp: <RFC3339>          # OKF
# --- pixkb extensions ---
epoch: <int>                  # epoch that produced/last-changed this concept
content_sha: <sha256>         # change detection / dedup
source_uri: <origin>          # repo@commit | pdf#page | iso-msg-id | api-path
language: <pt|en>             # picks tsvector config (portuguese|english)
embedded_at: <RFC3339>        # when its vector was computed
embed_model: <minilm|hashing> # which embedder produced the vector
---
```

## 5. Architecture (hexagonal — `/scaffold:go` layout)

```
cmd/pixkb/                 Cobra entrypoint + subcommands
internal/
  ingest/                  source adapters → normalized []Concept
    git.go                 clone/read repos (go-git, offline mirrors)
    pdf.go                 PDF → text → section chunks
    isospec.go             PACS/CAMT message concepts (from XSD / curated)
    apidoc.go              API-DICT HTML → endpoint concepts
  okf/                     OKF read/write — CANONICAL bundle
    writer.go reader.go frontmatter.go graph.go
  store/postgres/          pgx pool; concept, FTS, pgvector, bitemporal, edges
    schema/                golang-migrate SQL migrations
    repo.go upsert.go query.go asof.go
  embed/                   Embedder interface
    embedder.go            interface + RRF helpers
    onnx.go                //go:build cgo  — MiniLM via onnxruntime (vendored)
    hashing.go             //go:build !cgo — pure-Go char-ngram/hashing
  epoch/                   snapshot + diff; writes log.md + git commit; pg tx
  watch/                   fsnotify on ingest/ → epoch.Run()
  query/                   hybrid FTS ∪ vector (RRF), filters (type/tag/as-of)
pkg/okf/                   exported OKF types for reuse
deploy/                    docker-compose.yml + pinned pgvector image notes
docs/  Taskfile.yml  .goreleaser.yaml  README.md
```

## 6. Postgres Schema (pgvector — index, rebuildable from bundle)

```sql
CREATE EXTENSION IF NOT EXISTS vector;   -- pgvector

CREATE TABLE concept (
  id          TEXT PRIMARY KEY,          -- bundle-relative path = OKF identity
  type        TEXT NOT NULL,
  title       TEXT, description TEXT, resource TEXT,
  tags        TEXT[],
  language    TEXT DEFAULT 'pt',
  body        TEXT NOT NULL,
  content_sha TEXT NOT NULL,
  source_uri  TEXT,
  first_epoch INT NOT NULL,
  last_epoch  INT NOT NULL,
  updated_at  TIMESTAMPTZ NOT NULL,
  fts tsvector GENERATED ALWAYS AS (
        to_tsvector('simple', coalesce(title,'') || ' ' || body)) STORED
);
CREATE INDEX concept_fts_gin ON concept USING GIN (fts);
CREATE INDEX concept_tags_gin ON concept USING GIN (tags);

CREATE TABLE embedding (
  id          TEXT REFERENCES concept(id) ON DELETE CASCADE,
  epoch       INT  NOT NULL,
  embed_model TEXT NOT NULL,
  dim         INT  NOT NULL,
  vec         vector,                    -- 384 (MiniLM) or N (hashing)
  embedded_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (id, epoch)
);
CREATE INDEX embedding_hnsw ON embedding
  USING hnsw (vec vector_cosine_ops);    -- pgvector ANN

CREATE TABLE epoch (
  n          INT PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL,       -- datetime per epoch
  source     TEXT, git_commit TEXT,
  added INT, changed INT, removed INT
);

-- bitemporal: query "as of" any epoch/date; append-only (no destructive update)
CREATE TABLE concept_fact (
  id TEXT, type TEXT, title TEXT, content_sha TEXT, epoch INT,
  valid  tstzrange NOT NULL,             -- when the fact held
  tx     tstzrange NOT NULL,             -- when recorded (tx_to='infinity' = current)
  EXCLUDE USING gist (id WITH =, tx WITH &&)  -- no overlapping tx windows per id
);

CREATE TABLE edge (src TEXT, dst TEXT, kind TEXT);  -- OKF link graph
```

## 7. Embedding & Vector Search

`embed.Embedder` interface (unchanged by the DB pivot — produces vectors;
pgvector stores/indexes them):
```go
type Embedder interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)
    Dim() int
    Name() string   // "minilm" | "hashing"
}
```
- **`onnx.go`** (`//go:build cgo`): all-MiniLM-L6-v2 (384-dim) ONNX +
  onnxruntime, both **vendored** into the air-gap release. Default when
  CGO + model present.
- **`hashing.go`** (`//go:build !cgo`): deterministic char-ngram/hashing.
  No model file, cleanest cross-compile, smaller binary, lower semantic
  quality. Auto-selected for no-CGO builds.

**Search modes** (`query/`):
- `--fts`    → Postgres `websearch_to_tsquery` + `ts_rank_cd` (BM25-ish).
- `--vector` → embed query → pgvector `<=>` cosine ANN (HNSW).
- `--hybrid` (default) → Reciprocal-Rank-Fusion of FTS + vector in one SQL.
- Filters: `--type`, `--tag`, `--as-of <epoch|date>` (bitemporal join).

## 8. Temporal / Epoch Model

- An **epoch** = one ingest pass: monotonic `n` + UTC `created_at`.
- **Human history:** OKF `log.md` line per change; **git commit per epoch**
  snapshots the whole bundle → full time-travel via git.
- **Queryable history:** Postgres bitemporal. Changed concepts insert a new
  `tx` row; superseded rows get `tx` upper bound closed (append-only).
  `--as-of T` → `WHERE valid @> T AND tx @> now()`.
- `pixkb diff N M` → concept-level added/changed/removed between epochs.

## 9. Ingest & Continuous-Update Pipeline

1. **Online gather (once):** `pixkb ingest` clones repos to local mirrors,
   reads PDFs, builds message + API concepts → writes **OKF bundle
   (canonical)** → epoch 0 → in **one pg transaction** upserts concept +
   FTS + embeddings + bitemporal rows + edges → git commit → `NOTIFY`.
2. **Air-gap update loop:** `pixkb watch` (offline) fsnotify on `ingest/`.
   New artifact (sneakernet/diode) → normalize → `content_sha` dedup (skip
   unchanged) → upsert changed concepts to bundle + pg → cut epoch `n+1`
   (re-embed changed only) → append `log.md` → git commit.
3. **Rebuild:** `pixkb reindex` drops + rebuilds the entire pg index from
   the canonical bundle — the no-lock-in guarantee.
4. **In-DB maintenance:** optional `pg_cron` jobs for VACUUM / HNSW reindex
   / bitemporal retention. `LISTEN/NOTIFY` lets `serve` refresh on new epoch.

## 10. CLI Surface

```
pixkb ingest        [--sources cfg]    one-time online gather → epoch 0
pixkb watch         [--dir ingest/]    offline daemon, auto-epoch on drops
pixkb epoch                            cut an epoch from current ingest/mirrors
pixkb reindex                          rebuild pg index from the OKF bundle
pixkb search "q" [--fts|--vector|--hybrid] [--type T] [--tag G] [--as-of N|DATE]
pixkb diff N M                         concept-level epoch diff
pixkb export-bundle [--out tar]        ship the OKF bundle (portable)
pixkb db up|down                       run/rollback pg migrations
pixkb serve         [--addr]           optional read-only browse/query API
pixkb doctor                           air-gap readiness checks (pg reachable, model present)
```

## 11. Air-Gap Packaging & Delivery

- **Postgres:** pinned `pgvector/pgvector:pgNN` container image saved with
  `docker save` → loaded on the air-gap host with `docker load`. A
  `deploy/docker-compose.yml` brings it up; `pixkb` connects via DSN
  (env/flag). pgvector ships **inside** the image — no on-host extension
  build.
- **Binary:** pure-Go default (`CGO_ENABLED=0`) → hashing embedder, no
  native deps. "Full" build (`CGO_ENABLED=1`) vendors onnxruntime + MiniLM
  for semantic vectors.
- **Release artifact:** `pixkb` binary + pgvector image tarball + compose
  file + seed OKF bundle + (optional) model/lib. No network at runtime.

## 12. Dependencies

- `github.com/spf13/cobra` — CLI.
- `github.com/jackc/pgx/v5` — Postgres driver/pool.
- `github.com/pgvector/pgvector-go` — vector type binding for pgx.
- `github.com/golang-migrate/migrate/v4` — pg schema migrations.
- `gopkg.in/yaml.v3` — frontmatter.
- `github.com/go-git/go-git/v5` — offline repo read.
- `github.com/ledongthuc/pdf` (or `pdfcpu`) — pure-Go PDF text.
- `github.com/yalue/onnxruntime_go` — ONNX (cgo build only).
- `github.com/fsnotify/fsnotify` — watcher.

## 13. Testing Strategy

- Table-driven unit tests, 80%+ coverage; slow tests (onnx, big PDF, pg
  integration) gated behind `testing.Short()` + `t.Skip`.
- Postgres integration tests via `dockertest`/testcontainers (pinned
  pgvector image) — also proves the air-gap image works.
- Golden tests for OKF writer (frontmatter + body stable).
- Bitemporal `as-of` queries tested against fixed epoch fixtures.
- Hybrid RRF tested with synthetic FTS/vector rank sets.
- `reindex` round-trip: bundle → pg → query parity vs pre-reindex.

## 14. Risks / Open Questions

- Running Postgres in the air gap adds ops surface; mitigated by the
  container image (pgvector preinstalled) + OKF bundle remaining canonical
  and rebuildable, so pg is disposable/replaceable.
- pgvector HNSW build memory for large corpora — fine at KB scale
  (thousands of concepts), tune `m`/`ef_construction` if needed.
- Bilingual FTS (pt/en): use per-concept `language` to pick `to_tsvector`
  config; `'simple'` fallback in the generated column, language-specific
  rank at query time.
- ISO 20022 XSDs: if official XSDs are not freely redistributable, store
  curated concept summaries instead of raw XSD.
- ONNX + onnxruntime adds CGO + a vendored native lib; pure-Go hashing
  fallback keeps a no-CGO path.

## 15. Next Steps

1. User reviews this spec (rev 2).
2. `writing-plans` → implementation plan.
3. `/scaffold:go` → project skeleton; bring up pgvector container; build per plan.
