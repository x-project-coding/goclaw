# Phase 05 — Stores Refactor (PG + SQLite, drop tenant_id, swap user_id type)

## Context Links

- Master § 4.1 (Store layer impact, 1131 tenant_id refs)
- Decisions Q1-Q12, Q-10 (sessions rename)
- Phase 03 + 04 (schema source-of-truth)
- v3-baseline.md (Phase 02)

## Overview

- Priority: P0
- Status: pending (PR-05A merged on dev-v4: 02c9a3fb + be0a0a46 + 2a118df2 + c25e6508 + 9e8758ca)
- Effort: 22 dev-days (largest phase)
- Description: Refactor 90 PG store files + 88 SQLite store files to match v4 schema. Drop 1131 tenant_id refs. Swap `user_id VARCHAR(255)` → `string` (UUID-as-string). Rename sessions store → agent_sessions. Delete tenant_store + tenant_config_store + scope.QueryScope.TenantID. Add new stores: users, user_sessions, skill_versions, curator_runs, user_hook_budget. Wire EventBus UserID validator (R2). Parse UserID UUID in episodic worker (R3).

## PR-05B sub-split (post-scout 2026-05-02)

Original PR-05B too large. Scout reports `scout-260502-2010-pr-05b-1-foundation.md` and `scout-260502-2025-tenant-full-drop.md` reveal: agents.go references `tenant_id` columns the v4 schema does NOT have (broken at runtime); TenantStore interface still has 5 external callers (`agent/resolver`, `http/skills`, `gateway/router`, `gateway/methods/tenants`, test mocks); 90 callers of `TenantIDFromContext` + 205 `MasterTenantID` refs app-wide.

Split into 3 Layered PRs:

**L1 — PR-05B-1a: Agents store bug fix (this session)**
- Drop `AgentData.TenantID` field (no schema column).
- Refactor `pg/agents.go` + `sqlitestore/agents.go` 7 CRUD methods (Create, GetByKey, GetByID, Update, Delete, List, GetDefault).
- Drop `tenant_id` from `agent_shares` INSERT/WHERE in same files.
- Defer sibling files (`agents_access.go`, `agents_context.go`, `agents_batch.go`) — they share helpers with 35 other PG stores, must move with L3.
- Test 03 (`agents_no_tenant_test.go`) TDD red→green.
- ~6 files, ~+50 LOC net.

**L2 — PR-05B-1b: TenantStore interface teardown (next session)**
- DELETE `internal/store/tenant_store.go` + `pg/tenant_store.go` + `sqlitestore/tenant*.go`.
- DELETE `internal/store/tenant_config_store.go` + impls (orthogonal feature flags).
- Refactor 5 callers:
  - `internal/agent/resolver.go` — drop `resolveTenantSlug`, switch workspace path to `~/.goclaw/workspace/<owner_user_id>/<agent_key>/`.
  - `internal/http/skills.go` — replace `tenantStore` + `requireTenantAdmin` with role-based check (`requireUserAdmin`).
  - `internal/gateway/router.go` — drop tenant enrichment in connect response.
  - `internal/gateway/methods/tenants.go` — DELETE entire file.
  - Test mocks (`internal/http/auth_test.go`, `tenant_backup_auth_helpers_test.go`).
- Wire factories to drop Tenants field.
- ~10-15 files, ~-300 LOC net (mostly deletes).

**L3 — PR-05B-2/3: Context purge + scope refactor (multi-session)**
- DELETE `TenantIDKey`, `WithTenantID`, `TenantIDFromContext` (~90 callers).
- DELETE `IsCrossTenant` (~35 callers); replace agents access path with `IsAdminRole(ctx)`.
- REFACTOR `IsMasterScope` → `IsOwnerRole(ctx) || IsAdminRole(ctx)`.
- DELETE `MasterTenantID` (205 refs across ~50 files).
- DROP `QueryScope.TenantID` field; refactor `BuildScopeClause` in `store/base/query_builder.go`.
- Refactor 35 PG store files + 71 SQLite files: drop scopeClause, replace with owner_user_id / agent_id filtering or unscoped (per Q-7 root-owns-global).
- Run in batches of 8-10 files per sub-PR.
- ~150 files, ~3-5 dev-days (was Phase 13).

### Q-decisions (locked 2026-05-02 20:30)
- **Workspace path:** `~/.goclaw/workspace/<owner_user_id>/<agent_key>/` (not `~/workspace/` — conflicts with other apps).
- **Cross-user model:** Owner-only by default; admin/root role bypass; explicit grants via `agent_shares` (granted_by + user_id).
- **Scoping field:** `owner_user_id` (UUID FK to users.id). `owner_id` (legacy string) untouched, Phase 13 decides drop.
- **`IsMasterScope`:** `return IsOwnerRole(ctx) || IsAdminRole(ctx)` — refactored in L3.
- **Path scoping:** `owner_user_id` UUID, no slug.
- **`tenant_config_store`:** Delete in L2 (feature flags orthogonal but tied to tenant concept).

## Key Insights

