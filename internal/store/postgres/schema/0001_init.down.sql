DROP TABLE IF EXISTS edge;
DROP TABLE IF EXISTS concept_fact;
DROP TABLE IF EXISTS epoch;
DROP TABLE IF EXISTS embedding;
DROP TABLE IF EXISTS concept;
-- Extensions (vector, btree_gist) are intentionally NOT dropped: in real
-- deployments a DBA/superuser installs them and the app role does not own them
-- (DROP EXTENSION would fail with "must be owner"). They are cluster/db-level
-- infrastructure shared beyond pixkb; leave their lifecycle to the operator.
