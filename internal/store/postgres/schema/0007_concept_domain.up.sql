-- Multi-domain foundation: tag every concept with the knowledge domain it
-- belongs to. The DEFAULT 'pix' back-fills existing rows, so the column is
-- additive and non-breaking. The index supports domain-scoped retrieval.
ALTER TABLE concept ADD COLUMN IF NOT EXISTS domain text NOT NULL DEFAULT 'pix';
CREATE INDEX IF NOT EXISTS concept_domain_idx ON concept(domain);
