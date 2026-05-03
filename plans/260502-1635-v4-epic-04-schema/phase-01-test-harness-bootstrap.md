# Phase 01 — Test Harness + E2E Bootstrap (TDD foundation)

## Context Links

- Master § 11 (E2E Test APIs): `plans/260502-1323-goclaw-v4-brainstorm/reports/master-260502-1555-epic-04-research.md`
- E2E env: `/Users/viettran/Documents/coding/next-level-builder/goclaw/env.e2e-tests/.env`
- R5 (parallel collision): master § 5
- Existing tests dirs: `tests/{integration,contracts,scenarios,invariants,zalo_e2e}`

## Overview

- Priority: P0 (blocker for all impl phases)
- Status: completed
- Effort: 5 dev-days
- Description: Build red-tests-first foundation. Real PG18 + pgvector port 5435 (`goclaw_v4_e2e`). Real LLM (Bailian + OpenRouter). Reusable helpers: `seedUser`, `seedAgent`, `randHex8`, `ResetDB`, `LoginAs`. All later phases consume this harness.

## Key Insights

- Existing harness uses `//go:build integration` (port 5433). v4 uses NEW `//go:build e2e` (port 5435) parallel to keep v3 patterns isolated.
- R5 fix mandatory: random-suffix email per test; `t.Parallel()` safe.
- LLM gating: `if testing.Short() { t.Skip(...) }` for real-call tests.
- Reset strategy: TRUNCATE ... CASCADE between tests, NOT drop+migrate (3-5x faster).
- Build artifact: gateway binary spawned per package via `t.Setenv` + helper.

## Tests to write FIRST (TDD red step)

These tests MUST live + fail until later phases ship. They're the spec.

| Test file | Cases (skeleton, all must FAIL initially) |
|---|---|
| `tests/e2e/00_harness_self_test.go` | `TestE2EEnvLoaded` (asserts BAILIAN_API_KEY + OPENROUTER_API_KEY present), `TestPgConnect` (real PG ping), `TestResetDB` (TRUNCATE leaves 0 rows in `users`, `agents`) |
| `tests/e2e/helpers/fixtures_test.go` | `TestRandHex8Uniqueness` (1000 calls, 0 collisions), `TestSeedUserUUID` (returns valid UUID, email globally unique) |

**Verify red:** `go test -tags e2e -v -run TestE2EEnv ./tests/e2e/` → fail compile (no harness yet) → write impl → green.

## Requirements

### Functional

- `tests/e2e/helpers/api_client.go`: HTTP client wrapping `http.Client` + base URL + bearer-token injection. Methods: `GET/POST/PATCH/DELETE` returning `*http.Response` + parsed body.
- `tests/e2e/helpers/ws_client.go`: WS client (gorilla/websocket) supporting `connect`+`req`+`event` frames; helpers `Connect(userID, jwt)`, `SendReq(method, params)`, `WaitEvent(timeout)`.
- `tests/e2e/helpers/fixtures.go`: `SeedUser(t, opts)`, `SeedAgent(t, ownerID, type)`, `RandHex8()`, `RandEmail(prefix)`, `LoginAs(t, email, password) → tokens`.
- `tests/e2e/helpers/reset_db.go`: `ResetDB(t)` — TRUNCATE all v4 tables + re-insert root user (idempotent). Discover tables via `pg_catalog.pg_tables` schema=public.
- `tests/e2e/helpers/gateway.go`: `StartGateway(t)` — go build + spawn binary; cleanup via `t.Cleanup`. Health-check via `GET /healthz` until ready (10s timeout).
- `tests/e2e/helpers/env.go`: load `env.e2e-tests/.env`; expose `BailianKey()`, `OpenRouterKey()`, `RootEmail()`, `RootPassword()`.
- `Makefile` target: `test-e2e` running `go test -tags e2e -v -timeout 30m ./tests/e2e/...`.

### Non-functional

