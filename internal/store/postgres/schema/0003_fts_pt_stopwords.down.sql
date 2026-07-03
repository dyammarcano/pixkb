-- Revert the generated fts to the 'simple' config and drop the custom config.
DROP INDEX IF EXISTS concept_fts_gin;
ALTER TABLE concept DROP COLUMN IF EXISTS fts;
ALTER TABLE concept ADD COLUMN fts tsvector GENERATED ALWAYS AS (
  to_tsvector('simple',
    coalesce(title, '') || ' ' || coalesce(intent_terms, '') || ' ' || body)) STORED;
CREATE INDEX IF NOT EXISTS concept_fts_gin ON concept USING GIN (fts);

DROP TEXT SEARCH CONFIGURATION IF EXISTS pixpt;
DROP TEXT SEARCH DICTIONARY IF EXISTS pt_simple_nostem;
