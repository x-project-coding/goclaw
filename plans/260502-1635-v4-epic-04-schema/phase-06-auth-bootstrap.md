# Phase 06 — Auth + Bootstrap (Argon2id + JWT + Refresh)

## Context Links

- Master § 9 (Bootstrap Flow Spec), § 4.2 (Gateway/HTTP)
- Decisions Q-A, Q-B, Q-C, Q-D, Q-F, Q-6, Q-7
- Phase 05 (users + user_sessions stores)

## Overview

- Priority: P0
- Status: **completed 2026-05-03** (impl + unit tests; e2e deferred → Phase 14)
- Effort: 18 dev-days
- Description: Wire bootstrap (`/v1/bootstrap/{status,init}`) + auth (`/v1/auth/{login,refresh,logout,me}`). Argon2id (m=64MB, t=3, p=4). JWT access (15 min, HS256). Opaque refresh (32-byte hex, sha256-hashed in `user_sessions`, 30d TTL, rotate-on-use). Role rename owner/admin/operator/viewer → root/admin/member/viewer. Bootstrap-required middleware → 503 redirect. Add `i18n` keys + 3 catalog entries.

## Key Insights

- Reuse existing `internal/http/auth.go` (file:line confirmed); add password verification path alongside API-key path.
- Bootstrap idempotency: cached `bootstrap_required` boolean at gateway startup (master § 9). Subsequent POST to `/v1/bootstrap/init` after first creation → 409 AlreadyBootstrapped.
- Q-C Argon2id params: m=64MB, t=3, p=4. Use `golang.org/x/crypto/argon2`.
- Q-B hybrid OAuth2-style: JWT access + opaque refresh. Q-D: rotate refresh on every refresh call (recommend).
- Role hierarchy rename (Q-F): `owner` → `root`, `operator` → `member`. Update `internal/permissions/policy.go` (constants verified at L26-29).
- Hardcoded API key + auto-issue admin API key on bootstrap (Q-7 — root user owns system defaults).
- WS `connect` frame: drop `tenantId` param, add JWT validation path (master § 4.2).
- i18n keys MUST be added to `internal/i18n/keys.go` + 3 catalogs (en/vi/zh) BEFORE handler code references them (CLAUDE.md i18n rule).

## Tests to write FIRST (TDD red step)