- 1131 tenant_id refs verified by `grep -rn tenant_id internal/store/{pg,sqlitestore}/ | wc -l` (matches master § 2 audit-corrected D-6).
- Audit D-4: `tenant_config_store.go` paths corrected — DELETE `internal/store/tenant_config_store.go`, `internal/store/pg/tenant_configs.go`, `internal/store/sqlitestore/tenant-configs.go` (3 files).
- DELETE `internal/store/scope.go` `QueryScope.TenantID` field (audit-corrected D-1: file exists at `internal/store/scope.go`, 2679 bytes).
- DELETE `internal/store/tenant_store.go` + `internal/store/pg/tenant_store.go`.
- ADD: `internal/store/users_store.go` (interface) + `internal/store/{pg,sqlitestore}/users.go` (impl).
- ADD: `internal/store/user_sessions_store.go` + `{pg,sqlitestore}/user_sessions.go`.
- RENAME: `internal/sessions/` package → `internal/agentsessions/` (Q-10) + Go store: `SessionStore` → `AgentSessionStore`.
- 22 day effort = ~1 day per 8-9 files (90+88 / 22 days).
- This phase too big as one PR — split into 4 logical sub-PRs (see "Sub-PR Strategy" below).

## Tests to write FIRST (TDD red step)

| Test file | Cases (must FAIL until impl green) |
|---|---|
| `tests/e2e/stores/01_users_store_test.go` | `TestUsersCRUD` — create/read/update/delete via PG store; password_hash stored as opaque text; email unique constraint enforced |
| `tests/e2e/stores/02_user_sessions_store_test.go` | `TestUserSessionsCreateRevoke` — create refresh session, lookup by hash, revoke, ensure revoked is not returned in active query |
| `tests/e2e/stores/03_agents_no_tenant_test.go` | `TestAgentCreateNoTenant` — `AgentStore.Create()` no longer accepts tenant_id; uses `owner_user_id`; verifies SELECT returns row scoped by owner |
| `tests/e2e/stores/04_sessions_renamed_test.go` | `TestAgentSessionStoreUsesNewTable` — `AgentSessionStore.Save()` writes to `agent_sessions` table (not `sessions`); reading from `sessions` errors (table missing) |
| `tests/e2e/stores/05_kg_user_id_uuid_test.go` | `TestKGEntityUserIDNullable` — insert kg_entity with user_id=NULL succeeds; insert with valid UUID succeeds; insert with malformed string fails (PG type check or store-layer validation) |
| `tests/e2e/stores/06_eventbus_validate_user_id_test.go` | `TestValidateUserID` — publishing DomainEvent with non-UUID UserID logs warning (slog assert via test handler); publishing with valid UUID does NOT warn |
| `tests/e2e/stores/07_episodic_worker_uuid_parse_test.go` | `TestEpisodicWorkerRejectsBadUUID` — episodic worker entry rejects non-UUID UserID with structured error; valid UUID processes normally |
| `tests/e2e/stores/08_skill_versions_store_test.go` | `TestSkillVersionsStore` — create version, archive (sets archived_at + clears content), list active versions excludes archived |
| `tests/e2e/stores/09_curator_runs_store_test.go` | `TestCuratorRunsStore` — start run, append events, complete run; status transitions enforced |
| `tests/e2e/stores/10_user_hook_budget_test.go` | `TestUserHookBudgetMonthlyReset` — budget resets at month boundary; per-user isolation (user A budget independent from user B) |
| `tests/e2e/stores/11_no_master_tenant_in_store_layer_test.go` | `TestNoMasterTenantUsage` — `grep -rn MasterTenantID internal/store/` returns 0 (smoke test via shell — phase 13 deep cleanup; here just store layer is clean) |

**Red verification:** All 11 tests fail because users/user_sessions/etc. stores don't exist yet, agents store still requires tenant_id, etc.

## Requirements

### Functional

#### NEW interfaces (`internal/store/`)

- `users_store.go` — `UsersStore { Create, Get, GetByEmail, List, Update, Delete }`.
- `user_sessions_store.go` — `UserSessionsStore { Create, GetByHash, Revoke, ListActiveByUser }`.
- `skill_versions_store.go` — `SkillVersionsStore { CreateVersion, Archive, ListByDocID, GetActive }`.
- `curator_runs_store.go` — `CuratorRunsStore { Start, AppendEvent, Complete, ListByDocID }`.
- `user_hook_budget_store.go` — `UserHookBudgetStore { Get, Increment, ResetMonthly }`.
- `agent_sessions_store.go` — RENAMED from `sessions_store.go` (Q-10).

#### NEW PG impl (`internal/store/pg/`)

- `users.go`, `user_sessions.go`, `skill_versions.go`, `curator_runs.go`, `user_hook_budget.go`.
- `agent_sessions.go` + `agent_sessions_list.go` (RENAMED from `sessions.go` + `sessions_list.go`).

#### NEW SQLite impl (`internal/store/sqlitestore/`)

- `users.go`, `user_sessions.go`, `skill_versions.go`, `curator_runs.go`, `user_hook_budget.go`, `agent_sessions.go`.

#### REFACTOR PG impl (90 files)

- All `tenant_id` SQL clauses removed.
- All `userID string` params validated as UUID at entry (or rely on PG type check).
- All `WHERE tenant_id = $N` clauses removed; replace with `WHERE owner_user_id = $N` (where applicable per Q-decisions) OR drop scope clause entirely (root user owns global tables per Q-7).
- `store/base/scope.go` (`BuildScopeClause`) — drop `TenantID` field; keep agent/team/owner-user scoping.

#### REFACTOR SQLite impl (88 files)

- Mirror PG changes.

