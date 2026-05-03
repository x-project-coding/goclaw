# ADR: Hook Prompt-Handler Budget — UserID Rewire (Phase 14A-3)

**Date:** 2026-05-03
**Status:** Accepted
**Supersedes:** `docs/adr/2026-05-v4-hook-budget-deferred.md`

---

## Context

The deferred ADR documented why budget enforcement was silently disabled in v4:
every `hooks.Event` carried `TenantID = uuid.Nil`, causing the `!= uuid.Nil` guard
in `prompt.go` to short-circuit the entire deduct path. Additionally the underlying
table was renamed to `user_hook_budget` (keyed by `user_id`), so passing `TenantID`
would have FK-failed even if the guard were dropped.

## Decision

Rewire `hooks.Event` and the prompt handler to use `UserID` (per-user) instead of
`TenantID` (legacy multi-tenant concept):

1. **`hooks.Event`** — add `UserID uuid.UUID` field. `TenantID` retained for logging
   compatibility; scheduled for removal in a later cleanup.
2. **Pipeline `FireHook` callsites** — populate `UserID` via `parseUserUUID(ctx)`,
   a helper in `internal/pipeline/deps.go` that parses `store.UserIDFromContext(ctx)`.
   Group-prefix senders (`"group:..."`) return `uuid.Nil` — no per-user budget applies.
3. **`budget.Store.Deduct`** — parameter renamed `tenantID` → `userID`; error message
   updated from `"budget: nil tenant_id"` to `"budget: nil user_id"`.
4. **`prompt.go`** — guard changed to `ev.UserID != uuid.Nil`; log fields `"tenant"` →
   `"user"`; TODO comment removed.
5. **`GET /v1/hooks/budget`** — new endpoint in `internal/http/hooks_budget.go`.
   Returns the caller's own budget row from `user_hook_budget`. UserID sourced
   exclusively from JWT context — no cross-user data leakage possible. 404 when no
   row exists yet (row is seeded on first prompt hook call of the month).

## Consequences

- Prompt hook budget enforcement is now live for authenticated individual users.
- Group-prefix senders (channels routing messages as a group identity) bypass
  budget gracefully — `UserID=uuid.Nil` → budget code skipped, no FK error.
- `Event.TenantID` field remains but is no longer used by budget logic.
- Desktop (SQLite) edition gains the same wiring via `SqliteHookBudget` dialect.