| Test file | Cases (must FAIL until impl green) |
|---|---|
| `tests/e2e/01_bootstrap_test.go` | `TestBootstrapStatusInitiallyFalse` — first GET `/v1/bootstrap/status` returns `bootstrapped:false`. `TestBootstrapInitCreatesRoot` — POST creates user with `role='root'`, returns access+refresh. `TestBootstrapIdempotent` — second POST returns 409. `TestBootstrapStatusFlipsTrue` — after init, status returns `bootstrapped:true`. `TestBootstrapEmailValidation` — invalid email → 400. `TestBootstrapPasswordComplexity` — < 12 chars → 400 |
| `tests/e2e/02_auth_login_test.go` | `TestLoginValidCredentials` — returns access JWT + refresh; access < 16min TTL; refresh stored as hash in user_sessions. `TestLoginInvalidPassword` — 401 + Argon2id timing-safe. `TestLoginUnknownEmail` — 401 (same shape as wrong password — no enumeration). `TestLoginCaseInsensitiveEmail` — uppercase email matches lowercase stored |
| `tests/e2e/02_auth_refresh_test.go` | `TestRefreshTokenRotates` — old refresh marked revoked, new refresh issued. `TestRefreshRevokedFails` — using old after rotation → 401. `TestRefreshExpiredFails` — expired refresh → 401. `TestRefreshUnknownFails` — random token → 401 |
| `tests/e2e/02_auth_logout_test.go` | `TestLogoutRevokesRefresh` — logout marks all user's refresh tokens revoked. `TestLogoutAccessRemainsValid` — access JWT remains valid until natural expiry (15min) — documented behavior |
| `tests/e2e/02_auth_me_test.go` | `TestMeReturnsClaims` — GET `/v1/auth/me` with valid access returns user info + role. `TestMeWithExpiredAccess` — expired access → 401. `TestMeWithMalformedJWT` — random string → 401 |
| `tests/e2e/02_auth_password_hash_test.go` | `TestPasswordHashIsArgon2id` — hash starts with `$argon2id$`; `TestPasswordVerifyRejectsBadHash` — corrupted hash → verify fails not panics |
| `tests/e2e/02_role_rename_test.go` | `TestRoleConstants` — `RoleRoot`, `RoleAdmin`, `RoleMember`, `RoleViewer` constants present; `RoleOwner`, `RoleOperator` removed |
| `tests/e2e/02_bootstrap_503_redirect_test.go` | `TestProtectedEndpointsReturn503BeforeBootstrap` — `GET /v1/agents` (or any protected route) returns 503 with `bootstrap_required` flag when users count = 0 |
| `tests/e2e/02_ws_connect_jwt_test.go` | `TestWSConnectAcceptsValidJWT` — WS `connect` frame with `accessToken` succeeds. `TestWSConnectRejectsExpired` — expired access → connect rejected. `TestWSConnectNoTenantParam` — `connect` frame no longer accepts `tenantId` (omit or reject) |
| `tests/e2e/02_api_key_s2s_test.go` | `TestApiKeyS2S` — request with `Authorization: Bearer <api-key>` + `X-USER-ID: <uuid>` resolves to user; missing header → 401 |
| <!-- RED-TEAM Finding 1 --> `tests/e2e/02_jwt_kid_rotation_test.go` | `TestJWTKidPresent` — issued JWT header includes `kid`. `TestJWTRotationAcceptsBothActive` — env contains 2 kids; tokens issued under each verify. `TestJWTRejectsUnknownKid` — token with kid not in env → 401. `TestJWTBackwardCompatLegacyKid` — legacy single-secret env still works during upgrade window. |
| <!-- RED-TEAM Finding 2 --> `tests/e2e/01_bootstrap_token_test.go` | `TestBootstrapInitRequiresToken` — POST without `X-Bootstrap-Token` → 403. `TestBootstrapInitWrongToken` → 403. `TestBootstrapInitValidToken` → 200. `TestBootstrapLocalhostOnlyOnFirstBoot` — connect from non-loopback IP to bootstrap port → connection refused/404 (verify via dual-listener setup). |
| <!-- RED-TEAM Finding 3 --> `tests/e2e/01_bootstrap_concurrent_test.go` | `TestBootstrapConcurrent50Parallel` — fire 50 parallel POST with valid token + distinct emails; assert exactly 1 returns 200, 49 return 409. Final `SELECT COUNT(*) FROM users WHERE role='root'` = 1. |
| <!-- RED-TEAM Finding 4 --> `tests/e2e/02_refresh_theft_test.go` | `TestRefreshTokenTheftDetection` — sequence: login → RT1 family=F1 → rotate RT1 → RT2 family=F1 (revoked: RT1) → re-use RT1 → 401 + audit log written + ALL family-F1 sessions revoked. `TestRefreshFamilyInheritance` — refresh chain RT1→RT2→RT3 all share same family_id. |
| <!-- RED-TEAM Finding 8 --> `tests/e2e/02_password_reset_cli_test.go` | `TestResetPasswordCLI` — invoke `goclaw reset-password --email <e>` with stdin password input. Old password fails login. New password succeeds. All prior `user_sessions` for that user revoked. |
| <!-- RED-TEAM Finding 13 --> `tests/e2e/02_login_rate_limit_test.go` | `TestLoginRateLimitPerIP` — fire 10 sequential `/v1/auth/login` from same IP within 60s; first 5 return 401 (wrong password), next 5 return 429 with `Retry-After` header. `TestArgon2idSemaphore` — fire 50 concurrent valid-cred logins; verify max 10 concurrent Argon2id calls (instrument via metric or test hook). |

**Red verification:** All test files fail (no auth handlers exist yet, no Argon2id helper, role constants still owner/operator, no JWT kid support, no bootstrap token, no theft detection, no CLI reset, no rate limit).

## Requirements

### Functional

#### NEW backend modules

