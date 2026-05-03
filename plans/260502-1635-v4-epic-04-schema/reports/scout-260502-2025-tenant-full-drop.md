# Scout Report: Tenant Full-Drop Scope Analysis (v4 Single-Tenant Refactor)

**Date**: 2026-05-02 | **Thoroughness**: Deep code-level audit  
**Target**: Comprehensive scope of removing tenant concept entirely (not just column drop)  
**User Decision Verified**: "drop tenant luôn" — complete tenant interface, context, and plumbing removal  
**Status**: Scope defined; PR-05B-1 strategy clarified

---

## A. TenantStore Callers — Load-Bearing Uses

### Finding: TenantStore is tightly coupled to resolver + HTTP layer

**Direct callers outside store layer:**

1. **`internal/agent/resolver.go:111`** — `TenantStore` field in `ResolverDeps` struct
   - Line 266: `resolveTenantSlug(deps.TenantStore, ag.TenantID)` call
   - Line 595-603: `resolveTenantSlug()` function body — looks up tenant slug for workspace path scoping
   - **Impact**: If v4 drops tenant concept, workspace path scoping strategy changes (no more tenant-per-path)
   - **Can be replaced?**: Yes, but requires design decision — v4 uses single user-scoped workspace or global?

2. **`internal/http/skills.go:39,45`** — `tenantStore` field + wired in `NewSkillsHandler`
   - Line 51-54: `tenantSkillsDir(r *http.Request)` uses `TenantSlugFromContext` for skill path scoping
   - Lines 585, 631: `requireTenantAdmin(w, r, h.tenantStore)` — permission guard
   - **Impact**: Drop means skill directory scoping changes (currently tenant-based, v4 = user-scoped or global)
   - **Can be replaced?**: Partially; `requireTenantAdmin` becomes `requireUserAdmin` (role-based)

3. **`internal/gateway/router.go:343-344, 372-375, 408, 424-432`** — Router enrichment
   - Looks up tenant by ID or slug for connect response enrichment
   - **Impact**: If tenant gone, router cannot resolve tenant name; enrich response differently
   - **Can be replaced?**: Yes; use agent metadata or skip enrichment

4. **`internal/gateway/methods/tenants.go:25,31,54,89,136`** — TenantsMethods handler
   - Full CRUD: CreateTenant, GetTenant, ListTenants, UpdateTenant
   - **Impact**: These methods become orphaned; v4 has no tenant CRUD routes
   - **Can be replaced?**: Delete entirely; or stub with 404

5. **Test mocks**: `internal/http/auth_test.go`, `internal/http/tenant_backup_auth_helpers_test.go`
   - Mock TenantStore for auth tests
   - **Impact**: Tests must remove tenant setup; use user-coped mocks instead

**Verdict on TenantStore interface deletion:**
- **Cannot delete in PR-05B-1** (breaks agent/resolver, http/skills, gateway/router immediately)
- **Delete-safe in Phase 13** (final tenant cleanup PR)
- **PR-05B-1 strategy**: Keep interface, delete implementations (PG + SQLite), stub with nil or fallback

---

## B. v4 Schema Verification — tenant_id Fully Absent

### Critical Finding: Schema-Code Mismatch (BUG in HEAD)

**Schema files (migrations + SQLite):**

1. **`migrations/000001_initial.up.sql`** — Line 2 comment: "Single-tenant, user-centric model. No tenant_id columns anywhere."
   - Verified: Zero `tenant_id` column in agents table (lines 90-132)
   - Verified: Zero `tenant_id` in agent_shares table (lines 140-147) — only `agent_id, user_id, role, granted_by, created_at`
   - Verified: Zero `tenant_id` in user_context_files, user_agent_profiles, user_agent_overrides
   - **Verdict**: Schema is v4-compliant (no tenant_id anywhere).

