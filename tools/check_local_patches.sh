#!/usr/bin/env bash
# Verifies every patch documented in LOCAL_PATCHES.md still applies to the
# working tree. Run after any upstream merge:
#
#   git merge upstream/dev
#   tools/check_local_patches.sh
#
# Exits non-zero if any patch token has gone missing — at that point, walk
# LOCAL_PATCHES.md, re-apply the failing patch by hand, and re-run.
#
# When adding a new fork patch: add a `check` invocation below AND a section
# in LOCAL_PATCHES.md. Keep the two in sync.
set -uo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null)"
[[ -n "$REPO_ROOT" ]] || { echo "[check_local_patches] not in a git repo"; exit 2; }
cd "$REPO_ROOT"

errors=0

# Each check() prints PASS / FAIL with the patch name, but only fails the
# script overall (errors++) on FAIL.
check_grep() {
  local name="$1"; shift
  local expect_min="$1"; shift
  local hits
  hits=$(grep -nE "$@" 2>/dev/null | wc -l | tr -d ' ')
  if [[ "$hits" -ge "$expect_min" ]]; then
    printf '\033[32m[PASS]\033[0m %s (expected ≥%s, got %s)\n' "$name" "$expect_min" "$hits"
  else
    printf '\033[31m[FAIL]\033[0m %s (expected ≥%s, got %s)\n' "$name" "$expect_min" "$hits"
    errors=$((errors + 1))
  fi
}

check_file() {
  local name="$1"; shift
  if [[ -f "$1" ]]; then
    printf '\033[32m[PASS]\033[0m %s (file present: %s)\n' "$name" "$1"
  else
    printf '\033[31m[FAIL]\033[0m %s (file MISSING: %s)\n' "$name" "$1"
    errors=$((errors + 1))
  fi
}

# Patch 1 — subagent caps 5→30 / 20→50
check_grep "Patch 1: subagent caps" 4 \
  'MaxChildrenPerAgent: 30|range 1-50|MaxChildrenPerAgent, 50' \
  internal/tools/subagent_config.go internal/config/config.go \
  internal/gateway/methods/config_defaults.go cmd/gateway_agents.go

# Patch 2 — fork-image CI workflow
check_grep "Patch 2: fork-image CI" 2 \
  'Build fork image|ghcr.io/\$\{\{ github.repository \}\}' \
  .github/workflows/build-fork.yaml

# Patch 3 — require provider/model on agent-create ingress.
# Three ingress paths: direct create (agents.go), single-agent import
# (agents_import_agent.go), team import (teams_import.go). The resolver.go
# changes are covered separately by Patch 5 + Patch 6.
check_grep "Patch 3: agents require provider/model" 3 \
  'MsgProviderModelRequired|archive missing provider/model' \
  internal/http/agents.go internal/http/agents_import_agent.go \
  internal/http/teams_import.go

# Patch 4 — warn-not-reject on agent-import
check_grep "Patch 4: agents-import warn-not-reject" 1 \
  'agents\.import: archive missing provider/model — agent will be unusable' \
  internal/http/agents_import_agent.go

# Patch 5 — resolver backfill from system_config
check_grep "Patch 5: resolver system_config backfill" 1 \
  'Backfill empty provider/model from system_config defaults' \
  internal/agent/resolver.go

# Patch 6 — resolver master-tenant fallback
check_grep "Patch 6: resolver master-tenant fallback" 1 \
  'fall back to master tenant|MasterTenantID' \
  internal/agent/resolver.go

# Patch 7 — tenants hard-delete + cascade migration
check_grep "Patch 7: tenants hard-delete code" 6 \
  'DeleteTenant|TopicTenantDeleted|handleDelete' \
  internal/store/tenant_store.go internal/store/pg/tenant_store.go \
  internal/store/sqlitestore/tenants.go internal/http/tenants.go \
  internal/gateway/methods/tenants.go internal/bus/types.go
check_grep "Patch 7: RequiredSchemaVersion bump" 1 \
  'RequiredSchemaVersion uint = 99000' \
  internal/upgrade/version.go
check_file "Patch 7: tenant_cascade migration (up)" \
  migrations/099000_tenant_cascade.up.sql
check_file "Patch 7: tenant_cascade migration (down)" \
  migrations/099000_tenant_cascade.down.sql

# Patch 8 — xrouter provider + adapter scaffolding (workspace billing).
# Two file groups: live integration (xrouter.go composing OpenAIProvider with
# a context-aware RoundTripper) and parallel adapter scaffolding (kept for
# future ProviderAdapter wiring), plus the provider_type constant and the
# registry switch case. Upstream is extremely unlikely to ship matching
# tokens since they're 42bucks-specific.
check_grep "Patch 8: xrouter provider + adapter" 10 \
  'X-Router-Agent-Id|X-Router-User-Id|X-Router-Session-Id|NewXRouterProvider|NewXRouterAdapter|ProviderXRouter' \
  internal/providers/xrouter.go \
  internal/providers/xrouter_test.go \
  internal/providers/adapter_xrouter.go \
  internal/providers/adapter_xrouter_test.go \
  internal/providers/adapter_register.go \
  internal/store/provider_store.go \
  internal/http/providers.go

if [[ "$errors" -eq 0 ]]; then
  printf '\n\033[32mAll fork patches present.\033[0m\n'
  exit 0
fi

printf '\n\033[31m%s patch check(s) failed — walk LOCAL_PATCHES.md and re-apply.\033[0m\n' "$errors"
exit 1
