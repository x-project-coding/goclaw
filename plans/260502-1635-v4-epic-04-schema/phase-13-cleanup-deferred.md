# Phase 13 — Cleanup (MasterTenantID Purge + Dead Code + Polish)

## Context Links

- Master § 4.12 (Hidden infra), § 6 DELETE list
- Audit D-1: 171 NON-test lines / ~50 files MasterTenantID (audit-corrected)
- Audit LOG-2 (Q-3 dead `scope='custom'` ADR)
- Audit LOG-3 (Q-10 BE/FE naming divergence)
- Phase 02 v3-baseline.md MasterTenantID enumeration

## Overview

- Priority: P1 (final polish — gates Phase 14)
- Status: pending
- Effort: <!-- RED-TEAM Finding 15 --> 8-10 dev-days (was 4d; bumped due to corrected file count) <!-- /RED-TEAM Finding 15 -->
- Description: Sweep remaining MasterTenantID references — store layer already cleaned in Phase 05; here finish providers, agent, http, gateway, oauth, hooks, tracing, vault, heartbeat, upgrade, workspace, browser. Delete dead code paths from dropped CLI commands. Polish env.e2e-tests README. Add ADR for vault custom scope (LOG-2). Clarify BE/FE sessions naming divergence (LOG-3).

<!-- RED-TEAM Finding 15: MasterTenantID file count 50 vs 81 — Phase 13 budget undercount (HIGH) -->
**Corrected file count (verified via live grep on 2026-05-02):**
- Real: **81 non-test files** with MasterTenantID references (NOT ~50 as plan.md claimed). 171 lines correct.
- Phase exit gate: enumerate every modified file in commit message (no batch "go vet clean" hand-wave).
- Bump effort 4d → 8-10d to absorb the 60% file count miss.

**Files MISSED from earlier inventory (must be added to MODIFY list below):**
- `cmd/gateway*.go` (7 files in cmd/ that wire MasterTenantID for legacy boot)
- `internal/http/agents_codex_pool.go`
- `internal/http/storage.go`
- `internal/http/oauth.go`
- `internal/skills/seeder.go`
- `internal/hooks/types.go`
- `internal/gateway/router.go`
- `internal/gateway/methods/heartbeat.go`
- `internal/vault/enrich_worker.go`
- `internal/store/context.go`

Each missed file = compile break OR ghost tenant runtime ref if skipped. Enumeration MANDATORY at phase start.
<!-- /RED-TEAM Finding 15 -->

## Key Insights

- Audit D-1 confirmed: MasterTenantID NOT just 6 files; actual ~50 files / 171 NON-test lines.
- Phase 05 already cleaned `internal/store/{pg,sqlitestore}/` portion + `internal/store/tenant_store.go:13`.
- Remaining MasterTenantID files (verified via live grep — 20 file paths from earlier verification):
  - `internal/upgrade/hook_web_search_migrate.go`
  - `internal/tools/web_search_chain_test.go`
  - `internal/tools/subagent_tracing.go`
  - `internal/oauth/token.go`
  - `internal/tools/workspace_resolver_test.go`
  - `internal/config/tenant_paths.go`
  - `internal/providers/registry.go` (Phase 07 already addressed key format; here remove constant ref)
  - `internal/agent/resolver.go`
  - `internal/agent/loop_tracing.go`
  - `internal/heartbeat/ticker.go`
  - `internal/http/summoner_regenerate.go`
  - `internal/http/tenant_scope_hotfix_test.go` (DELETE entire test)
  - `internal/http/skills_upload.go`
  - `internal/http/agents_import_helpers.go`
  - `internal/http/skills.go`
  - `internal/http/skills_import.go`
  - `internal/http/voices_test.go` (test refactor)
  - `internal/http/auth_test.go` (test refactor)
  - `internal/http/auth.go` (Phase 06 partial; here finalize)
  - `internal/http/skills_upload_test.go` (test refactor)
  - `pkg/browser/browser.go:249` (const string)
  - `internal/workspace/resolver_impl.go:15`
- Plus tests (post-refactor): some `*_test.go` files reference MasterTenantID — clean during this phase.

## Tests to write FIRST (TDD red step)

