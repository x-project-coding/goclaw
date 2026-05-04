# Phase 14 — Validation Final (full e2e + RBAC matrix + backup/restore round-trip)

## Context Links

- Master § 11 (E2E Test APIs) — definitive coverage matrix
- All preceding phases (01-13)
- env.e2e-tests/.env

## Overview

- Priority: P0 (release gate)
- Status: **code-complete 2026-05-04** — pending only the live e2e run on pgvector container before tag `v4.0.0-rc1`
- Effort: 6 dev-days + 2d Phase 14A endpoint impl + 1d gap closure (14C below)
- Sub-phase 14A (impl missing endpoints): committed in `feat(v4): phase-14a` (Users CRUD, hook budget rewire + GET /v1/hooks/budget, secure-cli ADR, channel pairing audit-correction)
- Sub-phase 14B (write 16 e2e test files): committed across 4 batches (b1: users+agents+hooks; b2: memory+vault+oauth+mcp+cli; b3: teams+sessions+chat+ws+cron; b4: rbac+isolation+backup)
- Sub-phase 14C (gap closure 2026-05-04): 12 missing test funcs added across 8 files — `TestUsersDeleteCascadesToOwnedResources`, `TestAgentDeleteCascadesContext`, `TestSessionResume`, `TestSessionMessageHistory`, `TestVaultWikilinksResolve`, `TestProviderBailian`, `TestHookExecutionLogs`, `TestUserHookBudgetMonthlyReset`, `TestWSChatStreamEvents`, `TestWSReconnectAfterDisconnect`, `TestBackupWorkspace`, `TestRestoreFreshDB`. ADRs added: `2026-05-v4-localstorage-tokens-defer.md`, `2026-05-v4-password-reset-http-defer.md`. Makefile: `test-e2e-short` / `test-e2e-full` / `test-release-gate`. CI: `e2e-fast` per-PR (skips LLM), `e2e-full` nightly cron 03:00 UTC.
- **Remaining red-state work:** ✅ both fixed in commit `7cbd10ea` (2026-05-04). (1) `RevokeAllSessionsPostRestore` wired into `backup.Restore` for both PG and SQLite. (2) KG handler `auth()` now gates `WithSharedKG` on `IsMasterScope` so non-admin callers get per-user filter already enforced by the store layer.
- Description: Implement remaining e2e tests covering full master § 11 matrix not yet covered by per-phase tests. RBAC matrix (root/admin/member/viewer × all resources). Multi-user isolation. Backup/restore round-trip. WebSocket frame coverage. LLM real-call smoke (Bailian + OpenRouter). Final regression run. Branch merge gate.

## Key Insights

- Earlier phases each ship their domain e2e tests (bootstrap, auth, channels, skills, etc.). Phase 14 fills gaps from master § 11 not yet covered:
  - Full RBAC matrix (currently scattered).
  - Multi-user isolation tests (per § 11.4).
  - WS frame type 100% coverage (req/res/event types).
  - LLM real-call smoke tests (Bailian + OpenRouter).
  - Backup/restore round-trip with row-count + checksum.
  - 503 + bootstrap + edge-case coverage.
- Master § 11.6 targets: 100% public API endpoints, 100% role × resource cells, migration round-trip, backup/restore equivalence.
- This phase is BLOCKING for release — no merge to `main` without all tests green.

## Tests to write FIRST (TDD red step)

This phase IS the test phase — all tests are first-class deliverables (no impl after; just verify earlier phases hold up).

