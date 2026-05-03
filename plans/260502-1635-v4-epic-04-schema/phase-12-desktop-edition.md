# Phase 12 — Desktop Edition (sqliteonly) First-Run Setup [DEFERRED → EPIC-05-desktop]

> **Validation V2 (2026-05-02 17:37): DEFERRED.** Moved out of EPIC-04 scope. EPIC-04 ends at Phase 11 (web frontend). Reason: schema-foundation EPIC should not block on Wails build matrix issues; desktop is its own concern. Content preserved as starting point for EPIC-05-desktop plan.

## Context Links

- Master § 4.6 (Edition), § 10 LOG-1 resolution (option c — first-run setup form)
- Decisions Q-9 (no recurring login), Q-11 (LOG-1 — first-run setup CÓ)
- Phase 04 (SQLite schema)
- Phase 06 (auth — bootstrap endpoints)
- Phase 11 (FE bootstrap form — desktop reuses)
- 8 sqliteonly build-tag files verified live

## Overview

- Priority: P1
- Status: pending
- Effort: 6 dev-days
- Description: Wire desktop (Wails v2 + sqliteonly) first-run setup form. Reuse Phase 11 `/bootstrap` page in Wails frontend. Single-user local-only (no recurring login per Q-9). Auto-bind to root user post-setup. Edition struct cleanup (drop tenant gating, keep Lite limits per CLAUDE.md). 5 ui/desktop cosmetic tenant refs cleaned.

## Key Insights

- Q-9 audit-clarified: NO recurring login challenge, but DOES have first-run setup form (LOG-1 option c).
- Existing 8 sqliteonly build-tag files (verified live):
  - `cmd/gateway_stores_sqliteonly.go`
  - `internal/backup/{preflight,db_dump,db_restore,tenant_discover}_sqlite.go`
  - `ui/desktop/{main,keyring,app}.go`
- Wails frontend at `ui/desktop/frontend/src/` (verified). Pages may differ from `ui/web/`; share auth module via copy or symlink/workspace.
- Lite edition limits per CLAUDE.md: 5 agents, 1 team, 5 members, 50 sessions. NO channels, heartbeat, file storage UI, skill self-manage, KG, RBAC, multi-tenant.
- Desktop port 18790 (localhost only).
- Auto-update via `internal/updater/updater.go` (Lite edition feature; preserve).

## Tests to write FIRST (TDD red step)

| Test file | Cases (must FAIL until impl green) |
|---|---|
| `tests/e2e/12_desktop_first_run_test.go` | (build-tag `e2e_desktop`) — `TestFirstRunDetectsZeroUsers` — Wails app on launch with empty SQLite shows bootstrap form. `TestFirstRunCreatesRoot` — submit creates user role=root. `TestSubsequentRunsAutoLogin` — second launch with users>0 auto-logs in (no login prompt) |
| `tests/e2e/12_desktop_lite_limits_test.go` | `TestLiteAgentLimit` — creating 6th agent rejected with edition limit error. `TestLiteSingleTeam` — second team creation rejected. `TestLiteNoChannels` — channel manage UI not rendered |
| `tests/e2e/12_desktop_no_tenant_refs_test.go` | Static AST scan: `grep -rn 'tenant\|Tenant' ui/desktop/frontend/src/` returns 0 in identifiers |
| `tests/e2e/12_desktop_build_tag_test.go` | `TestSqliteOnlyBuild` — `go build -tags sqliteonly ./...` clean (smoke). `TestNoPgImportsInSqliteOnly` — files compiled with sqliteonly tag don't import `internal/store/pg` |

**Red verification:** Tests fail because first-run flow not wired in Wails app.

## Requirements

### Functional

#### Desktop bootstrap flow

- `ui/desktop/main.go` startup:
  - Initialize SQLite at `~/.goclaw/data/`.
  - Apply schema (Phase 04).
  - Embedded gateway starts on port 18790.
  - Frontend loads.
- `ui/desktop/frontend/src/App.tsx`:
  - On mount → call `GET /v1/bootstrap/status`.
  - If `bootstrapped:false` → render `<BootstrapForm />` (reuse from `ui/web/src/pages/bootstrap/`).
  - If `bootstrapped:true` → auto-login via stored token in OS keyring; render `<Dashboard />`.