#### EventBus validator (R2)

- ADD `internal/eventbus/validate_user_id.go` — mirror existing `validate_agent_id.go` pattern (file:line confirmed at `internal/eventbus/validate_agent_id.go`). Logs `non_uuid_user_id` slog.Warn at publish time.
- Wire into `bus_impl.go` Publish path (look at how `validateAgentID` is wired).

#### Episodic worker UUID parse (R3)

- `internal/consolidation/` (or wherever episodic worker lives — verify path during impl). At entry, parse `UserID` as UUID; on parse error, increment metric + skip event with structured log.

#### MasterTenantID purge (partial — store layer only)

- DELETE `internal/store/tenant_store.go` (var declaration line 13).
- DELETE `internal/providers/registry.go:15` reference (refactor to no-master).
- Leave non-store MasterTenantID references for Phase 13.

### Non-functional

- File size policy: each refactored store file stays ≤ 300 LOC (some v3 files >700 LOC; split during refactor).
- Build clean: `go build ./...` AND `go build -tags sqliteonly ./...` after every sub-PR.
- Vet clean.

## Architecture

```
Sub-PR strategy (4 PRs, sequential):

PR-05A — New stores skeleton (5 dev-days)
  ADD users + user_sessions + skill_versions + curator_runs + user_hook_budget
  ADD interfaces + PG impl + SQLite impl + 5 store tests
  Verify: tests/e2e/stores/01,02,08,09,10 green

PR-05B — Drop tenant_id from existing PG stores (8 dev-days)
  Refactor 90 PG store files
  Drop tenant_store.go, tenant_configs.go, scope.go QueryScope.TenantID
  Refactor BuildScopeClause helper
  Verify: tests/e2e/stores/03 green

PR-05C — Mirror SQLite refactor + agent_sessions rename (6 dev-days)
  Refactor 88 SQLite store files
  Rename sessions/ → agentsessions/ package + SessionStore → AgentSessionStore
  Update 37+ SQL refs from `sessions` to `agent_sessions`
  Verify: tests/e2e/stores/04 green + go build -tags sqliteonly clean

PR-05D — EventBus + worker validators (3 dev-days)
  ADD validate_user_id.go (mirror agent_id pattern)
  Wire into bus_impl.go publish
  Episodic worker UUID parse at entry
  Verify: tests/e2e/stores/05,06,07 green
```

## Related Code Files

### Create

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/users_store.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/user_sessions_store.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/skill_versions_store.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/curator_runs_store.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/user_hook_budget_store.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/agent_sessions_store.go` (rename of sessions_store)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/pg/users.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/pg/user_sessions.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/pg/skill_versions.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/pg/curator_runs.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/pg/user_hook_budget.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/pg/agent_sessions.go` (rename pg/sessions.go)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/pg/agent_sessions_list.go` (rename pg/sessions_list.go)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/sqlitestore/users.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/sqlitestore/user_sessions.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/sqlitestore/skill_versions.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/sqlitestore/curator_runs.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/sqlitestore/user_hook_budget.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/sqlitestore/agent_sessions.go` (rename sqlitestore/sessions related files)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/eventbus/validate_user_id.go` (mirror validate_agent_id.go)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/eventbus/validate_user_id_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/stores/01_users_store_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/stores/02_user_sessions_store_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/stores/03_agents_no_tenant_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/stores/04_sessions_renamed_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/stores/05_kg_user_id_uuid_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/stores/06_eventbus_validate_user_id_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/stores/07_episodic_worker_uuid_parse_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/stores/08_skill_versions_store_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/stores/09_curator_runs_store_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/stores/10_user_hook_budget_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/stores/11_no_master_tenant_in_store_layer_test.go`

### Modify

- All 90 files in `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/pg/` — drop tenant_id from SQL + Go signatures
- All 88 files in `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/sqlitestore/` — same
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/scope.go` — drop `TenantID` field from `QueryScope`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/base/dialect.go` (verify path during impl) — drop tenant clauses
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/eventbus/bus_impl.go` — wire `validateUserID` alongside existing `validateAgentID`
- Rename `internal/sessions/` → `internal/agentsessions/`:
  - `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/sessions/key.go` → `internal/agentsessions/key.go`
  - `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/sessions/manager.go` → `internal/agentsessions/manager.go`
  - All test files (*_test.go x4)
- All Go imports `internal/sessions` → `internal/agentsessions` (~20 caller files)
- Episodic worker entry (verify path: `internal/consolidation/episodic*.go` or `internal/memory/`)

### Delete

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/tenant_store.go` (interface)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/pg/tenant_store.go` (PG impl)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/tenant_config_store.go` (interface — audit D-4 corrected path)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/pg/tenant_configs.go` (PG impl — audit D-4 corrected path)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/sqlitestore/tenant-configs.go` (SQLite impl — audit D-4 corrected path)

### Read for context

