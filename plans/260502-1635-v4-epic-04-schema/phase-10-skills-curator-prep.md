# Phase 10 — Skills + skill_versions + curator_runs (S9 prep)

## Context Links

- Master § 4.7 (Skills deep-dive)
- Decisions Q-2 (drop is_system), Q9 (source enum), Q10 (skill_versions archive)
- Phase 03+04 schema (skills + skill_versions + curator_runs created)
- Phase 05 PR-05A (skill_versions + curator_runs stores created)

## Overview

- Priority: P1
- Status: completed 2026-05-03
- Effort: 6 dev-days
- Description: Refactor `skills` business logic — drop `is_system` flag (Q-2), populate `source` enum (Q9: 5 values). Add sidecar columns wiring (last_used_at, last_viewed_at, last_patched_at, pinned, usage_count). Wire `skill_versions` audit trail (Q10 archive semantics). Wire `curator_runs` state machine for EPIC-06 S9 prep.

## Key Insights

- v3 skills store: 268 LOC (Phase 02 baseline).
- Q-2: DROP `skills.is_system BOOLEAN`. Use `source='builtin'` from Q9 enum instead. Schema already enforces (Phase 03+04).
- Q9: `source` enum: `builtin | hub-verified | hub-unverified | agent-created | user-uploaded`.
- Q10 archive: `archived_at TIMESTAMPTZ NULL`, `archive_path TEXT NULL`, content empty when archived.
- Sidecar columns existed in v3 — verify wired (last_used_at increments on tool use, etc.).
- `curator_runs` state machine — minimal scaffold for EPIC-06 S9 (don't fully implement S9 logic; just CRUD + state transitions).
- Phase 10 can run AFTER 09 OR parallel with 11.

## Tests to write FIRST (TDD red step)

| Test file | Cases (must FAIL until impl green) |
|---|---|
| `tests/e2e/07_skills_test.go` | `TestSkillsCRUD` — create/list/update/delete skills via HTTP. `TestSkillSourceEnum` — accepts 5 enum values; rejects invalid (e.g., "system" rejected post-Q-2). `TestSkillsNoIsSystemColumn` — schema does NOT have `is_system` column |
| `tests/e2e/07_skill_versions_test.go` | `TestVersionCreated` — `POST /v1/skills/:id/versions` creates version row. `TestVersionListExcludesArchived` — list active excludes archived. `TestVersionArchive` — archive sets `archived_at + archive_path` + clears `content` (empty). `TestVersionContentImmutable` — non-archived version content can't be edited |
| `tests/e2e/07_skill_grants_test.go` | `TestSkillAgentGrant` — grant skill to agent. `TestSkillUserGrant` — grant skill to user. `TestSkillGrantsRBAC` — viewer can read grants, member can grant, admin can revoke |
| `tests/e2e/07_skill_sidecar_metadata_test.go` | `TestLastUsedAtIncrements` — invoking skill via tool updates `last_used_at`. `TestUsageCountIncrements` — usage_count increments per use. `TestPinnedSurvivesUpdates` — pinned bool persists across edits |
| `tests/e2e/07_curator_runs_test.go` | `TestCuratorRunStart` — `POST /v1/skills/:id/curator-runs` creates run with state='running'. `TestCuratorEvent` — append event. `TestCuratorComplete` — state='running' → 'completed'; invalid transition (e.g., 'completed'→'running') rejected |
| `tests/e2e/07_skill_export_import_test.go` | `TestSkillExportNoTenant` — exported skill bundle no tenant_id field |

**Red verification:** Tests fail because curator_runs HTTP API not wired, skill source enum not enforced at handler, etc.

## Requirements

### Functional

#### Skills business logic refactor

- `internal/http/skills.go` — handler layer:
  - Validate `source` enum on create/update (5 values from Q9).
  - Reject `is_system` field in payloads (return 400 if present).
  - Sidecar columns: `last_used_at` updated on tool invocation (via `internal/tools/skills.go` or similar), `last_viewed_at` on detail view, `usage_count` on use, `pinned` toggleable.
- `internal/store/pg/skills.go` + SQLite mirror:
  - SELECT/UPDATE statements drop `is_system`.
  - Insert `source` enum value (default `'user-uploaded'`).
  - Sidecar update helpers: `MarkUsed(skillID)`, `MarkViewed(skillID)`, `Pin(skillID, bool)`.

#### Skill versions wiring (Q10)

- `internal/http/skills_versions.go` — NEW handler:
  - `POST /v1/skills/:id/versions` — create version (snapshot content + version metadata).
  - `GET /v1/skills/:id/versions` — list (filter active vs archived).
  - `POST /v1/skills/:id/versions/:vid/archive` — set `archived_at` + `archive_path` + clear `content`.
- Store layer (P05 PR-05A already created `skill_versions_store.go` interfaces); wire HTTP layer here.

#### Curator runs scaffold (S9 prep)

- `internal/http/curator_runs.go` — NEW handler:
  - `POST /v1/skills/:id/curator-runs` — start run.
  - `POST /v1/curator-runs/:rid/events` — append event JSON.
  - `POST /v1/curator-runs/:rid/complete` — state transition to 'completed'.
  - `GET /v1/skills/:id/curator-runs` — list runs.
- State machine: `running → completed | failed`. No other transitions.
- Store layer (P05 PR-05A already): `curator_runs_store.go`.

#### i18n keys (BEFORE handlers)

In `internal/i18n/keys.go`:
- `MsgInvalidSkillSource = "error.invalid_skill_source"`
- `MsgIsSystemDeprecated = "error.is_system_deprecated"`
- `MsgVersionAlreadyArchived = "error.version_already_archived"`
- `MsgCuratorInvalidTransition = "error.curator_invalid_transition"`

In all 3 catalogs.

### Non-functional

- File size: each new handler file ≤ 200 LOC.
- HTTP handlers reuse middleware from Phase 06 (JWT + role gates).
- Tests gated `//go:build e2e`.

## Architecture

```
Skills v4 layout:
  HTTP layer:
   ├─ internal/http/skills.go          (CRUD + grants — refactor)
   ├─ internal/http/skill_versions.go  (NEW — version CRUD + archive)
   └─ internal/http/curator_runs.go    (NEW — S9 prep CRUD)
  Store layer (P05 PR-05A already):
   ├─ internal/store/skill_versions_store.go
   ├─ internal/store/curator_runs_store.go
   ├─ internal/store/pg/skills.go              (refactor — drop is_system)
   ├─ internal/store/pg/skill_versions.go
   └─ internal/store/pg/curator_runs.go
  Sidecar update wiring:
   └─ internal/tools/skills.go (or similar) — emit Mark* events when tool invoked

Curator state machine (S9 prep):
  running ──► completed
         └──► failed
  (no other transitions)
```

## Related Code Files

### Modify

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/skills.go` (drop is_system, source enum validation, sidecar wiring)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/pg/skills.go` (Phase 05 already drops tenant_id; here wire sidecar updates)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/store/sqlitestore/skills.go` (mirror)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/i18n/keys.go` (4 new keys)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/i18n/catalog_{en,vi,zh}.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/server.go` (route registration for new handlers)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/tools/skills.go` (verify path; wire `MarkUsed` on tool invocation)

### Create

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/skill_versions.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/skill_versions_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/curator_runs.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/curator_runs_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/07_skills_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/07_skill_versions_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/07_skill_grants_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/07_skill_sidecar_metadata_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/07_curator_runs_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/07_skill_export_import_test.go`

### Delete

- None (refactor in-place).

## Implementation Steps

1. Verify Phase 05 PR-05A merged (skill_versions + curator_runs stores ready).
2. Verify Phase 03+04 schemas have `skills.source` enum + no `is_system` column.
3. Add 4 i18n keys + 3 catalog entries each (12 entries total).
4. Write 6 e2e skills test files (red).
5. Refactor `internal/http/skills.go`:
   a. Validate `source` enum on create/update.
   b. Reject `is_system` field if present in payload (return 400 + MsgIsSystemDeprecated).
   c. Wire sidecar column updates.
6. Write `internal/http/skill_versions.go`:
   a. `POST /v1/skills/:id/versions` — snapshot content.
   b. `GET /v1/skills/:id/versions` — list with `?archived=true` filter.
   c. `POST /v1/skills/:id/versions/:vid/archive` — set archive metadata + clear content.
7. Write `internal/http/curator_runs.go`:
   a. `POST /v1/skills/:id/curator-runs` — Start.
   b. `POST /v1/curator-runs/:rid/events` — Append event JSON.
   c. `POST /v1/curator-runs/:rid/complete` — Transition to completed.
   d. State machine validation (reject invalid transitions with MsgCuratorInvalidTransition).
   e. `GET /v1/skills/:id/curator-runs` — List.
8. Wire routes in `internal/http/server.go`.
9. Wire sidecar updates: locate skill tool invocation path (`internal/tools/skills.go`) and call `MarkUsed` post-invocation.
10. `go build ./...` + `go build -tags sqliteonly ./...` + `go vet ./...` clean.
11. Run all 6 e2e skills tests → green.
12. Earlier phase tests still green.

## Todo List

- [x] 4 i18n keys + 12 catalog entries (en/vi/zh)
- [x] Schema (PG + SQLite): drop `is_system`, add `source` CHECK enum + sidecar cols + skill_versions archive cols + curator_runs status CHECK + new `curator_events` table
- [x] Store layer refactor: `IsSystem` → `Source`; sidecar helpers (`MarkSkillUsed/MarkSkillViewed/PinSkill`); `Archive(id, skillID, archivePath)` with cross-skill guard; new `CuratorEventsStore` (PG + SQLite)
- [x] 6 store-layer e2e tests under `tests/e2e/stores/` (HTTP-level deferred to Phase 14 per `helpers.LoginAs` skip)
- [x] internal/http/skills.go refactored (source enum at HTTP + DB CHECK; reject `is_system` payload; reject server-only `source ∈ {builtin, agent-created}` from update path)
- [x] internal/http/skills_versions_archive.go — archive endpoint with skill_id guard
- [x] internal/http/curator_runs.go (CRUD + state machine + events)
- [x] Sidecar wiring (MarkSkillUsed on `use_skill` tool activation)
- [x] Routes wired in server.go + cmd/gateway_http_*.go
- [x] go build (PG + sqliteonly) + go vet clean
- [x] All 6 Phase 10 e2e tests green
- [x] Code-review pass (score 7.5 → C1+C2+H1+H3 fixed)

### Pre-existing failures (NOT Phase 10 regressions, separate ticket)

- `TestHandleUpload_AutoInstallsMissingDepsAndKeepsSkillActive`
- `TestRequireMasterScope_NilTenant_Allows`
- `TestAPIKeyRevoke_AllowsOwnTenantKey`

Root cause: Phase 06 role rename (`IsOwnerRole` → `IsRootRole`) + master-scope hotfix work. Untouched by Phase 10.

## Success Criteria

- 6 e2e skills tests green.
- `skills.source` enum enforced at handler layer.
- `is_system` rejected with 400 + i18n message.
- skill_versions archive: content cleared, archived_at + archive_path set, list excludes archived by default.
- curator_runs: state transitions enforced (running → completed/failed only).
- Sidecar metadata increments on tool use.

## Risk Assessment

| Risk | Severity | Mitigation |
|---|---|---|
| Existing v3 skills with is_system payloads break upgrade | Low | v4 = fresh install (Q-14); no upgrade path |
| Sidecar updates race-conditioned on hot skills | Low | Use atomic UPDATE ... SET usage_count = usage_count + 1; PG handles concurrency |
| Archive path collision on shared filesystem | Med | Path = `archives/skills/{skillID}/{versionID}/{timestamp}.tar.gz` — UUID + timestamp uniqueness |
| curator_runs state machine over-engineered for v4 | Med | Minimal scaffold per phase scope; full S9 logic deferred to EPIC-06 |
| skill_versions content blob too large for inline column | Low | Store reference (path) for archived; live versions store content inline (existing v3 pattern) |

## Security Considerations

- Skill creation requires `RoleMember+` (RBAC matrix from Phase 06).
- `agent-created` source: only set by agent context (server-side); user payload cannot claim this source.
- `builtin` source: only set during seed (Phase 06 bootstrap creates root + seeds builtin skills).
- Archive path traversal protection: validate path stays under workspace root (existing helper).
- Curator run input sanitized (JSON event payload validated against schema).

## Cross-phase Gates

- **Entry:** Phase 05 PR-05A merged (skill_versions + curator_runs stores).
- **Exit:** All 6 skills tests green + go build/vet clean. Gates Phase 14 final validation.

## Next Steps

- Phase 11 — FE skills page consumes new HTTP API.
- EPIC-06 (separate epic) — curator full S9 logic builds on this scaffold.
