# ADR: localStorage Tokens for v4.0 — HttpOnly Cookies Deferred to v4.1

**Date:** 2026-05-04
**Status:** Accepted
**Deciders:** v4 frontend bootstrap review

---

## Context

Browser-side storage of JWT access tokens + opaque refresh tokens has two
established patterns:

1. **localStorage (or sessionStorage)** — JS-readable, easy to attach to fetch
   `Authorization` headers, survives page reloads, no CSRF vector. Vulnerable to
   token exfiltration via XSS (any injected script can read the token and
   exfiltrate the 30-day refresh token).
2. **HttpOnly + Secure + SameSite=Strict cookies** — JS cannot read; browser
   attaches automatically. XSS-resistant for the token itself but introduces:
   (a) CSRF surface that must be defended (double-submit token, SameSite, or
   per-request CSRF token), (b) cross-origin CORS complexity for desktop-vs-web
   shared backend, (c) a per-request cookie size cost.

The v4 frontend (`ui/web/src/stores/use-auth-store.ts`) and the desktop frontend
(`ui/desktop/frontend/src/`) currently use `localStorage` for tokens. Phase 11
shipped this pattern as the baseline.

A separate concern is that desktop builds (Wails, no real network origin) and
single-tenant local-network deployments derive limited benefit from cookie-based
isolation, while still paying the CSRF + CORS engineering cost.

## Decision

For **v4.0**, localStorage is the chosen token storage for both web and desktop
frontends.

Migration to **HttpOnly cookies** is **deferred to v4.1** as a coordinated
backend + frontend change.

### v4.0 mitigations in lieu of HttpOnly

- Strict CSP on the web SPA (`script-src 'self'` baseline) — limits XSS payload
  surface.
- Refresh-token family revocation (see `internal/auth/session.go` and migration
  `000001_initial.up.sql` `user_sessions.family_id`): if a stolen refresh token
  is used after the legitimate client rotates, the entire family is revoked,
  forcing re-auth and surfacing the theft to operators via the access log.
- Short access-token lifetime (15 min) — bounds the window an exfiltrated
  access token is useful.
- `goclaw restore` revokes all sessions post-restore (see ADR
  `2026-05-v4-...` Finding 6 fix in `internal/backup/db_restore.go`).

## Consequences

**Positive:**
- Ships v4.0 on schedule without coordinated FE/BE rewrite.
- Avoids CSRF surface that would otherwise need a defense scheme designed
  twice (web vs desktop-Wails).
- Desktop edition (no real cross-origin browser context) sees no security loss.

**Negative / Accepted risks:**
- An XSS payload that lands in either frontend can exfiltrate the 30-day refresh
  token. Family-revocation detects post-facto reuse but does not prevent the
  initial theft window.
- Users with strict threat models (ops with public-internet exposure) should
  enforce CSP rigorously and treat XSS regressions as P0.

**Follow-up (v4.1):**
- Backend: dual-mode token issuance — `Set-Cookie` with HttpOnly+Secure+SameSite,
  CSRF token in a readable cookie or response body for double-submit pattern.
- Frontend: drop `localStorage.setItem(token, ...)` paths, route refresh through
  cookie-bearing fetch (`credentials: 'include'`), wire CSRF token to fetch
  middleware.
- Migration: feature flag for transition window; old localStorage tokens
  continue working until cookie endpoint is live, then localStorage is purged
  on first cookie-mode login.
- Desktop edition can opt out of cookies (Wails has no real origin) — keep
  localStorage as the implementation but isolate via OS keyring write-through.