| Test file | Cases (must FAIL until earlier phases green) |
|---|---|
| `tests/e2e/03_users_test.go` | `TestUsersCRUDByAdmin`, `TestUsersListSelfOnlyAsMember`, `TestUsersDeleteCascadesToOwnedResources` |
| `tests/e2e/04_agents_test.go` | `TestAgentCRUDOpenAndPredefined`, `TestAgentShareWithUser`, `TestAgentDeleteCascadesContext` |
| `tests/e2e/05_teams_test.go` | `TestTeamCRUD`, `TestTeamGrantsRoles`, `TestTeamTaskWorkflow` (create + comment + event + attachment) |
| `tests/e2e/06_sessions_test.go` | `TestAgentSessionCRUD`, `TestSessionResume`, `TestSessionMessageHistory` |
| `tests/e2e/08_memory_test.go` | `TestMemoryDocCRUD`, `TestMemoryHybridSearch`, `TestKGEntitiesCRUD`, `TestKGRelationsTraversal`, `TestKGUserScopeNullVsNotNull` |
| `tests/e2e/09_vault_test.go` | `TestVaultDocCRUD`, `TestVaultWikilinksResolve`, `TestVaultHybridSearch`, `TestVaultScopeCustomReserved` (Q-3 dead path test) |
| `tests/e2e/10_chat_test.go` | `TestChatNonStream`, `TestChatStream`, `TestChatToolUseTurn`, `TestProviderBailian` (real call), `TestProviderOpenRouter` (real call), `TestChatSkippedInShortMode` |
| `tests/e2e/11_websocket_test.go` | `TestWSConnectFirstFrame`, `TestWSChatStreamEvents`, `TestWSPingHeartbeat`, `TestWSAllFrameTypes`, `TestWSReconnectAfterDisconnect` |
| `tests/e2e/13_cron_test.go` | `TestCronJobCRUD`, `TestCronAtSchedule`, `TestCronEverySchedule`, `TestCronExprSchedule`, `TestCronRunLogs` |
| `tests/e2e/14_hooks_test.go` | `TestHookCRUD`, `TestHookExecutionLogs`, `TestUserHookBudgetMonthlyReset` |
| `tests/e2e/15_oauth_test.go` | `TestOAuthListProviders`, `TestOAuthLink`, `TestOAuthRevoke`, `TestOAuthPerUserTokens` |
| `tests/e2e/16_mcp_test.go` | `TestMCPServerCRUD`, `TestMCPAgentGrant`, `TestMCPUserGrant`, `TestMCPAccessRequestFlow` |
| `tests/e2e/17_secure_cli_test.go` | `TestSecureCLIBinariesList`, `TestSecureCLIGrant`, `TestSecureCLICredentials` |
| `tests/e2e/18_rbac_test.go` | **CRITICAL** — full matrix per master § 11.3: 4 roles × 7 resource types = 28 cells. Each cell asserts exact authz behavior |
| `tests/e2e/19_isolation_test.go` | Multi-user isolation per master § 11.4: 9 scenarios (memory, KG, vault, cron, hooks, OAuth, etc.) |
| `tests/e2e/20_backup_restore_test.go` | `TestBackupFullDB`, `TestBackupWorkspace`, `TestRestoreEquivalence` (row count + key-row spot-check, NO checksum equivalence per Finding 6), `TestRestoreFreshDB` (drop+restore), `TestRestoreIntegrity` (FK consistency). <!-- RED-TEAM Finding 6 --> `TestRestoreRevokesAllSessions` — pre-restore: 3 active user_sessions rows; trigger restore; post-restore: SELECT COUNT(*) FROM user_sessions WHERE revoked_at IS NULL = 0; SELECT COUNT(*) FROM user_sessions = 3 (rows survive, just revoked). All users must re-authenticate. |

**Red verification:** All earlier phases must be green. Phase 14 simply assembles + runs the comprehensive matrix.

## Requirements

### Functional

#### RBAC matrix test (18_rbac_test.go)

Per master § 11.3 — 4 roles × 7 resource types = 28 cells:

| Role | Bootstrap | Users | Agents | Teams | Skills global | System configs | Backup/Restore |
|---|---|---|---|---|---|---|---|
| root | initial | full CRUD | all | all | full | full | yes |
| admin | denied | full (no role change) | all | manage members | full | denied | denied |
| member | denied | self only | own + shared | task work | grant scope | denied | denied |
| viewer | denied | self read | shared read | task read | grant read | denied | denied |

Each cell: positive (allowed) + negative (denied) test with assert HTTP status.

#### Multi-user isolation (19_isolation_test.go)

Per master § 11.4 — 9 scenarios:

1. User A creates `memory_documents.user_id=A` → User B cannot read; agent (NULL) shared.
2. KG entity user_id NULL → visible to all.
3. KG entity user_id=A → only A sees.
4. Vault scope private → A only.
5. Vault scope shared → all on agent.
6. Vault scope custom → matches `custom_scope` (Q-3 dead path; document expectation = 0 writers).
7. Cron job user_id=A → B cannot list/cancel.
8. Hook budget per-user → A independent from B.
9. OAuth tokens → A's Google token does NOT leak to B.

#### Backup/restore round-trip (20_backup_restore_test.go)

