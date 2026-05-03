# Phase 11 — Frontend Bootstrap + Login + Refresh Rotation (ui/web)

## Context Links

- Master § 4.11 (Frontend, 65 ui/web files)
- Decisions Q-6 (single-page form), Q-B (refresh), Q-D (rotate-on-use)
- Phase 06 (HTTP auth endpoints + JWT/refresh)
- Phase 09 (channels endpoints — FE channels page)
- Phase 10 (skills endpoints — FE skills page)

## Overview

- Priority: P0 (UX-critical)
- Status: pending
- Effort: 25 dev-days
- Description: New `/bootstrap` + `/login` + `/profile` pages. Refactor 65 ui/web/src/ files to drop tenant refs (~9226 LOC). Auth context with JWT refresh interceptor (rotate-on-use). 503 redirect to `/bootstrap`. Add ~30-40 i18n keys × 3 languages = ~90-120 entries. Drop `/tenants-admin/` (2 files).

<!-- RED-TEAM Finding 11: Phase 11 file count 65 vs 693 — estimate basis questionable (CRITICAL — verify) -->
**File count caveat:** "65 files" claim is based on tenant-ref grep. Real total non-test ts/tsx in `ui/web/src/` = 693 files (84,833 LOC). Tenant-ref grep yields 67 files (verified). If only tenant-ref files matter, the claim is correct. If broader Zustand/router/api-client cleanup is needed, scope expands.

**Day 0 step (BLOCKING phase start):**
1. Re-scout `ui/web/src/` properly: walk call graph from `stores/auth.ts` outward.
2. Enumerate every file that touches: tenant identifiers, auth tokens, WS connect frame, login/bootstrap routes.
3. Adjust phase estimate based on real scope. Likely range: 8-18d for tenant sweep + auth, NOT necessarily 25d.
4. Defer non-essential UI work to v4.0.1 if scope balloons (e.g., profile page enhancements 11D, e2e browser test deferred to FE owner discretion).
5. Update `## Overview > Effort` line in this file with re-scouted estimate before starting Sub-11A.
<!-- /RED-TEAM Finding 11 -->

## Key Insights

- 65 ui/web FE files affected (~9226 LOC) — most are mechanical tenant ref drops.
- 3 new pages: `/bootstrap`, `/login`, `/profile`.
- Refresh interceptor must handle race (multiple parallel requests during rotation) — use single-flight pattern.
- Vite hot reload friendly — refactor incrementally per route.
- Existing pages dirs: `pages/{activity,agents,api-keys,approvals,backup-restore,builtin-tools,channels,chat,cli-credentials,config,contacts,cron,events,hooks,import-export,knowledge-graph,login,logs,mcp,memory,nodes,overview,packages,pending-messages,providers,sessions,setup,skills,storage,teams}` (verified live).
- `pages/login` exists already — refactor for v4 password auth.
- `pages/setup` exists — refactor → bootstrap form.

## Tests to write FIRST (TDD red step)

| Test file | Cases (must FAIL until impl green) |
|---|---|
| `ui/web/src/pages/bootstrap/__tests__/bootstrap.test.tsx` | `Bootstrap renders form`, `Bootstrap submits valid payload`, `Bootstrap shows error on weak password`, `Bootstrap redirects to /chat post-success` |
| `ui/web/src/pages/login/__tests__/login.test.tsx` | `Login renders form`, `Login submits and stores tokens`, `Login redirects to /bootstrap when bootstrap_required`, `Login shows error on invalid credentials` |
| `ui/web/src/auth/__tests__/auth-context.test.tsx` | `AuthContext provides user state`, `Logout clears tokens`, `Refresh interceptor rotates on 401`, `Refresh single-flight prevents duplicate requests` |
| `ui/web/src/auth/__tests__/refresh-interceptor.test.tsx` | `Refresh on 401 retries original request`, `Refresh failure logs out`, `Multiple parallel 401s share single refresh call (single-flight)` |
| `ui/web/src/__tests__/no-tenant-refs.test.ts` | Static AST scan: `grep -rn 'tenant\|Tenant' ui/web/src/` returns 0 hits in non-test code |
| `tests/e2e/11_fe_bootstrap_e2e_test.go` | Browser-driven via Playwright/rod: open `/`, redirected to `/bootstrap` (503), submit form, redirected to dashboard, refresh page → still authenticated |