| Test file | Cases (must FAIL until impl green) |
|---|---|
| `tests/e2e/13_no_master_tenant_id_test.go` | `TestNoMasterTenantIDProjectWide` — `grep -rn 'MasterTenantID' --include='*.go' | grep -v _test.go` returns 0; in test files: only allowed within fixtures helpers (allow-list pattern documented) |
| `tests/e2e/13_no_tenant_id_in_schema_test.go` | `TestNoTenantIDColumnsAnywhere` — PG `information_schema.columns WHERE column_name='tenant_id'` returns 0; SQLite `pragma_table_info` across all tables returns 0 |
| `tests/e2e/13_dead_code_purge_test.go` | `TestNoOrphanedFunctions` — known dead helpers (e.g., functions only called by deleted CLI) report compile-warning OR removed; verify via `go vet` + manual list |
| `tests/e2e/13_adr_docs_present_test.go` | `TestADRsExist` — `docs/adr/2026-05-v4-vault-custom-scope-reserved.md`, `docs/adr/2026-05-v4-vault-no-encryption-defer.md`, `docs/adr/2026-05-v4-sessions-naming-divergence.md` exist + non-empty |

**Red verification:** Tests fail because MasterTenantID still in many files; ADR docs not all written.

## Requirements

### Functional

#### MasterTenantID purge (NON-store; ~21 files)

For each remaining file (per audit D-1 + live grep):

- **Replace pattern:** Where `MasterTenantID` appeared as scope filter or default — drop entirely (single-user world has no master scope).
- **Where it was a const string (e.g., `pkg/browser/browser.go:249`):** drop the constant; replace usage with empty/nil (verify caller logic still correct).
- **Where it was a tenant_paths helper:** delete `internal/config/tenant_paths.go` entirely; refactor callers (likely `internal/workspace/resolver_impl.go`).
- **Test file refactor:** convert tenant fixtures to user fixtures (e.g., `MasterTenantID` → `RootUserID(t)` from helpers).
- **DELETE test file `internal/http/tenant_scope_hotfix_test.go`** (legacy v3 hotfix test, no v4 relevance).

#### Dead code purge

- Functions only called by deleted CLI commands (Phase 08): identify via `gopls` or `go vet -unused` (if available); remove.
- **Phase 08 deferred — `tenant`/`Tenant` refs in `cmd/gateway*.go`:** Phase 08 cleared the CLI surface (deleted ~25 user-facing CLI files, `cmd/root.go` registrations clean) but did NOT touch `cmd/gateway*.go` (`gateway_consumer*.go`, `gateway_agents.go`, `gateway_managed.go`, `gateway_subagent_announce_queue.go`, `gateway_http_wiring.go`, `gateway_hooks.go`, `gateway.go`, `gateway_lifecycle.go`, `gateway_tools_wiring.go`, `gateway_setup.go`). These still reference `tenant`/`TenantID` in struct fields, comments, and runtime plumbing — to be handled by Phase 07 (pool/cache refactor of 13 structs) + this phase's MasterTenantID purge. Verification: `grep -rln 'tenant\|TenantID' cmd/ --include='*.go'` should return 0 after Phase 13 sweeps.
- **Phase 07 deferred — `internal/channels/manager.go` RunContext.TenantID + ChannelTenantID method:** Per scout, channels/manager.go was "verify keyed by runID, no change" since runs map is runID-keyed. The `TenantID uuid.UUID` field on RunContext (line 35) and `ChannelTenantID(channelName)` method (line 281–295) are tenant-ROUTING plumbing (separate from cache keys). Drop both as part of this phase's MasterTenantID purge — they only ever return MasterTenantID in v4. Touch sites: `cmd/gateway.go:438` (SetChannelTenantChecker), `cmd/gateway_consumer*.go` (RunContext construction), `cmd/gateway_managed.go:280`, `internal/channels/manager.go:281–295`.
- **Phase 07 deferred — `internal/gateway/client.go` tenantID field + TenantID() method:** Originally deferred from Phase 06 (auth) due to wide blast radius. The `tenantID` field on Client always equals MasterTenantID in v4. Touch sites: `internal/gateway/client.go:42,213–214`, `internal/gateway/event_filter.go:33,37`, `internal/gateway/event_filter_test.go:19`, `internal/gateway/router.go` (multiple `client.tenantID = store.MasterTenantID` sets), and method-handler files in `internal/gateway/methods/` that read `client.TenantID()` (api_keys, chat, etc.). Coordinate with TenantID-on-events drop in eventbus.
- `seedTenantAgent` and similar test fixtures referenced from 300 callsites (audit D-8; was 326 in research): replaced in Phase 05 tests; here delete the original helper if any callsite remains references it.
- `internal/store/scope.go` — verify `QueryScope` post-Phase-05 has no orphan members.
- Audit `tests/invariants/tenant_isolation_test.go` — DELETE entire file (per master § 4.10; v3-only invariant).

#### env.e2e-tests README polish

- Update `env.e2e-tests/README.md`:
  - v4 setup instructions (port 5435, db `goclaw_v4_e2e`, vector pg18).
  - Run e2e: `make test-e2e`.
  - Required env: BAILIAN_API_KEY, OPENROUTER_API_KEY (already in .env).
  - Document that `.env` is gitignored.
  - Add troubleshooting section.

