---
title: "v4 rc1 Phase B sharing model rebuild — granular share flags + agent_shares role enum + privacy hard rule"
description: "TDD-first split share_workspace/share_memory; agent_shares.role enum (viewer/member/editor) + user|team target; implicit team grant; privacy bypass blocked."
status: pending
priority: P1
effort: 3d
branch: dev-v4
tags: [sharing, permissions, schema, tdd, greenfield, phase-b]
created: 2026-05-04
---

# v4 rc1 Phase B — Sharing Model Rebuild

Greenfield rebuild of agent sharing semantics. Replaces ad-hoc `workspace_sharing JSONB` blob + `agent_shares.role='user'` placeholder with **granular boolean flags** at agent level + **typed role enum** at grant level + **target-mutex** (user xor team) + **implicit team-membership grant**.

## Source-of-truth references
- `plans/reports/audit-260504-1749-workspace-rebuild-v4-d3-final.md` (L24, L25, L30, L31, L42, section B)
- `plans/reports/Explore-260504-1711-workspace-sharing-scout.md` (sharing dimensions A–G)
- `plans/260504-2230-foundation-identity-metadata-tenant-purge/plan.md` (BLOCKING dependency — provides `users.user_key`, `agent_shares.metadata JSONB`)
- `migrations/000001_initial.up.sql` line 100–150 (current `agents.workspace_sharing`, `agent_shares`)
- `internal/store/agent_store.go:339-349` (current WorkspaceSharingConfig blob), `:622-678` (AgentShareData + AgentAccessStore iface)
- `internal/http/agents_sharing.go` (current handlers)

## Tracks
- **A. Flags split (L24).** `agents.share_workspace BOOL`, `agents.share_memory BOOL` separate columns. Drop `agents.workspace_sharing JSONB` blob. Granular UX: a user can share workspace without leaking memory or vice versa.
- **B. agent_shares typed (L25, L42).** Replace `role VARCHAR(20) DEFAULT 'user'` placeholder with `role ∈ {viewer,member,editor}` CHECK enum. Owner role implicit via `agents.owner_id` (no enum value). Add `shared_with_user_id UUID NULL` + `shared_with_team_id UUID NULL` with target-mutex CHECK + `UNIQUE NULLS NOT DISTINCT (agent_id, shared_with_user_id, shared_with_team_id)`.
- **C. Implicit team grant (L30).** Resolver in `internal/permissions/agent_access.go`: explicit `agent_shares` row OR team-membership grant (member of team T → role=`member` on agents shared to T). Reduces UX clicks.
- **D. Privacy hard rule (L31).** `users/{user_key}/` zone INVIOLABLE. Workspace resolver enforces `senderUserID == fileUserID` for per-user file access regardless of agent owner/share role. Bypass-attempt tests are blocking.

## Phases

| # | File | Status | Effort | Depends |
|---|------|--------|--------|---------|
| 01 | [phase-01-tests-share-flags-split.md](phase-01-tests-share-flags-split.md) | completed (2026-05-05) | 3h | foundation #1 |
| 02 | [phase-02-schema-share-flags.md](phase-02-schema-share-flags.md) | completed (2026-05-05) | 2h | 01 |
| 03 | [phase-03-tests-agent-shares-table.md](phase-03-tests-agent-shares-table.md) | completed (2026-05-05) | 3h | 02 |
| 04 | [phase-04-schema-agent-shares.md](phase-04-schema-agent-shares.md) | completed (2026-05-05) | 3h | 03 |
| 05 | [phase-05-implicit-team-grant-resolver.md](phase-05-implicit-team-grant-resolver.md) | completed (2026-05-05) | 4h | 04 |
| 06 | [phase-06-privacy-hard-rule-tests.md](phase-06-privacy-hard-rule-tests.md) | **partial** (2026-05-05 — guard delivered, tool wiring deferred) | 3h | 05 |
| 07 | [phase-07-sqlite-mirror-and-integration.md](phase-07-sqlite-mirror-and-integration.md) | completed (2026-05-05) | 4h | 06 |

## Phase 06 deferral note

`internal/workspace/user_zone_guard.go` is implemented + unit-tested (13
sub-tests covering path extraction edge cases + 5 enforce scenarios). The
guard rejects cross-user-zone access with `ErrUserZoneViolation`, emits
`security.privacy_zone_violation` slog warnings, and is deny-by-default on
unknown user keys.

Tool-layer integration (calling `EnforceUserZoneAccess` from `ReadFileTool` /
`WriteFileTool` / `ListDirTool` after `resolvePathWithAllowed`) is **deferred**
because it requires three side dependencies that exceed this plan's scope:
1. `UserStore.GetByUserKey` + a `permissions.UserKeyResolver` adapter.
2. A tool-layer ctx key carrying the sender's user UUID — current
   `UserIDFromContext` returns a string that may be a channel-style identity
   (e.g. `telegram:386...`), not a UUID.
3. Per-tool integration at every I/O entry point (read, write, list_dir,
   delete, plus glob via vault).

Track follow-up under v4 rc1 → "wire user-zone guard into filesystem tools".
The 8 bypass-attempt integration tests from the original phase plan move
forward with the wiring.

**Total:** ~22h (~3 working days). Target S-size.

## Cross-cutting constraints (HARD)
- **TDD-first.** Each schema/code phase starts with red test → make green → refactor.
- **No load/p95/benchmark tests.**
- **EXCLUDE `ui/desktop/` Wails app** (per audit L136).
- **PG + SQLite parity** verified per phase. SQLite uses `INTEGER` for booleans.
- **Greenfield discipline.** Edit `migrations/000001_initial.up.sql` + `internal/store/sqlitestore/schema.sql` in-place. No new migration version.
- **No plan-references in code** — no "L24", "Phase 03" in comments / migration filenames / test names.
- **Files <200 LOC** per CLAUDE.md.

## Key risks
1. Existing `agents.workspace_sharing JSONB` blob has callers (resolver, loop). Phase 02 must rip + re-route to two booleans, not leave dual paths.
2. `agent_shares.user_id` rename → `shared_with_user_id` ripples through PG store, SQLite store, HTTP handlers, AgentAccessStore iface. Phase 04 grep gate.
3. Privacy bypass tests must be **blocking** — if any path lets owner read other-user zone, do NOT merge. Phase 06.
4. Implicit team grant must compose with explicit grants without duplication or precedence ambiguity. Phase 05 truth-table tests.

## Success criteria (rollup)
- `agents.share_workspace`, `agents.share_memory` BOOL columns present in PG + SQLite. `agents.workspace_sharing` JSONB column REMOVED.
- `agent_shares` shape: `id, agent_id, shared_with_user_id NULL, shared_with_team_id NULL, role CHECK IN (viewer/member/editor), metadata JSONB, created_by, created_at, updated_at` + target-mutex CHECK + unique constraint.
- `internal/permissions/agent_access.go` resolver returns highest-precedence role from union(explicit, implicit-team-membership).
- `tests/integration/sharing_privacy_bypass_test.go` proves: owner of agent A cannot read `users/{otherUser}/...` files via filesystem tool, regardless of agent type / share role.
- `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`, `go test -race -tags integration ./tests/integration/sharing_*` all pass.

## Out of scope
- Project-level grants (`project_grants` PO-B model) — separate plan per audit L26.
- `agent_config_permissions` 4-axis model (audit L79–L82) — separate plan.
- Channel-contact merge / `users.kind` flows — covered by foundation plan + separate channel-merge plan.
- UI surfaces for new sharing toggles — separate UI plan.