- Phase 03 + 04 schema files (column source-of-truth)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/eventbus/validate_agent_id.go` (mirror pattern)

## Implementation Steps

### Sub-PR 05A — New stores skeleton

1. Write 5 store interfaces (users, user_sessions, skill_versions, curator_runs, user_hook_budget) in `internal/store/`.
2. Write 5 PG impl files in `internal/store/pg/`.
3. Write 5 SQLite impl files in `internal/store/sqlitestore/`.
4. Write 5 e2e tests (01, 02, 08, 09, 10) — must fail until impl ready.
5. Wire stores into `internal/store/pg/factory.go` + `internal/store/sqlitestore/factory.go`.
6. `go build ./...` + `go build -tags sqliteonly ./...` clean.
7. e2e tests 01,02,08,09,10 green.

### Sub-PR 05B — Drop tenant_id from PG stores

1. `git rm internal/store/tenant_store.go internal/store/pg/tenant_store.go internal/store/tenant_config_store.go internal/store/pg/tenant_configs.go internal/store/sqlitestore/tenant-configs.go`.
2. Drop `TenantID` field from `internal/store/scope.go` `QueryScope`.
3. Refactor `internal/store/base/dialect.go` `BuildScopeClause` — drop tenant arm.
4. For each of 90 PG files: open, remove `tenant_id` from SELECT/INSERT/UPDATE/DELETE, remove `WHERE tenant_id = $N` (replace with `owner_user_id` where Q-decisions specify, otherwise drop), fix Go param signatures.
5. `go build ./...` clean (PG default build).
6. `go vet ./...` clean.
7. e2e test 03 (`agents_no_tenant`) green.
8. e2e test 11 (`no_master_tenant_in_store_layer`) — run `grep -rn MasterTenantID internal/store/pg/ internal/store/` returns 0.

### Sub-PR 05C — Mirror SQLite refactor + agent_sessions rename

1. For each of 88 SQLite store files: same drop-tenant_id treatment as PG.
2. Rename `internal/sessions/` package → `internal/agentsessions/`:
   - `git mv internal/sessions/ internal/agentsessions/`
   - Edit package declaration in 6 files (`package sessions` → `package agentsessions`)
   - Search-replace import paths in ~20 caller files (`grep -rln 'internal/sessions"' .`)
3. Rename Go store interfaces + types:
   - `SessionStore` → `AgentSessionStore`
   - `Session` type → `AgentSession`
4. Rename store files:
   - `internal/store/pg/sessions.go` → `agent_sessions.go`
   - `internal/store/pg/sessions_list.go` → `agent_sessions_list.go`
   - SQLite mirror
5. Update all SQL strings: `FROM sessions` → `FROM agent_sessions` (37 sites narrow grep / 55 broad — verified live).
6. `go build ./...` + `go build -tags sqliteonly ./...` clean.
7. e2e test 04 (`sessions_renamed`) green.

### Sub-PR 05D — EventBus + worker validators

1. Read `internal/eventbus/validate_agent_id.go` carefully (the canonical mirror pattern).
2. Write `internal/eventbus/validate_user_id.go` with same shape:
   - Function `validateUserID(event DomainEvent)`.
   - Skip if `event.UserID == ""`.
   - Parse `event.UserID` as UUID; on error → `slog.Warn("eventbus.non_uuid_user_id", ...)`.
   - Emit observability log only; never block publish.
3. Write companion test `validate_user_id_test.go` mirroring `validate_agent_id_test.go`.
4. Wire `validateUserID` into `bus_impl.go` Publish path next to `validateAgentID` call.
5. Locate episodic worker entry (verify file via grep `episodic.*UserID` once during impl). At entry: `if _, err := uuid.Parse(event.UserID); err != nil { slog.Warn(...); return }`.
6. e2e tests 05, 06, 07 green.
7. `go vet ./...` clean.

### Final phase verification

- `go build ./...` + `go build -tags sqliteonly ./...` + `go vet ./...` all clean.
- All 11 e2e store tests green.
- Phase 03+04 schema tests still green (regression-safe).

## Todo List

### PR-05A
- [x] 5 store interfaces created
- [x] 5 PG impl files created
- [x] 5 SQLite impl files created
- [x] Wired into factory.go (PG + SQLite)
- [x] Tests 01,02,08,09,10 green

### PR-05B-1a (L1 — Agents bug fix) — COMPLETED 2026-05-02
- [x] Test 03 written (TDD red): `tests/e2e/stores/03_agents_no_tenant_test.go` (3 cases)
- [x] AgentData.TenantID field deprecated (`db:"-"`, transitional in-memory only); OwnerUserID added (`*uuid.UUID`)
- [x] pg/agents.go refactored: drop tenant_id from `agentSelectCols`, INSERT, all WHERE clauses (Create, GetByKey, GetByID, Update, Delete, List, GetDefault); add `agentOwnerFilter` helper that bypasses on owner/root/admin role and otherwise scopes by owner_user_id
- [x] sqlitestore/agents.go mirrored
- [x] agent_shares: drop tenant_id from INSERT (ShareAgent) + WHERE (RevokeShare, ListShares, CanAccess, ListAccessible) — schema has no tenant_id column on agent_shares
- [x] go build ./... + go build -tags sqliteonly ./... + go vet clean
- [x] Test 03 green (3/3 cases) + 11/11 e2e store tests pass — no regression
- **Carry-over to L2:** AgentData.TenantID field still read by ~30 callsites (`internal/agent/resolver.go`, `internal/heartbeat/ticker.go`, `internal/http/agents*.go`, `internal/providerresolve/*`); they compile but always see `uuid.Nil`. L2 must remove field + readers in same PR.

### PR-05B-1b (L2 — TenantStore teardown) — COMPLETED 2026-05-02 (commit 1d5b4e26)
- [x] DELETE tenant_store interface + impls (PG + SQLite); compat shim retains MasterTenantID + role constants
- [x] DELETE tenant_config_store interface + impls
- [x] Refactor agent/resolver.go (drop TenantStore + BuiltinToolTenantCfgs + SkillTenantCfgs deps; drop resolveTenantSlug + tenant-scoped workspace paths + per-tenant tool overrides)
- [x] Refactor http/skills.go (drop tenantCfgStore + tenantStore + handleSet/DeleteTenantConfig)
- [x] Refactor http/builtin_tools.go (drop tenantCfgStore + tenantStore + handleGet/Set/DeleteTenantConfig + per-tenant override merging)
- [x] Refactor http/evolution_handlers.go (drop toolTenantCfgs + WithToolTenantCfgs + SuggestToolOrder disable arm)
- [x] Refactor http/voices.go + voices_test.go (drop tenantStore param)
- [x] Refactor http/channel_instances.go (drop tenantStore + dead route registrations for merged contacts + tenant_users)
- [x] Refactor http/mcp_user_credentials.go (drop tenantStore; admin-role-only target user resolution)
- [x] Refactor http/user_search.go (drop tenant_users branch; contacts-only)
- [x] Refactor http/auth.go (drop pkgTenantCache + InitTenantStore + resolveScopedTenant + resolveTenantHint; authResult.TenantID always MasterTenantID)
- [x] Refactor http/tenant_auth_helpers.go (replace requireTenantAdmin with requireUserAdmin via RoleFromContext; keep requireMasterScope)
- [x] Refactor gateway/router.go (drop SetTenantStore + applyTenantScope + resolveTenantHint + getUserTenantRole + tenant enrichment in connect response)
- [x] Refactor gateway/methods/skills.go + test (drop tenantCfgStore + tenant-override merging)
- [x] DELETE gateway/methods/tenants.go
- [x] DELETE internal/http/{tenants.go, tenant_cache.go, tenant_backup_handler.go, tenant_restore_handler.go, tenant_backup_auth_helpers*.go, contact_merge_handlers.go, builtin_tools_tenant_settings_test.go}
- [x] DELETE internal/backup/tenant_*.go (8 files)
- [x] DELETE cmd/{tenant_backup,tenant_restore}*.go + cmd/gateway_system_config_sync.go (recreated as lean seedConfigForContext)
- [x] DELETE tests/integration/v3_tenant_configs_test.go
- [x] Update test mocks (auth_test.go: drop mockTenantStore + tenant-scoping tests; gateway/methods/skills_test.go: drop nil tenantCfg arg)
- [x] factories drop Tenants + BuiltinToolTenantCfgs + SkillTenantCfgs fields
- [x] sqlx_scan_structs drop tenantRow + tenantUserRow
- [x] cmd wiring: gateway.go drop NewTenantsHandler/NewTenantsMethods/SetTenantStore/InitTenantStore/NewTenantBackupHandler; gateway_managed.go drop ResolverDeps.TenantStore + tenant cfg fields; gateway_methods.go signature drop tenantStore + skillTenantCfgStore params; gateway_setup.go seedSystemConfigs drop ts param; gateway_http_handlers.go fix all NewXxxHandler signatures; gateway_http_wiring.go drop voicesH tenantStore + WithToolTenantCfgs
- [x] Build verification: go build (PG) clean, go build -tags sqliteonly clean, go vet clean. Unit tests green. e2e store tests 11/11 green.
- **DEFERRED to L3:** AgentData.TenantID field + 30+ readers (heartbeat/ticker, http/agents*, providerresolve/agent_provider, agent/agents_create struct literal, gateway/methods/agents_import_agent struct literal). Field is db:"-" transitional; readers compile cleanly with always-uuid.Nil → no-op tenant filter. Drop with broader MasterTenantID purge.
- [ ] **Carry-over from L1 review (`code-review-260502-2037-pr-05b-1a-agents-store.md`):**
  - [ ] H1: `pg/agents.go` + `sqlitestore/agents.go` Update/Delete check `RowsAffected()` → return `store.ErrNotFound` instead of silent no-op for non-admin foreign-row writes
  - [ ] H2: tighten `store.ValidateUserID` to `uuid.Parse` (currently length/control-chars only); `agent_shares.user_id` is UUID column → non-UUID strings fail INSERT silently today
  - [ ] H3: add owner-check guards in `pg/agents.go` `RevokeShare`/`ListShares`/`CanAccess` — currently any caller knowing IDs can revoke/list. Confirm split with HTTP layer auth before adding (see Q-L2-1).
  - [ ] M1: lift `execMapUpdateWhereOwner` (PG + SQLite) into `store/base/query_builder.go` as `BuildMapUpdateWhereOwner` — mirror existing `BuildMapUpdateWhereTenant`. Apply when ≥3 stores need it (agents + future user-scoped tables).
  - [ ] M2: replace `agentOwnerFilter(ctx, 0)` validation-only call with explicit `requireAgentOwnerScope(ctx)` (no magic placeholder index)
  - [ ] M3: cache `agentOwnerFilter` result in `Update()` (currently called twice — unset_default + actual update)
  - [ ] M5: extend Test 03 with foreign-user `Update`/`Delete`, `GetDefault` scoping, agent_shares roundtrip, non-UUID UserID, orphan agents
  - [ ] PG/SQLite parity drift: SQLite admin `Update` doesn't filter `deleted_at IS NULL` while PG does (pre-existing; pick a side)
- [ ] **Q-L2-1:** Confirm authorization split — does HTTP/WS gateway layer already enforce "caller is agent owner or admin" before calling `RevokeShare`/`ListShares`/`CanAccess`, or should store layer enforce? Decide before H3 fix.

### PR-05B-2/3 (L3 — Context purge + scope refactor)
- [ ] scope.go QueryScope.TenantID dropped
- [ ] BuildScopeClause refactored
- [ ] 35 PG store files refactored (drop tenant_id from SQL + helpers)
- [ ] DELETE TenantIDKey/WithTenantID/TenantIDFromContext
- [ ] DELETE IsCrossTenant; replace with IsAdminRole(ctx)
- [ ] IsMasterScope → IsOwnerRole(ctx) || IsAdminRole(ctx)
- [ ] DELETE MasterTenantID (205 refs)
- [ ] Test 03 + 11 green
- [ ] **Carry-over from L2 (commit 1d5b4e26):**
  - [ ] DELETE `AgentData.TenantID` field (transitional db:"-" since L1) and the compat shim in `internal/store/tenant_store.go` (MasterTenantID + TenantRole*/TenantStatus* constants)
  - [ ] AgentData.TenantID readers (compile cleanly today as no-op tenant filter via uuid.Nil):
    - `internal/heartbeat/ticker.go:174-175` — drop `WithTenantID(ctx, ag.TenantID)` and `if ag.TenantID != uuid.Nil` guard
    - `internal/heartbeat/ticker.go:256` — `providerReg.GetForTenant(ag.TenantID, ...)` → drop tenant arg or rename to `GetByName`
    - `internal/providerresolve/agent_provider.go:17,35` — `registry.GetForTenant(agent.TenantID, ...)` (2 sites)
    - `internal/http/agents.go:243,244,247,309,384,610` — `req.TenantID` (AgentRequest field, separate from AgentData but tied to tenant context)
    - `internal/http/agents_codex_pool.go:123,134,167,223` — `agent.TenantID` (4 sites; `lookupProviderByNameWithMasterFallback`, `GetForTenant`, `registeredCodexPoolProviders`, `ListCodexPoolSpans`)
    - Struct literals: `internal/agent/agents_create.go` + `internal/gateway/methods/agents_import_agent.go` set `AgentData{TenantID:...}` — drop fields
  - [ ] `providers.Registry.GetForTenant(tenantID, name)` → rename `GetByName(name)` since v4 has no per-tenant providers (1131 PG callsites likely affected)
  - [ ] `internal/store/tracing/...` ListCodexPoolSpans + similar — drop tenantID param from signature
  - [ ] `cmd/gateway_managed.go` ResolverDeps construction: dropped TenantStore/BuiltinToolTenantCfgs/SkillTenantCfgs in L2 — verify no field re-introduced when readers cleanup lands
  - [ ] After all readers gone, drop the compat shim file entirely; `MasterTenantID`/`TenantRole*`/`TenantStatus*` constants vanish; downstream callers in cmd/, internal/oauth, internal/config/tenant_paths, internal/upgrade, internal/tools, internal/vault, internal/consolidation, internal/memory, internal/hooks, internal/tasks, internal/tracing, internal/workspace, internal/providers all need refactor (190 non-test refs scouted 2026-05-02)
  - [ ] DELETE `internal/config/tenant_paths.go` (TenantWorkspace, TenantDataDir, TenantSkillsStoreDir helpers — single-tenant collapse)
  - [ ] Audit `*.TenantID` field reads beyond AgentData: tracing collector, tasks ticker, tools subagent_tracing/delegate/web_search, memory auto_injector, providers claude_cli_mcp, workspace resolver_impl, agent loop_*, oauth/token, upgrade hook_web_search_migrate, hooks config/delegate_bridge/script*, vault enrich/rescan/search, consolidation episodic/semantic/dreaming, gateway event_filter, gateway/methods chat/hooks/api_keys/agents_create — 367 total non-test reads (scouted 2026-05-02)
  - [ ] Verify `cmd/gateway_methods.go` `seedConfigForContext` callsite still passes ctx; in L2 we pass `context.Background()` for fresh ctx (msgBus payload). After context purge, simplify if MasterTenantID gating gone.

