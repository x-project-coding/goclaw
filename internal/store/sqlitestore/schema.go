//go:build sqlite || sqliteonly

package sqlitestore

import (
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
)

//go:embed schema.sql
var schemaSQL string

// SchemaVersion is the current SQLite schema version for v4.
// Bump this and add an entry to migrations when schema changes are needed.
// v1 → v2: adds user_key/kind/channel_type to users, team_key to agent_teams,
// and metadata to all 13 entity tables (foundation rebuild).
// v2 → v3: adds password_reset_tokens table for self-serve password reset.
// v3 → v4: splits agents.workspace_sharing JSONB into share_workspace + share_memory BOOL.
// v4 → v5: rebuilds agent_shares with target mutex (user XOR team), role enum, FK created_by, updated_at.
// v5 → v6: adds projects table (top-level entity, owner FK to users, immutable slug, status check).
// v6 → v7: adds team_user_members join table (user↔team membership with role + added_by audit trail).
// v7 → v8: adds project_grants table (project-level access control for users and teams).
// v8 → v9: adds project_id FK (SET NULL) to agent_sessions for per-session project binding.
// v9 → v10: adds default_project_id FK (SET NULL) to channel_contacts for group-chat project default.
// v10 → v11: widens idx_projects_status to cover all status values (was active-only partial index).
// Note: projects.owner_user_id ON DELETE RESTRICT was added to schema.sql for fresh DBs but does NOT
// have a v11→v12 incremental migration. SQLite cannot alter FK actions on existing tables without a
// full table-rebuild. The practical behavior is identical: SQLite's default NO ACTION (for FKs with
// PRAGMA foreign_keys=ON) already prevents deletion of owner users that have projects, same as RESTRICT.
const SchemaVersion = 11

