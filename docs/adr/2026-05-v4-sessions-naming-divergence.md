# ADR: Sessions Table Renamed to `agent_sessions`, FE Route Stays `/sessions/`

**Date:** 2026-05
**Status:** Accepted
**Deciders:** v4 schema design review (logged as LOG-3 / Q-10)

---

## Context

In v3 the database table for agent conversation sessions was named `sessions`. During
v4 schema redesign the table was renamed to `agent_sessions` to make the entity scope
explicit at the SQL layer (other domain entities — chat sessions, browser sessions,
auth sessions — all live in different stores; `sessions` was ambiguous).

The web UI in `ui/web/` already routes to `/sessions/` and the React Query keys, the
Zustand store slice, and the URL pattern `/chat/:sessionKey` are wired off the
`sessions` namespace. Renaming all of those would touch ~30 frontend files purely for
naming alignment with the backend table — pure churn with zero user benefit and a
real risk of breaking deep-link bookmarks.

## Decision

- **Backend (Go + SQL):** the table is `agent_sessions`; store types are
  `AgentSession`, `AgentSessionStore`, etc. Method names use the `AgentSession` prefix
  (`GetAgentSession`, `ListAgentSessions`).
- **Frontend (React):** keeps `/sessions/` route, `useSessionsQuery`, `sessionsSlice`,
  `SessionList` component names, and the `:sessionKey` URL param. No frontend rename.

This divergence is intentional. The wire format (REST + WS) preserves the FE-facing
name `session` so the divergence is invisible to the API consumer. Backend serializers
map `AgentSession` → JSON `session` at the boundary.

## Rationale

1. **Backend disambiguation matters more than frontend naming.** SQL is read by ops,
   migrations, and future joiners — `sessions` would collide with multiple plausible
   meanings. `agent_sessions` is unambiguous and survives schema growth.
2. **Frontend deep-link continuity.** `/sessions/<key>` URLs are already shared in
   chats, bookmarks, and onboarding screenshots. Breaking them on the v4 cutover
   produces user-visible regression with no functional gain.
3. **Wire format hides the rename.** Because the API still emits `session` in JSON,
   the divergence is contained to two languages (Go internals vs SQL on one side,
   TS/React on the other) and never surfaces to clients.

## Consequences

- Future LLMs / contributors WILL notice the FE/BE name mismatch and may try to
  "fix" it. This ADR is the canonical record that the mismatch is intentional.
- New backend code referring to the conversation entity MUST use `AgentSession*`.
  Mixing `Session*` and `AgentSession*` in store/handler code is a code-smell and
  should be flagged in review.
- New frontend code MUST keep using `session` in route paths, query keys, and URL
  params for continuity with existing bookmarks.
- Boundary layer (HTTP handlers + WS method router) is the only place the two names
  meet — JSON tags map `AgentSession` Go struct fields to `session` JSON keys.

## Notes

If a future redesign decides the rename is worth the FE churn, that work belongs in
a dedicated breaking-change release with deprecation headers and a redirect window
on `/sessions/*` → `/agent-sessions/*`. Until then, this ADR is binding.