### PR-05C-1 — SQLite sessions hot-fix — COMPLETED 2026-05-02 (commit c2667521)
- [x] `internal/store/sqlitestore/sessions.go` + `sessions_list.go` + `sessions_ops.go`: SQL `sessions` → `agent_sessions`, drop tenant_id from INSERT/SELECT/WHERE/UPDATE/DELETE, simplify ON CONFLICT to (session_key) only, drop tenant prefix from cache key (session_key globally unique in v4)
- [x] Drop `IsCrossTenant` + `TenantIDFromContext` scoping in `List()`
- [x] `buildSessionFilter` ignores `SessionListOpts.TenantID` (struct field still exists for PG compat until L3)
- [x] 3 test files updated: `sessions_list_heuristic_test.go`, `sessions_list_metadata_tokens_test.go`, `sessions_display_tokens_integration_test.go` — INSERT INTO sessions → agent_sessions, drop tenant_id col, replace `WithTenantID(ctx, MasterTenantID)` with `context.Background()`
- [x] Build PG + sqliteonly + vet clean. e2e store tests still 13/13 green. 6 SQLite session tests green (was failing on `no such table: sessions`).

### PR-05C-2 / L3 (combined — IN PROGRESS, multi-session)

**Session 1 (2026-05-02 → 05-03 dawn) — store-layer SQL drop COMPLETE**
- [x] B0 — Foundation neutralize: BuildScopeClause/BuildScopeClauseAlias/QueryScope methods/BuildMapUpdateWhereTenant emit no-op for tenant_id (commit 71776b21)
- [x] B1 — SQLite agents siblings + cron family (commit 6281a27d, 9 files)
- [x] B2 — SQLite hooks+skills+secure-cli (commit 8f3a9cc2, 13 files)
- [x] B3 — SQLite kg+memory+episodic+tracing (commit 07a78d4b, 11 files, partial — most already clean)
- [x] B4 — SQLite teams+tasks+channels+activity (commit af3d55d7, 12 files)
- [x] B5 — SQLite mcp+providers+vault+configs+misc (commit ee9b5a66, ~17 files)
- [x] B5b — SQLite supplemental sweep deep cleanup (commit 9213cb23, 17 files)
- [x] B6 — PG agents siblings + cron family (commit ed3c35f8)
- [x] B7 — PG hooks+skills+secure_cli (commit 52b78fae)
- [x] B8 — PG kg+memory+episodic+tracing+evolution+snapshot (commit f24e3e7e)
- [x] B9 — PG kg+mcp+system+helpers+heartbeat (commit 571e3894)
- [x] B10 — PG teams+tasks+channels+activity (commit 8f2fc994)
- [x] B11 — PG vault+providers+configs+api_keys (commit cfe17395)
- [x] B12 — PG sessions trio: rename `sessions` → `agent_sessions`, drop tenant scoping (commit c7219f87, mirrors SQLite c2667521)
- [x] All builds clean: `go build ./...` + `go build -tags sqliteonly ./...` + vet (both tags)
- [x] All store tests green: sqlitestore + base + store packages
- **Final remaining tenant_id refs in store layer:** 6 occurrences (3 PG dead-code/comments: heartbeat.go param compat comment, helpers.go execMapUpdateWhereTenant unused fn, skills_embedding_test.go testing dead helper) + 2 SQLite comments. Acceptable; cleanup in next session.

