#!/usr/bin/env bash
# GoClaw v4 E2E Postgres tear-down.

set -euo pipefail

CONTAINER="${E2E_PG_CONTAINER:-goclaw_v4_e2e_pg}"

if docker ps -a --format '{{.Names}}' | grep -qx "$CONTAINER"; then
  docker rm -f "$CONTAINER" >/dev/null
  echo "removed $CONTAINER"
else
  echo "no container named $CONTAINER (skip)"
fi
