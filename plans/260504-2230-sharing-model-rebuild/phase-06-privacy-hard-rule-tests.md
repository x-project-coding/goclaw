# Phase 06 — Privacy hard rule (TDD blocking)

> **PARTIAL — guard delivered, tool-layer wiring deferred (2026-05-05).**
> The privacy hard-rule guard `internal/workspace/user_zone_guard.go` is
> implemented and unit-tested (13 cases — extraction edge cases + 5 enforce
> scenarios). Wiring into `internal/tools/filesystem.go` (ReadFile +
> WriteFile + ListDir) was deferred because it requires three side
> dependencies that are out-of-scope for this plan: (a) `UserStore.GetByUserKey`
> + a `permissions.UserKeyResolver` adapter, (b) a tool-layer ctx key
> propagating the sender's user UUID (current `UserIDFromContext` returns a
> string that may be a channel-style ID, not a UUID), (c) per-tool integration
> at every I/O entry point. Track follow-up under v4 rc1 → "wire user-zone
> guard into filesystem tools".

## Context Links
- Audit L31: `users/{user_key}/` zone INVIOLABLE — owner of an agent CANNOT read other users' user-zones.
- Phase 05 resolver returns role; this phase enforces a separate orthogonal rule: per-user file zone access requires `senderUserID == fileUserID`, regardless of agent role.
- Workspace resolver: `internal/workspace/resolver_impl.go:61-117`. Filesystem boundary check: `internal/tools/filesystem.go:367-399`.

## Overview
- **Priority:** P1 (BLOCKING merge)
- **Status:** pending
- **Depends on:** Phase 05 green
- **Effort:** 3h
- **Description:** Add bypass-attempt tests against `users/{user_key}/` zone. Tests assert that even with role=editor, owner role, or shared workspace flag enabled, an agent operating as user A cannot read or write into `users/{user_B_key}/...`. Implement enforcement in workspace path resolver if any test passes (allowed) for a non-owning user. **Failing privacy tests block merge.**

## Key Insights
- The two existing axes are NOT enough:
  1. Filesystem boundary (`resolvePathWithAllowed`) — guards against escape from allowed root.
  2. Phase 05 resolver — grants visibility/role on agent.
- Privacy hard rule is a THIRD axis: even when both above pass, the user-zone subdir is per-user-locked.
- Implementation: when path matches glob `users/<user_key>/...` under any agent root, extract `<user_key>`, look up `users.user_key == <key>` → `users.id`, compare to `senderUserID`. If mismatch → reject regardless of agent share/role.

