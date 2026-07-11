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
check_grep "Patch 7: RequiredSchemaVersion in fork block" 1 \
  'RequiredSchemaVersion uint = 99[0-9]{3}' \
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

# Patch 9 — per-call model override on both HTTP (X-GoClaw-Model header) and
# WS (chat.send modelOverride field) entry points so x-api's per-session
# routing can pin the LLM model without PATCHing the agent. The WS path is
# the load-bearing one (x-api uses chat.send RPC, not HTTP).
check_grep "Patch 9: model-override entry points" 5 \
  'X-GoClaw-Model|ModelOverride:[[:space:]]+(modelOverride|params\.ModelOverride)|"modelOverride,omitempty"' \
  internal/http/chat_completions.go internal/gateway/methods/chat.go

# Patch 10 — modelOverride also swaps provider to tenant xrouter so the
# agent's stored provider doesn't 400 on models it can't serve (e.g.
# openai-codex/ChatGPT-OAuth refusing ~anthropic/claude-sonnet-latest).
check_grep "Patch 10: provider-swap on modelOverride" 3 \
  'SetProviderRegistry|ProviderOverride: providerOverride|providerReg\.Get\(runCtx, "xrouter"\)' \
  internal/gateway/methods/chat.go cmd/gateway_methods.go

# Patch 11 — per-session routing mode ('auto'|'fast'|'complex') threaded from
# the WS chat.send RPC down to x-router as the X-Router-Mode HTTP header.
# x-api resolves session.routingMode and passes it on chat.send; goclaw
# carries it RunRequest → RunInput → Options[OptRoutingMode] → xrouter
# RoundTripper. Mode 'custom' uses modelOverride only and sends no header.
check_grep "Patch 11: routingMode → X-Router-Mode header" 6 \
  'X-Router-Mode|OptRoutingMode|RoutingMode:[[:space:]]+(req|params)\.RoutingMode|"routingMode,omitempty"' \
  internal/gateway/methods/chat.go internal/agent/loop_types.go \
  internal/agent/loop_pipeline_adapter.go internal/agent/loop_pipeline_callbacks.go \
  internal/pipeline/run_state.go internal/providers/claude_cli.go \
  internal/providers/xrouter.go

# Patch 12 — opt-in inline SKILL.md body for pinned skills. Pinned summaries
# can inline opted-in SKILL.md bodies while non-pinned skill_search summaries
# remain metadata-only.
check_grep "Patch 12: pinned skill inline body" 12 \
  'GOCLAW_INLINE_BODY|InlineBody|inline_body|inlineBodyMaxBytes' \
  internal/skills/loader.go internal/skills/loader_test.go \
  internal/agent/systemprompt_sections.go

# Patch 13 — backfill skipped upstream v3.12.0 migrations for DBs already at 099000.
check_file "Patch 13: upstream v3.12 backfill migration (up)" \
  migrations/099001_upstream_v3_12_backfill.up.sql
check_file "Patch 13: upstream v3.12 backfill migration (down)" \
  migrations/099001_upstream_v3_12_backfill.down.sql
check_grep "Patch 13: RequiredSchemaVersion 99002" 1 \
  'RequiredSchemaVersion uint = 99002' \
  internal/upgrade/version.go
check_grep "Patch 13: upstream v3.12 backfill contents" 10 \
  'secure_cli_agent_grants|webhooks|webhook_calls|workstations|workstation_permissions|workstation_activity|model_fallback|skill_agent_grants|can_manage|DELETE FROM skill_agent_grants' \
  migrations/099001_upstream_v3_12_backfill.up.sql

# Patch 14 — backfill skipped upstream v3.13/v3.14 migrations for DBs already at 099001.
check_file "Patch 14: upstream v3.13/v3.14 backfill migration (up)" \
  migrations/099002_upstream_v3_13_v3_14_backfill.up.sql
check_file "Patch 14: upstream v3.13/v3.14 backfill migration (down)" \
  migrations/099002_upstream_v3_13_v3_14_backfill.down.sql
check_grep "Patch 14: upstream v3.13/v3.14 backfill contents" 10 \
  'bitrix_portals|browser_cookies|usage_pricing_catalog|usage_cap_policies|run_timeline_items|mcp_context_grants|channel_memory_extraction_runs|secure_cli_agent_credentials|skill_user_grants_skill_id_user_id_tenant_id_key|skill_versions|usage_events|usage_event_rollups' \
  migrations/099002_upstream_v3_13_v3_14_backfill.up.sql

# Patch 24 — sessions.list managedBy filter (ops-lead delegation). The WS
# param `managedBy` threads to SessionListOpts.ManagedBy which adds a
# metadata->>'managedBy' equality clause in both DB stores.
check_grep "Patch 24: managedBy list filter" 4 \
  "metadata->>'managedBy'|\"managedBy\"|ManagedBy string" \
  internal/store/pg/sessions_list.go internal/store/sqlitestore/sessions_list.go \
  internal/gateway/methods/sessions.go internal/store/session_store.go

if [[ "$errors" -eq 0 ]]; then
  printf '\n\033[32mAll fork patches present.\033[0m\n'
  exit 0
fi

printf '\n\033[31m%s patch check(s) failed — walk LOCAL_PATCHES.md and re-apply.\033[0m\n' "$errors"
exit 1
