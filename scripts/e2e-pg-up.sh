#!/usr/bin/env bash
# GoClaw v4 E2E Postgres bring-up — pgvector pg18 on port 5435.
# Reuses existing container if present (idempotent).

set -euo pipefail

CONTAINER="${E2E_PG_CONTAINER:-goclaw_v4_e2e_pg}"
PORT="${E2E_PG_PORT:-5435}"
DB="${E2E_PG_DB:-goclaw_v4_e2e}"
USER_DB="${E2E_PG_USER:-dev}"
PASS="${E2E_PG_PASSWORD:-devpass}"
IMAGE="${E2E_PG_IMAGE:-pgvector/pgvector:pg18}"

# Reuse if running on port 5435 with correct DB
if PGPASSWORD="$PASS" psql -h 127.0.0.1 -p "$PORT" -U "$USER_DB" -d "$DB" -c "SELECT 1" >/dev/null 2>&1; then
  echo "e2e PG already up on port $PORT (db=$DB)"
  exit 0
fi

# Skip start if our named container exists but is stopped
if docker ps -a --format '{{.Names}}' | grep -qx "$CONTAINER"; then
  docker start "$CONTAINER" >/dev/null
else
  docker run -d \
    --name "$CONTAINER" \
    -p "$PORT":5432 \
    -e POSTGRES_DB="$DB" \
    -e POSTGRES_USER="$USER_DB" \
    -e POSTGRES_PASSWORD="$PASS" \
    "$IMAGE" >/dev/null
fi

# Wait for ready (max 30s)
for i in $(seq 1 30); do
  if PGPASSWORD="$PASS" psql -h 127.0.0.1 -p "$PORT" -U "$USER_DB" -d "$DB" -c "SELECT 1" >/dev/null 2>&1; then
    PGPASSWORD="$PASS" psql -h 127.0.0.1 -p "$PORT" -U "$USER_DB" -d "$DB" -c "CREATE EXTENSION IF NOT EXISTS vector; CREATE EXTENSION IF NOT EXISTS pgcrypto;" >/dev/null
    echo "e2e PG up on port $PORT (db=$DB, container=$CONTAINER)"
    exit 0
  fi
  sleep 1
done

echo "ERROR: e2e PG failed to become ready within 30s" >&2
exit 1
