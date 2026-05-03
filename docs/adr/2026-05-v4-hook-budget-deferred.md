# ADR: Hook Prompt-Handler Budget Enforcement Deferred to v4.x

**Date:** 2026-05
**Status:** SUPERSEDED — implemented in Phase 14A-3 (2026-05-03). See `docs/adr/2026-05-v4-hook-budget-implemented.md`.
**Deciders:** v4 EPIC-04 Phase 13 cleanup review (code-reviewer finding H2)

---

## Context

The `prompt` hook handler at `internal/hooks/handlers/prompt.go:140-185` includes
a per-tenant token-budget enforcement path that consults
`hooks/budget.Store.Deduct(ctx, tenantID, cost)`:

- Pre-call: a `Deduct(0)` probe rejects if the budget is already exhausted.
- Post-call: the real prompt cost is deducted from the budget; `ErrBudgetExceeded`
  is converted to `DecisionBlock`.

Both calls are gated by `ev.TenantID != uuid.Nil`. In v3 multi-tenant world the
gate fired correctly because each tenant had a unique non-Nil UUID. In v4
single-user, every event carries `TenantID == uuid.Nil`, so the guard is always
false — the budget code is unreachable.

Additionally, the underlying schema was renamed during Phase 03 from `hook_budget`
keyed by `tenant_id` (FK to a v3 tenants table) to `user_hook_budget` keyed by
`user_id` (FK to v4 `users.id`). The Go consumer in `prompt.go` was not rewired —
even if the guard were dropped, the code passes `ev.TenantID` to `Deduct(...)`
which would attempt to UPSERT a row into `user_hook_budget` with `user_id =
uuid.Nil`, failing the FK to `users.id`.

## Decision

**Defer hook prompt-handler budget enforcement to v4.x.** v4.0 ships with the
budget code present but unreachable (current state). No silent regression — the
v3 path was already dead-by-guard before Phase 13.

For v4.0:
- Prompt hooks fire without per-user token budget caps.
- Operators relying on budget caps in v3 must migrate to a different cost-control
  mechanism (e.g. provider-side rate limits, agent quota config) until v4.x lands
  the rewire.

## Rationale

1. **Pre-existing regression, not Phase 13 fault.** The guard `ev.TenantID !=
   MasterTenantID` already always evaluated false in v3 → v4 transition because
   the agent loop always set `event.TenantID = MasterTenantID` and the
   handler's `Deduct` call passed the same value. Phase 13 changed the literal
   from `MasterTenantID` to `uuid.Nil` but did not unmask new behaviour.
2. **Rewiring requires schema-aware consumer change.** Adding a `UserID
   uuid.UUID` field to `hooks.Event` and threading it through every `FireHook`
   callsite in agent loop / pipeline / channels is a focused but non-trivial
   refactor — better contained in a dedicated phase than tacked onto a cleanup
   sweep.
3. **Budget enforcement is opt-in.** `Budget *budget.Store` is nil in default
   wiring (`Budget` is set only by callers that explicitly want it). Operators
   who never enabled it see zero behaviour change.

## Consequences

- **`internal/hooks/handlers/prompt.go:140-185`** — guard + Deduct call kept as
  dead code with a `TODO(v4.x)` comment so the rewire is discoverable.
- **`internal/hooks/budget/budget.go:69`** — error message `"budget: nil
  tenant_id"` is misleading in v4 single-user; will be updated to `"budget: nil
  user_id"` as part of the v4.x rewire.
- **`internal/hooks/types.go`** — `Event.TenantID` field retained (used by
  trace/log fields elsewhere); `Event.UserID` will be added in v4.x.
- **No documentation surfaced to users.** The budget feature was not advertised
  in v3 release notes either, so deferral does not break a published contract.

## Re-opening Triggers

This ADR should be reversed (rewire applied + ADR deprecated) when ANY of:

- A v4.x release scopes prompt-hook budget caps as a feature.
- A user reports unbounded prompt-hook spend pinning their LLM provider bill.
- A regulatory or compliance driver requires per-user spend caps.

## Implementation Note

When the rewire happens, the minimum change set is:
- `internal/hooks/types.go` — add `UserID uuid.UUID` to `Event`.
- `internal/agent/loop_*.go`, `internal/pipeline/*.go` — populate
  `Event.UserID` from `store.UserIDFromContext(ctx)` at every `FireHook` site.
- `internal/hooks/handlers/prompt.go` — replace `ev.TenantID` with `ev.UserID`
  in both pre/post-call sites and drop the `!= uuid.Nil` guard (rely on
  `Budget.Deduct` to FK-fail loud on misconfiguration).
- `internal/hooks/budget/budget.go` — rename error message + parameter name
  `tenantID` → `userID`.