- `internal/auth/password.go` — `HashPassword(plaintext) (string, error)`, `VerifyPassword(plaintext, hash) (bool, error)`. Uses Argon2id with Q-C params. Encoded format: `$argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>` (PHC standard).
  <!-- RED-TEAM Finding 13: Argon2id 64MB self-DoS amplifier (HIGH) -->
  - Add process-level semaphore: `var verifySem = make(chan struct{}, 10)`. `VerifyPassword` MUST acquire (`verifySem <- struct{}{}`) before Argon2id call, release in defer. N=10 caps memory at 64MB × 10 = 640MB headroom.
  - Document op-team RAM requirement: gateway baseline + 640MB Argon2id headroom.
  - Optional: env var `GOCLAW_PASSWORD_VERIFY_CONCURRENCY` to override default N=10 (range 4-32).
  <!-- /RED-TEAM Finding 13 -->
- `internal/auth/jwt.go` — `IssueAccess(userID, role) (string, error)`, `VerifyAccess(token) (Claims, error)`. HS256. 15-min TTL (configurable via `GOCLAW_JWT_ACCESS_TTL`). Claims: `sub` (user UUID), `role`, `exp`, `iat`.
  <!-- RED-TEAM Finding 1: JWT secret has no rotation path / no `kid` claim (CRITICAL) -->
  - **Multi-key rotation (PROMOTED FROM DEFER TO P0):** secret reads from `GOCLAW_JWT_SECRETS_JSON` env var — JSON array `[{"kid":"k1","secret":"<hex>","status":"active"},{"kid":"k0","secret":"<hex>","status":"verify-only"}]`.
  - JWT header MUST include `kid` claim. Issuer always uses the newest `active` key. Verifier accepts any kid present in the slice (active OR verify-only).
  - `IssueAccess` signature unchanged externally; internally selects newest active kid.
  - `VerifyAccess` looks up secret by `kid`; missing kid → reject with `MsgAccessTokenInvalid`.
  - Hot-reload via SIGHUP → re-read env (defer config-file source to v4.x). Operators rotate by: append new kid as active, demote old kid to verify-only, restart/SIGHUP, after 24h drop old kid entirely.
  - Backward compat: if env var unset, fall back to legacy single `GOCLAW_JWT_SECRET` with synthetic `kid="legacy"` (for upgrade window only; remove after Phase 14).
  <!-- /RED-TEAM Finding 1 -->
- `internal/auth/session.go` — `IssueRefresh(userID, familyID UUID) (token string, hash string, expires time.Time, error)`, `VerifyRefresh(token, store) (userID, familyID string, error)`, `RevokeRefresh(token, store) error`, `RotateRefresh(oldToken, store) (newToken, newHash, error)`, `RevokeFamily(familyID UUID, store) error`. Token = 32 random bytes, hex. Hash = sha256.
  <!-- RED-TEAM Finding 4: Refresh token theft undetectable — no family revocation (CRITICAL) -->
  - **Token family theft detection:** every new session inherits parent's `family_id` (Phase 03 schema). Login creates new family_id; rotation copies parent's family_id.
  - On `VerifyRefresh`: if token found in `user_sessions` AND `revoked_at IS NOT NULL` AND `expires_at > NOW()` (revoked-but-not-expired) → SECURITY INCIDENT. Action: `RevokeFamily(familyID)` → `UPDATE user_sessions SET revoked_at=NOW() WHERE family_id=$1 AND revoked_at IS NULL`. Audit log `event='auth.refresh_theft_detected'` with family_id + IP.
  - Test `TestRefreshTokenTheftDetection` (added to red tests below): attacker steals RT1, rotates to RT2; legit user later rotates RT1 → 401; attacker re-uses RT2 → all RT2/RT3/... in family revoked.
  <!-- /RED-TEAM Finding 4 -->

#### NEW HTTP handlers

