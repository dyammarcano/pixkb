-- initdb hook (runs once on first cluster init as the superuser).
-- pgvector ships compiled in the base image; we still must CREATE EXTENSION.
-- btree_gist backs the bitemporal tstzrange exclusion/GiST constraints.
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS btree_gist;