- Single-user mode:
  - After bootstrap, JWT token stored in OS keyring (`go-keyring`).
  - On subsequent launches: read token, call `/v1/auth/me`; if valid → mount dashboard. If expired → silent refresh via stored refresh token.
  - NO login prompt unless refresh fails (then prompt for password — fallback only).

#### Edition struct cleanup

- `internal/edition/edition.go`:
  - Drop tenant-related fields (multi-tenant flag, tenant gating).
  - Keep: `Name`, `MaxAgents`, `MaxTeams`, `MaxMembers`, `MaxSessions`, `RBACEnabled`, etc. (verify field names live).
  - `edition.Current()` returns `Lite` when SQLite backend detected.
- Tool gating in `internal/tools/team_action_policy.go` already exists per CLAUDE.md — verify intact (no work needed unless tenant ref present).

#### Frontend reuse pattern

Two options (pick during impl):

- **Option A (preferred):** pnpm workspace — `ui/web/src/auth/*` and `ui/web/src/pages/bootstrap/*` shared via package alias. Smaller bundle, single source of truth.
- **Option B:** copy bootstrap form + auth module into `ui/desktop/frontend/src/`. More duplication but build-isolated.

Recommend Option B initially (KISS). Refactor to Option A if duplication becomes painful.

#### NEW i18n keys for desktop

Reuse `ui/web/` `auth.json` namespace if Option A chosen; otherwise copy file into `ui/desktop/frontend/src/i18n/`.

### Non-functional

- Build: `make desktop-build VERSION=0.1.0-dev` succeeds on macOS + Windows.
- Bundle size impact: < 100KB increase from auth module.
- First-run flow < 5s.
- OS keyring access: graceful fallback to file `~/.goclaw/secrets/` per CLAUDE.md.

## Architecture

```
Desktop launch flow:
  1. Wails main → embedded gateway starts on :18790
  2. Frontend loads → GET /v1/bootstrap/status
     ├─ bootstrapped:false → BootstrapForm
     │   └─ submit → POST /v1/bootstrap/init → store tokens in OS keyring → Dashboard
     └─ bootstrapped:true → check OS keyring for tokens
         ├─ found + valid (via /me) → Dashboard
         ├─ expired access → POST /v1/auth/refresh → Dashboard
         └─ refresh failed → LoginForm (rare; user changed password)

Edition gating:
  edition.Current() = Lite (SQLite backend)
   ├─ MaxAgents=5  → handler rejects 6th create
   ├─ MaxTeams=1   → handler rejects 2nd create
   ├─ Channels disabled → routes return 404
   └─ RBAC simplified → only root user
```

## Related Code Files

### Modify

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/desktop/main.go` — first-run setup detection
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/desktop/app.go` — Wails methods for bootstrap status
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/desktop/frontend/src/App.tsx` — bootstrap form rendering
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/desktop/keyring.go` — store/retrieve JWT + refresh
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/edition/edition.go` — drop tenant fields, keep Lite limits

### Create

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/desktop/frontend/src/pages/bootstrap/index.tsx` (Option B: copy from ui/web)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/desktop/frontend/src/auth/auth-context.tsx` (Option B: copy)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/desktop/frontend/src/auth/keyring-bridge.ts` (Wails bridge to OS keyring)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/12_desktop_first_run_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/12_desktop_lite_limits_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/12_desktop_no_tenant_refs_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/12_desktop_build_tag_test.go`

### Delete

- ui/desktop tenant cosmetic refs: 5 files per master § 4.11 (verify during impl which files; likely const refs to `system` userId)

## Implementation Steps

1. Verify Phase 11 merged (web bootstrap form + auth module reference).
2. Verify Phase 04 + 06 merged (SQLite schema + auth endpoints work in sqliteonly build).
3. Write 4 desktop e2e test files (red).
4. Refactor `internal/edition/edition.go`:
   a. Drop tenant fields.
   b. Keep Lite limits constants.
   c. Verify `edition.Current()` returns `Lite` for SQLite backend.
5. Wails frontend setup:
   a. Decide Option A vs B (recommend B for KISS).
   b. Copy `ui/web/src/auth/` → `ui/desktop/frontend/src/auth/`.
   c. Copy `ui/web/src/pages/bootstrap/` → `ui/desktop/frontend/src/pages/bootstrap/`.
   d. Adapt API base URL to `http://localhost:18790`.
