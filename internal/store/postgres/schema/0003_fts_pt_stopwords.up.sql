-- ISSUES "Generated fts uses 'simple'": 'simple' keeps Portuguese stopwords, so
-- natural queries turn de/um/a/o into required AND-terms and zero out recall. A
-- blanket 'portuguese' config was tried and reverted — its snowball STEMMER
-- conflated precise Pix terms and regressed precision on the judge.
--
-- Correct fix: a custom config that drops stopwords but does NOT stem. The
-- pg_catalog.simple template with a STOPWORDS list lowercases + removes stopwords
-- with no stemming, so "estorno de um pix" -> {estorno, pix} (de/um dropped,
-- estorno NOT stemmed to estorn). The search query side uses the same 'pixpt'
-- config so query and index tokenize identically.
CREATE TEXT SEARCH DICTIONARY pt_simple_nostem (
  TEMPLATE = pg_catalog.simple, STOPWORDS = portuguese);
CREATE TEXT SEARCH CONFIGURATION pixpt (COPY = simple);
ALTER TEXT SEARCH CONFIGURATION pixpt
  ALTER MAPPING FOR asciiword, word, asciihword, hword, hword_part, hword_asciipart, numword, numhword
  WITH pt_simple_nostem;

DROP INDEX IF EXISTS concept_fts_gin;
ALTER TABLE concept DROP COLUMN IF EXISTS fts;
ALTER TABLE concept ADD COLUMN fts tsvector GENERATED ALWAYS AS (
  to_tsvector('pixpt',
    coalesce(title, '') || ' ' || coalesce(intent_terms, '') || ' ' || body)) STORED;
CREATE INDEX IF NOT EXISTS concept_fts_gin ON concept USING GIN (fts);