**Red verification:** Frontend tests fail (pages don't exist / tenant refs still present).

## Requirements

### Functional

#### NEW pages

- `ui/web/src/pages/bootstrap/index.tsx`:
  - Single-page form: email + password + display_name.
  - Calls `GET /v1/bootstrap/status` on mount; if `bootstrapped:true` → redirect to `/login`.
  - Submits `POST /v1/bootstrap/init` → on success: store tokens via auth context → redirect to `/chat` (default landing).
  - Form validation: email format, password complexity (≥12 chars + 1 letter + 1 digit + 1 symbol), display_name ≥ 2 chars.
  - Error display via i18n.
- `ui/web/src/pages/login/index.tsx` (refactor existing):
  - email + password fields (drop tenant_slug if present).
  - On 503 from any API → redirect to `/bootstrap`.
  - Submit `POST /v1/auth/login` → store tokens → redirect to `/chat`.
- `ui/web/src/pages/profile/index.tsx`:
  - Display current user (from `/v1/auth/me`).
  - Update display_name + password (`PATCH /v1/users/:id`).
  - Logout button → `POST /v1/auth/logout` + clear tokens + redirect `/login`.

#### NEW auth module

<!-- RED-TEAM Finding 5: localStorage XSS = full account takeover (CRITICAL) -->
**Token storage strategy (CRITICAL — overrides Q-B "store in localStorage"):**

- **Refresh token:** HttpOnly + SameSite=Strict + Secure cookie. Path scope = `/v1/auth/refresh` (cookie attached only to refresh requests). Max-Age = 30 days. Set by backend on `/v1/auth/login` response, NOT readable from JS.
- **Access token:** in-memory only (React state / Zustand store). NEVER persisted to localStorage / sessionStorage / IndexedDB. Page reload = silent refresh (cookie still attached) → new access token in memory.
- **Backend changes (Phase 06 ripple):**
  - `/v1/auth/login` response: `Set-Cookie: refresh_token=<token>; HttpOnly; Secure; SameSite=Strict; Path=/v1/auth/refresh; Max-Age=2592000`. Body returns access_token only.
  - `/v1/auth/refresh`: reads cookie (no body), rotates, sets new cookie + returns new access in body.
  - `/v1/auth/logout`: clears cookie via `Set-Cookie: refresh_token=; Max-Age=0`.
- **CSP header (REQUIRED):** `Content-Security-Policy: default-src 'self'; script-src 'self' 'nonce-{random}'; style-src 'self' 'unsafe-inline'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'`. Nonce regenerated per page load.
- **Trusted Types policy:** add `Content-Security-Policy: require-trusted-types-for 'script'; trusted-types default` for browsers that support it. React 19 ships Trusted Types-compatible default. Custom HTML injection sites (e.g., agent description rendered as markdown) MUST use DOMPurify.
- **DOMPurify** for any user-rendered HTML contexts: agent name, KG entity description, vault doc titles, channel contact display name, anywhere user input renders to DOM via `dangerouslySetInnerHTML` or markdown→HTML pipelines.
- **Migration rule:** any existing localStorage token reference in pre-v4 code MUST be deleted. Audit grep `localStorage\.(getItem|setItem).*(token|jwt|access|refresh)` repo-wide.
<!-- /RED-TEAM Finding 5 -->

- `ui/web/src/auth/AuthContext.tsx`:
  - React context: `{ user, accessToken, isAuthenticated, login, logout }`.
  - On mount: silent refresh via `POST /v1/auth/refresh` (cookie attached) → access token loaded into memory → call `/v1/auth/me` to populate user.
  - Refresh on app focus / visibility change.
- `ui/web/src/auth/refresh-interceptor.ts`:
  - Axios/fetch interceptor wraps API client.
  - On 401: try refresh via `POST /v1/auth/refresh` (cookie auto-attached); on success: retry original; on failure: logout + redirect.
  - Single-flight: parallel 401s share one refresh call (mutex/promise pattern).
  - Configure axios `withCredentials: true` for refresh endpoint to send cookie.
- `ui/web/src/auth/bootstrap-redirect.ts`:
  - Global response interceptor: if 503 + `bootstrap_required` flag → redirect to `/bootstrap`.
- `ui/web/src/auth/jwt.ts`:
  - Decode JWT, parse `exp`, check expiry locally to preempt 401 (proactive refresh < 60s before expiry).

#### REFACTOR existing files

- `ui/web/src/api/client.ts` (or similar HTTP client) — drop tenant header injection if present.
- `ui/web/src/api/ws.ts` (WS client) — drop `tenantId` from `connect` frame; add `accessToken` from auth context.
- `ui/web/src/stores/auth.ts` (Zustand) — refactor for password auth + refresh.
- `ui/web/src/components/Sidebar.tsx` (or AppShell) — drop tenant switcher; show user display_name.
- `ui/web/src/components/Topbar.tsx` — same.
- All page components reading tenant from store — drop tenant refs (~60+ files).
- React Router setup — add `/bootstrap`, `/login`, `/profile` routes; protect non-bootstrap/login routes via auth gate.

#### DROP pages

- `ui/web/src/pages/tenants-admin/` (2 files; verify via `ls ui/web/src/pages/tenants-admin/` during impl — listing did not show this dir; may already be removed or named differently).

#### NEW i18n keys

In `ui/web/src/i18n/locales/{en,vi,zh}/auth.json` (NEW namespace):
- `bootstrap.title`, `bootstrap.email`, `bootstrap.password`, `bootstrap.displayName`, `bootstrap.submit`, `bootstrap.success`, `bootstrap.error.weak_password`, `bootstrap.error.invalid_email`, `bootstrap.error.idempotent`
- `login.title`, `login.email`, `login.password`, `login.submit`, `login.error.invalid_credentials`, `login.forgot_password`
- `auth.logout`, `auth.session_expired`, `auth.refresh_failed`
- `profile.title`, `profile.display_name`, `profile.change_password`, `profile.save`, `profile.logout`
- ~30 keys × 3 languages = ~90 entries

### Non-functional

- React 19 + Vite 6 + Tailwind 4 + Radix UI + Zustand stack (project conv).
- pnpm (NOT npm) — verified via CLAUDE.md.
- Mobile UI/UX rules apply (h-dvh, text-base md:text-sm, safe areas — per CLAUDE.md).
- Bundle size impact < 50KB gzip from new auth module.

## Architecture

```
FE auth flow:
  1. App mount → AuthContext.tsx checks localStorage for tokens
  2. If tokens present → call /v1/auth/me to verify
     ├─ 200 → set isAuthenticated=true
     └─ 401 → try refresh (interceptor) → fail → clear tokens → redirect /login
  3. If no tokens → redirect /login
  4. /login submit → POST /v1/auth/login → store tokens → redirect /chat
  5. Any API 503 + bootstrap_required → redirect /bootstrap
  6. /bootstrap submit → POST /v1/bootstrap/init → tokens returned → redirect /chat

Refresh interceptor (single-flight):
  axios.interceptors.response.use(success, async (err) => {
    if (err.status === 401 && !err.config._retry) {
      err.config._retry = true;
      const newTokens = await refreshSingleFlight();  // dedupe parallel calls
      err.config.headers.Authorization = `Bearer ${newTokens.access}`;
      return axios.request(err.config);
    }
    throw err;
  });
```

## Related Code Files

### Create

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/web/src/pages/bootstrap/index.tsx`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/web/src/pages/bootstrap/bootstrap-form.tsx`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/web/src/pages/bootstrap/__tests__/bootstrap.test.tsx`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/web/src/pages/profile/index.tsx`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/web/src/pages/profile/profile-form.tsx`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/web/src/pages/login/__tests__/login.test.tsx`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/web/src/auth/auth-context.tsx`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/web/src/auth/refresh-interceptor.ts`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/web/src/auth/bootstrap-redirect.ts`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/web/src/auth/jwt.ts`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/web/src/auth/__tests__/auth-context.test.tsx`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/web/src/auth/__tests__/refresh-interceptor.test.tsx`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/web/src/__tests__/no-tenant-refs.test.ts`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/web/src/i18n/locales/en/auth.json`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/web/src/i18n/locales/vi/auth.json`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/web/src/i18n/locales/zh/auth.json`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/11_fe_bootstrap_e2e_test.go`

### Modify (~65 files)

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/web/src/pages/login/index.tsx` (refactor for password)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/web/src/api/client.ts` (drop tenant header)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/web/src/api/ws.ts` (drop tenantId, add accessToken)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/web/src/stores/auth.ts`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/web/src/App.tsx` (wrap with AuthContext + BrowserRouter routes)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/web/src/main.tsx` (i18next init for `auth` namespace)
- 60+ files in `ui/web/src/pages/*` and `ui/web/src/components/*` — drop tenant refs

### Delete

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/ui/web/src/pages/tenants-admin/` (verify exists; if not, skip)

## Implementation Steps

### Sub-phase 11A — Auth module foundation (5 days)

1. Verify Phase 06 merged (auth endpoints + JWT/refresh live).
2. Add 3 `auth.json` i18n locale files (en/vi/zh) with ~30 keys each.
3. Init i18next namespace in `ui/web/src/main.tsx`.
4. Write FE auth tests (red).
5. Write `ui/web/src/auth/jwt.ts` — decode + expiry helpers.
6. Write `ui/web/src/auth/auth-context.tsx` — Zustand-backed context.
7. Write `ui/web/src/auth/refresh-interceptor.ts` — single-flight refresh.
8. Write `ui/web/src/auth/bootstrap-redirect.ts` — 503 redirect.
9. Wire interceptors into `ui/web/src/api/client.ts`.
10. `pnpm test` for auth module → green.

### Sub-phase 11B — Bootstrap + Login + Profile pages (8 days)

1. Refactor `ui/web/src/pages/login/index.tsx` — drop tenant_slug; password form; redirect on 503.
2. Create `ui/web/src/pages/bootstrap/index.tsx` + `bootstrap-form.tsx`.
3. Create `ui/web/src/pages/profile/index.tsx` + `profile-form.tsx`.
4. Add routes to `ui/web/src/App.tsx` BrowserRouter.
5. Wire AuthContext provider at App root.
6. `pnpm test` page tests → green.
7. Manual smoke: `pnpm dev` → visit `/`, observe redirect chain.

### Sub-phase 11C — Drop tenant refs from 60+ files (10 days)

1. `grep -rn 'tenant\|tenantId\|Tenant' ui/web/src/ --include='*.ts*'` enumerate.
2. For each file:
   a. Drop tenant from props, store reads, API calls.
   b. If file is now empty/dead → delete.
3. After each batch (10 files): `pnpm tsc --noEmit` for type errors.
4. Run no-tenant-refs static test → green.
5. `pnpm build` clean.

### Sub-phase 11D — E2E browser test (2 days)

1. Write `tests/e2e/11_fe_bootstrap_e2e_test.go` using rod (already in CLAUDE.md tech stack).
2. Test: open `/`, expect `/bootstrap` redirect (503), submit form, expect dashboard, refresh page, expect still authenticated.
3. Run e2e test → green.

## Todo List

<!-- RED-TEAM Finding 11 todo -->
### Day 0 (BLOCKING phase start)
- [ ] (Finding 11) Re-scout `ui/web/src/` from `stores/auth.ts` call graph
- [ ] (Finding 11) Adjust effort estimate; defer non-essentials to v4.0.1
- [ ] (Finding 11) Update phase Overview > Effort line

### Sub-11A
- [ ] auth.json i18n × 3 languages
- [ ] auth jwt.ts + tests
- [ ] auth-context.tsx + tests
- [ ] refresh-interceptor.ts + tests (single-flight)
- [ ] bootstrap-redirect.ts
- [ ] api/client.ts wiring
<!-- RED-TEAM Finding 5 todos -->
- [ ] (Finding 5) Backend `/v1/auth/login` sets HttpOnly cookie (Phase 06 ripple)
- [ ] (Finding 5) Backend `/v1/auth/refresh` reads cookie, rotates, sets new cookie
- [ ] (Finding 5) Backend `/v1/auth/logout` clears cookie
- [ ] (Finding 5) Frontend axios `withCredentials: true` for refresh endpoint
- [ ] (Finding 5) Access token stored in-memory only (audit `localStorage` token references = 0)
- [ ] (Finding 5) CSP header configured at backend
- [ ] (Finding 5) Trusted Types policy enabled
- [ ] (Finding 5) DOMPurify wrapper for HTML-rendered user input

### Sub-11B
- [ ] login/index.tsx refactor
- [ ] bootstrap/index.tsx + bootstrap-form.tsx
- [ ] profile/index.tsx + profile-form.tsx
- [ ] App.tsx routes
- [ ] AuthContext provider wired

### Sub-11C
- [ ] Tenant ref enumeration
- [ ] 60+ files refactored (drop tenant)
- [ ] tenants-admin/ deleted (if exists)
- [ ] no-tenant-refs static test green
- [ ] pnpm tsc --noEmit clean
- [ ] pnpm build clean

### Sub-11D
- [ ] e2e browser test (rod)
- [ ] E2E green

## Success Criteria

- All FE unit tests green.
- e2e browser test green (full bootstrap → login flow).
- `pnpm tsc --noEmit` clean.
- `pnpm build` produces clean bundle (no tenant refs).
- 503 → /bootstrap redirect works.
- Refresh single-flight: 5 parallel 401s → 1 refresh call (verified by mock).
- Mobile UX rules respected (h-dvh on root layout, safe areas, text-base inputs).

## Risk Assessment

| Risk | Severity | Mitigation |
|---|---|---|
| Refresh interceptor infinite loop on persistent 401 | High | `_retry` flag prevents re-entry; logout after one failure |
| Single-flight race during page refresh | Med | Mutex via `Promise` reuse; pattern tested in 11A unit tests |
| 60+ file refactor introduces UI regression | High | Component-level test coverage via React Testing Library; manual smoke per page |
| i18n key missing at runtime | Med | i18next missing-key handler logs to console; staging review catches |
| Mobile UX broken on small screens | Med | CLAUDE.md rules followed; manual viewport test |
| Tenant ref grep false positives ("tenant" in copy text) | Low | Static test scopes to identifiers only (regex `\\btenant\\w*\\b`) |

## Security Considerations

<!-- RED-TEAM Finding 5: localStorage XSS replacement -->
- **Refresh token in HttpOnly cookie** (NOT localStorage). Path-scoped to `/v1/auth/refresh`. SameSite=Strict + Secure flags. JS cannot read.
- **Access token in-memory only** (Zustand or React state). Never persisted. Page reload triggers silent refresh via cookie.
- **CSP header** at backend: `default-src 'self'; script-src 'self' 'nonce-{random}'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'`.
- **Trusted Types** policy: `require-trusted-types-for 'script'`.
- **DOMPurify** for all user-rendered HTML: agent name, KG entity, vault title, channel contact display name, markdown-rendered descriptions.
<!-- /RED-TEAM Finding 5 -->
- Refresh token never in URL.
- Logout clears in-memory access token + invokes `POST /v1/auth/logout` (which clears cookie server-side).
- Form input validation client-side; server is source of truth.
- Password field `type="password"` + autocomplete=current-password.
- 503 interceptor — never redirect on cross-origin requests (scope to API base URL).

## Cross-phase Gates

- **Entry:** Phase 06 merged (HTTP auth) + Phase 09 merged (channels endpoints) + Phase 10 merged (skills endpoints).
- **Exit:** All FE tests + e2e browser test green + pnpm build clean. Gates Phase 13 (cleanup), Phase 14 final. (Phase 12 deferred → EPIC-05-desktop per V2.)

## Next Steps

- ~~Phase 12 desktop reuse~~ → moved to EPIC-05-desktop (Validation V2). FE auth module copied verbatim into `ui/desktop/frontend/` at that EPIC's start (Option B per V3).
- Phase 13 — final cleanup (MasterTenantID purge across 81 files per F15).