**Session 2 (2026-05-03) — Final purge + sessions package rename COMPLETE**
- [x] C1 — Rename `Registry.GetForTenant` → `GetByName` (commit 2d14307f)
- [x] C2 — Drop `AgentData.TenantID` + struct TenantID fields + 30+ readers (commit c953876e)
- [x] C3 — Purge `TenantIDKey`/`WithTenantID`/`TenantIDFromContext`/`IsCrossTenant`/`MasterTenantID` foundation + refactor `IsMasterScope` → `IsOwnerRole(ctx) || IsAdminRole(ctx)`; DELETE `store/scope.go::QueryScope.TenantID` field (commit 261f2138)
- [x] C4 — DELETE `internal/config/tenant_paths.go` + `internal/store/base/tenant.go` + dead update helpers (commit 746818c8)
- [x] C5 — Rename `internal/sessions/` → `internal/agentsessions/` package (commit 9e080de8)
- [x] D1+D2 — Drop ~250 + ~195 stub call sites across cmd/, internal/, tests/ (commits 9bc4bfbc + d5b7155e)
- [x] E2 — DELETE stub functions themselves from `internal/store/context.go` + DELETE `internal/store/tenant_store.go` compat shim (commit 36e04618). `MasterTenantID` const moved to `context.go` as fixed UUID for event routing (no longer "tenant" semantics).
- [x] Integration test vet fixes (commit d3a298f5)
- [x] All builds clean: PG + sqliteonly + integration; `go vet` clean (3 tag combinations)
- [x] All store tests green
- **Final ref counts (non-pkg/browser):**
  - `TenantIDFromContext`: 0 ✓
  - `IsCrossTenant`: 0 ✓
  - `GetForTenant`: 0 ✓
  - `WithTenantID`: 5 (all `oauth.DBTokenSource.WithTenantID()` builder method, unrelated to deleted store stub)
  - `MasterTenantID`: 161 (intentionally kept as fixed UUID const in `internal/store/context.go` for event routing — `client.tenantID` requires concrete UUID)
  - Store layer `tenant_id`: 6 (dead comments in 2 files: `sessions_list.go`, `schema.sql`)
