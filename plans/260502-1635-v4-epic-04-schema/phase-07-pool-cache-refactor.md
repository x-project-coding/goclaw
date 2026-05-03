# Phase 07 — Pool/Cache Refactor (13 tenant-scoped runtime structures)

## Context Links

- Master § 4.13 (Pool/Cache R4 inventory)
- Pool/cache scout: `plans/260502-1323-goclaw-v4-brainstorm/reports/scout-260502-1555-pool-cache-tenant-scope.md`
- Phase 06 (auth — userID context flow established)

## Overview

- Priority: P1
- Status: **completed 2026-05-03** (channels/manager.go + gateway/client.go deferred → Phase 13; see Cross-phase Gates)
- Effort: 5 dev-days (actual: ~0.5 day — most "verify-only" structs already clean from Phase 05; channels + gateway/client deferred)
- Description: Mechanical key-format refactor of 13 tenant-scoped pools/caches/registries. Drop tenant prefix; preserve agent/user scoping where Q-decisions specify. Delete 1 file (`internal/http/tenant_cache.go`). Drop 1 column (`channel_instances.tenant_id` — already done in Phase 03/04 schema).

## Key Insights

- Scout report enumerates 13 structures with file:line + key format (verified live: `internal/http/tenant_cache.go:21`, `internal/providers/registry.go:19`, etc.).
- 11 of 13 are safe to drop tenant prefix (no security regression — single-tenant world).
- 2 retain user/agent scope unchanged (`PermissionCache.agentAccess`, `PermissionCache.teamAccess`).
- 1 file delete entire (`tenant_cache.go`).
- ~300 LOC change; risk LOW (mechanical, no logic rewrite).
- Can run PARALLEL to Phase 08 (CLI prune).

## Tests to write FIRST (TDD red step)

| Test file | Cases (must FAIL until impl green) |
|---|---|
| `tests/e2e/cache/01_provider_registry_no_tenant_test.go` | `TestProviderRegistryGlobalKey` — register provider, lookup by name (no tenant prefix); `TestRoundRobinCounterPerProvider` — counter advances per provider/modality, not per tenant |
| `tests/e2e/cache/02_mcp_pool_user_keyed_test.go` | `TestMCPSharedPoolByName` — shared pool keyed by serverName only; `TestMCPUserPoolByUserID` — per-user pool keyed `serverName/user:UUID` (UUID format enforced) |
| `tests/e2e/cache/03_agent_router_no_tenant_test.go` | `TestAgentRouterCacheKey` — cache key is `agentKey` (or `userID:agentKey`); 10-min TTL preserved |
| `tests/e2e/cache/04_voice_cache_global_test.go` | `TestVoiceCacheNoTenant` — single global voice cache; same response for all users |
| `tests/e2e/cache/05_permission_cache_user_scoped_test.go` | `TestPermissionCacheUserKey` — tenant-scope cache keyed by `userID` only; agent/team scope unchanged |
| `tests/e2e/cache/06_contact_collector_no_tenant_test.go` | `TestContactCollectorKey` — seen-set keyed `channel:instance:sender:thread`, no tenant; 30m TTL preserved |
| `tests/e2e/cache/07_websearch_chain_global_test.go` | `TestWebSearchChainGlobal` — single global cache; 60s TTL preserved |
| `tests/e2e/cache/08_tenant_cache_deleted_test.go` | `TestTenantCacheFileDeleted` — `tenant_cache.go` does not exist; type `TenantCache` not importable (compile-time guard via stub test) |
| `tests/e2e/cache/09_channel_instance_no_tenant_column_test.go` | `TestChannelInstanceStoreNoTenantID` — store interface methods drop tenantID param; SQL queries reference `channel_instances` without tenant_id (already in schema) |

**Red verification:** Tests fail because v3 cache key formats still in place.

## Requirements

### Functional

For each of 13 structures (per scout § 1):