// migrations maps version → ordered slice of SQL statements to apply when
// upgrading FROM that version to the next.
// schema.sql always represents the LATEST full schema (for fresh DBs).
// Existing DBs are patched incrementally via these steps.
//
// Add future migrations as: migrations[N] = []string{...} and bump SchemaVersion.
var migrations = map[int][]string{
	// Upgrade v1 → v2: foundation identity + metadata columns.
	// Adds stable slug identifiers (user_key, team_key), identity kind columns,
	// and a generic metadata JSONB-equivalent column to all main entity tables.
	// The shape constraint CHECK (kind/channel_type coherence) cannot be added
	// to an existing column via SQLite ALTER TABLE; new rows on upgraded DBs
	// are validated at the application layer. Fresh DBs get the full constraint
	// via schema.sql.
	1: {
		// --- users: identity slug + kind ---
		`ALTER TABLE users ADD COLUMN user_key    VARCHAR(100) NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN kind        VARCHAR(20)  NOT NULL DEFAULT 'human' CHECK (kind IN ('human','channel'))`,
		`ALTER TABLE users ADD COLUMN channel_type VARCHAR(20) NULL`,
		`ALTER TABLE users ADD COLUMN metadata    TEXT         NOT NULL DEFAULT '{}'`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_user_key ON users(user_key)`,
		// --- agent_teams: team slug ---
		`ALTER TABLE agent_teams ADD COLUMN team_key VARCHAR(100) NOT NULL DEFAULT ''`,
		`ALTER TABLE agent_teams ADD COLUMN metadata TEXT        NOT NULL DEFAULT '{}'`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_teams_team_key ON agent_teams(team_key)`,
		// --- 11 remaining entity tables: metadata column ---
		`ALTER TABLE agents            ADD COLUMN metadata TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE agent_shares      ADD COLUMN metadata TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE agent_links       ADD COLUMN metadata TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE memory_documents  ADD COLUMN metadata TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE skills            ADD COLUMN metadata TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE skill_versions    ADD COLUMN metadata TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE channel_instances ADD COLUMN metadata TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE mcp_servers       ADD COLUMN metadata TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE cron_jobs         ADD COLUMN metadata TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE llm_providers     ADD COLUMN metadata TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE system_configs    ADD COLUMN metadata TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE user_sessions     ADD COLUMN metadata TEXT NOT NULL DEFAULT '{}'`,
		// --- best-effort slug backfill for legacy rows ---
		// Derives user_key from email local-part (lowercase, strip dots/plus).
		// May produce collisions on legacy data; application layer regenerates
		// unique slugs on next gateway start if needed.
		`UPDATE users SET user_key = lower(replace(replace(substr(email, 1, instr(email||'@', '@')-1), '.', ''), '+', '')) WHERE user_key = ''`,
		`UPDATE agent_teams SET team_key = lower(replace(replace(name, ' ', '-'), '_', '-')) WHERE team_key = ''`,
	},
	// Upgrade v2 → v3: password_reset_tokens table for self-serve password reset.
	// Single-use, time-bounded; raw token mailed once, only SHA-256 hex stored.
	2: {
		`CREATE TABLE IF NOT EXISTS password_reset_tokens (
			id         TEXT NOT NULL PRIMARY KEY,
			user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			token_hash TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			used_at    TEXT,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_password_reset_token_hash ON password_reset_tokens(token_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_password_reset_user   ON password_reset_tokens(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_password_reset_active ON password_reset_tokens(token_hash) WHERE used_at IS NULL`,
	},
	// Upgrade v3 → v4: split workspace_sharing JSONB blob into two boolean
	// flags. share_workspace controls per-user file zone collapse; share_memory
	// covers memory + KG + sessions sharing. Default-false preserves
	// privacy-by-default. The legacy column is dropped.
	3: {
		`ALTER TABLE agents ADD COLUMN share_workspace INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE agents ADD COLUMN share_memory    INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE agents DROP COLUMN workspace_sharing`,
	},
	// Upgrade v4 → v5: rebuild agent_shares with target mutex + role enum.
	// Drop legacy table (greenfield, no data preservation) and recreate to
	// match PG shape. Includes FK to agent_teams via shared_with_team_id.
	4: {
		`DROP TABLE IF EXISTS agent_shares`,
		`CREATE TABLE agent_shares (
			id                  TEXT         NOT NULL PRIMARY KEY,
			agent_id            TEXT         NOT NULL REFERENCES agents(id)      ON DELETE CASCADE,
			shared_with_user_id TEXT         NULL     REFERENCES users(id)       ON DELETE CASCADE,
			shared_with_team_id TEXT         NULL     REFERENCES agent_teams(id) ON DELETE CASCADE,
			role                VARCHAR(20)  NOT NULL CHECK (role IN ('viewer','member','editor')),
			metadata            TEXT         NOT NULL DEFAULT '{}',
			created_by          TEXT         NOT NULL REFERENCES users(id)       ON DELETE RESTRICT,
			created_at          TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			updated_at          TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			CONSTRAINT agent_shares_target_mutex CHECK (
				(shared_with_user_id IS NOT NULL AND shared_with_team_id IS NULL) OR
				(shared_with_user_id IS NULL     AND shared_with_team_id IS NOT NULL)
			)
		)`,
		`CREATE INDEX idx_agent_shares_agent ON agent_shares(agent_id)`,
		`CREATE UNIQUE INDEX idx_agent_shares_user ON agent_shares(agent_id, shared_with_user_id) WHERE shared_with_user_id IS NOT NULL`,
		`CREATE UNIQUE INDEX idx_agent_shares_team ON agent_shares(agent_id, shared_with_team_id) WHERE shared_with_team_id IS NOT NULL`,
	},
	// Upgrade v5 → v6: adds projects table for top-level project entities.
	// Slug is immutable post-create (FS path coupling). Archive via status.
	5: {
		`CREATE TABLE IF NOT EXISTS projects (
			id            TEXT         NOT NULL PRIMARY KEY,
			slug          VARCHAR(100) NOT NULL UNIQUE
			                  CHECK (slug GLOB '[a-z0-9]*' AND slug NOT GLOB '*[^a-z0-9-]*' AND
			                         length(slug) >= 3 AND length(slug) <= 100 AND
			                         substr(slug, 1, 1) != '-' AND substr(slug, length(slug), 1) != '-'),
			owner_user_id TEXT         NOT NULL REFERENCES users(id),
			status        VARCHAR(20)  NOT NULL DEFAULT 'active'
			                  CHECK (status IN ('active', 'archived')),
			metadata      TEXT         NOT NULL DEFAULT '{}',
			created_at    TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			updated_at    TEXT         NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_projects_owner  ON projects(owner_user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_projects_status ON projects(status)`,
	},
	// Upgrade v6 → v7: adds team_user_members join table.
	// User↔team membership with role enum and added_by audit trail.
	// Composite PK (team_id, user_id) prevents duplicate membership rows.
	6: {
		`CREATE TABLE IF NOT EXISTS team_user_members (
			team_id    TEXT        NOT NULL REFERENCES agent_teams(id) ON DELETE CASCADE,
			user_id    TEXT        NOT NULL REFERENCES users(id)       ON DELETE CASCADE,
			role       VARCHAR(20) NOT NULL CHECK (role IN ('viewer', 'member', 'admin')),
			added_by   TEXT        REFERENCES users(id) ON DELETE SET NULL,
			created_at TEXT        NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			PRIMARY KEY (team_id, user_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_team_user_members_user ON team_user_members(user_id)`,
	},
	// Upgrade v7 → v8: adds project_grants table for project-level access control.
	// Exactly one of user_id/team_id must be set (XOR via CHECK constraint).
	// Separate partial unique indexes replicate PG UNIQUE NULLS NOT DISTINCT behaviour.
	7: {
		`CREATE TABLE IF NOT EXISTS project_grants (
			id          TEXT        NOT NULL PRIMARY KEY,
			project_id  TEXT        NOT NULL REFERENCES projects(id)     ON DELETE CASCADE,
			user_id     TEXT                 REFERENCES users(id)        ON DELETE CASCADE,
			team_id     TEXT                 REFERENCES agent_teams(id)  ON DELETE CASCADE,
			role        VARCHAR(20) NOT NULL CHECK (role IN ('viewer', 'member', 'editor')),
			granted_by  TEXT                 REFERENCES users(id)        ON DELETE SET NULL,
			created_at  TEXT        NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			CHECK (
				(user_id IS NOT NULL AND team_id IS NULL) OR
				(user_id IS NULL     AND team_id IS NOT NULL)
			)
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_project_grants_unique_user
		    ON project_grants(project_id, user_id) WHERE user_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_project_grants_unique_team
		    ON project_grants(project_id, team_id) WHERE team_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_project_grants_project ON project_grants(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_project_grants_user ON project_grants(user_id) WHERE user_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_project_grants_team ON project_grants(team_id) WHERE team_id IS NOT NULL`,
	},
	// Upgrade v8 → v9: adds project_id FK to agent_sessions.
	// Nullable (SET NULL on project delete) so legacy sessions without a project
	// continue to work unchanged. Index is partial — only rows with a project set.
	8: {
		`ALTER TABLE agent_sessions ADD COLUMN project_id TEXT NULL REFERENCES projects(id) ON DELETE SET NULL`,
		`CREATE INDEX IF NOT EXISTS idx_agent_sessions_project ON agent_sessions(project_id) WHERE project_id IS NOT NULL`,
	},
	// Upgrade v9 → v10: adds default_project_id FK to channel_contacts.
	// Group contacts can have a default project; FK SET NULL on project delete
	// so existing contacts remain valid without a project binding.
	9: {
		`ALTER TABLE channel_contacts ADD COLUMN default_project_id TEXT NULL REFERENCES projects(id) ON DELETE SET NULL`,
		`CREATE INDEX IF NOT EXISTS idx_channel_contacts_default_project ON channel_contacts(default_project_id) WHERE default_project_id IS NOT NULL`,
	},
	// Upgrade v10 → v11: widen idx_projects_status to cover both active and archived rows.
	// The original partial index (WHERE status = 'active') did not benefit queries
	// filtering on archived status. Drop and recreate as a full index on (status).
	10: {
		`DROP INDEX IF EXISTS idx_projects_status`,
		`CREATE INDEX IF NOT EXISTS idx_projects_status ON projects(status)`,
	},
}

