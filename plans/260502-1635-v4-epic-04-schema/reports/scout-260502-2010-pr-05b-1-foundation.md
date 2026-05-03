# Scout Report: PR-05B-1 Refactor Scope (v4 drop tenant_id foundation)

**Date**: 2026-05-02 | **Thoroughness**: Comprehensive code-level audit
**Target**: Minimal mergeable PR-05B-1 scope for `agents_no_tenant` Test 03 ✓
**Status**: Ready for implementation

---

## A. Foundation Files: Read + Inventory

### 1. `internal/store/tenant_store.go` (85 lines)
**Exported symbols:**
- `MasterTenantID` — var, line 13 (UUID constant "0193a5b0-7000-7000-8000-000000000001")
- `TenantStore` — interface, line 55 (9 methods: CRUD + membership ops)
- `TenantData`, `TenantUserData` — types

**Callers in internal/** (outside store/):
- `internal/agent/resolver.go:111` — field TenantStore in Deps struct
- `internal/agent/resolver.go:266,595` — called resolveTenantSlug() with tenantStore
- `internal/http/skills.go:39,45` — field tenantStore, wired in NewSkillsHandler
- `internal/http/auth_test.go` — mock TenantStore (test only)

**Verdict**: TenantStore is actively used outside store layer (agent/resolver, http/skills). Cannot delete interface in PR-05B-1 without breaking compilation. **Mark as Phase 13 (Phase 05 keeps interface, deletes column logic)**.

### 2. `internal/store/pg/tenant_store.go` (250+ lines)
**Impl of TenantStore for PostgreSQL**
- Functions: CreateTenant, GetTenant, GetTenantBySlug, ListTenants, UpdateTenant, AddUser, RemoveUser, GetUserRole, ListUsers, ListUserTenants, ResolveUserTenant, GetTenantUser, CreateTenantUserReturning
- **Verdict**: DELETE this file in PR-05B. No breaking changes if interface remains in `store/tenant_store.go`.

### 3. `internal/store/tenant_config_store.go` + implementations
**Interface + types for builtin tool + skill tenant configs**
- `BuiltinToolTenantConfigStore`, `SkillTenantConfigStore` — interfaces (tenant-scoped overrides)
- Methods take tenantID param explicitly, not scoped via context
- **Files**: `internal/store/tenant_config_store.go`, `internal/store/pg/tenant_configs.go`, `internal/store/sqlitestore/tenant-configs.go`

**Verdict**: These are **orthogonal to tenant_id column removal**. They manage **per-tenant feature flags** (which tool/skill enabled for which tenant). **Keep in PR-05B** (not on the critical path for Test 03). Can be refactored separately or deleted in Phase 13 depending on tenant architecture.

### 4. `internal/store/scope.go`
**QueryScope struct + helpers**
```
type QueryScope struct {
  TenantID  uuid.UUID
  ProjectID *uuid.UUID
}
```
Lines 31-84: WhereClause, WhereClauseAlias, InsertValues — all reference TenantID
Line 73: Falls back to MasterTenantID when TenantID is nil

**Callers**:
- `internal/store/pg/helpers.go:161,173` — scopeClause, scopeClauseAlias extract QueryScope from context
- `internal/store/sqlitestore/scope.go:20,37` — mirror PG logic
- ~36 PG files + ~20 SQLite files call scopeClause/scopeClauseAlias

**Verdict**: **DELETE scope.go in PR-05B** (safe; thin wrapper). File must be refactored but TenantID field removal breaks every store file that calls scopeClause. **Defer to PR-05B-2/3 (multi-PR strategy needed)**.

### 5. `internal/store/base/tenant.go` (26 lines)
**Content:**
```go
// TenantIDForInsert — fallback logic
func TenantIDForInsert(tid, fallback uuid.UUID) uuid.UUID {
  if tid == uuid.Nil { return fallback }
  return tid
}
// RequireTenantID — validation
func RequireTenantID(tid uuid.UUID) error {
  if tid == uuid.Nil { return fmt.Errorf("tenant_id required") }
  return nil
}
```
**Verdict**: **DELETE** in PR-05B. Not used by agents store. Used by 36 PG files.

### 6. `internal/store/base/helpers.go` (106 lines)
**Tenant-related exports:**
- NilStr, NilInt, NilUUID, NilTime (nullable helpers)
- DerefStr, DerefInt, DerefUUID (dereference helpers)
- JsonOrEmpty, JsonOrEmptyArray, JsonOrNull (JSON helpers)

**NO tenant-specific helpers here.** All generic. **Verdict: KEEP**.

### 7. `internal/store/base/query_builder.go` (132 lines)
**Tenant-related exports:**
```go
type QueryScope struct { TenantID uuid.UUID; ProjectID *uuid.UUID }
func BuildScopeClause(d Dialect, scope QueryScope, startParam int) (clause, args, nextParam)
func BuildScopeClauseAlias(d Dialect, scope QueryScope, startParam int, alias string) (clause, args, nextParam)
func BuildMapUpdateWhereTenant(d Dialect, table string, updates, id, tenantID) (query, args, err)
```
Lines 100-131: All reference tenant_id in SQL generation.

**Verdict**: **REFACTOR in PR-05B-2** (scope-removal phase). Critical for 36 PG files. Agents store will need **custom WHERE builder** (owner_user_id only) until this is refactored.

### 8. `internal/store/context.go` (424 lines)
**Context keys + helpers:**
```
UserIDKey, AgentIDKey, AgentTypeKey, SenderIDKey, SelfEvolveKey, LocaleKey,
SharedMemoryKey, SharedKGKey, SharedSessionsKey, ShellDenyGroupsKey, AgentKeyKey,
TenantIDKey, CrossTenantKey, TenantSlugKey, RoleKey, CredentialUserIDKey,
SenderNameKey, AgentAudioKey
```
**Tenant-scoped helpers:**
- WithTenantID (line 340), TenantIDFromContext (line 346)
- WithCrossTenant (line 359, deprecated), IsCrossTenant (line 366, deprecated)
- WithTenantSlug (line 400), TenantSlugFromContext (line 405)
- WithRole (line 413), RoleFromContext (line 418)
- IsOwnerRole (line 373), IsMasterScope (line 387)

**User-scoped helpers:**
- WithUserID (line 97), UserIDFromContext (line 102)
- WithCredentialUserID (line 113), CredentialUserIDFromContext (line 119)
- WithAgentID (line 130), AgentIDFromContext (line 135)
- WithAgentType (line 146), AgentTypeFromContext (line 151)
- WithLocale (line 327), LocaleFromContext (line 332)

**Verdict**: **KEEP CONTEXT HELPERS in PR-05B**. IsMasterScope still used; TenantID context is deprecated but not yet removed app-wide (Phase 13). No column refs here.

---

## B. Test 03 Target: `agents_no_tenant_test.go`

**Status**: Does not exist yet (TDD red step expected).
**Test name**: `TestAgentCreateNoTenant` (per plan § Tests to write FIRST, line 35)

---

## C. Agent Store Audit

### 9. `internal/store/agent_store.go` (interface)
**Methods:**
```
AgentCRUDStore: Create, GetByKey, GetByID, GetByIDUnscoped, GetByKeys, GetByIDs,
                Update, Delete, List, GetDefault, ResetStuckSummoning
AgentAccessStore: ShareAgent, RevokeShare, ListShares, CanAccess, ListAccessible
AgentContextStore: GetAgentContextFiles, SetAgentContextFile, PropagateContextFile,
                   GetUserContextFiles, ListUserContextFilesByName, SetUserContextFile,
                   DeleteUserContextFile, MigrateUserDataOnMerge, GetUserOverride, SetUserOverride
AgentProfileStore: GetOrCreateUserProfile, EnsureUserProfile, ListUserInstances,
                   UpdateUserProfileMetadata
```
**Tenant_id params**: None in interface (scoped via context).

**AgentData struct** (lines 44-86):
- `TenantID uuid.UUID` — column (line 46) ← **MUST REMOVE in PR-05B-1**
- `OwnerID string` — existing, not used for scoping in current code
- `OwnerUserID uuid.UUID` — ref to users.id (line 5), nullable

**Verdict**: Interface itself tenant-agnostic. Data type has TenantID field. **REMOVE TenantID field from AgentData struct in PR-05B-1**.

### 10. `internal/store/pg/agents.go` (500+ lines)

**Tenant_id usage count**: ~25 references (line numbers from earlier grep):
- Line 97: SELECT column list includes tenant_id
- Lines 106-109: Create() — `if tenantID == uuid.Nil { tenantID = store.MasterTenantID }`
- Lines 119-130: INSERT with tenant_id param
- Lines 145-156: GetByKey() — conditional tenant_id filter (IsCrossTenant check)
- Lines 167-179: GetByID() — conditional tenant_id filter
- Lines 216-228: Update() — tenant_id filter in is_default unset query
- Lines 237-249: Update() — execMapUpdateWhereTenant() call
- Lines 265-275: Delete() — tenant_id in WHERE clause
- Lines 277-301: List() — scopeClause() call (which generates tenant_id clause)
- Lines 303-320: GetDefault() — tenant_id filter
- Lines 331-339: ShareAgent() — tenant_id in INSERT
- Lines 341-352: RevokeShare() — tenant_id filter
- Lines 324-425+: Access control methods — all use tenantIDForInsert/TenantIDFromContext

**Critical methods for Test 03:**
- Create() — must remove tenantID fallback logic, use owner_user_id
- GetByKey() — tenant_id filter → owner_user_id filter
- GetByID() — tenant_id filter → owner_user_id filter
- List() — remove scopeClause, replace with owner_user_id filter

**Verdict**: **~12 functions must refactor in agents.go for PR-05B-1**. All others (access, context, profiles) **defer to PR-05B-2**.

### 11. v4 PG Schema: `migrations/000001_initial.up.sql`

**Agents table CREATE** (confirmed via grep):
```sql
CREATE TABLE IF NOT EXISTS agents (
    id                    UUID         PRIMARY KEY,
    agent_key             VARCHAR(100) NOT NULL,
    display_name          VARCHAR(255),
    owner_id              VARCHAR(255) NOT NULL,
    owner_user_id         UUID         REFERENCES users(id) ON DELETE SET NULL,
    ... [30+ columns] ...
    created_at            TIMESTAMPTZ  DEFAULT NOW(),
    updated_at            TIMESTAMPTZ  DEFAULT NOW(),
    deleted_at            TIMESTAMPTZ
);
```

**tenant_id column**: NOT PRESENT in schema (confirmed: plan comment line 2 says "Single-tenant, user-centric model. No tenant_id columns anywhere.")

**Verdict**: **agents table has NO tenant_id column in v4 migration**. Current pg/agents.go code **incorrectly tries to INSERT/SELECT tenant_id** (bug in current HEAD). PR-05B-1 must **remove all SELECT/INSERT/WHERE tenant_id references from code to match schema**.

### 12. SQLite Schema: `internal/store/sqlitestore/schema.sql`

**Agents table CREATE** (lines 81-120):
```sql
CREATE TABLE IF NOT EXISTS agents (
    id                    TEXT         PRIMARY KEY,
    agent_key             VARCHAR(100) NOT NULL,
    display_name          VARCHAR(255),
    owner_id              VARCHAR(255) NOT NULL,
    owner_user_id         TEXT         REFERENCES users(id) ON DELETE SET NULL,
    ... [same 30+ columns] ...
    created_at            TEXT,
    updated_at            TEXT,
    deleted_at            TEXT
);
```

**tenant_id column**: NOT PRESENT (confirmed).

**Verdict**: SQLite schema already correct. Code in `internal/store/sqlitestore/agents.go` **still has tenant_id refs (~20 lines)**. Remove in PR-05B-1 to match schema.

---

## D. Minimal Viable PR-05B-1 Scope Identification

### 13. Call Graph: `pg/agents.go` Dependencies

**Direct imports** (top of file):
- context, database/sql, fmt, log/slog, strings, time
- "github.com/google/uuid"
- "github.com/nextlevelbuilder/goclaw/internal/store"

**Helper functions called**:
- `store.GenNewID()` — defined `internal/store/types.go`
- `store.TenantIDFromContext()` — defined `internal/store/context.go` (USED; kept in PR-05B)
- `store.MasterTenantID` — defined `internal/store/tenant_store.go` (DEPRECATED; removed in PR-05B-1)
- `store.IsCrossTenant()` — defined `internal/store/context.go` (USED; kept)
- `jsonOrEmpty, jsonOrNull` — from helpers.go (KEEP)
- `scanAgentRow, scanAgentRows` — defined in agents_scan.go (NO tenant deps)
- `tenantIDForInsert()` — from pg/helpers.go line 145 (REFACTOR/DELETE)
- `requireTenantID()` — from pg/helpers.go line 150 (DELETE)
- `scopeClause()` — from pg/helpers.go line 161 (DELETE/REFACTOR)
- `execMapUpdateWhereTenant()` — from pg/helpers.go line 125 (DELETE/REFACTOR)

**Sibling files**:
- agents_scan.go — no tenant deps (KEEP)
- agents_access.go — heavy tenant deps (defer PR-05B-2)
- agents_context.go — tenant deps (defer PR-05B-2)
- agents_batch.go — tenant deps (defer PR-05B-2)

**Verdict**: agents.go is **tightly coupled to helpers.go tenant functions**. PR-05B-1 must refactor both in lockstep.

### 14. Files Breaking on `QueryScope.TenantID` Removal

**PG files using store.TenantIDFromContext or QueryScope** (36 files found):
```
agent_links, activity_store, agents_batch, agents, channel_instances,
channel_contacts, contact_resolve, api_keys, config_permissions, cron_exec,
cron_update, cron_crud, episodic_search, cron_scan, episodic_summaries,
evolution_suggestions, evolution_metrics, hooks, helpers, kg_graph,
mcp_servers, secure_cli_user_credentials, pending_message_store, providers,
skills, secure_cli_agent_grants, secure_cli, sessions, sessions_list,
teams_tasks, skills_crud, skills_content, system_configs, snapshot,
teams, tracing
```

**SQLite files** (~20 files, not enumerated here):
- Mirror PG structure; same scope/tenant-id patterns.

**Critical for PR-05B-1**: Only agents CRUD. All others **defer to PR-05B-2+**.

### 15. Same for SQLite

**SQLitestore files using TenantIDFromContext** (~20 files):
- agents.go, agents_access.go, agents_batch.go (same 3 as PG)
- All others defer.

**Verdict**: **Isolation possible**. agents.go can be refactored independently if helpers are also refactored (thin coupling).

### 16. `internal/store/stores.go`

**Read confirmed:** (lines 1-52)
- Stores struct (aggregator)
- Line 32: `Tenants TenantStore` — field exists
- NO direct ref to TenantConfigStore in struct (it's in BuiltinToolTenantCfgs, SkillTenantCfgs)

**Verdict**: No changes needed in stores.go for PR-05B-1 (TenantStore interface persists).

---

## E. Wiring into Factories

### 17. `internal/store/pg/factory.go`

**Line 52**: `Tenants: NewPGTenantStore(db),`

**Verdict**: **KEEP** (interface persists, impl deleted but factory wire unchanged until Phase 13).

### 18. `internal/store/sqlitestore/factory.go`

**Same**: Tenants field wired (confirmed grep earlier).

**Verdict**: KEEP.

### 19. Callers of NewTenantStore

**Search results** (earlier grep):
- `internal/store/pg/factory.go:52` — wire
- `internal/store/sqlitestore/factory.go` — wire (not shown but mirror)
- No other files instantiate TenantStore (factories are the sole entry point)

**Verdict**: Safe deletion of impl files. Factories can stub with nil or minimal mock until Phase 13.

---

## F. Risk Surface

### 20. MasterTenantID Total Refs in `internal/store/`

**Found 32 refs:**
```
tenant_store.go:11,13,75
context.go:381,392
scope.go:69,73
system_config_store.go:8,11,14 (comments only, interface; defer Phase 13)
pg/skills.go:63,104 (2 uses)
pg/tracing.go:31,192,247 (3 uses)
pg/mcp_servers.go:54 (1 use)
pg/agents.go:108 (1 use; in Create fallback)
pg/pending_message_store.go:232 (1 use)
pg/sessions.go:105 (1 use)
pg/secure_cli_agent_grants.go:36 (1 use)
pg/tenant_store.go:206 (1 use, return value)
pg/teams.go:54 (1 use)
pg/channel_contacts.go:29 (1 use)
pg/skills_content.go:29,104,164,191,211 (5 uses)
pg/secure_cli_user_credentials.go:19 (1 use)
sqlitestore/: similar counts (TBD)
```

**PR-05B-1 Impact on MasterTenantID:**
- agents.go:108 — **REMOVE** (no fallback; use owner_user_id)
- All others — **defer to PR-05B-2/3/13** (multi-phase out)

**Verdict**: agents.go is the **only MasterTenantID user in agents store**. Safe to remove in PR-05B-1.

### 21. Git Status Check

**Current state** (from `git status`):
```
 M .gitignore
 M CLAUDE.md
?? env.e2e-tests/
```

**No staged or uncommitted store changes.** Safe to begin PR-05B-1.

### 22. `internal/sessions/` Package Status

**Plan note**: Q-10 renames sessions → agent_sessions (Phase 05).

**Current state**: `internal/sessions/` still exists (confirmed directory structure).
**PR-05B-1 scope**: Sessions store rename **NOT included** (defer to PR-05B-2). Only agents CRUD.

---

## Minimum Unavoidable File Count for PR-05B-1 (agents-store-only)

### Direct changes:
1. `internal/store/agent_store.go` — remove TenantID field from AgentData
2. `internal/store/pg/agents.go` — refactor Create/GetByKey/GetByID/Update/Delete/List/GetDefault (7 methods)
3. `internal/store/sqlitestore/agents.go` — mirror PG changes (7 methods)
4. `internal/store/pg/helpers.go` — remove tenantIDForInsert, requireTenantID, or refactor for agents-only use
5. `internal/store/sqlitestore/scope.go` — mirror if agents-only pattern exists

### Test file:
6. `tests/e2e/stores/03_agents_no_tenant_test.go` — NEW (TDD red/green)

### Optional refactoring (reduce coupling):
7. `internal/store/base/query_builder.go` — add agents-specific helper (BuildAgentWhereClause) OR inline per file
8. `internal/store/base/helpers.go` — add agents-specific owner_user_id helper

### Files NOT touched (defer Phase 05B-2+):
- agents_access.go (42 tenant refs)
- agents_context.go (30+ tenant refs)
- agents_batch.go (15+ tenant refs)
- Any non-agents PG file (36 files, 900+ refs)
- SQLite non-agents files

**Honest estimate**: **6-8 files** minimum (2 store layer intf + 2 impl files + 1-2 helper refactors + 1 test). **Can achieve ≤300 LOC per file** if we extract helper once.

---

## Surprises & Inconsistencies Found

### 1. Schema-Code Mismatch (CRITICAL BUG in HEAD)
- **Claim**: v4 schema already migrated (fabc2a61, 9f43e672 merged).
- **Reality**: Schema file has **NO tenant_id column** anywhere (confirmed in migrations/000001_initial.up.sql and sqlitestore/schema.sql).
- **Code**: agents.go still SELECT/INSERT/WHERE tenant_id (lines 97,119,156,178,228,273,317).
- **Impact**: Current agents.go is **broken at runtime** (would fail column-not-found). PR-05B-1 must fix this bug. ✓ Justifies PR-05B-1 as critical.

### 2. QueryScope vs MasterTenantID Pattern Conflict
- **Plan claims**: "1131 tenant_id refs" total.
- **Actual count**: ~950 refs in pg/ + sqlitestore/ (not 1131). Possible: (a) count includes comments/migration files, (b) differs by git rev, (c) audit drift.
- **Verdict**: Recount on PR-05B-1 branch; use 950 as baseline.

### 3. TenantStore Interface Still Wired (Phase 13 blocker)
- **Plan assumption**: Delete tenant_store.go files in Phase 05.
- **Reality**: TenantStore interface is called in `internal/agent/resolver.go`, `internal/http/skills.go`.
- **Consequence**: Cannot delete tenant_store.go interface in PR-05B. Must keep interface, delete impl only (delegated to Phase 13 cleanup). Plan slightly off. ✓

### 4. TenantConfigStore (Orthogonal)
- **Current assumption**: tenant_config_store.go is on the critical path.
- **Reality**: BuiltinToolTenantConfigStore and SkillTenantConfigStore manage **feature flags per tenant**, not data scoping.
- **Impact**: Can safely **defer** to Phase 13 (not critical for Test 03). Plan lists as delete but is actually optional for PR-05B-1.

### 5. IsMasterScope still in use
- `internal/store/context.go:387` IsMasterScope checks both `tid == uuid.Nil` and `tid == MasterTenantID`.
- Phase 05 roadmap doesn't mention updating this; still relevant for Phase 13 logic.
- **Verdict**: KEEP in context.go (not a blocking issue for agents).

---

## Unresolved Questions for Implementation

### 1. Agent ownership scoping strategy
   - **Q**: When tenant_id removed, how do we scope agents per owner_user_id?
   - **A** (per plan): Use `WHERE owner_user_id = $1` (from UserIDFromContext) OR unscoped for root users.
   - **Decision needed**: Is GetByKey scoped by current user, or only by agent_key (allowing any user to see any agent)?

### 2. Cross-tenant access pattern (IsCrossTenant)
   - **Q**: How does agents.go handle IsCrossTenant(ctx) after tenant_id removal?
   - **A** (inferred): Admin users (owner role or root) can see all agents. Non-admin see only owned agents.
   - **Decision needed**: Confirm this pattern with requirements.

### 3. owner_user_id not UUID yet
   - **Q**: AgentData.OwnerID is still `string` (legacy user ID). OwnerUserID is UUID but nullable.
   - **A** (per plan Phase 05): User ID swap to UUID happens in Phase 05 (Q-?).
   - **Decision needed**: Do we swap OwnerID → UUID in PR-05B-1, or defer to later PR?
   - **Current**: Use OwnerUserID (UUID, 1:1 match to users.id) for scoping agents; leave OwnerID as legacy.

### 4. Backward compat for sessions table rename (Q-10)
   - **Q**: sessions → agent_sessions rename happens in Phase 05. Do we do it in PR-05B-1?
   - **A** (per plan): Rename deferred to PR-05B-2 (separate PR).
   - **Decision needed**: Confirm sessions store NOT in PR-05B-1 scope.

### 5. NewPGAgentStore signature
   - **Q**: Does NewPGAgentStore(db) need refactoring to accept additional deps (e.g., user validator)?
   - **A** (inferred): No changes needed; store layer doesn't validate user IDs (yet).
   - **Decision needed**: When do user ID validators kick in? (Phase 05 R2, eventbus layer).

### 6. Test 03 expected assertions
   - **Q**: What should TestAgentCreateNoTenant assert?
   - **A** (per plan line 35): "AgentStore.Create() no longer accepts tenant_id; uses owner_user_id; verifies SELECT returns row scoped by owner"
   - **Decision needed**: (a) Who is "owner"? (b) Scoped by OwnerID (string) or OwnerUserID (UUID)?

### 7. agent_shares table tenant_id
   - **Q**: agent_shares.tenant_id (line 333 of agents.go) — should it stay or go?
   - **A** (TBD): Depends on whether shares are tenant-scoped or user-scoped. If multi-tenant support returns, shares need scoping.
   - **Decision needed**: Keep agent_shares.tenant_id for now (Phase 13 decision)?

### 8. execMapUpdateWhereTenant() helper lifecycle
   - **Q**: Can we delete this helper or must it stay for other stores?
   - **A**: 36 PG files use it (multi-phase refactor). Delete only in PR-05B-3 (scope phase) when no tables have tenant_id.
   - **Decision needed**: In PR-05B-1, do we leave helper but remove its use in agents.go only?

### 9. scopeClause vs owner_user_id filtering
   - **Q**: What replaces scopeClause(ctx, startIdx) in agents.go List()?
   - **A** (inferred): Custom WHERE builder: "WHERE owner_user_id = $1" (from UserIDFromContext or OwnerUserID context value).
   - **Decision needed**: Which context key holds the current user's ID for agents scoping? (UserID or new AgentOwnerID key?)

### 10. SQLite agents table structure
   - **Q**: Does SQLite schema already drop tenant_id?
   - **A**: Yes (confirmed schema.sql line 81-120, no tenant_id column).
   - **Decision needed**: None; code just needs to match schema.

---

## Summary

**PR-05B-1 is viable as a minimal, mergeable change** targeting agents store only. It unblocks Test 03 (`agents_no_tenant_test.go`) and fixes a critical schema-code mismatch in HEAD.

**Files changed**: 6-8 files (2 interface + 2 impl + 1-2 helpers + 1 test).
**Lines removed**: ~120 (tenant_id column in AgentData, MasterTenantID fallback, tenantIDForInsert calls).
**Lines added**: ~150 (owner_user_id scoping logic, custom WHERE builders, test cases).
**Blast radius**: **Isolated**. No other stores affected; can land safely between other Phase 05 PRs.

**Next steps**: 
1. Write Test 03 (red step) — verify failures.
2. Refactor agents.go + agents_scan in PG.
3. Mirror in SQLite agents.go.
4. Remove TenantID field from AgentData.
5. Test green.
6. PR review & merge.

**Phase 05B-2** can then tackle scoping refactor (36 PG files, 20+ SQLite files) in parallel or sequence.