6. Add Wails keyring bridge:
   a. `ui/desktop/keyring.go` already exists — extend with `StoreToken(key, value)`, `RetrieveToken(key)`.
   b. `ui/desktop/frontend/src/auth/keyring-bridge.ts` — call Wails method via `window.go.app.StoreToken(...)`.
7. Update `ui/desktop/frontend/src/App.tsx`:
   a. On mount: call `GET /v1/bootstrap/status` via local API.
   b. Branch: bootstrap form vs auto-login from keyring.
8. Update `ui/desktop/main.go` first-run detection (defer to frontend; backend just exposes API).
9. Clean 5 cosmetic tenant refs in `ui/desktop/frontend/src/`:
   a. `grep -rn 'tenant' ui/desktop/frontend/src/` enumerate.
   b. For each: drop ref, swap hardcoded `userId='system'` to actual user UUID from auth context.
10. `go build -tags sqliteonly ./...` + `go vet -tags sqliteonly ./...` clean.
11. `make desktop-build VERSION=0.1.0-dev` smoke build.
12. Run all 4 desktop e2e tests → green.
13. Manual: launch desktop binary → verify first-run form appears → submit → verify dashboard → relaunch → verify auto-login.

## Todo List

- [ ] 4 desktop e2e tests written (red)
- [ ] internal/edition/edition.go cleanup
- [ ] Decision: Option A vs B (frontend reuse strategy)
- [ ] Copy/share auth module to ui/desktop/frontend
- [ ] Copy/share bootstrap page to ui/desktop/frontend
- [ ] Wails keyring bridge (Go side)
- [ ] Wails keyring bridge (TS side)
- [ ] App.tsx bootstrap detection + auto-login
- [ ] 5 cosmetic tenant refs in ui/desktop/ cleaned
- [ ] go build -tags sqliteonly + go vet clean
- [ ] make desktop-build smoke test passes
- [ ] All 4 desktop e2e tests green
- [ ] Manual launch + first-run + relaunch test

## Success Criteria

- 4 desktop e2e tests green.
- `make desktop-build VERSION=0.1.0-dev` produces working .app/.exe.
- First-run launches show bootstrap form.
- Subsequent launches auto-login from keyring.
- Lite limits enforced (5 agents, 1 team, no channels).
- 0 tenant refs in `ui/desktop/frontend/src/` (excluding tests).
- `go build -tags sqliteonly ./...` clean.

## Risk Assessment

| Risk | Severity | Mitigation |
|---|---|---|
| OS keyring access denied (no user permission) | High | Fallback to `~/.goclaw/secrets/` file with 0600 perms; document in startup log |
| Wails frontend build fails on Windows | Med | CI matrix already exists per CLAUDE.md release-desktop.yaml; smoke test in CI |
| FE code duplication via Option B drifts | Med | Defer Option A migration to v4.x backlog; document divergence |
| Lite limits enforced inconsistently | Med | Server-side limit checks in handler; FE displays graceful error |
| First-run race (frontend bootstraps before backend ready) | Med | Frontend retries `/v1/bootstrap/status` with backoff (3s × 5 tries) |
| Auto-update breaks post-v4 (lite-v* tag) | Low | Existing `internal/updater/updater.go` works; just verify no tenant refs in update payload |

## Security Considerations

- OS keyring preferred (encrypted at rest by OS); file fallback uses 0600 perms.
- JWT token leaked via debugger — mitigated by short access TTL (15min); refresh token can be rotated.
- Single-user mode: auto-login means anyone with physical machine access can use app — document for users.
- Embedded gateway listens on 127.0.0.1 only (not 0.0.0.0) per CLAUDE.md.
- No remote auth in Lite — paired devices only (existing pattern).

## Cross-phase Gates

- **Entry:** Phase 11 merged (FE auth module + bootstrap page exist to copy/share).
- **Exit:** Desktop e2e tests green + smoke build passes. Gates Phase 14 final.

## Next Steps

- Phase 13 — final cleanup of any remaining tenant refs project-wide.
- Phase 14 — full validation suite covers desktop scenarios.
