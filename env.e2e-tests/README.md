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