// EnsureSchema creates tables if they don't exist and applies incremental migrations.
//
// Flow:
//  1. Fresh DB (no schema_version row) → apply full schema.sql + set version = SchemaVersion
//  2. Existing DB with version < SchemaVersion → apply patches sequentially
//  3. Existing DB with version == SchemaVersion → no-op
func EnsureSchema(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER NOT NULL PRIMARY KEY
	)`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	var current int
	err := db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		// Fresh database — apply full schema.
		slog.Info("sqlite: applying initial schema", "version", SchemaVersion)
		tx, txErr := db.Begin()
		if txErr != nil {
			return fmt.Errorf("begin schema tx: %w", txErr)
		}
		if _, err := tx.Exec(schemaSQL); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply schema: %w", err)
		}
		if _, err := tx.Exec("INSERT INTO schema_version (version) VALUES (?)", SchemaVersion); err != nil {
			tx.Rollback()
			return fmt.Errorf("set schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit schema tx: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	// Apply incremental migrations for existing DBs.
	if current < SchemaVersion {
		slog.Info("sqlite: migrating schema", "from", current, "to", SchemaVersion)
		for v := current; v < SchemaVersion; v++ {
			stmts, ok := migrations[v]
			if !ok {
				return fmt.Errorf("sqlite: missing migration for version %d → %d", v, v+1)
			}
			tx, txErr := db.Begin()
			if txErr != nil {
				return fmt.Errorf("begin migration tx v%d: %w", v, txErr)
			}
			for _, stmt := range stmts {
				if _, err := tx.Exec(stmt); err != nil {
					tx.Rollback()
					return fmt.Errorf("apply migration v%d stmt %q: %w", v, stmt[:min(40, len(stmt))], err)
				}
			}
			if _, err := tx.Exec(
				"UPDATE schema_version SET version = ? WHERE version = ?", v+1, v,
			); err != nil {
				tx.Rollback()
				return fmt.Errorf("update schema version v%d: %w", v, err)
			}
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("commit migration v%d: %w", v, err)
			}
			slog.Info("sqlite: applied migration", "version", v+1)
		}
	}

	return nil
}