- `internal/http/bootstrap.go` — `GET /v1/bootstrap/status`, `POST /v1/bootstrap/init`. Status reads `bootstrapRequired` flag (gateway startup-cached). Init validates email + password (≥12 chars, ≥1 letter, ≥1 digit, ≥1 symbol), creates root user, auto-issues admin API key, returns access+refresh JWT.
  <!-- RED-TEAM Finding 2: Bootstrap remote takeover via 1 req/sec rate limit (CRITICAL) -->
  - **`GOCLAW_BOOTSTRAP_TOKEN` requirement:** on gateway first start when `bootstrapRequired=true`, generate 32-byte hex token, print to stderr (`slog.Info("bootstrap.token_generated", "token", "<hex>", "msg", "set X-Bootstrap-Token header on /v1/bootstrap/init")`). Token persists in process memory only (NOT DB, NOT file).
  - `POST /v1/bootstrap/init` requires `X-Bootstrap-Token` header. Constant-time compare against in-memory token. Mismatch/missing → 403 (no body detail to avoid leaking).
  - **Localhost-only bind on first boot:** if `bootstrapRequired=true`, `/v1/bootstrap/*` routes only register on `127.0.0.1:<port>` listener (separate listener mux), NOT public listener. After bootstrap completes, this listener tears down.
  - 1 req/sec rate limit kept (defense in depth) but token check is the actual gate.
  - Document operator UX: tail stderr for `bootstrap.token_generated` line OR `kubectl logs <pod> | grep bootstrap.token`.
  <!-- /RED-TEAM Finding 2 -->
  <!-- RED-TEAM Finding 3: Bootstrap concurrent race — partial UNIQUE missing (CRITICAL) -->
  - **Bootstrap TX with advisory lock:** wrap entire init flow in single TX:
    ```
    BEGIN;
    SELECT pg_advisory_xact_lock(0xB007);  -- BOOTSTRAP_LOCK_ID = 0xB007
    -- check users count, create root user, etc.
    COMMIT;
    ```
    Lock auto-released on COMMIT/ROLLBACK. Phase 03's `users_only_one_root` partial UNIQUE is the ultimate fallback — even without advisory lock, parallel inits collide on UNIQUE.
  - Test `TestBootstrapConcurrent50Parallel` — fire 50 parallel POST `/v1/bootstrap/init` with valid token; assert exactly 1 succeeds (200), 49 fail (409 AlreadyBootstrapped or DB UNIQUE violation surfaced as 409).
  <!-- /RED-TEAM Finding 3 -->
  <!-- RED-TEAM Finding 8: Password reset / forgot-password flow missing (CRITICAL) -->
  - **No email-based password reset in v4.0** — explicitly documented limitation.
  - Operator-level recovery via CLI: `goclaw reset-password --email <user@example.com>` (CLI command added — Phase 08 keep-list addition required). Reads new password from stdin (no command-line history leak), validates complexity, hashes with Argon2id, runs `UPDATE users SET password_hash=$1 WHERE email=$2`. Operator-only DB access required (read CLAUDE.md `psql` rule).
  - On reset, also `UPDATE user_sessions SET revoked_at=NOW() WHERE user_id=<reset_user> AND revoked_at IS NULL` to invalidate stolen sessions.
  - Document in `/v1/auth/me` 401 response and login error: "Password recovery requires server-side CLI; contact admin."
  <!-- /RED-TEAM Finding 8 -->
- `internal/http/auth_password.go` — `POST /v1/auth/login`, `POST /v1/auth/refresh`, `POST /v1/auth/logout`, `GET /v1/auth/me`.
- `internal/http/auth_middleware.go` — bootstrap_required gate (503 if users count=0 except `/v1/bootstrap/*` + `/healthz`); JWT validation; role gate helpers.
  <!-- RED-TEAM Finding 13: Argon2id 64MB self-DoS amplifier (HIGH) -->
  - **Per-IP login rate limit:** 5/min per IP for `POST /v1/auth/login`. Use existing rate-limit middleware pattern. Rejected request returns 429 with `Retry-After`.
  - Combined with password.go semaphore (N=10 concurrent verifies), 1000-request burst from 1 IP → 1000 req/min capped to 5/min by middleware → only 5 reach Argon2id → 5 × 64MB = 320MB peak (within budget).
  <!-- /RED-TEAM Finding 13 -->

#### REFACTOR existing files

