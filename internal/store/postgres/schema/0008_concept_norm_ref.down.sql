-- Revert the norm_ref tagging: drop the index first, then the column.
DROP INDEX IF EXISTS concept_norm_ref_idx;
ALTER TABLE concept DROP COLUMN IF EXISTS norm_ref;