2. **`internal/store/sqlitestore/schema.sql`** — Line 2 same comment
   - Verified: agents table (lines 81-120) — no tenant_id
   - Verified: agent_shares table (lines 140-150) — no tenant_id
   - **Verdict**: SQLite schema matches PG (no tenant_id).

### Code-Schema Mismatch (CRITICAL):

**`internal/store/pg/agents.go` still references tenant_id:**
- Line 97: SELECT includes tenant_id column (does not exist) ← **BUG**
- Line 119-130: INSERT with tenant_id param (column does not exist) ← **BUG**
- Line 156, 178, 228, 273, 317: WHERE tenant_id clauses ← **BUG**
- Line 333-336: `agent_shares` INSERT with `tenant_id` column (not in schema) ← **BUG**
- Line 352, 364-365, 420, 467: `agent_shares` WHERE tenant_id clauses ← **BUG**

**Total tenant_id refs in agents.go: 18 lines**

**`internal/store/sqlitestore/agents.go` mirrors PG:** ~8 tenant_id refs (same bug)

**Cross-file tenant_id count:**
- `internal/store/pg/`: 497 refs across 78 files
- `internal/store/sqlitestore/`: 453 refs across 71 files
- **Total**: 950 refs (vs plan claim of 1131; possible count drift or comment inclusion)

**Verdict**: Current HEAD code is **broken at runtime** (column-not-found errors). PR-05B-1 MUST fix this by removing all tenant_id refs.

---

## C. Context Helpers Cleanup Scope

### Tenant-Related Symbols in `internal/store/context.go`

**Full inventory with line numbers:**

| Symbol | Line(s) | Type | Purpose | Status |
|--------|---------|------|---------|--------|
| `TenantIDKey` | 329 | const | Context key for tenant UUID | DELETE (Phase 13) |
| `WithTenantID` | 340 | func | Set tenant in context | DELETE (Phase 13) |
| `TenantIDFromContext` | 346 | func | Extract tenant from context | DELETE (Phase 13) |
| `WithCrossTenant` | 359 | func | Flag cross-tenant access | DELETE (Phase 13; marked "deprecated") |
| `IsCrossTenant` | 366 | func | Check cross-tenant flag | DELETE (Phase 13) |
| `WithTenantSlug` | 400 | func | Set tenant slug in context | DELETE (Phase 13) |
| `TenantSlugFromContext` | 405 | func | Extract tenant slug | DELETE (Phase 13) |
| `IsMasterScope` | 387 | func | Check if ctx is master/owner (uses MasterTenantID) | REFACTOR (Phase 13) |
| `IsOwnerRole` | 373 | func | Check role == "owner" | KEEP (role system stays) |
| `MasterTenantID` | 13 (tenant_store.go) | const | UUID constant | DELETE (Phase 13) |

**Total LOC of tenant-related symbols**: ~80 lines (WithTenantID, IsCrossTenant, WithTenantSlug, IsMasterScope + docstrings)

### Callers of tenant context symbols (non-store/context.go):

**High-use symbols (>20 callers each):**
- `TenantIDFromContext`: ~90 callers across pg/, sqlitestore/, gateway/, http/
- `IsCrossTenant`: ~35 callers
- `IsMasterScope`: ~25 callers
- `WithTenantID`: ~15 callers
- `TenantSlugFromContext`: ~8 callers
- `WithTenantSlug`: ~5 callers

**Verdict**: Tenant context symbols are **deeply wired**. Cannot delete in PR-05B-1. Must refactor in multi-PR strategy:
- PR-05B-1: Keep symbols; refactor agents store only
- PR-05B-2/3: Refactor 78 PG + 71 SQLite files to eliminate TenantIDFromContext calls
- Phase 13: Delete symbols from context.go + MasterTenantID

---

## D. MasterTenantID Full Purge Count

### Total refs by grep: 205 across internal/

**Breakdown by package (detailed count):**

