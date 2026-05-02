# GoClaw v4 E2E Test Harness

End-to-end test foundation for the v4 schema rebuild. Tests run against:

- Real PostgreSQL 18 + pgvector on port **5435** (DB: `goclaw_v4_e2e`)
- Real LLM providers (Bailian + OpenRouter) via `env.e2e-tests/.env`
- A locally compiled `goclaw` binary spawned per package

Build tag: `//go:build e2e` — isolated from v3 `integration` tests.

## Prerequisites

- Docker (or local PG18 + pgvector on `127.0.0.1:5435`)
- `env.e2e-tests/.env` populated (see `env.e2e-tests/README.md` to generate)
- POSIX shell (Linux / macOS) — `helpers/gateway.go` uses POSIX process groups for clean shutdown; build tag `e2e && !windows` excludes Windows

## Quick start

```bash
# Bring up the e2e Postgres (idempotent — reuses existing container)
make e2e-pg-up

# Run the entire e2e suite
make test-e2e

# Tear down (when finished for the day)
make e2e-pg-down
```

## Layout

```
tests/e2e/
├── helpers/                    Shared fixtures/helpers (build tag e2e)
│   ├── env.go                  env.e2e-tests/.env loader + typed accessors
│   ├── reset_db.go             ResetDB(t): TRUNCATE all + reseed root
│   ├── api_client.go           HTTP wrapper with bearer-token injection
│   ├── ws_client.go            WS protocol wrapper (req/res/event)
│   ├── fixtures.go             SeedUser, SeedAgent, RandHex8, RandEmail, LoginAs
│   ├── gateway.go              StartGateway(t): build + spawn + healthcheck
│   ├── assertions.go           AssertStatus / AssertJSONEq / AssertEventType
│   └── fixtures_test.go        Self-tests for the helpers
└── 00_harness_self_test.go     Env / PG / ResetDB sanity checks (Phase 01)
```

Later phases add `01_bootstrap_test.go`, `02_login_test.go`, etc.

## Per-test isolation (R5)

Every fixture (`SeedUser`, `RandEmail`, …) appends `crypto/rand`-based 8-hex
suffix so concurrent `t.Parallel()` tests never collide on UNIQUE columns.

## When LLM tests run

Real-LLM tests gate themselves with `if testing.Short() { t.Skip(...) }`.

```bash
# Skip LLM-heavy paths (CI per-PR cadence)
go test -tags e2e -short ./tests/e2e/...

# Full run (nightly)
make test-e2e
```

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `e2e env file not found` | Test run from outside repo | `cd` to repo root |
| `connect refused 127.0.0.1:5435` | PG container down | `make e2e-pg-up` |
| `e2e: required env var X is empty` | `.env` missing key | regenerate per `env.e2e-tests/README.md` |
| `gateway not ready` | port collision OR DB unreachable | check `dev-pgvector` container + free port |

## Adding new e2e tests

1. Create `tests/e2e/NN_feature_test.go` with `//go:build e2e` and `package e2e_test`.
2. Import `github.com/nextlevelbuilder/goclaw/tests/e2e/helpers`.
3. Begin each test with `helpers.MustLoadEnv()` then `helpers.ResetDB(t)` for isolation.
4. Use `helpers.RandEmail("prefix")` for any UNIQUE columns (R5 collision fix).
5. Spawn the gateway via `gw := helpers.StartGateway(t)` — t.Cleanup tears down.

See Phase 01 plan: `plans/260502-1635-v4-epic-04-schema/phase-01-test-harness-bootstrap.md`.