- Seed N users, M agents, K sessions.
- Run `goclaw backup --output /tmp/v4-test.tar.gz`.
- Drop DB.
- Run `goclaw restore --input /tmp/v4-test.tar.gz`.
- Compare: row counts per table match; key-row spot-check on critical tables (`users.email`, `agents.id`, `agent_sessions.session_key`).
<!-- RED-TEAM Finding 6: Backup restore reactivates revoked refresh tokens (CRITICAL) -->
- **Drop checksum equivalence assertion** — checksums after restore will NOT match because of the post-restore session revocation step (below). Per scope-critic F7 medium overlap, checksum equivalence is brittle anyway across OS/DB-version skew. Replace with row-count + key-row spot-check.
- **Post-restore session revocation (REQUIRED step in `goclaw restore` command):** after schema + data load complete, execute:
  ```sql
  UPDATE user_sessions SET revoked_at = NOW() WHERE revoked_at IS NULL;
  ```
  This forces all users to re-authenticate post-restore. Prevents the day-2-backup → day-3-revocation → day-4-restore-reactivates-stolen-token attack chain.
- Test `TestRestoreRevokesAllSessions` (added to red tests above):
  - Pre-restore: assert `SELECT COUNT(*) FROM user_sessions WHERE revoked_at IS NULL > 0`.
  - Post-restore: assert `SELECT COUNT(*) FROM user_sessions WHERE revoked_at IS NULL = 0`.
  - Assert total session row count unchanged (rows survive, just revoked).
- **Append-only revocation log (hash-chained, outside backed-up DB) DEFERRED to v4.x** — out of scope here; document in ADR.
- **JWT iat-vs-restore-timestamp rejection DEFERRED to v4.x** — relies on persisted "last restore timestamp" anchor not yet built.
<!-- /RED-TEAM Finding 6 -->
- Verify FKs intact (no orphan rows).

#### LLM real-call smoke (10_chat_test.go)

- Call `POST /v1/chat/completions` with model `anthropic/claude-sonnet-4-5` via OpenRouter.
- Call same with Bailian provider.
- Assert: response not empty, `usage.total_tokens > 0`.
- Skipped in `-short` mode (rate-limit + cost protection).

#### WebSocket frame coverage (11_websocket_test.go)

- All 3 frame types: `req` / `res` / `event`.
- Methods: connect, chat, ping (master § 11).
- Negative: wrong userId, expired JWT, malformed JSON.

### Non-functional

- All tests gated `//go:build e2e`.
- LLM tests skipped if `-short`.
- Real provider tests skipped if API keys absent (env check).
- Max test duration: 30 min total e2e suite.

## Architecture

```
Phase 14 = test orchestration ONLY:
  tests/e2e/03_users_test.go     ┐
  tests/e2e/04_agents_test.go    │
  tests/e2e/05_teams_test.go     ├── Coverage matrix completion
  tests/e2e/06_sessions_test.go  │   (master § 11)
  tests/e2e/08_memory_test.go    │
  ...                            │
  tests/e2e/18_rbac_test.go      │   ← critical RBAC matrix
  tests/e2e/19_isolation_test.go │   ← multi-user isolation
  tests/e2e/20_backup_test.go    ┘   ← round-trip integrity

Verification chain:
  Phase 01-13 e2e per-phase tests
   + Phase 14 coverage tests
   = full master § 11 satisfied
   → release gate green
```

## Related Code Files