- All e2e tests `t.Parallel()`-safe (random-suffix R5).
- ResetDB completes < 200ms (TRUNCATE not DROP).
- Gateway spawn < 5s.
- LLM-calling tests gated by `-short` flag.
- Per-test isolation: each test gets fresh user(s), no shared state.

## Architecture

```
┌── tests/e2e/ ─────────────────────────────────────┐
│  helpers/                                         │
│   ├─ env.go         (load env.e2e-tests/.env)     │
│   ├─ api_client.go  (HTTP wrapper)                │
│   ├─ ws_client.go   (WS wrapper)                  │
│   ├─ fixtures.go    (SeedUser/Agent, randHex8)    │
│   ├─ reset_db.go    (TRUNCATE + reseed)           │
│   ├─ gateway.go     (spawn + healthcheck)         │
│   └─ assertions.go  (custom asserts)              │
│  00_harness_self_test.go                          │
└───────────────────────────────────────────────────┘
        │ used by phases 06,09,10,11,12,14
        ▼
   tests/e2e/01_bootstrap_test.go ... 20_backup_test.go
```

## Related Code Files

### Create

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/helpers/env.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/helpers/api_client.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/helpers/ws_client.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/helpers/fixtures.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/helpers/reset_db.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/helpers/gateway.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/helpers/assertions.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/00_harness_self_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/helpers/fixtures_test.go`
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/e2e/README.md` (how to run)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/scripts/e2e-pg-up.sh` (docker run pgvector pg18 port 5435)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/scripts/e2e-pg-down.sh`

### Modify

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/Makefile` — add `test-e2e`, `e2e-pg-up`, `e2e-pg-down` targets

### Read for context

- `/Users/viettran/Documents/coding/next-level-builder/goclaw/tests/integration/abort_test_helpers.go` (existing helper pattern)
- `/Users/viettran/Documents/coding/next-level-builder/goclaw/env.e2e-tests/.env`

## Implementation Steps

1. Create `scripts/e2e-pg-up.sh` — `docker run -d --name goclaw_v4_e2e_pg -p 5435:5432 -e POSTGRES_DB=goclaw_v4_e2e -e POSTGRES_USER=dev -e POSTGRES_PASSWORD=devpass pgvector/pgvector:pg18`. Wait for ready (`pg_isready` loop max 30s).
2. Create `scripts/e2e-pg-down.sh` — `docker rm -f goclaw_v4_e2e_pg`.
3. Add Makefile targets: `e2e-pg-up`, `e2e-pg-down`, `test-e2e: e2e-pg-up && go test -tags e2e -v -timeout 30m ./tests/e2e/...`.
4. Write `tests/e2e/helpers/env.go` — `godotenv.Load("env.e2e-tests/.env")`. Expose accessors. Build tag `//go:build e2e`.
5. Write `tests/e2e/helpers/reset_db.go` — `ResetDB(t)` queries `pg_tables WHERE schemaname='public'` and runs `TRUNCATE ... CASCADE`. Re-inserts root user from `E2E_ROOT_EMAIL` + bcrypt-of `E2E_ROOT_PASSWORD` (Argon2id once impl ready in P06; for P01 use placeholder hash that bootstrap test will overwrite).
6. Write `tests/e2e/helpers/api_client.go` — `Client{baseURL, token, http.Client}`. Methods return `(*http.Response, []byte, error)`. Auto-inject `Authorization: Bearer <token>` if set. JSON marshal body.
7. Write `tests/e2e/helpers/ws_client.go` — wraps gorilla/websocket. `Connect(userID, jwt) error`, `SendReq(method, params) error`, `WaitEvent(eventType, timeout) (Event, error)`. JSON frames per `pkg/protocol`.
8. Write `tests/e2e/helpers/fixtures.go` — `RandHex8()` via `crypto/rand`. `SeedUser(t, opts) *User` calls `POST /v1/users` (or direct DB insert pre-P06). `SeedAgent(t, ownerID, agentType) *Agent`.
9. Write `tests/e2e/helpers/gateway.go` — `StartGateway(t) *Gateway` runs `go build -o /tmp/goclaw-e2e .` then spawns with env vars, polls `GET /healthz` until OK, registers `t.Cleanup` to kill + drain.
10. Write `tests/e2e/helpers/assertions.go` — `AssertJSONEq(t, want, got)`, `AssertStatus(t, resp, code)`, `AssertEventType(t, ev, want)`.
11. Write `tests/e2e/00_harness_self_test.go` — verifies env loaded, PG connect works, ResetDB leaves zero rows, gateway starts.
12. Write `tests/e2e/helpers/fixtures_test.go` — randomness + UUID validity tests.
13. Add `tests/e2e/README.md` — how to run, prerequisite docker, debug tips.
14. Run `go vet` + `go build -tags e2e ./tests/e2e/...` to confirm compile.
15. Run `make test-e2e` — only `00_harness_self_test.go` should pass; other test files don't exist yet.

