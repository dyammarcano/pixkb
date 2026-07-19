-- Revert the domain tagging: drop the index first, then the column.
DROP INDEX IF EXISTS concept_domain_idx;
ALTER TABLE concept DROP COLUMN IF EXISTS domain;
