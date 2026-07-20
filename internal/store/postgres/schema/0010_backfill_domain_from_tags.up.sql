-- Reconcile the first-class domain COLUMN (added in 0007, default 'pix') with
-- the pre-existing domain:* TAG stamped by internal/ingest/gather.go. Before
-- this branch the ingest path never set the column, so tax concepts carried the
-- correct domain:tax tag but the wrong domain='pix' column. This backfill makes
-- the column authoritative by copying each row's domain:* tag into the column.
UPDATE concept c SET domain = replace(t.tag, 'domain:', '')
FROM (SELECT id, tag FROM concept, unnest(tags) AS tag WHERE tag LIKE 'domain:%') t
WHERE c.id = t.id AND replace(t.tag,'domain:','') <> '';
