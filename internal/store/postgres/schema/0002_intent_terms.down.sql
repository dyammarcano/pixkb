-- Revert the FTS column to title+body and drop intent_terms.
DROP INDEX IF EXISTS concept_fts_gin;
ALTER TABLE concept DROP COLUMN IF EXISTS fts;
ALTER TABLE concept ADD COLUMN fts tsvector GENERATED ALWAYS AS (
  to_tsvector('simple', coalesce(title, '') || ' ' || body)) STORED;
CREATE INDEX IF NOT EXISTS concept_fts_gin ON concept USING GIN (fts);

ALTER TABLE concept DROP COLUMN IF EXISTS intent_terms;
