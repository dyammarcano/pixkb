#!/usr/bin/env bash
#
# Spin a local throwaway pgvector container for pixkb integration tests, so the
# suite never touches the production KB. Point PIXKB_TEST_DSN at it:
#   PIXKB_TEST_DSN=postgres://pixkb:pixkb@localhost:5433/pixkb?sslmode=disable
#
# Usage:
#   bash deploy/local-testdb.sh up     # start + create extensions
#   bash deploy/local-testdb.sh down   # remove the container
set -euo pipefail

NAME=pixkb-testdb
IMAGE="${PIXKB_TESTDB_IMAGE:-pgvector/pgvector:pg17}"
PORT="${PIXKB_TESTDB_PORT:-5433}"
cmd="${1:-up}"

case "$cmd" in
up)
  docker rm -f "$NAME" >/dev/null 2>&1 || true
  echo ">> starting $IMAGE as $NAME on :$PORT"
  docker run -d --name "$NAME" \
    -e POSTGRES_USER=pixkb -e POSTGRES_PASSWORD=pixkb -e POSTGRES_DB=pixkb \
    -p "${PORT}:5432" "$IMAGE" >/dev/null
  for i in $(seq 1 60); do
    if docker exec "$NAME" pg_isready -U pixkb >/dev/null 2>&1; then
      echo "ready after ${i}s"
      break
    fi
    sleep 1
  done
  # pixkb is the container superuser, so it can install the extensions itself.
  docker exec "$NAME" psql -U pixkb -d pixkb \
    -c "CREATE EXTENSION IF NOT EXISTS vector; CREATE EXTENSION IF NOT EXISTS btree_gist;"
  echo ">> ready: postgres://pixkb:pixkb@localhost:${PORT}/pixkb?sslmode=disable"
  ;;
down)
  docker rm -f "$NAME" >/dev/null 2>&1 || true
  echo ">> removed $NAME"
  ;;
*)
  echo "usage: $0 up|down" >&2
  exit 1
  ;;
esac
