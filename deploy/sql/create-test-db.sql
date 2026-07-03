-- Provision a SEPARATE throwaway database for pixkb integration tests.
--
-- Why: the test suite truncates and drops tables. PIXKB_TEST_DSN must NEVER
-- point at the production KB database (pixkb) or it wipes the index.
--
-- Run ONCE as a SUPERUSER (the app role pixkb_app cannot CREATE DATABASE or
-- CREATE EXTENSION). From a machine that can reach the server:
--
--   psql "postgres://<superuser>@192.168.15.100:5432/postgres" -f deploy/sql/create-test-db.sql
--
-- Then tests use:
--   PIXKB_TEST_DSN=postgres://pixkb_app:***@192.168.15.100:5432/pixkb_test?sslmode=disable

-- 1. Create the test database owned by the app role (CREATE DATABASE cannot run
--    inside a transaction block; psql runs each statement autonomously here).
CREATE DATABASE pixkb_test OWNER pixkb_app;

-- 2. Enable the required extensions IN the new database (must reconnect to it).
\connect pixkb_test
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS btree_gist;

-- 3. Let the app role create/own schema objects there (pixkb db up runs as it).
GRANT ALL ON SCHEMA public TO pixkb_app;

-- pixkb itself applies the table schema; tests run `db up` automatically.
-- To reset later:  DROP DATABASE pixkb_test;  then re-run this file.