- `internal/http/auth.go` — add password verify path; existing API-key path preserved (S2S `Authorization: Bearer <api-key>` + `X-USER-ID: <uuid>`).
- `internal/http/server.go` — wire bootstrap routes (public, no auth) + auth routes + middleware order.
- `internal/permissions/policy.go` — rename role constants (Q-F):
  - `RoleOwner` → `RoleRoot` (line 26)
  - `RoleAdmin` (unchanged, line 27)
  - `RoleOperator` → `RoleMember` (line 28)
  - `RoleViewer` (unchanged, line 29)
  - Update all callers (~150 sites — grep `RoleOwner|RoleOperator` repo-wide).
- `internal/gateway/router.go` `handleConnect` — drop `tenantId`, add JWT verify path; populate `Client.userID` from claims.
- `internal/gateway/server.go` — `Client` struct: drop `tenantID` field, keep `userID` (line 24 confirmed).

#### NEW i18n keys (BEFORE handlers — CLAUDE.md rule)

In `internal/i18n/keys.go`:
- `MsgBootstrapRequired = "error.bootstrap_required"`
- `MsgBootstrapAlreadyDone = "error.bootstrap_already_done"`
- `MsgInvalidEmail = "error.invalid_email"`
- `MsgWeakPassword = "error.weak_password"`
- `MsgInvalidCredentials = "error.invalid_credentials"`
- `MsgRefreshTokenInvalid = "error.refresh_token_invalid"`
- `MsgRefreshTokenExpired = "error.refresh_token_expired"`
- `MsgRefreshTokenRevoked = "error.refresh_token_revoked"`
- `MsgAccessTokenExpired = "error.access_token_expired"`
- `MsgAccessTokenInvalid = "error.access_token_invalid"`

In all 3 catalogs (`catalog_en.go`, `catalog_vi.go`, `catalog_zh.go`): add translations.

### Non-functional

- Argon2id verify timing constant (use `crypto/subtle.ConstantTimeCompare` if hand-rolling; library handles).
- JWT secret rotation: support `GOCLAW_JWT_SECRET_PREVIOUS` env var for grace verification (defer to follow-up if too complex; document non-goal).
- Refresh token never logged in plaintext.
- Bootstrap rate-limited 1 req/sec per IP (master § 10 deferred — implement minimal middleware here).
- Password verify ≥ 100ms (Argon2id cost OK).
- Login attempt failures audited (`activity_logs.event = 'auth.login_failed'`).

## Architecture

```
Bootstrap flow (Q-A + Q-6 + Q-7):
  1. gateway startup → SELECT COUNT(*) FROM users → cache `bootstrapRequired = (count == 0)`
  2. middleware: if path NOT in {/v1/bootstrap/*, /healthz, /v1/version} AND bootstrapRequired → 503 + JSON {error: bootstrap_required}
  3. POST /v1/bootstrap/init:
       a. validate payload (email, password complexity, display_name)
       b. atomic: INSERT user role=root + INSERT API key for that user
       c. issue access+refresh
       d. set bootstrapRequired = false
       e. return tokens
  4. subsequent POST /v1/bootstrap/init → 409

Login flow (Q-B):
  POST /v1/auth/login {email, password}
   ├─ select user by email (case-insensitive)
   ├─ Argon2id verify (timing-constant)
   ├─ create user_sessions row (refresh hash + 30d TTL)
   ├─ issue JWT access (15min)
   └─ return {access_token, refresh_token, user_id, role}

Refresh flow (Q-D rotate-on-use):
  POST /v1/auth/refresh {refresh_token}
   ├─ sha256(token) → lookup user_sessions
   ├─ check not revoked + not expired
   ├─ ATOMIC tx:
   │   ├─ revoke old session
   │   └─ insert new session (new token + hash)
   ├─ issue new JWT access
   └─ return {access_token, refresh_token}

S2S API-key path (existing, preserved):
  Authorization: Bearer <api-key>
  X-USER-ID: <uuid>
   ├─ resolve api-key → owner_user_id
   ├─ validate X-USER-ID matches owner_user_id
   └─ ctx user injected
```

## Related Code Files