```
internal/providers/registry.go:           10 refs
internal/gateway/router.go:                8 refs
internal/http/storage_test.go:             7 refs
internal/upgrade/hook_web_search_migrate:  6 refs
internal/store/sqlitestore/skills_content: 6 refs
internal/http/auth.go:                     6 refs
internal/store/sqlitestore/skills_crud.go: 5 refs
internal/store/pg/skills_content.go:       5 refs
internal/store/pg/hooks.go:                5 refs
internal/http/skills.go:                   3 refs
internal/store/tenant_store.go:            3 refs
internal/store/context.go:                 2 refs (includes var declaration + IsMasterScope logic)
... [50+ more files with 1-3 refs each]
```

**Load-bearing uses (require refactoring):**

1. **`internal/store/pg/agents.go:108`** — MasterTenantID fallback in Create()
   - Only MasterTenantID ref in agents store
   - **PR-05B-1 action**: REMOVE (no fallback; use owner_user_id instead)

2. **`internal/store/context.go:392`** — IsMasterScope checks `tid == MasterTenantID`
   - **PR-05B-1 action**: KEEP (deferred to Phase 13 refactor)

3. **`internal/providers/registry.go:15`** — var MasterTenantID used in provider defaults
   - **PR-05B-1 action**: KEEP (not on critical path)

4. **All others**: Non-critical context setup, tests, or comments
   - **PR-05B-1 action**: KEEP (defer Phase 13)

**Verdict**: Only `agents.go:108` is PR-05B-1 critical. All others (200+ refs) defer to Phase 13.

---

## E. Other "Tenant" Concepts to Evaluate

### 1. `resolveTenantSlug()` function chain

**Location**: `internal/agent/resolver.go:595-603`

**Purpose**: Looks up TenantSlug from TenantID for workspace path scoping
```go
func resolveTenantSlug(ts store.TenantStore, tenantID uuid.UUID) string {
    if ts == nil { return tenantID.String() }
    tenant, err := ts.GetTenant(context.Background(), tenantID)
    if err != nil || tenant == nil { return tenantID.String() }
    return tenant.Slug
}
```

**v4 replacement strategy**:
- Option A: Drop tenant slug entirely; use single global workspace
- Option B: Use owner_user_id as path component instead of tenant slug
- Option C: Use agent_key (already unique) as workspace scoping

**Decision needed**: What is v4 workspace scoping strategy?

### 2. Bootstrap tenant seeding

**Checked**: No `seedTenant()`, `CreateTenant()`, or default tenant creation in `internal/bootstrap/`

**Verdict**: Bootstrap is already tenant-agnostic. No changes needed.

### 3. Onboard flow tenant handling

**Checked**: cmd/onboard*.go files

**Finding**: Onboard flow does NOT mention tenant creation (need to read files to confirm).

**Decision needed**: Does v4 onboard create default tenant, or is it user-only?

### 4. RBAC system (internal/permissions/)

**Checked**: Role constants exist (root, admin, member, viewer) in users table

**Finding**: RBAC is role-based, NOT tenant-scoped. Users have global role.

**Verdict**: Permission system is **already tenant-agnostic** in v4 schema. No changes needed.

### 5. Struct field "tenant" mentions

**Checked**: Grep for `TenantID`, `Tenant`, `TenantSlug` field names

**Result**: All refs are in:
- `internal/store/context.go` (context helpers)
- `internal/store/tenant_store.go` (data types)
- `internal/store/pg/agents.go` (AgentData struct — TenantID field)
- `internal/agent/resolver.go` (ResolverDeps.TenantStore field)
- `internal/http/skills.go` (SkillsHandler.tenantStore field)
- Test files

**Verdict**: Tenant struct fields are **localized to expected places**. No surprise hidden tenant fields.

---

## F. Realistic PR-05B-1 Scope (Agent Store + Full Tenant Drop)

### Decision Matrix: Delete vs Defer

