//go:build e2e

// Shared insert/assert helpers for contact-merge tests.
// Kept here (not in helpers/) so the SQL stays close to the tests asserting
// against it, and so we don't ship test fixtures into the helper package.

package stores_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
)

// mustInsertContact creates an unmerged contact row owned by ownerID.
func mustInsertContact(t *testing.T, db *sql.DB, ownerID uuid.UUID, channelType, channelInstance, senderID string) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = db.ExecContext(ctx, `
		INSERT INTO channel_contacts (id, channel_type, channel_instance, sender_id, user_id, contact_type)
		VALUES ($1, $2, $3, $4, $5, 'user')`,
		id, channelType, channelInstance, senderID, ownerID,
	)
	if err != nil {
		t.Fatalf("insert contact: %v", err)
	}
	return id
}

// mustInsertContactWithMerge creates a contact whose merged_id is already set
// — used to seed chained-merge tripwires.
func mustInsertContactWithMerge(t *testing.T, db *sql.DB, ownerID, mergedInto uuid.UUID, channelType, channelInstance, senderID string) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = db.ExecContext(ctx, `
		INSERT INTO channel_contacts (id, channel_type, channel_instance, sender_id, user_id, merged_id, contact_type)
		VALUES ($1, $2, $3, $4, $5, $6, 'user')`,
		id, channelType, channelInstance, senderID, ownerID, mergedInto,
	)
	if err != nil {
		t.Fatalf("insert merged contact: %v", err)
	}
	return id
}

// mustInsertAgentSession is the R1 fixture: a session row whose user_id MUST
// flip atomically with the merge.
func mustInsertAgentSession(t *testing.T, db *sql.DB, agentID, userID uuid.UUID, sessionKey string) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = db.ExecContext(ctx, `
		INSERT INTO agent_sessions (id, session_key, agent_id, user_id)
		VALUES ($1, $2, $3, $4)`,
		id, sessionKey, agentID, userID,
	)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}
	return id
}

// mustInsertContextFile creates a per-user context file row.
func mustInsertContextFile(t *testing.T, db *sql.DB, agentID, userID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = db.ExecContext(ctx, `
		INSERT INTO user_context_files (id, agent_id, user_id, file_name, content)
		VALUES ($1, $2, $3, $4, '')`,
		id, agentID, userID, name,
	)
	if err != nil {
		t.Fatalf("insert ctx file: %v", err)
	}
	return id
}

// mustInsertMemoryDoc creates a memory_documents row.
func mustInsertMemoryDoc(t *testing.T, db *sql.DB, agentID, userID uuid.UUID, path string) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = db.ExecContext(ctx, `
		INSERT INTO memory_documents (id, agent_id, user_id, path, content, hash)
		VALUES ($1, $2, $3, $4, '', $5)`,
		id, agentID, userID, path, "hash-"+id.String(),
	)
	if err != nil {
		t.Fatalf("insert memdoc: %v", err)
	}
	return id
}

// assertColumnEquals reads `column` from `table` for row `id` and compares to
// `want`. Empty `want` matches NULL. Pass `column::text` to coerce UUIDs.
func assertColumnEquals(t *testing.T, db *sql.DB, table, column string, id uuid.UUID, want string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var got sql.NullString
	if err := db.QueryRowContext(ctx,
		"SELECT "+column+" FROM "+table+" WHERE id = $1", id,
	).Scan(&got); err != nil {
		t.Fatalf("read %s.%s for %s: %v", table, column, id, err)
	}
	if want == "" {
		if got.Valid && got.String != "" {
			t.Fatalf("%s.%s want NULL/empty, got %q", table, column, got.String)
		}
		return
	}
	if !got.Valid || got.String != want {
		t.Fatalf("%s.%s want %q, got %q (valid=%v)", table, column, want, got.String, got.Valid)
	}
}