- **Structural deletes confirmed:**
  - `internal/sessions/` → `internal/agentsessions/` ✓
  - `internal/config/tenant_paths.go` deleted ✓
  - `internal/store/tenant_store.go` deleted ✓
  - `internal/store/base/tenant.go` deleted ✓
  - `internal/store/pg/skills_embedding_test.go` deleted (dead test of removed helper) ✓
  - `tests/integration/tenant_restore_replace_test.go` deleted (referenced removed `backup.TenantTables`) ✓
- [ ] DELETE `store/base/tenant.go` (RequireTenantID/TenantIDForInsert helpers — dead)
- [ ] DELETE PG `helpers.go::execMapUpdateWhereTenant` + `skills_embedding_test.go` dead test
- [ ] DELETE `AgentData.TenantID` field + 30+ readers (heartbeat/ticker, providerresolve, http/agents*, struct literals)
- [ ] DELETE TenantID field from store interface row structs: `provider_store.go`, `vault_store.go`, `channel_instance_store.go`, `subagent_store.go`, `team_store.go`, `episodic_store.go`, `cron_store.go`, `evolution_store.go`, `api_key_store.go`, `tracing_store.go`
- [ ] `providers.Registry.GetForTenant` → rename `GetByName`
- [ ] DELETE `internal/config/tenant_paths.go`
- [ ] Rename `internal/sessions/` → `internal/agentsessions/` package + `SessionStore` → `AgentSessionStore` type + ~12 importer updates
- [ ] DELETE compat shim `internal/store/tenant_store.go` (MasterTenantID + TenantRole*/TenantStatus* constants) once all readers gone
- [ ] Test 04 (`tests/e2e/stores/04_sessions_renamed_test.go`) green
- [ ] All 11 e2e store tests + Phase 03/04 schema tests still green