### Create (~17 test files)

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/03_users_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/04_agents_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/05_teams_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/06_sessions_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/08_memory_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/09_vault_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/10_chat_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/11_websocket_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/13_cron_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/14_hooks_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/15_oauth_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/16_mcp_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/17_secure_cli_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/18_rbac_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/19_isolation_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/20_backup_restore_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/Makefile` — add `test-e2e-full` (full timeout) and `test-e2e-short` (skip LLM)

### Modify

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/Makefile` — release-gate target `test-release-gate: test-e2e-short` (or full); document
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/.github/workflows/ci.yaml` — **add `e2e-fast` job per PR (skip LLM, ~10 min, BLOCK MERGE on fail)** + **`e2e-full` job nightly (real LLM via Bailian + OpenRouter)**. Required per Validation V1 (2026-05-02 17:37). Bootstrap CI scaffold in Phase 01 — Phase 14 wires the full matrix.

### Delete

- None.

## Implementation Steps

1. Verify Phase 13 merged + all earlier phase tests green.
2. Write 17 e2e test files following master § 11 matrix, in 3 batches:
   - **Batch 1** (3 days): 03 users, 04 agents, 05 teams, 06 sessions, 08 memory, 09 vault.
   - **Batch 2** (1.5 days): 10 chat, 11 websocket, 13 cron, 14 hooks, 15 oauth, 16 mcp, 17 secure_cli.
   - **Batch 3** (1.5 days): 18 rbac, 19 isolation, 20 backup_restore.
3. Each test uses helpers from Phase 01 (`SeedUser`, `LoginAs`, `ResetDB`).
4. Run `make test-e2e-short` (skip LLM) — must be green.
5. Run `make test-e2e-full` (with LLM) — must be green when API keys present.
6. Run all earlier phase tests once more — green (regression).
7. Final compile check: `go build ./...` + `go build -tags sqliteonly ./...` + `go vet ./...` clean.
8. Frontend: `pnpm tsc --noEmit && pnpm build` clean.
9. Manual smoke: bootstrap → login → create agent → chat → backup → restore.
10. Tag release: `git tag v4.0.0-rc1` (or per release strategy).

## Todo List

- [x] Batch 1: users, agents, teams, sessions, memory, vault tests
- [x] Batch 2: chat (LLM), websocket, cron, hooks, oauth, mcp, secure_cli
- [x] Batch 3: rbac matrix, isolation, backup/restore round-trip
- [x] Sub-14C gap closure: 12 missing test funcs across 8 files
- [x] `goclaw restore` post-step: `UPDATE user_sessions SET revoked_at = NOW() WHERE revoked_at IS NULL` (PG + SQLite)
- [x] `TestRestoreRevokesAllSessions` written
- [x] Drop checksum equivalence; row-count + key-row spot-check applied
- [x] Makefile `test-e2e-short` + `test-e2e-full` + `test-release-gate`
- [x] CI `e2e-fast` per-PR + `e2e-full` nightly cron
- [x] ADR `2026-05-v4-localstorage-tokens-defer.md` (Finding 5 deferral)
- [x] ADR `2026-05-v4-password-reset-http-defer.md` (Finding 8 deferral)
- [x] `go build` (PG + sqliteonly) + `go vet` clean
- [ ] **Live e2e run on dev pgvector container** (pending — operator-driven)
- [ ] Frontend build clean (verified earlier; re-run before tag)
- [ ] Manual smoke checklist (operator-driven)
- [ ] All e2e tests green on live pgvector → tag `v4.0.0-rc1`

## Success Criteria

- 100% of master § 11 endpoint matrix covered.
- 28 RBAC matrix cells green.
- 9 isolation scenarios green.
- Backup/restore round-trip integrity confirmed.
- LLM real-call green (when keys present).
- Earlier phase tests still green.
- `go build` + `go vet` clean (both build tags).
- `pnpm build` clean.

## Risk Assessment

| Risk | Severity | Mitigation |
|---|---|---|
| Real LLM call rate-limited | Med | Skip in `-short` mode; gate by env var presence |
| RBAC matrix tests flaky on parallel exec | Med | Per-test fresh user fixtures (Phase 01 R5 random suffix) |
| Backup tar.gz format brittle across OS | Low | Test runs on macOS dev + Linux CI; document any divergence |
| Test suite duration > 30min | Med | Parallel test packages where possible; selectively skip LLM in CI |
| Hidden regression discovered late | Med | Full regression run gates merge; rollback plan = revert phase 14 commit only |

## Security Considerations

- e2e tests inject E2E_ROOT_PASSWORD from env — never hardcoded.
- LLM API keys read from env, never logged.
- Backup tar.gz contains DB dump — test cleanup `os.Remove` ensures no lingering artifacts in `/tmp`.
- RBAC tests assert correct denials (negative tests) — security-critical.
- Isolation tests assert no cross-user data leakage.
<!-- RED-TEAM Finding 6 -->
- **Backup/restore session-replay protection:** `goclaw restore` MUST execute `UPDATE user_sessions SET revoked_at = NOW() WHERE revoked_at IS NULL` post-load. Pre-revocation backup → post-revocation restore reactivates stolen refresh tokens otherwise. RFC 6749 §10.4 implication: revocation is not a physical delete, so backup includes them; force re-auth post-restore is the simplest defense.
<!-- /RED-TEAM Finding 6 -->

## Cross-phase Gates

- **Entry:** All Phases 01-13 merged + green.
- **Exit:** All e2e tests green + earlier phase tests still green + manual smoke OK. Gates merge to `main`.

## Next Steps

- Tag release `v4.0.0-rc1` post-validation.
- Beta release via `release-beta.yaml` (tag `v4.0.0-beta.1`).
- Document v4 release notes in `docs/project-changelog.md`.
- v3 → v4 migration tool (EPIC-07) builds on validated v4 baseline.
