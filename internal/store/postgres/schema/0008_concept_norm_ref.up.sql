-- Multi-domain foundation: record an OPTIONAL normative reference (e.g. a BCB
-- resolution id such as RES-BCB-1-2020) on the concept that IS that source.
-- Unlike domain this is nullable with no default. The partial index serves the
-- `pixkb link` citation lookup (citation norm_ref -> concept id).
ALTER TABLE concept ADD COLUMN IF NOT EXISTS norm_ref TEXT;
CREATE INDEX IF NOT EXISTS concept_norm_ref_idx ON concept(norm_ref) WHERE norm_ref IS NOT NULL;