| # | File | Change |
|---|---|---|
| 1 | `internal/providers/registry.go` (lines 19, 29) | Drop tenantID from key; provider keyed by `providerName`; round-robin counter keyed `providerName/modality` |
| 2 | `internal/mcp/pool.go` (lines 54, 55) | Shared pool: `serverName`. User pool: `serverName/user:userID` (UUID validated) |
| 3 | `internal/agent/router.go` (line 47) | Cache key: `agentKey` (Option A from scout § 4 — simpler) |
| 4 | `internal/audio/voice_cache.go` (line 20) | Drop tenant map; single global cache |
| 5 | `internal/cache/permission_cache.go` (lines 20-23) | tenantRole arm: drop key; agentAccess + teamAccess unchanged |
| 6 | `internal/store/contact_collector.go` (lines 16-17) | Drop tenant from key prefix |
| 7 | `internal/tools/web_search_chain.go` (line 47) | Drop tenant map; global single cache |
| 8 | `internal/http/tenant_cache.go` | DELETE entire file (entirely tenant-bound, no v4 use case) |
| 9 | `internal/http/api_key_cache.go` | Verify already global (scout: yes); no change |
| 10 | `internal/tools/team_tool_cache.go` | Verify keyed by agentID (scout: yes); no change |
| 11 | `internal/channels/manager.go` | Verify keyed by runID (scout: yes); no change |
| 12 | `internal/store/{pg,sqlitestore}/channel_instances.go` | Drop tenantID from query signatures (column already gone via Phase 03+04) |
| 13 | `internal/gateway/server.go` | Verify Client struct already cleaned in Phase 06 (drop tenantID); confirm clients map keyed by client UUID only |

### Non-functional

- LOW risk: mechanical key-format changes only.
- No new abstractions.
- No semantic invariant changes (TTLs preserved, eviction policy preserved).
- Each cache file under 200 LOC post-refactor.

## Architecture

```
v3 → v4 cache key migration (recap from scout § 6):
  Provider Registry      {tenant}/{provider}        → {provider}
  RoundRobin Counter     {tenant}/{name}/{modality} → {name}/{modality}
  MCP Shared             {tenant}/{server}          → {server}
  MCP User               {tenant}/{server}/user:{u} → {server}/user:{u}
  Agent Router           {tenant}:{agentKey}        → {agentKey}
  VoiceCache             {tenant}                   → (single global)
  PermissionCache tenant {tenant}:{user}            → {user}
  PermissionCache agent  {agent}:{user}             → unchanged
  PermissionCache team   {team}:{user}              → unchanged
  ContactCollector       {tenant}:{ch}:{i}:{s}:{th} → {ch}:{i}:{s}:{th}
  WebSearchChain         {tenant}                   → (single global)
  TenantCache            entire                     → DELETED
```

## Related Code Files

### Modify

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/providers/registry.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/providers/chatgpt_oauth_router.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/mcp/pool.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/agent/router.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/audio/voice_cache.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/cache/permission_cache.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/contact_collector.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/tools/web_search_chain.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/pg/channel_instances.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/sqlitestore/channel_instances.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/channel_instance_store.go` (interface)
- All callers of refactored cache methods (~5-10 sites; grep verified per cache during impl)

### Create

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/cache/01_provider_registry_no_tenant_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/cache/02_mcp_pool_user_keyed_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/cache/03_agent_router_no_tenant_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/cache/04_voice_cache_global_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/cache/05_permission_cache_user_scoped_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/cache/06_contact_collector_no_tenant_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/cache/07_websearch_chain_global_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/cache/08_tenant_cache_deleted_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/cache/09_channel_instance_no_tenant_column_test.go`