### PR-05D — EventBus + worker validators — COMPLETED 2026-05-02
- [x] `internal/eventbus/validate_user_id.go` + `validate_user_id_test.go` (mirror agent_id pattern; logs `eventbus.non_uuid_user_id` with distinct field name to avoid observability collision)
- [x] `internal/eventbus/bus_impl.go` Publish wired: `validateUserID(event)` next to `validateAgentID(event)`
- [x] `internal/consolidation/episodic_worker.go` parses `event.UserID` as UUID at entry (after agent_id parse, before any store call); returns `fmt.Errorf("episodic: invalid user_id %q: %w", ...)` on failure
- [x] Updated existing episodic worker unit tests in `workers_test.go` (TestEpisodicWorkerHandle_WithSummary/DuplicateSourceID) to use real UUID for UserID — non-UUID guards already covered the rest
- [x] Test 05 (`tests/e2e/stores/05_kg_user_id_uuid_test.go`): 4 cases — NULL succeeds, valid UUID succeeds, malformed string fails (PG type check), random UUID FK miss fails
- [x] Test 06 (`tests/e2e/stores/06_eventbus_validate_user_id_test.go`): 3 cases via public `eventbus.NewDomainEventBus()` — non_uuid_warns, valid_uuid_no_warn, empty_no_warn; verifies distinct field name + non-blocking dispatch
- [x] Test 07: lives in `internal/consolidation/workers_test.go` as `TestEpisodicWorkerHandle_NonUUIDUserID` (mirrors existing NonUUIDAgentID/TenantID pattern, uses package-internal `mockEpisodicStore`); cross-package e2e harness would have required ~14 method stubs of `EpisodicStore` for the same code path coverage — YAGNI
- [x] Build verification: `go build ./...` clean, `go build -tags sqliteonly ./...` clean, `go vet ./...` clean
- [x] All 13 e2e store tests pass (PR-05A + PR-05B-1a + PR-05D additions)
- [x] Consolidation + eventbus unit tests green

### Phase exit
- [ ] go build (PG + sqliteonly) + go vet clean
- [ ] All 11 store tests green
- [ ] Phase 03+04 schema tests still green

## Success Criteria

- `grep -rn "tenant_id" internal/store/{pg,sqlitestore}/ | wc -l` returns 0 (was 1131).
- `grep -rn "TenantIDFromContext" internal/store/pg/ | wc -l` returns 0 (was 153).
- All 11 e2e store tests green.
- 5 new stores wired + functional.
- `internal/sessions/` → `internal/agentsessions/` complete; no compile error from stale imports.
- EventBus warns on non-UUID UserID; episodic worker rejects malformed UserID.

## Risk Assessment

| Risk | Severity | Mitigation |
|---|---|---|
| 22-day phase too big — context overload | High | Hard split into 4 sub-PRs above; merge each independently |
| Missed tenant_id ref breaks runtime queries | High | Phase 11 tenant_id check (test 11) + post-PR grep gate |
| Sessions rename breaks 20+ caller files | Med | Use `gopls rename` or Goland refactor for safety; grep audit pre-merge |
| SQLite-only build breaks (sqliteonly tag missed) | High | Run `go build -tags sqliteonly ./...` per sub-PR |
| EventBus regression — event consumers see UUID-typed UserID for first time | Med | validate_user_id.go is observation-only (no block); regression-safe |
| Episodic worker hard-fails on legacy non-UUID UserID | Med | Skip event + log + metric (not panic); document in comment |

## Security Considerations

- `users.password_hash` written + read as opaque text; Phase 06 enforces Argon2id format on write.
- `user_sessions.refresh_token_hash` indexed UNIQUE; sha256 of opaque token (Phase 06).
- All FKs CASCADE on user delete (intentional per Q-7 cleanup model).
- SQL params remain `$1, $2` (PG) / `?` (SQLite) — no string concat (CLAUDE.md SQL safety rule).

## Cross-phase Gates

- **Entry:** Phase 03 + Phase 04 merged + green.
- **Exit:** All 4 sub-PRs merged. 11 e2e store tests + earlier schema tests green. Gates Phase 06 (auth needs users + user_sessions stores).

## Next Steps

- Phase 06 — auth + bootstrap layered on top of users/user_sessions stores.
- Phase 07 — pool/cache refactor (parallel to 08); references the new agentsessions package + episodic worker.
- Phase 09 — channels merge fix uses agent_sessions store (R1).