## Requirements
- Failing tests covering bypass attempts:
  1. **Owner reading other user zone:** owner of predefined agent A; user B has chatted with A; owner attempts `read_file(users/{user_B_key}/notes.md)` → MUST reject.
  2. **Editor sharing:** user X shares agent A with user Y as `editor`; Y attempts `read_file(users/{user_X_key}/notes.md)` via A's filesystem → MUST reject.
  3. **share_workspace=true:** agent A has `share_workspace=true`; user B reads `users/{user_C_key}/notes.md` (C unrelated) → MUST reject.
  4. **Symlink bypass:** user A creates symlink under their own zone pointing into `users/{user_B_key}/`; resolution → MUST reject (existing symlink check should already cover; verify).
  5. **Path traversal:** `users/{user_A_key}/../{user_B_key}/file.md` → MUST reject.
  6. **Write attempt:** user A `write_file(users/{user_B_key}/...)` → MUST reject.
  7. **Allowed-self path:** user A reading own `users/{user_A_key}/notes.md` → MUST succeed (positive control).
  8. **Allowed-shared path:** user A reading `shared/team-doc.md` (under team workspace shared dir) where they have role=member → MUST succeed (positive control to confirm rule isn't over-blocking).

## Architecture
- Test file: `tests/integration/sharing_privacy_bypass_test.go`. Each scenario is a sub-test.
- Enforcement helper: `internal/workspace/user_zone_guard.go` (~80 LOC).
  - `EnforceUserZoneAccess(ctx, resolvedAbsPath, senderUserID, userKeyResolver) error`
  - Called from `internal/tools/filesystem.go` after path resolution but before read/write.
- `userKeyResolver`: small interface with `LookupByUserKey(ctx, key string) (uuid.UUID, error)`. Backed by `UserStore` lookup with cache.
- Error returned uses `errors.Is(err, ErrUserZoneViolation)` sentinel for test stability.

## Related Code Files
- Read: `internal/workspace/resolver_impl.go` (path computation).
- Read: `internal/tools/filesystem.go` (resolution + boundary check).
- Read: `internal/store/pg/users.go` (LookupByUserKey path).
- Create: `internal/workspace/user_zone_guard.go`.
- Create: `internal/workspace/user_zone_guard_test.go` (unit-level glob extraction).
- Create: `tests/integration/sharing_privacy_bypass_test.go`.
- Modify: `internal/tools/filesystem.go` to call guard after path resolution.

## Implementation Steps
1. Write `tests/integration/sharing_privacy_bypass_test.go` with 8 sub-tests (6 negative, 2 positive). Use real workspace + PG. RED on every negative bypass attempt that currently succeeds; positive tests should pass even pre-impl (sanity).
2. Write `user_zone_guard_test.go` covering glob/extraction edge cases:
   - Path under `users/<key>/...` with key match → allow.
   - Path under `users/<key>/...` with key mismatch → deny.
   - Path NOT under `users/...` → pass-through (no opinion).
   - Path with `..` segments after resolution should already be rejected upstream — guard sees absolute path.
3. Implement `user_zone_guard.go`:
   - Compile regex `^.+/users/([^/]+)/.+$` keyed off the agent workspace root, robust to nested `users/` (only first match after agent root counts).
   - Resolve `userKey → user_id` via injected resolver; if not found → reject (treat as unknown user zone, deny by default).
   - Compare to `senderUserID`; mismatch → return `ErrUserZoneViolation`.
4. Wire into `filesystem.go` after `resolvePathWithAllowed` returns. Apply to both read AND write paths. Apply BEFORE I/O syscalls.
5. Add `slog.Warn("security.privacy_zone_violation", ...)` log on rejection (per CLAUDE.md security log convention).
6. Run all tests GREEN.

## Todo List
- [ ] Scout existing path resolver + filesystem tool layout (re-confirm hook points)
- [ ] Write integration bypass-attempt tests (8 scenarios) — confirm RED on negatives
- [ ] Write unit guard tests
- [ ] Implement `user_zone_guard.go`
- [ ] Wire into `filesystem.go` (read + write)
- [ ] Add `security.privacy_zone_violation` slog warning
- [ ] All tests GREEN
- [ ] `go build ./... && go build -tags sqliteonly ./... && go vet ./...`
- [ ] Commit `feat(v4): privacy hard rule for users zone with bypass tests`

## Success Criteria
- All 6 negative bypass attempts return rejection error (errors.Is `ErrUserZoneViolation`).
- 2 positive controls pass (no over-blocking).
- `slog.Warn("security.privacy_zone_violation"...)` emitted on each rejection.
- Guard is short, leaf-level, no external imports beyond `userStore` interface.

## Risk Assessment
- Risk: false-positives blocking legitimate self-access. Mitigation: positive controls in test set.
- Risk: missing one I/O entry path (e.g. `list_dir` returns child paths in another user's zone). Mitigation: enumerate all filesystem tool entry points; apply guard at each. Add test for `list_dir` enumeration leak.
- Risk: case sensitivity on keys. Mitigation: store/compare lower-case `user_key` consistently.

## Security Considerations
- This is the LAST line of defence. Any false negative = privacy breach. Treat every red test as P0 blocker.
- Log line MUST not contain other-user file content; log only path + senderUserID + targetUserKey.
- Cache `userKey → uuid` lookup with short TTL (30s) — but reject on cache miss + DB miss (do not cache the negative result to avoid lockout from late-created users).

## Next Steps
- Phase 07 SQLite parity + integration end-to-end suite.