| Symbol / File | PR-05B-1 | PR-05B-2 | Phase 13 | Blast Radius | Reason |
|---|---|---|---|---|---|
| `internal/store/tenant_store.go` (interface) | ❌ KEEP | | ✅ DELETE | 5 direct callers | Breaks resolver, http/skills, gateway/router |
| `internal/store/pg/tenant_store.go` (impl) | ❌ KEEP | | ✅ DELETE | None (impl only) | Can keep if interface persists; safe to defer |
| `TenantID` field in `AgentData` struct | ✅ DELETE | | | Agents only | Matches schema (no tenant_id column) |
| `agents.go` CRUD methods (Create, Get, List) | ✅ REFACTOR | | | Agents only | Remove tenant_id from SQL + fallback logic |
| `agents.go` access methods (ShareAgent, etc.) | ❌ DEFER | ✅ 05B-2 | | Heavy tenant deps | ~20 refs; defer to scoping phase |
| `agent_shares` table tenant_id refs | ✅ REMOVE | | | Agents access | Schema has no tenant_id; code bug |
| `TenantIDFromContext` calls in agents.go | ✅ REPLACE | | | 8 calls | Replace with owner_user_id logic |
| `IsCrossTenant` checks in agents.go | ❌ DEFER | ✅ 05B-2 | | Heavy perms logic | Complex; defer to permission refactor |
| `MasterTenantID` in agents.go:108 | ✅ DELETE | | | Single line | No fallback; use owner_user_id |
| `IsMasterScope` in context.go | ❌ KEEP | | ✅ REFACTOR | ~25 callers | Still used app-wide; Phase 13 updates meaning |
| `scope.go` QueryScope struct | ❌ DEFER | ✅ 05B-2 | | 36 PG files | Delete QueryScope.TenantID; refactor whole file |
| `internal/store/base/query_builder.go` | ❌ DEFER | ✅ 05B-2 | | 36 PG files | BuildScopeClause needs refactor |

### Minimum File Count for PR-05B-1 (agents-only, tenant fully dropped from store layer)

**Delete (3 files):**
1. ~~`internal/store/tenant_store.go`~~ (interface) → KEEP to Phase 13
2. ~~`internal/store/pg/tenant_store.go`~~ (PG impl) → KEEP to Phase 13
3. ~~`internal/store/tenant_config_store.go`~~ (feature flags) → KEEP to Phase 13

**Modify directly (4 files):**
1. `internal/store/agent_store.go` — remove `TenantID` field from `AgentData`
2. `internal/store/pg/agents.go` — refactor Create/GetByKey/GetByID/Update/Delete/List (7 methods)
3. `internal/store/sqlitestore/agents.go` — mirror PG refactors (7 methods)
4. `internal/store/pg/agents_scan.go` (or inline in agents.go) — update scan to exclude tenant_id

**Optional (improve structure, reduce coupling):**
5. `internal/store/base/helpers.go` — add `OwnerUserIDFilter()` helper (extract WhereClause builder)

**Test (1 file):**
6. `tests/e2e/stores/03_agents_no_tenant_test.go` — NEW test (TDD red step)

### Honest PR-05B-1 File Count: **6-7 files** (vs 90 PG files + 71 SQLite files if full scope)

**Lines of code changes (realistic estimate):**
- Removals: ~100 lines (tenant_id columns, MasterTenantID fallback, tenantIDForInsert calls)
- Additions: ~150 lines (owner_user_id scoping logic, custom WHERE builders, test cases)
- **Net delta**: ~+50 LOC

---

## G. Surprises & Contradictions vs Prior Scout Report

### 1. **agent_shares tenant_id BUG (NEW finding)**

**Prior report (scout-260502-2010) assumption**: Schema already has no tenant_id → code will be updated.

**Reality**: Code still has 18 refs to `agent_shares.tenant_id`, but schema does NOT have tenant_id column.

**Impact**: Current HEAD code would fail at runtime on ShareAgent() call. PR-05B-1 MUST fix this bug.

