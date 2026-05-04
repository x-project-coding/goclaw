# ADR: Password Reset — CLI-Only in v4.0, Web/Email Flow Deferred to v4.1

**Date:** 2026-05-04
**Status:** Accepted
**Deciders:** v4 auth bootstrap review

---

## Context

A self-service password reset flow ("forgot password" → email link → reset form)
requires several backend primitives:

1. **Outbound email transport** — SMTP credentials, queue, retry, bounce
   handling, deliverability hygiene (SPF/DKIM/DMARC documentation).
2. **Reset-token store** — single-use, short-lived (15 min typical), opaque,
   user-id bound, with rate limit per user + per IP to prevent enumeration.
3. **Public unauthenticated HTTP endpoint** (`POST /v1/auth/password/reset-request`
   + `POST /v1/auth/password/reset-confirm`) — must not leak user existence in
   responses (constant-time response shape regardless of whether the email
   matches a real user).
4. **Frontend reset UI** — separate route, deep-link handling, anti-bot measures
   (turnstile / hCaptcha).

GoClaw v4 is a single-tenant gateway predominantly deployed on-prem or as a
desktop binary. The "lost password" scenario is operationally different from a
SaaS multi-tenant app:

- Single root user — root credential loss is a recovery scenario, not a routine
  flow. Best handled with operator-shell access to the host.
- Few member users — admin can reset other users' passwords directly via
  authenticated `PATCH /v1/users/{id}` with a new password (already wired in
  `internal/http/users_handlers.go`).
- Email infra is not an assumed dependency — desktop builds have no SMTP at all.

The v4 auth bootstrap review (Phase 06) considered but did not implement a web
reset flow.

## Decision

For **v4.0**, password reset is supported via two mechanisms only:

1. **CLI tool** `goclaw reset-password <email>` — operator-shell access required;
   intended for root-credential recovery and admin-assisted member resets when
   the admin web UI is unavailable.
2. **Authenticated admin write** `PATCH /v1/users/{id}` with a new password
   field — for routine member-password changes initiated by admin/root.

Self-service web/email password reset (`/forgot-password` + email token flow) is
**deferred to v4.1**.

## Consequences

**Positive:**
- Zero email infrastructure dependency for v4.0 — runs on any host without
  outbound SMTP.
- No public unauthenticated reset endpoint = smaller attack surface (no user
  enumeration, no rate-limit-evasion lottery, no token-reuse class of bugs).
- Desktop edition shipped without a feature it cannot meaningfully implement
  (no SMTP).

**Negative / Accepted risks:**
- Member users who forget their password and have no admin available are locked
  out until shell access can run `goclaw reset-password`. Acceptable for the
  small-team / single-tenant target deployments.
- Public-facing deployments (rare for v4.0) lose the self-service convenience
  users may expect. Mitigation: documentation explicitly calls out the v4.0
  limitation in the deployment guide.

**Follow-up (v4.1):**
- Optional `internal/email/` package with SMTP + Mailgun/SES adapter (build-tag
  gated so desktop builds stay SMTP-free).
- `password_reset_tokens` table with `user_id`, `token_hash`, `expires_at`,
  `consumed_at`, `created_ip`, plus per-user + per-IP rate-limit counters.
- Public endpoints `POST /v1/auth/password/reset-request` and `POST
  /v1/auth/password/reset-confirm` with constant-time response shape (response
  always 202 Accepted regardless of email existence).
- Frontend `/forgot-password` route + `/reset-password?token=...` deep link.
- Edition gate: standard edition only. Lite edition keeps CLI-only model.