#### ADR docs (LOG-2, LOG-3)

- `docs/adr/2026-05-v4-vault-custom-scope-reserved.md` (LOG-2):
  - Reserve `vault_documents.scope='custom'` for future use.
  - 0 current write sites (Phase 02 evidence).
  - DO NOT add writers without RFC.
- `docs/adr/2026-05-v4-sessions-naming-divergence.md` (LOG-3):
  - BE table renamed `sessions` → `agent_sessions` (Q-10).
  - FE route stays `/sessions/` (intentional — UX continuity).
  - Document divergence so future LLMs don't "fix" naming mismatch.
- `docs/adr/2026-05-v4-vault-no-encryption-defer.md` (Q-14 audit MISS-2):
  - Per-user vault encryption deferred to v4.x.
  - v4.0 = "trust admin model" — admin can read all vault content.
- `docs/adr/2026-05-v4-activity-logs-retention-defer.md` (Q-14 audit MISS-3):
  - activity_logs retention cron deferred to v4.x.
  - Operations team monitors growth.

### Non-functional

- All commits squash-friendly (single "v4 cleanup" commit acceptable).
- Tests gated `//go:build e2e` for project-wide grep tests.
- ADR docs follow existing template if any in `docs/adr/`.

## Architecture

```
Phase 13 sweep targets (REAL file count = 81 non-test, per Finding 15):
  Backend Go (~31+ files in original list + 16 missed):
   ├─ internal/upgrade/hook_web_search_migrate.go
   ├─ internal/tools/{web_search_chain_test, subagent_tracing, workspace_resolver_test}.go
   ├─ internal/oauth/token.go
   ├─ internal/config/tenant_paths.go (DELETE entirely)
   ├─ internal/providers/registry.go (constant ref drop)
   ├─ internal/agent/{resolver, loop_tracing}.go
   ├─ internal/heartbeat/ticker.go
   ├─ internal/http/{summoner_regenerate, skills_upload, agents_import_helpers, skills, skills_import, auth, voices_test, auth_test, skills_upload_test}.go
   ├─ internal/http/tenant_scope_hotfix_test.go (DELETE)
   ├─ pkg/browser/browser.go:249
   ├─ internal/workspace/resolver_impl.go:15
   ├─── (Finding 15 — MISSED in earlier inventory)
   ├─ cmd/gateway*.go (7 files)
   ├─ internal/http/agents_codex_pool.go
   ├─ internal/http/storage.go
   ├─ internal/http/oauth.go
   ├─ internal/skills/seeder.go
   ├─ internal/hooks/types.go
   ├─ internal/gateway/router.go
   ├─ internal/gateway/methods/heartbeat.go
   ├─ internal/vault/enrich_worker.go
   └─ internal/store/context.go

  Tests (~50 internal *_test.go files referencing tenant fixtures — use grep enumeration during impl)
  
  Docs:
   ├─ env.e2e-tests/README.md (polish)
   ├─ docs/adr/2026-05-v4-vault-custom-scope-reserved.md (NEW)
   ├─ docs/adr/2026-05-v4-sessions-naming-divergence.md (NEW)
   ├─ docs/adr/2026-05-v4-vault-no-encryption-defer.md (NEW — Phase 03 may have created)
   └─ docs/adr/2026-05-v4-activity-logs-retention-defer.md (NEW)

  Tests to delete:
   └─ tests/invariants/tenant_isolation_test.go (legacy v3 invariant, no v4 use)
```

## Related Code Files

### Modify (~21 + tests)

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/upgrade/hook_web_search_migrate.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/tools/subagent_tracing.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/tools/web_search_chain_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/tools/workspace_resolver_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/oauth/token.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/providers/registry.go` (final constant ref drop)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/agent/resolver.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/agent/loop_tracing.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/heartbeat/ticker.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/summoner_regenerate.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/skills_upload.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/agents_import_helpers.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/skills.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/skills_import.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/voices_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/auth_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/auth.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/skills_upload_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/pkg/browser/browser.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/workspace/resolver_impl.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/env.e2e-tests/README.md`
<!-- RED-TEAM Finding 15: missed files added -->
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/gateway*.go` (7 files — enumerate via `grep -l 'MasterTenantID' cmd/gateway*.go` at phase start)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/agents_codex_pool.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/storage.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/oauth.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/skills/seeder.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/hooks/types.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/gateway/router.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/gateway/methods/heartbeat.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/vault/enrich_worker.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/context.go`
<!-- /RED-TEAM Finding 15 -->

### Create

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/docs/adr/2026-05-v4-vault-custom-scope-reserved.md`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/docs/adr/2026-05-v4-sessions-naming-divergence.md`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/docs/adr/2026-05-v4-activity-logs-retention-defer.md`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/13_no_master_tenant_id_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/13_no_tenant_id_in_schema_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/13_dead_code_purge_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/13_adr_docs_present_test.go`

