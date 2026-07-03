#!/usr/bin/env bash
# initdb hook: apply the pixkb derived schema once the local cluster is up.
# Runs as part of the Postgres entrypoint's first-init phase (extensions from
# 10-extensions.sql are already in place). Idempotent — `pixkb db up` is safe
# to re-run.
set -euo pipefail

# During initdb the server listens on the UNIX SOCKET ONLY (TCP is not up yet),
# so connect via the socket dir, not localhost:5432 (which is refused here).
export PIXKB_DSN="postgres://${POSTGRES_USER:-pixkb}:${POSTGRES_PASSWORD:-pixkb}@/${POSTGRES_DB:-pixkb}?host=/var/run/postgresql&sslmode=disable"

echo "[pixkb-initdb] applying schema via 'pixkb db up'"
pixkb db up
echo "[pixkb-initdb] schema applied"
