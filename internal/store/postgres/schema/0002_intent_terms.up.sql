-- ADR 0001: agent-generated recall terms live in a dedicated intent_terms field
-- woven into the FTS index (not in tags, which are unindexed, nor the body,
-- which must stay BACEN-normative).

ALTER TABLE concept ADD COLUMN IF NOT EXISTS intent_terms text;

-- The fts column is GENERATED, so its expression can only change via drop+add;
-- the GIN index is dropped with it and recreated. Existing rows regenerate fts
-- automatically on the ADD.
DROP INDEX IF EXISTS concept_fts_gin;
ALTER TABLE concept DROP COLUMN IF EXISTS fts;
ALTER TABLE concept ADD COLUMN fts tsvector GENERATED ALWAYS AS (
  to_tsvector('simple',
    coalesce(title, '') || ' ' || coalesce(intent_terms, '') || ' ' || body)) STORED;
CREATE INDEX IF NOT EXISTS concept_fts_gin ON concept USING GIN (fts);
