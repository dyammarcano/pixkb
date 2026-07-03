CREATE EXTENSION IF NOT EXISTS vector;
-- btree_gist provides the `=` operator class so concept_fact's
-- EXCLUDE USING gist (id WITH =, tx WITH &&) can mix equality on id with
-- range-overlap on the tx window.
CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE IF NOT EXISTS concept (
  id          TEXT PRIMARY KEY,
  type        TEXT NOT NULL,
  title       TEXT,
  description TEXT,
  resource    TEXT,
  tags        TEXT[],
  language    TEXT DEFAULT 'pt',
  body        TEXT NOT NULL,
  content_sha TEXT NOT NULL,
  source_uri  TEXT,
  first_epoch INT NOT NULL,
  last_epoch  INT NOT NULL,
  updated_at  TIMESTAMPTZ NOT NULL,
  fts tsvector GENERATED ALWAYS AS (
        to_tsvector('simple', coalesce(title, '') || ' ' || body)) STORED
);
CREATE INDEX IF NOT EXISTS concept_fts_gin  ON concept USING GIN (fts);
CREATE INDEX IF NOT EXISTS concept_tags_gin ON concept USING GIN (tags);

-- vec is an UNTYPED `vector` column so a single schema serves both embedders:
-- hashing (256-dim, pure-Go default) and MiniLM/onnx (384-dim, cgo). The actual
-- width is recorded per-row in `dim` and enforced per-DB at runtime (Store.Open
-- guards that the configured embedder Dim() matches existing embedding.dim rows);
-- never mix dims in one HNSW index.
CREATE TABLE IF NOT EXISTS embedding (
  id          TEXT REFERENCES concept(id) ON DELETE CASCADE,
  epoch       INT  NOT NULL,
  embed_model TEXT NOT NULL,
  dim         INT  NOT NULL,
  vec         vector,
  embedded_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (id, epoch)
);
-- No ANN index: pgvector requires a fixed-dimension column to build HNSW, and
-- embedding.vec is intentionally untyped `vector` (serves hashing-256 and
-- minilm-384). Vector search uses exact cosine KNN (ORDER BY vec <=> q LIMIT k),
-- which is sub-millisecond with perfect recall at KB scale. A fixed-dim HNSW
-- index can be added via a follow-up migration if the corpus grows large.

CREATE TABLE IF NOT EXISTS epoch (
  n          INT PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL,
  source     TEXT,
  git_commit TEXT,
  added   INT,
  changed INT,
  removed INT
);

CREATE TABLE IF NOT EXISTS concept_fact (
  id          TEXT,
  type        TEXT,
  title       TEXT,
  content_sha TEXT,
  epoch       INT,
  valid  tstzrange NOT NULL,
  tx     tstzrange NOT NULL,
  EXCLUDE USING gist (id WITH =, tx WITH &&)
);

CREATE TABLE IF NOT EXISTS edge (
  src  TEXT,
  dst  TEXT,
  kind TEXT
);