**Contradiction**: Schema is correct; code is wrong. No schema changes needed; code must align to schema.

### 2. **TenantStore interface lifecycle (CLARIFIED)**

**Prior assumption**: Delete TenantStore in PR-05B.

**Reality**: TenantStore interface is called from:
- `internal/agent/resolver.go:266` (resolveTenantSlug)
- `internal/http/skills.go:39,45,51,585,631` (tenantSkillsDir, requireTenantAdmin)
- `internal/gateway/router.go` (enrichment)
- `internal/gateway/methods/tenants.go` (handler)

**Consequence**: Cannot delete interface in PR-05B-1; must defer to Phase 13.

**Plan correction**: phase-05-stores-refactor.md lists "DELETE tenant_store.go" under PR-05B, but this is unsafe. Adjust plan to:
- PR-05B: Keep interface; delete implementations (PG + SQLite)
- Phase 13: Delete interface + all tenant concept plumbing

### 3. **IsMasterScope still in use (NOT OBSOLETE)**

**Prior assumption**: IsMasterScope becomes irrelevant after tenant removal.

**Reality**: `IsMasterScope(ctx)` in context.go:387 checks `tid == uuid.Nil || tid == MasterTenantID`. This is still used by:
- HTTP admin routes (WS config)
- Shell/filesystem guards
- ~25 callers app-wide

**In v4**: "Master scope" becomes "system owner role scope" (IsOwnerRole). But the function persists until Phase 13.

**Verdict**: Keep IsMasterScope in context.go through Phase 13; refactor meaning later.

### 4. **TenantIDFromContext still heavily wired (~90 callers)**

**Prior report**: Expected this; listed as "keep in PR-05B".

**Confirmation**: ~90 callers across pg/, sqlitestore/, gateway/, http/. Removing from agents.go only is safe isolation.

**Verdict**: Consistent with prior report. PR-05B-1 refactors agents.go only; Phase 13 purges the symbol.

---

## H. Unresolved Questions Requiring User Decision

### 1. **Agent workspace scoping in v4**

**Q**: After tenant_id removal, how do agents' workspace paths get scoped?

**Options**:
- A: Single global workspace (all agents share same disk path)
- B: Per-user workspace (use owner_user_id to scope paths)
- C: Per-agent workspace (use agent_key or agent_id to scope paths)

**Decision needed**: Which for PR-05B-1 refactor?

### 2. **Cross-tenant access model in v4**

**Q**: What does `IsCrossTenant(ctx)` mean after tenant removal?

**Options**:
- A: Remove cross-tenant concept entirely (every call fails tenant check)
- B: Redefine as "admin bypass" (admin users can see all agents/data)
- C: Redefine as "system role" (root/owner role bypasses scoping)

**Current code**: agents.go uses `IsCrossTenant` to bypass tenant_id filter. If tenant gone, how do admins access others' agents?

**Decision needed**: Admin scoping strategy for PR-05B-2 (scope refactor)?

### 3. **owner_user_id scoping in PR-05B-1**

**Q**: Should agents.go GetByKey/List scope by `owner_user_id = current_user`?

**Options**:
- A: Yes; users see only their own agents by default (unless admin/root)
- B: No; agents are globally visible; use agent shares for access control
- C: Mixed; key-based access is scoped, list access is global

**Impact**: Determines agents.go WHERE clause rewrite.

**Decision needed**: Scoping semantics for PR-05B-1 refactor?

### 4. **OwnerID vs OwnerUserID field swap**

**Q**: agents.AgentData has both:
- `OwnerID string` (legacy, currently "username" or similar)
- `OwnerUserID uuid.UUID` (FK to users.id)

**For PR-05B-1**: Which field should scope queries?

**Options**:
- A: Use OwnerUserID (UUID) for scoping; leave OwnerID as legacy field
- B: Swap OwnerID to UUID in schema (Phase 05 user_id refactor); use new type
- C: Drop OwnerID entirely; use OwnerUserID only