## Todo List

- [x] scripts/e2e-pg-up.sh + e2e-pg-down.sh
- [x] Makefile targets (test-e2e, e2e-pg-up, e2e-pg-down)
- [x] tests/e2e/helpers/env.go
- [x] tests/e2e/helpers/reset_db.go
- [x] tests/e2e/helpers/api_client.go
- [x] tests/e2e/helpers/ws_client.go
- [x] tests/e2e/helpers/fixtures.go
- [x] tests/e2e/helpers/gateway.go
- [x] tests/e2e/helpers/assertions.go
- [x] tests/e2e/00_harness_self_test.go (red → green)
- [x] tests/e2e/helpers/fixtures_test.go (red → green)
- [x] tests/e2e/README.md
- [x] Verify `go test -tags e2e ./tests/e2e/...` passes harness self-tests

## Success Criteria

- `make e2e-pg-up && make test-e2e` runs cleanly, harness self-tests pass.
- ResetDB completes < 200ms on empty DB.
- Gateway starts in < 5s, `/healthz` returns 200.
- 1000-iteration `randHex8()` test produces 0 duplicates.
- All helpers compile with `-tags e2e`.
- `go vet ./tests/e2e/...` clean.

## Risk Assessment

| Risk | Mitigation |
|---|---|
| Docker not available on dev machine | README documents prereq; CI skips e2e job if no docker |
| pgvector pg18 image unavailable | Pin to `pgvector/pgvector:pg18` Docker Hub tag; vendor fallback URL in script |
| Gateway port 18790 collision | Helper picks random free port via `net.Listen(":0")`, sets `GOCLAW_PORT` env var |
| LLM API rate limiting | Real-LLM tests gated by `testing.Short()`; document `go test -short` to skip |
| Helpers leak goroutines | `t.Cleanup` registers gateway shutdown + WS close |

## Security Considerations

- env file `env.e2e-tests/.env` already gitignored; harness reads via godotenv.
- API keys never logged (helpers redact `Authorization` from request dumps).
- Root password hash placeholder uses Argon2id-compatible default once P06 ships.

## Cross-phase Gates

- **Entry:** None (foundational — first phase).
- **Exit:** Harness self-tests green + `go vet` + `go build -tags e2e` clean. Gates Phase 03+04+05+06+09+10+11+12+14.

## Completion Log

**Completed:** 2026-05-02

**Test count:** 6/6 green (harness self-tests all passing)

**Reviewer findings fixed:** B1 (WS frame format aligned with `pkg/protocol/frames.go`), H2 (POSIX-only build tag on gateway.go), M2 (WS failPending non-blocking sends avoid deadlock)

**Reviewer report:** `/Users/viettran/Documents/coding/next-level-builder/goclaw/plans/reports/code-reviewer-260502-1755-phase01-e2e-harness.md`

**Verification:** `go vet -tags e2e ./tests/e2e/...` clean, `go build -tags e2e ./tests/e2e/...` clean, `go build ./...` clean.

## Next Steps

- Phase 02 (paper analysis of v3 baseline) starts immediately after entry gate.
- Phase 03 + 04 (PG + SQLite schema) gated by Phase 01 exit (helpers needed for migration round-trip tests).