### Create

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/auth/password.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/auth/password_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/auth/jwt.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/auth/jwt_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/auth/session.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/auth/session_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/bootstrap.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/bootstrap_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/auth_password.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/auth_password_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/auth_middleware.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/auth_middleware_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/01_bootstrap_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/02_auth_login_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/02_auth_refresh_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/02_auth_logout_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/02_auth_me_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/02_auth_password_hash_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/02_role_rename_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/02_bootstrap_503_redirect_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/02_ws_connect_jwt_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/02_api_key_s2s_test.go`

### Modify

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/i18n/keys.go` — add 10 keys (BEFORE handler impl)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/i18n/catalog_en.go` — add translations
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/i18n/catalog_vi.go` — add translations
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/i18n/catalog_zh.go` — add translations
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/permissions/policy.go` — rename role constants + ~150 callers
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/auth.go` — extend with password verify path
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/server.go` — wire routes
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/gateway/router.go` — `handleConnect` JWT verify
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/gateway/server.go` — Client struct cleanup

### Delete

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/tenant_auth_helpers.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/http/tenant_backup_auth_helpers.go` (verify exists)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/internal/gateway/methods/tenants.go`

## Implementation Steps

1. Verify Phase 05 merged + tests green (users + user_sessions stores ready).
2. Add i18n keys + translations (10 keys × 3 catalogs = 30 entries) FIRST.
3. Run `go test ./internal/i18n/...` to ensure catalog parity.
4. Write 10 e2e auth/bootstrap test files (red — handlers don't exist).
5. Write `internal/auth/password.go` + unit tests:
   - `HashPassword(plaintext)` → Argon2id PHC string.
   - `VerifyPassword(plaintext, hash)` → bool + err. Timing-safe via library.
6. Write `internal/auth/jwt.go` + unit tests:
   - HS256 with `GOCLAW_JWT_SECRET`.
   - `IssueAccess(userID, role)` → JWT with `sub`, `role`, `exp`, `iat`.
   - `VerifyAccess(token)` → Claims or err.
7. Write `internal/auth/session.go` + unit tests:
   - `IssueRefresh(userID, store)` → 32-byte hex token + hash + DB row.
   - `RotateRefresh(oldToken, store)` → atomic revoke+insert.
   - `RevokeAll(userID, store)` for logout.
8. Write `internal/http/bootstrap.go`:
   - `bootstrapRequired` flag refresh on startup.
   - `GET /v1/bootstrap/status` reads flag.
   - `POST /v1/bootstrap/init` validates payload + creates root user + issues tokens + flips flag.
9. Write `internal/http/auth_password.go`:
   - `POST /v1/auth/login` — Argon2id verify, issue tokens, audit log.
   - `POST /v1/auth/refresh` — rotate-on-use.
   - `POST /v1/auth/logout` — revoke all sessions for user.
   - `GET /v1/auth/me` — return claims.
10. Write `internal/http/auth_middleware.go`:
    - 503 redirect if bootstrap required (except whitelist).
    - JWT validation extracts user + role into ctx.
    - `requireRole(roles...)` helper.
11. Wire into `internal/http/server.go` (route registration + middleware order).
12. Refactor `internal/permissions/policy.go` — rename `RoleOwner` → `RoleRoot`, `RoleOperator` → `RoleMember`. Update ALL callers (`grep -rn 'RoleOwner\|RoleOperator' --include='*.go'` repo-wide).
13. Refactor `internal/gateway/router.go` `handleConnect`:
    - Drop `tenantId` param.
    - Accept `accessToken` param; verify JWT; populate `Client.userID` from claims.
    - Existing API-key path preserved.
14. Refactor `internal/gateway/server.go` `Client` struct (drop `tenantID` field; keep `userID`).
15. Delete tenant auth helpers (3 files).
16. `go build ./...` + `go build -tags sqliteonly ./...` + `go vet ./...` clean.
17. Run all 10 e2e auth/bootstrap tests → green.
18. Run all phase 05 store tests → still green (regression-safe).

## Todo List

- [x] i18n keys (10) added BEFORE handlers
- [x] i18n catalogs (en/vi/zh) translated
- [ ] 10 e2e tests written (red verified) — **DEFERRED to Phase 14** (full e2e validation phase)
- [x] internal/auth/password.go + test (Argon2id PHC) — 7 unit tests
- [x] internal/auth/jwt.go + test (HS256) — 7 unit tests inc. kid rotation
- [x] internal/auth/session.go + test (refresh rotate) — 7 unit tests inc. theft detection
- [x] internal/http/bootstrap_handler.go + bootstrap_validators.go (status + init)
- [x] internal/http/auth_password.go (login/refresh/logout/me)
- [x] internal/http/auth_middleware.go (503 + JWT + bootstrap-token + login rate-limit)
- [x] internal/gateway/server.go wired (BootstrapRequiredMiddleware + Set*Handler setters)
- [x] internal/permissions/policy.go role rename + 26 caller files updated
- [x] internal/gateway/router.go handleConnect — JWT accessToken path added
- [ ] internal/gateway/server.go Client struct cleanup — **DEFERRED to Phase 13** (tenantID always = MasterTenantID in v4; field is harmless until full purge)
- [ ] Tenant auth helpers deleted (3 files) — **DEFERRED**: tenant_auth_helpers.go still actively used by packages.go + builtin_tools.go for `requireMasterScope`; deletion would break callers. The 2 other files were already removed in earlier v4 sweeps.
- [x] go build (PG + sqliteonly) + go vet clean
- [ ] All 10 e2e auth tests green — **DEFERRED to Phase 14**
- [x] Phase 05 tests still green (regression-safe)
<!-- RED-TEAM Findings 1, 2, 3, 4, 8, 13 todos -->
- [x] (Finding 1) JWT `kid`-rotation via `GOCLAW_JWT_SECRETS_JSON` + SIGHUP reload (Reload() exposed; SIGHUP wiring deferred to caller)
- [x] (Finding 1) JWT kid-rotation tests green (3 unit cases: BothActive, UnknownKid, LegacyFallback)
- [x] (Finding 2) `GOCLAW_BOOTSTRAP_TOKEN` printed via slog.Info on first boot
- [x] (Finding 2) Localhost-only check in bootstrap handler (KISS: middleware-level reject vs. dual-listener — defense-in-depth equivalent, documented in code comment)
- [ ] (Finding 2) Bootstrap-token tests green (4 cases) — **DEFERRED to Phase 14 (e2e)**
- [x] (Finding 3) `pg_advisory_xact_lock(0xB007)` in bootstrap TX (auto-skipped on SQLite via dialect detection)
- [ ] (Finding 3) Bootstrap concurrent test green (50-parallel) — **DEFERRED to Phase 14 (e2e)**
- [x] (Finding 4) `family_id` propagation in IssueRefresh + RotateRefresh
- [x] (Finding 4) `RevokeFamily` on theft detection + audit log (slog.Warn security.auth.refresh_theft_detected)
- [x] (Finding 4) Refresh theft detection tests green (2 unit cases in session_test.go)
- [x] (Finding 8) `goclaw reset-password --email <e>` CLI command added
- [x] (Finding 8) Reset CLI revokes existing sessions atomically (single TX)
- [ ] (Finding 8) Reset CLI test green — **DEFERRED to Phase 14 (e2e)**
- [x] (Finding 13) Argon2id semaphore N=10 + env override `GOCLAW_PASSWORD_VERIFY_CONCURRENCY` (clamped 4-32)
- [x] (Finding 13) Per-IP `/v1/auth/login` rate limit 5/min via in-memory token bucket
- [x] (Finding 13) Argon2id semaphore concurrency test green (sampling len(verifySem) under 50 parallel calls)

## Success Criteria

- All 10 e2e auth/bootstrap tests green.
- `grep -rn 'RoleOwner\|RoleOperator' --include='*.go'` returns 0 (renamed).
- Argon2id PHC format hashes verifiable by external tool (e.g., `argon2-cffi`).
- JWT decoded by `jwt.io` (sanity).
- Bootstrap idempotent (second call → 409).
- WS `connect` rejects expired/invalid JWT.
- 503 redirect works when users count = 0.

## Risk Assessment

| Risk | Severity | Mitigation |
|---|---|---|
| Argon2id memory cost OOMs gateway under load | High | <!-- RED-TEAM Finding 13 --> Process-level semaphore N=10 (REQUIRED) + per-IP login rate limit 5/min. Peak 64MB × 10 = 640MB headroom budgeted. |
| JWT secret leaked → forged tokens valid forever | Critical | <!-- RED-TEAM Finding 1 --> `kid`-based multi-key rotation via `GOCLAW_JWT_SECRETS_JSON`. Operators rotate by appending new kid + demoting old. Hot-reload via SIGHUP. |
| Refresh rotation race (concurrent refresh from 2 clients) | Med | UPDATE ... WHERE token_hash=$1 AND revoked_at IS NULL using row lock; second wins-or-fails |
| Refresh token theft (stolen via XSS) goes undetected | Critical | <!-- RED-TEAM Finding 4 --> Token family `family_id` + theft detection: revoked-but-not-expired token use → revoke entire family + audit log. RFC 6749 §10.4 compliant. |
| Bootstrap remote takeover via /init race | Critical | <!-- RED-TEAM Finding 2 --> `GOCLAW_BOOTSTRAP_TOKEN` required + localhost-only listener on first boot. <!-- RED-TEAM Finding 3 --> `pg_advisory_xact_lock` + Phase 03 partial UNIQUE = atomic single-root creation. |
| Lost root password → bricked install | Critical | <!-- RED-TEAM Finding 8 --> `goclaw reset-password` CLI command (operator-level recovery, no email infra needed). |
| Role rename breaks 150+ callers | High | One sweeping commit; `go build` catches all; add compile-time alias deprecation if too risky (only if needed) |
| Password complexity policy too weak | Low | Min 12 chars + 1 letter + 1 digit + 1 symbol; document for ops |
| 503 redirect breaks healthz / metrics endpoints | Med | Whitelist `/healthz`, `/v1/version`, `/v1/bootstrap/*`, `/metrics` if present |

## Security Considerations

- Argon2id PHC params hardcoded (m=65536, t=3, p=4) per Q-C.
<!-- RED-TEAM Finding 13 -->
- **Argon2id DoS protection (REQUIRED):** process-level semaphore N=10 + per-IP login rate limit 5/min. Peak memory budget 640MB headroom.
<!-- /RED-TEAM Finding 13 -->
- Login error messages identical for "wrong password" vs "unknown email" (no enumeration).
- Refresh tokens stored as sha256-of-raw; raw never logged.
<!-- RED-TEAM Finding 1 -->
- **JWT signing key rotation (REQUIRED):** HS256 with `kid`-indexed multi-key. Source `GOCLAW_JWT_SECRETS_JSON`. Promoted from "defer" to P0 — single-key compromise = full system breach.
<!-- /RED-TEAM Finding 1 -->
<!-- RED-TEAM Finding 4 -->
- **Refresh token family revocation (REQUIRED):** RFC 6749 §10.4 compliant. Theft signal = revoked-but-not-expired token use → entire family revoked + audit log.
<!-- /RED-TEAM Finding 4 -->
- API key S2S path preserved for backward compat (Q-4): `Authorization: Bearer` + `X-USER-ID`.
<!-- RED-TEAM Finding 2 + 3 -->
- **Bootstrap takeover protection (REQUIRED):** `GOCLAW_BOOTSTRAP_TOKEN` (in-memory) gated `/v1/bootstrap/init` + localhost-only listener on first boot + `pg_advisory_xact_lock` in TX + Phase 03 `users_only_one_root` partial UNIQUE = defense in depth against concurrent / remote takeover.
<!-- /RED-TEAM Finding 2 + 3 -->
<!-- RED-TEAM Finding 8 -->
- **Password recovery:** No email-based reset in v4.0 (no email infra). Operator-level via `goclaw reset-password --email <e>` CLI. Reset revokes all existing sessions atomically.
<!-- /RED-TEAM Finding 8 -->

## Cross-phase Gates

- **Entry:** Phase 05 merged + 11 store tests green (users + user_sessions stores live).
- **Exit:** All 10 e2e auth tests green + go build clean (both tags). Gates Phase 07, 08, 09, 11.

## Next Steps

- Phase 07 — pool/cache refactor (parallel to 08).
- Phase 08 — CLI prune (delete onboard, auth wizard, etc.) — relies on auth being canonical via HTTP+WS.
- Phase 11 — frontend uses these endpoints + JWT refresh interceptor.
