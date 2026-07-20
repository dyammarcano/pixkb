-- Documented no-op. This migration is a DATA backfill, not a schema change: it
-- only copied each row's already-present domain:* tag into the domain column.
-- The domain:* tags remain untouched and are the recoverable source of truth,
-- so rolling back needs no destructive reset. Re-running 0010 up reconstructs
-- the column from the tags at any time.
SELECT 1;