### Delete

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/config/tenant_paths.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/tenant_scope_hotfix_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/invariants/tenant_isolation_test.go`

## Implementation Steps

1. Verify Phase 12 merged (last impl phase before final polish).
2. Write 4 e2e test files (red).
3. Write 4 ADR docs (each ~30-50 lines).
   - Verify Phase 03 already created `vault-no-encryption-defer.md`; if yes, skip duplicate.
4. Run `grep -rn 'MasterTenantID' --include='*.go' | grep -v _test.go` enumerate.
5. For each non-test file in remaining list:
   a. Read file, locate MasterTenantID usage.
   b. Drop usage (often: drop `if tenantID == MasterTenantID { ... }` branches; drop `WithTenantID(MasterTenantID)` calls).
   c. Verify caller logic still correct (single-user world; no master vs tenant distinction).
6. DELETE `internal/config/tenant_paths.go` + refactor callers (`grep -rn 'tenant_paths\.' --include='*.go'`).
7. DELETE `internal/http/tenant_scope_hotfix_test.go`.
8. DELETE `tests/invariants/tenant_isolation_test.go`.
9. For each test file referencing MasterTenantID: refactor fixtures to use `seedUser`/`RootUserID` helpers from Phase 01 harness.
10. Polish `env.e2e-tests/README.md` with v4 instructions.
11. `go build ./...` + `go build -tags sqliteonly ./...` + `go vet ./...` clean.
12. Run all 4 e2e cleanup tests → green.
13. Run all earlier phase tests → still green.

## Todo List

- [ ] 4 e2e cleanup test files written (red)
- [ ] 4 ADR docs (vault-custom-scope, sessions-divergence, activity-logs-retention, vault-no-encryption confirmed)
<!-- RED-TEAM Finding 15 -->
- [ ] (Finding 15) Re-grep at phase start: enumerate ALL 81 non-test files referencing MasterTenantID
- [ ] (Finding 15) MasterTenantID purge (~31+ original list + 16 missed files = ~47+ files; verify count matches re-grep)
- [ ] (Finding 15) Phase exit commit message enumerates every modified file
<!-- /RED-TEAM Finding 15 -->
- [ ] internal/config/tenant_paths.go DELETED
- [ ] internal/http/tenant_scope_hotfix_test.go DELETED
- [ ] tests/invariants/tenant_isolation_test.go DELETED
- [ ] Test fixtures refactored (seedTenant → seedUser equivalent)
- [ ] env.e2e-tests/README.md polished
- [ ] go build (PG + sqliteonly) + go vet clean
- [ ] All 4 e2e cleanup tests green
- [ ] Earlier phase tests still green

## Success Criteria

- `grep -rn 'MasterTenantID' --include='*.go'` returns 0 in non-test code.
- 4 ADR docs exist + non-empty.
- 0 columns named `tenant_id` in PG + SQLite schemas.
- `tenant_isolation_test.go` removed.
- `env.e2e-tests/README.md` covers v4 setup completely.
- `go build` + `go vet` clean both tag combinations.

## Risk Assessment

| Risk | Severity | Mitigation |
|---|---|---|
| MasterTenantID branch drop changes runtime behavior | High | Per-file review: verify single-user logic = master logic post-drop; manual trace |
| Hidden caller of `tenant_paths.go` breaks | Med | grep callsites BEFORE delete; refactor each |
| Test fixtures changed wholesale break unrelated tests | Med | Per-test refactor + run; smoke after each batch |
| ADR docs format inconsistent with existing | Low | Follow existing `docs/adr/` template if present; otherwise minimal MD |
| Dead code grep false positives | Low | `go vet -unused` if available; manual review |

## Security Considerations

- No security regression — MasterTenantID was never an isolation boundary; removing it just simplifies code.
- Test deletion (`tenant_isolation_test.go`) does NOT remove security; v4 has no tenants, no isolation needed.
- ADR `vault-no-encryption-defer.md` documents trust-admin model so users have informed consent.

## Cross-phase Gates

- **Entry:** Phase 12 merged (desktop edition complete).
- **Exit:** All 4 cleanup tests green + earlier phase tests still green + `grep MasterTenantID` returns 0 non-test. Gates Phase 14.
<!-- RED-TEAM Finding 15 -->
- **Exit gate enumeration:** commit message MUST list every modified file by path. No "go vet clean" hand-wave summary. Reviewer should be able to diff-spot any of the 81 files missed.
<!-- /RED-TEAM Finding 15 -->

## Next Steps

- Phase 14 — full validation suite + RBAC matrix.