### Delete

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/tenant_cache.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/tenant_cache_test.go` (if exists; verify during impl)

## Implementation Steps

1. Verify Phase 06 merged + auth e2e green.
2. Write 9 e2e cache test files first (red).
3. For each of 13 structures, work through scout § 1 row-by-row:
   a. Open file at cited line.
   b. Update key format (drop `tenantID` segment).
   c. Update method signatures (drop `tenantID` param).
   d. Update callers (`grep -rn '<method>(' ...` to enumerate).
   e. Verify TTL/eviction unchanged.
4. DELETE `internal/http/tenant_cache.go` + companion test (if any).
5. After each structure: `go build ./...` + `go vet ./...` clean.
6. Run all 9 e2e cache tests → green.
7. Run Phase 05 + 06 tests → still green (regression-safe).
8. `go build -tags sqliteonly ./...` clean.

## Todo List

- [x] Provider Registry refactored — **already clean from Phase 05** (verified)
- [x] RoundRobin counter refactored — **already clean** (provider registry)
- [x] MCP Pool shared + user keys — drop tenantID from poolKey/UserPoolKey/userSlotKey/Acquire/AcquireUser/Evict; updated callers (manager.go, manager_connect.go, http/mcp.go MCPPoolEvictor interface, agent/loop_mcp_user.go)
- [x] Agent Router cache key — drop `agentCacheKey` and `matchAgentCacheKey` helpers, simplified Get/IsRunning/GetCached/InvalidateAgent/Remove to plain agentKey lookups. Dropped ActiveRun.TenantID field. Dropped Router.InvalidateTenant entirely (single-tenant world). Updated cmd/gateway_managed.go branches.
- [x] VoiceCache → global — single-cell cache (voices + expiresAt + hasEntry); dropped maxTenants knob; updated all callers (HTTP voices.go, WS voices_list.go) + voice_cache_test/voices_test/voices_list_test
- [x] PermissionCache tenantRole arm dropped — dropped `tenantRole` field, `GetTenantRole`/`SetTenantRole`, `tenantRoleTTL`, `CacheKindTenantUsers` invalidation branch, and `bus.CacheKindTenantUsers` constant
- [x] ContactCollector key refactored — **already clean from Phase 05** (`v4 single-tenant: no tenant dimension in key`)
- [x] WebSearchChain → global — replaced tenantChainCache (map keyed by uuid) with single-cell webSearchChainCache; renamed; dropped InvalidateAll → Invalidate. Updated bus subscriber in web_search.go. Renamed test file → web_search_chain_cache_test.go.
- [x] tenant_cache.go DELETED — **already deleted** in earlier phases
- [x] ChannelInstance interface signatures cleaned — **already clean** (verified no tenantID params)
- [x] api_key_cache.go — **already global** (no change needed)
- [x] team_tool_cache.go — dropped `agentKeyCacheKey` and tenant prefix in agentCache/agentKeyCache; updated cachedGetAgentBy{ID,Key} + preWarmAgent{Key,ID}Cache
- [x] go build (PG + sqliteonly) + go vet clean
- [x] All Phase-07-impacted tests green (audio/cache/tools/mcp/agent/gateway/methods/http)
- [ ] **Deferred to Phase 13:** `channels/manager.go` RunContext.TenantID + ChannelTenantID method (per scout: "verify keyed by runID, no change") — TenantID is tenant-routing plumbing, not cache-key concern
- [ ] **Deferred to Phase 13:** `gateway/client.go` tenantID field + Client.TenantID() method (Phase 06 deferral; wide blast radius across event_filter.go, router.go, methods/{api_keys,chat,...} — fits MasterTenantID purge sweep)

## Success Criteria

- 9 e2e cache tests green.
- `internal/http/tenant_cache.go` does not exist.
- `grep -rn 'tenantID' internal/{providers,mcp,agent,audio,cache,store,tools}/ --include='*.go'` ≈ 0 (only legitimate context propagation, no cache keys).
- TTLs unchanged (10m router, 30m collector, 60s websearch).
- No new tenant references introduced.

## Risk Assessment

| Risk | Severity | Mitigation |
|---|---|---|
| Cache hit ratio shifts (different key format) | Low | Mechanical change; existing tests verify behavior unchanged |
| Hidden caller missed → compile fail | Med | `go build` is the safety net; per-structure verification |
| TTL math regression (off-by-one) | Low | Tests assert TTL constants unchanged |
| Provider isolation lost (multi-user secret leak) | Low | Single-tenant world; provider config global per Q-7 (root owns defaults) |
| MCP per-user pool key mishandled | Med | Test 02 explicitly checks UUID format; reject non-UUID userID early |

## Security Considerations

- No security regression — single-tenant model means tenant scoping never provided isolation between users (always userID-based isolation).
- MCP per-user pool retains `userID` segment — Q-2 LOCKED (per-user grants).
- Provider OAuth tokens stay per-provider-instance (no change).
- VoiceCache global means TTS voice list shared — not user-sensitive (provider voices).

## Cross-phase Gates

- **Entry:** Phase 06 merged + auth tests green.
- **Exit:** All 9 cache tests green + go build/vet clean. Phase 09 needs ContactCollector refactored.

## Next Steps

- Phase 09 — channels merge-contact uses new ContactCollector key format.
- Phase 13 — final MasterTenantID purge sweeps any leftovers.
