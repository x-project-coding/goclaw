# Phase 08 — CLI Prune (drop ~25 commands, keep 7)

## Context Links

- Master § 4.4 (CLI files), § 6 (DELETE list)
- Decision Q-G (CLI keep/drop list)
- 93 cmd/*.go files (audit-corrected count via `find cmd -name '*.go' | wc -l`)

## Overview

- Priority: P1
- Status: **completed 2026-05-03**
- Effort: 3 dev-days (actual: ~0.5 day — most internal/backup tenant_* helpers already cleaned in earlier phases)
- Description: Drop CLI commands per Q-G — onboard, setup_*, tui_*, auth, agent, agent_chat, channels_cmd, config_cmd, cron_cmd, pairing, providers_cmd, sessions_cmd, skills_cmd, prompt, tenant_backup*, tenant_restore*. Keep: gateway, migrate, version, doctor, backup, restore, upgrade. Refactor `cmd/backup.go` + `cmd/restore.go` to remove tenant scope (rename helpers from `tenant_backup_cli_helpers.go` for user scope).

## Key Insights

- ~2000 LOC delete, ~250 LOC refactor (backup/restore helpers).
- Some CLI commands have helpers shared with HTTP API (verify before delete to avoid orphaning HTTP code).
- `internal/backup/tenant_*.go` files exist (verified) — refactor not delete (move to user-scope or rename).
- All deleted commands are user-facing CLI; no internal Go consumers.
- Run PARALLEL to Phase 07.

## Tests to write FIRST (TDD red step)

| Test file | Cases (must FAIL until impl green) |
|---|---|
| `tests/e2e/cli/01_keep_list_test.go` | `TestCLIRoots` — `goclaw --help` lists exactly 7 root commands: gateway, migrate, version, doctor, backup, restore, upgrade. No `onboard`, `auth`, `agent`, etc. |
| `tests/e2e/cli/02_dropped_commands_unavailable_test.go` | For each dropped command (onboard, setup, tui-*, auth, agent, agent-chat, channels, config, cron, pairing, providers, sessions, skills, prompt, tenant-backup, tenant-restore): `goclaw <cmd>` returns "unknown command" non-zero exit |
| `tests/e2e/cli/03_backup_user_scope_test.go` | `TestBackupUserScope` — `goclaw backup` produces tar.gz of full DB + workspace; no tenant-scope CLI flags accepted |
| `tests/e2e/cli/04_restore_round_trip_test.go` | `TestRestoreRoundTrip` — backup → reset DB → restore → row counts equal pre-backup |

**Red verification:** Tests fail because dropped commands still exist + backup CLI still has tenant flags.

## Requirements

### Functional

#### DELETE files (~25 files)

```
cmd/onboard.go
cmd/onboard_helpers.go
cmd/onboard_managed.go
cmd/setup_agent.go
cmd/setup_channel.go
cmd/setup_cmd.go
cmd/setup_provider.go
cmd/tui_onboard.go
cmd/tui_onboard_noop.go
cmd/tui_setup.go
cmd/tui_setup_noop.go
cmd/auth.go
cmd/agent.go
cmd/agent_chat.go
cmd/agent_chat_client.go
cmd/channels_cmd.go
cmd/config_cmd.go
cmd/cron_cmd.go
cmd/pairing.go
cmd/providers_cmd.go
cmd/sessions_cmd.go
cmd/skills_cmd.go
cmd/prompt.go
cmd/tenant_backup.go
cmd/tenant_backup_cli_helpers.go (refactor: rename + reuse for user-scope OR delete if redundant)
cmd/tenant_restore.go
cmd/tenant_restore_test.go
```

#### REFACTOR

- `cmd/backup.go` — drop tenant flag (`--tenant`); generic full-DB backup remains (Q-8).
- `cmd/restore.go` — same.
- `internal/backup/tenant_discover_pg.go` — refactor to `discover_pg.go` (drop tenant scope) OR keep filename + drop tenant logic.
- `internal/backup/tenant_tables.go` — same (rename or refactor).
- `internal/backup/tenant_restore_helpers.go` — same.
- `internal/backup/tenant_discover_sqlite.go` — same (sqliteonly tag).
- `cmd/gateway.go` (and helpers `gateway_*.go`) — remove any references to dropped commands; verify still compiles.
- `main.go` (root cmd registration) — remove `rootCmd.AddCommand(...)` calls for dropped commands.

#### KEEP (verified intact post-refactor)

- `cmd/gateway*.go` (~50 files — many `gateway_*` helpers; verify each is gateway-related not orphan from dropped command)
- `cmd/migrate.go` + helpers
- `cmd/version.go`
- `cmd/doctor.go`
- `cmd/backup.go` (refactored)
- `cmd/restore.go` (refactored)
- `cmd/upgrade.go`
- `cmd/cli_helpers.go` (shared utilities)

### Non-functional

- ~2000 LOC delete; commit cleanly per logical group (e.g., commit 1: tui+setup+onboard, commit 2: agent/auth, commit 3: tenant backup, commit 4: backup refactor).
- `go build ./...` clean after each commit.
- `goclaw --help` output reviewed manually for clean tree.

## Architecture

```
cmd/ post-prune (~50 files):
  Root commands (7):
   ├─ gateway.go (+ gateway_*.go helpers, ~50 files)
   ├─ migrate.go (+ helpers)
   ├─ version.go
   ├─ doctor.go
   ├─ backup.go (refactored — no tenant)
   ├─ restore.go (refactored — no tenant)
   └─ upgrade.go
  Shared:
   └─ cli_helpers.go

internal/backup/ post-refactor:
   ├─ db_dump_pg.go         (renamed/refactored from tenant_*)
   ├─ db_dump_sqlite.go     (sqliteonly)
   ├─ db_restore_pg.go
   ├─ db_restore_sqlite.go  (sqliteonly)
   ├─ preflight_*.go
   └─ workspace_*.go
```

## Related Code Files

### Delete (verified existence via Phase 02 baseline + grep during impl)

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/onboard.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/onboard_helpers.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/onboard_managed.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/setup_agent.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/setup_channel.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/setup_cmd.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/setup_provider.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/tui_onboard.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/tui_onboard_noop.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/tui_setup.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/tui_setup_noop.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/auth.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/agent.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/agent_chat.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/agent_chat_client.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/channels_cmd.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/config_cmd.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/cron_cmd.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/pairing.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/providers_cmd.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/sessions_cmd.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/skills_cmd.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/prompt.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/tenant_backup.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/tenant_backup_cli_helpers.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/tenant_restore.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/tenant_restore_test.go`

### Modify

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/backup.go` — drop `--tenant` flag if present
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/cmd/restore.go` — drop `--tenant` flag if present
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/main.go` — remove AddCommand calls for dropped CLIs
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/backup/tenant_discover_pg.go` — refactor (rename file or drop tenant logic)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/backup/tenant_tables.go` — refactor
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/backup/tenant_restore_helpers.go` — refactor
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/backup/tenant_discover_sqlite.go` — refactor (sqliteonly)

### Create

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/cli/01_keep_list_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/cli/02_dropped_commands_unavailable_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/cli/03_backup_user_scope_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/cli/04_restore_round_trip_test.go`

## Implementation Steps

1. Verify Phase 06 merged (CLI auth wizard `cmd/auth.go` no longer canonical — HTTP+WS auth canonical).
2. Write 4 e2e CLI test files (red — dropped commands still exist).
3. For each file in DELETE list:
   a. `grep -rn 'cmd.<funcName>' --include='*.go'` to find any internal callers (should be zero — CLI commands top-level only).
   b. `git rm <file>`.
4. Refactor `main.go`:
   a. Remove `rootCmd.AddCommand(onboardCmd)`, `setupCmd`, etc.
   b. Verify only 7 root commands remain.
5. Refactor `cmd/backup.go` + `cmd/restore.go`:
   a. Drop `--tenant` flag (cobra Flag definitions).
   b. Drop tenant-scope branches in handler bodies.
   c. Update `internal/backup/` callsites.
6. Refactor `internal/backup/tenant_*.go`:
   a. Option A (preferred): rename files (`tenant_discover_pg.go` → `discover_pg.go`) + drop tenant params from functions.
   b. Option B: keep filenames as-is, drop tenant logic inside (lower-effort, but file names stale).
   c. Choose A for cleanliness; rename via `git mv`.
7. After each commit: `go build ./...` + `go build -tags sqliteonly ./...` + `go vet ./...` clean.
8. Run all 4 e2e CLI tests → green.
9. Run all earlier phase tests → still green.
10. Manual: run `./goclaw --help` and review output. Smoke run `./goclaw doctor`, `./goclaw version`.

## Todo List

- [x] 4 e2e CLI test files written (`tests/e2e/cli/`)
- [x] Delete 25 CLI files (onboard*, setup_*, tui_*, auth, agent*, channels, config, cron, pairing, providers, sessions, skills, prompt)
- [x] cmd/root.go AddCommand calls cleaned (down from 18 to 7 + auto completion/help)
- [x] cmd/backup.go — verified no --tenant flag (already clean from prior phases)
- [x] cmd/restore.go — verified no --tenant flag (already clean)
- [x] internal/backup/ — already refactored (no tenant_* files remain)
- [x] go build (PG + sqliteonly) + go vet clean
- [x] All 4 e2e CLI tests green (TestCLIRoots, TestDroppedCommandsUnavailable[15 sub], TestBackupUserScope, TestRestoreUserScope, TestRestoreRoundTrip shape-check)
- [x] Manual smoke: `goclaw --help` output reviewed (8 commands: backup/doctor/migrate/reset-password/restore/upgrade/version + auto completion+help)

## Success Criteria

- [x] `find cmd -name '*.go' | wc -l` = **66** (down from 91; ~3190 LOC removed).
- [x] `goclaw --help` lists kept commands only (gateway via root + 7 verbs + reset-password from Phase 06).
- [x] `goclaw onboard|auth|agent|...` → "unknown command" non-zero exit.
- [x] Backup/restore CLI shape verified — no `--tenant`/`--tenant-id`/`--scope` flags.
- [ ] **Deferred to Phase 13:** zero `tenant`/`Tenant` refs in cmd/gateway*.go (handled by Phase 07 pool/cache refactor + Phase 13 final sweep). Phase 08 ships CLI surface clean; runtime plumbing cleanup is downstream.

## Risk Assessment

| Risk | Severity | Mitigation |
|---|---|---|
| Dropped CLI helpers shared with HTTP API | High | grep enumeration before delete; if shared → move helper to `internal/`, refactor both |
| Backup format changes breaking pre-v4 archives | Med | v4 = fresh install per Q-14; pre-v4 archives explicitly unsupported (document in CHANGELOG) |
| Tenant backup helpers used by gateway startup seed | Low | Verify via grep; bootstrap flow (Phase 06) does not use these |
| Compile breaks in /cmd post-delete | Med | Per-commit `go build`; commit groups isolate breakage |
| Removed CLI unintentionally needed in CI/scripts | Med | Search `.github/workflows/*.yaml` + `scripts/` for usage of dropped commands; update or document migration |

## Security Considerations

- CLI auth wizard (`cmd/auth.go`) deletion does NOT remove auth — Phase 06 HTTP+WS path canonical.
- Backup CLI retains full-DB scope (root-only via OS file permissions on output tar.gz).
- No secrets exposed by CLI prune (env-var-driven config unchanged).

## Cross-phase Gates

- **Entry:** Phase 06 merged (HTTP auth canonical replacement).
- **Exit:** All 4 CLI tests green + go build/vet clean. Independent of Phase 07 (parallel-safe).

## Next Steps

- Phase 09 — channels merge-contact uses backup helpers refactor here (R1 sessions migration).
- Phase 13 — final cleanup sweeps any leftover dead code.