**Current code**: agents.go scopes by TenantID (column doesn't exist), not by OwnerID/OwnerUserID.

**Decision needed**: Which field + when to swap user_id types?

### 5. **Agent access control after tenant removal**

**Q**: `agent_shares` table has no tenant_id in v4 schema. How do we prevent user A from granting access to user B's agents?

**Options**:
- A: Add `owner_user_id` FK to agent_shares; check ownership before grant
- B: Move access control to agents table only (no shares table)
- C: Rely on agents.owner_user_id to validate membership

**Current code**: agents.go has 18 refs to `agent_shares.tenant_id` (buggy).

**Decision needed**: Access control model for agent shares in v4?

### 6. **Tenant context removal timeline**

**Q**: Can we remove `WithTenantID`, `TenantIDFromContext`, `IsCrossTenant` from context.go in PR-05B-1, or must they persist through Phase 13?

**Constraint**: ~90 callers across codebase still use TenantIDFromContext.

**Options**:
- A: Remove in PR-05B-1 (requires refactoring all 90 callers simultaneously — large blast)
- B: Keep dummy stubs through PR-05B-2/3 (slower rollout)
- C: Phase 13 only (keep stubs indefinitely)

**Plan guidance needed**: Multi-phase strategy?

### 7. **IsMasterScope refactoring**

**Q**: After tenant removal, what should IsMasterScope mean?

**Current definition** (line 387-392):
```go
func IsMasterScope(ctx context.Context) bool {
    if IsOwnerRole(ctx) { return true }
    tid := TenantIDFromContext(ctx)
    return tid == uuid.Nil || tid == MasterTenantID
}
```

**v4 replacement**:
```go
func IsMasterScope(ctx context.Context) bool {
    return IsOwnerRole(ctx)  // Only system owner has bypass
}
```

**Decision needed**: Confirm this refactoring matches v4 architecture?

### 8. **TenantSlugFromContext usage**

**Q**: After tenant removal, what replaces TenantSlugFromContext for path scoping?

**Current uses**:
- `internal/http/skills.go:53` — tenantSkillsDir()
- `internal/agent/resolver.go:271` — workspace directory scoping

**v4 strategy**: Use owner_user_id or agent_key for path scoping instead?

**Decision needed**: Path scoping semantics after tenant removal?

### 9. **Tenant field cleanup checklist**

**Q**: Are there any tenant-related fields hiding in domain models or config structs?

**Spot check findings**:
- `internal/hooks/types.go` — no Tenant field (checked)
- `internal/gateway/router.go` — no Tenant field (checked)
- All critical files checked; no surprises found

**Verdict**: Safe to assume struct fields are localized to expected places.

### 10. **Test mock cleanup scope**

**Q**: Which tests require tenant mock cleanup in PR-05B-1?

**Files**: `internal/http/auth_test.go`, `internal/http/tenant_backup_auth_helpers_test.go`

**Decision needed**: Should PR-05B-1 also clean up test mocks, or defer to Phase 13?

---

## Summary

### PR-05B-1 Minimum Scope (Agents Store, Full Tenant Drop)

**Files changed**: 6-7 files
- 1 interface file (AgentData struct)
- 2 impl files (PG + SQLite agents.go)
- 1 test file (NEW; TDD red step)
- 2-3 helper refactors (optional)

**Lines changed**: ~50 LOC net (+150 added, -100 removed)

**Blast radius**: Isolated to agents store only. No other stores affected; can land safely between Phase 05B-2 PRs.

**Risks**:
- **HIGH**: Schema-code mismatch (BUG in HEAD). agent_shares table has no tenant_id but code references it. PR-05B-1 must fix.
- **MEDIUM**: TenantStore interface not deletable in PR-05B-1 (5 callers). Defer to Phase 13; adjust plan.
- **MEDIUM**: IsMasterScope refactoring deferred to Phase 13; function persists but meaning changes.

**Justification**: PR-05B-1 is viable as critical bug-fix PR (schema-code alignment) + foundation for Phase 13 tenant cleanup.

---

## Big-Rocks for PR-05B-2 & Follow-Up PRs

### PR-05B-2 (Scope Refactor — 8 dev-days)

- Drop `TenantIDFromContext` from all 36 PG files
- Replace `scopeClause(ctx)` with custom owner_user_id filtering
- Refactor `internal/store/base/query_builder.go` BuildScopeClause
- Remove QueryScope.TenantID field
- Mirror in 20+ SQLite files

**Files affected**: 36 PG + ~20 SQLite (non-agents)

### PR-05B-3 (Sessions Rename + SQLite full refactor — 6 dev-days)

- Rename `internal/sessions/` → `internal/agentsessions/`
- SessionStore → AgentSessionStore type rename
- All `FROM sessions` SQL → `FROM agent_sessions` (37 sites)
- Refactor all 88 SQLite files

**Files affected**: 88 SQLite + 20+ import updates

### Phase 13 (Tenant Concept Purge — 3-5 dev-days)

- Delete `internal/store/tenant_store.go` interface
- Delete `internal/store/pg/tenant_store.go` impl
- Delete TenantStore fields from resolver.go, http/skills.go, gateway/router.go
- Refactor gateway/methods/tenants.go (delete or stub)
- Remove TenantIDKey, WithTenantID, TenantIDFromContext, IsCrossTenant, WithTenantSlug, TenantSlugFromContext from context.go
- Refactor IsMasterScope → IsOwnerRole only
- Delete MasterTenantID constant (replace 205 refs)

**Files affected**: 10+ critical files (resolver, http/*, gateway/*)

---

## Contradictions Discovered

| Assumption (Prior) | Reality (Found) | Impact | Resolution |
|---|---|---|---|
| Schema has no tenant_id; code clean | Code still refs tenant_id; schema correct | BUG in HEAD | PR-05B-1 fixes by removing code refs |
| Delete TenantStore in PR-05B | TenantStore has 5 active callers outside store | Unsafe | Defer interface deletion to Phase 13 |
| 1131 tenant_id refs total | ~950 refs found; possible count drift | Recount needed | Use 950 as baseline; recount on PR branch |
| IsMasterScope becomes irrelevant | IsMasterScope still used ~25 places; meaning changes | Not deleting yet | Keep through Phase 13; refactor meaning |
| All tenant symbols in context.go | Confirmed; no hidden tenant fields elsewhere | Low risk | Safe to focus context.go cleanup on Phase 13 |

---

## Unresolved Questions Summary

1. **Agent workspace scoping strategy in v4** (A: global, B: per-user, C: per-agent?)
2. **Cross-tenant access redefinition** (A: remove, B: admin-bypass, C: owner-bypass?)
3. **Agent visibility scoping** (A: owner-only, B: global, C: mixed?)
4. **OwnerID vs OwnerUserID field strategy** (when to swap to UUID?)
5. **Agent access control model** (how to prevent cross-user grants without tenant_id?)
6. **Tenant context symbol removal timeline** (PR-05B-1 vs Phase 13?)
7. **IsMasterScope refactoring** (confirm = IsOwnerRole only?)
8. **Path scoping replacement** (what replaces TenantSlugFromContext?)
9. **Test mock cleanup scope** (PR-05B-1 or Phase 13?)
10. **tenant_config_store cleanup** (keep as feature flags or delete?)

---

## Next Steps

1. **User confirms**: Workspace + access control strategy (Qs 1-3, 5, 8)
2. **Refine plan**: Adjust PR-05B-1 scope based on decisions above
3. **Start PR-05B-1**: Write Test 03 (TDD red), then refactor agents.go + AgentData
4. **Phase 13 planning**: Sequence the 5+ remaining cleanup PRs (resolver, http/skills, gateway/*, context.go)

