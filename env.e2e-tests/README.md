# GoClaw v4 — E2E Test Environment

Storage cho env keys/values dùng cho E2E test toàn hệ thống v4 sau migration.

## Setup

### 1. Database (đã ready)

```bash
# Container PG vector 18 đang chạy:
PGPASSWORD=devpass psql -h 127.0.0.1 -p 5435 -U dev -d postgres

# DB e2e đã tạo: goclaw_v4_e2e
# Extensions: vector, pgcrypto

# Verify:
PGPASSWORD=devpass psql -h 127.0.0.1 -p 5435 -U dev -d goclaw_v4_e2e -c "\dx"
```

### 2. Env file

```bash
cp env.e2e-tests/.env.example env.e2e-tests/.env
# Edit .env và paste thực tế:
#   - BAILIAN_API_KEY
#   - OPENROUTER_API_KEY
#   - GOCLAW_ENCRYPTION_KEY (openssl rand -hex 32)
#   - GOCLAW_JWT_SECRET (openssl rand -hex 32)
```

### 3. Generate secrets

```bash
echo "GOCLAW_ENCRYPTION_KEY=$(openssl rand -hex 32)"
echo "GOCLAW_JWT_SECRET=$(openssl rand -hex 32)"
```

## Usage

### Run E2E test suite

```bash
# Load env + run gateway against e2e DB
source env.e2e-tests/.env && ./goclaw migrate up
source env.e2e-tests/.env && ./goclaw

# In another shell — run E2E tests
source env.e2e-tests/.env && go test -v -tags e2e ./tests/e2e/...
```

### Reset DB between tests

```bash
PGPASSWORD=devpass psql -h 127.0.0.1 -p 5435 -U dev -d postgres \
  -c "DROP DATABASE goclaw_v4_e2e;" \
  -c "CREATE DATABASE goclaw_v4_e2e;"
PGPASSWORD=devpass psql -h 127.0.0.1 -p 5435 -U dev -d goclaw_v4_e2e \
  -c "CREATE EXTENSION vector; CREATE EXTENSION pgcrypto;"
source env.e2e-tests/.env && ./goclaw migrate up
```

## Files

| File | Purpose | Committed? |
|---|---|---|
| `.env.example` | Template với placeholder keys | ✅ Yes |
| `.env` | Actual keys (paste manually) | ❌ NO (gitignored) |
| `README.md` | This file | ✅ Yes |

## API providers configured

| Provider | Use case | Key var |
|---|---|---|
| Bailian (Alibaba Bailian Coding) | Primary E2E test provider — DashScope-compatible | `BAILIAN_API_KEY` |
| OpenRouter | Fallback + multi-model coverage | `OPENROUTER_API_KEY` |

## Security

- **NEVER commit** `.env` file
- **NEVER paste actual keys** vào markdown files / commits / PR
- API keys chỉ dùng cho E2E test, KHÔNG share
- Container PG `devpass` là local-only dev creds, không production

## E2E test scope (post-v4 migration)

E2E tests verify:
1. Bootstrap flow (`POST /v1/bootstrap/init`) idempotent + creates root user
2. Login (email + password) → JWT access + refresh token
3. Refresh token rotation
4. CRUD endpoints all entities (users, agents, teams, sessions, skills, etc.)
5. WebSocket connect with user_id (UUID) — no tenant_id
6. LLM call end-to-end (chat → real provider response)
7. Channel pairing flow
8. Multi-user isolation (memory, vault, cron, hooks)
9. RBAC (root vs admin vs member vs viewer permissions)
10. Backup/restore round-trip
11. Phase 13 cleanup gates — `MasterTenantID` purged, no `tenant_id` columns, ADRs landed (`tests/e2e/13_*.go`)

## Troubleshooting

### Tests skipped with `pg_dump not in PATH`
The schema round-trip test (`tests/e2e/schema/01_pg_migration_round_trip_test.go`)
shells out to `pg_dump`. Install PostgreSQL client tools (`brew install postgresql`
on macOS, `apt install postgresql-client` on Debian/Ubuntu) and re-run.

### `dial tcp 127.0.0.1:5435: connect: connection refused`
The dev pgvector container is not running. Bring it up with:
```bash
docker run -d --name dev-pgvector -p 5435:5432 \
  -e POSTGRES_USER=dev -e POSTGRES_PASSWORD=devpass \
  -e POSTGRES_DB=goclaw_v4_e2e \
  pgvector/pgvector:pg18
```
Then create the extensions:
```bash
PGPASSWORD=devpass psql -h 127.0.0.1 -p 5435 -U dev -d goclaw_v4_e2e \
  -c "CREATE EXTENSION IF NOT EXISTS vector; CREATE EXTENSION IF NOT EXISTS pgcrypto;"
```

### `env BAILIAN_API_KEY empty — check env.e2e-tests/.env`
You forgot to copy `.env.example` → `.env` (or forgot to source it). The harness
self-test (`tests/e2e/00_harness_self_test.go`) reports each missing key by name.

### `--- FAIL: TestNoTenantIDColumnsAnywherePG`
The Phase 13 cleanup invariant tripped. Either the schema regressed and a new
migration introduced a `tenant_id` column (revert it — v4 is single-user) or a
prior migration was not fully cleaned. Run `helpers.MustMigrateClean(t)` against
a fresh DB and re-test.

### Tests pass locally but fail in CI
The e2e suite expects exclusive DB access — a leftover row from a previous run
breaks downstream tests. Reset the DB (see "Reset DB between tests" above) and
re-run.

### `activity_logs` table growing unbounded
Expected behaviour in v4.0; see `docs/adr/2026-05-v4-activity-logs-retention-defer.md`.
Manual prune: `DELETE FROM activity_logs WHERE created_at < now() - interval '90 days';`
