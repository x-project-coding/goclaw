//go:build integration

package integration

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// seedMCPUserForScope inserts a minimal users row for MCP scope tests.
func seedMCPUserForScope(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	suffix := id.String()[:8]
	_, err := db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, role, kind, user_key)
		 VALUES ($1, $2, 'x', 'u', 'member', 'human', $3)`,
		id, "mcpscope-"+suffix+"@local", "mcpscope-"+suffix,
	)
	if err != nil {
		t.Fatalf("seedMCPUserForScope: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM users WHERE id = $1", id) })
	return id
}

// seedMCPAgentForScope inserts a minimal agents row for MCP scope tests.
func seedMCPAgentForScope(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(
		`INSERT INTO agents (id, agent_key, status, provider, model, owner_id)
		 VALUES ($1, $2, 'active', 'test', 'test-model', 'test-owner')`,
		id, "mcpscope-agent-"+id.String()[:8],
	)
	if err != nil {
		t.Fatalf("seedMCPAgentForScope: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM agents WHERE id = $1", id) })
	return id
}

// seedMCPServerForScope wraps the shared seedMCPServer using uuid.Nil for the
// unused v4 tenant arg.
func seedMCPServerForScope(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	return seedMCPServer(t, db, uuid.Nil)
}

// insertAccessRequest inserts a raw row into mcp_access_requests and returns error.
// Passing nil for agentID or userID omits those fields (NULL in DB).
func insertAccessRequest(db *sql.DB, serverID uuid.UUID, agentID *uuid.UUID, userID *uuid.UUID, scope, status string) error {
	var aID, uID interface{}
	if agentID != nil {
		aID = *agentID
	}
	if userID != nil {
		uID = *userID
	}
	_, err := db.Exec(
		`INSERT INTO mcp_access_requests (id, server_id, agent_id, user_id, scope, status, requested_by)
		 VALUES ($1, $2, $3, $4, $5, $6, 'test-requester')`,
		uuid.New(), serverID, aID, uID, scope, status,
	)
	return err
}

// --- Scope/shape tests ---

// TestMCPAccessRequest_ScopeEnum_RejectsUnknown: scope='both' must fail with 23514.
func TestMCPAccessRequest_ScopeEnum_RejectsUnknown(t *testing.T) {
	db := testDB(t)
	serverID := seedMCPServerForScope(t, db)
	agentID := seedMCPAgentForScope(t, db)

	err := insertAccessRequest(db, serverID, &agentID, nil, "both", "pending")
	if err == nil {
		t.Fatal("want error for unknown scope='both', got nil")
	}
	if !strings.Contains(err.Error(), "23514") {
		t.Errorf("want pg 23514 (CHECK violation), got: %v", err)
	}
}

// TestMCPAccessRequest_AgentShape_RequiresAgentID: scope='agent' with agent_id=NULL must fail.
func TestMCPAccessRequest_AgentShape_RequiresAgentID(t *testing.T) {
	db := testDB(t)
	serverID := seedMCPServerForScope(t, db)

	err := insertAccessRequest(db, serverID, nil, nil, "agent", "pending")
	if err == nil {
		t.Fatal("want error for scope='agent' with agent_id=NULL, got nil")
	}
	if !strings.Contains(err.Error(), "23514") {
		t.Errorf("want pg 23514 (shape CHECK), got: %v", err)
	}
}

// TestMCPAccessRequest_AgentShape_RejectsUserID: scope='agent' with both IDs set must fail.
func TestMCPAccessRequest_AgentShape_RejectsUserID(t *testing.T) {
	db := testDB(t)
	serverID := seedMCPServerForScope(t, db)
	agentID := seedMCPAgentForScope(t, db)
	userID := seedMCPUserForScope(t, db)

	err := insertAccessRequest(db, serverID, &agentID, &userID, "agent", "pending")
	if err == nil {
		t.Fatal("want error for scope='agent' with user_id set, got nil")
	}
	if !strings.Contains(err.Error(), "23514") {
		t.Errorf("want pg 23514 (shape CHECK), got: %v", err)
	}
}

// TestMCPAccessRequest_UserShape_RequiresUserID: scope='user' with user_id=NULL must fail.
func TestMCPAccessRequest_UserShape_RequiresUserID(t *testing.T) {
	db := testDB(t)
	serverID := seedMCPServerForScope(t, db)

	err := insertAccessRequest(db, serverID, nil, nil, "user", "pending")
	if err == nil {
		t.Fatal("want error for scope='user' with user_id=NULL, got nil")
	}
	if !strings.Contains(err.Error(), "23514") {
		t.Errorf("want pg 23514 (shape CHECK), got: %v", err)
	}
}

// TestMCPAccessRequest_UserShape_RejectsAgentID: scope='user' with both IDs set must fail.
func TestMCPAccessRequest_UserShape_RejectsAgentID(t *testing.T) {
	db := testDB(t)
	serverID := seedMCPServerForScope(t, db)
	agentID := seedMCPAgentForScope(t, db)
	userID := seedMCPUserForScope(t, db)

	err := insertAccessRequest(db, serverID, &agentID, &userID, "user", "pending")
	if err == nil {
		t.Fatal("want error for scope='user' with agent_id set, got nil")
	}
	if !strings.Contains(err.Error(), "23514") {
		t.Errorf("want pg 23514 (shape CHECK), got: %v", err)
	}
}

// TestMCPAccessRequest_HappyPath_Agent: scope='agent', agent_id set, user_id NULL → success.
func TestMCPAccessRequest_HappyPath_Agent(t *testing.T) {
	db := testDB(t)
	serverID := seedMCPServerForScope(t, db)
	agentID := seedMCPAgentForScope(t, db)

	id := uuid.New()
	_, err := db.Exec(
		`INSERT INTO mcp_access_requests (id, server_id, agent_id, user_id, scope, status, requested_by)
		 VALUES ($1, $2, $3, NULL, 'agent', 'pending', 'test-requester')`,
		id, serverID, agentID,
	)
	if err != nil {
		t.Fatalf("happy path agent scope: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM mcp_access_requests WHERE id = $1", id) })
}

// TestMCPAccessRequest_HappyPath_User: scope='user', user_id set, agent_id NULL → success.
func TestMCPAccessRequest_HappyPath_User(t *testing.T) {
	db := testDB(t)
	serverID := seedMCPServerForScope(t, db)
	userID := seedMCPUserForScope(t, db)

	id := uuid.New()
	_, err := db.Exec(
		`INSERT INTO mcp_access_requests (id, server_id, agent_id, user_id, scope, status, requested_by)
		 VALUES ($1, $2, NULL, $3, 'user', 'pending', 'test-requester')`,
		id, serverID, userID,
	)
	if err != nil {
		t.Fatalf("happy path user scope: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM mcp_access_requests WHERE id = $1", id) })
}

// --- Lifecycle status tests (L146) ---

// TestMCPAccessRequest_StatusEnum_RejectsUnknown: status='approved' (old vocab) must fail.
func TestMCPAccessRequest_StatusEnum_RejectsUnknown(t *testing.T) {
	db := testDB(t)
	serverID := seedMCPServerForScope(t, db)
	agentID := seedMCPAgentForScope(t, db)

	err := insertAccessRequest(db, serverID, &agentID, nil, "agent", "approved")
	if err == nil {
		t.Fatal("want error for status='approved' (invalid enum), got nil")
	}
	if !strings.Contains(err.Error(), "23514") {
		t.Errorf("want pg 23514 (status CHECK), got: %v", err)
	}
}

// TestMCPAccessRequest_StatusEnum_DefaultsToPending: omitting status defaults to 'pending'.
func TestMCPAccessRequest_StatusEnum_DefaultsToPending(t *testing.T) {
	db := testDB(t)
	serverID := seedMCPServerForScope(t, db)
	agentID := seedMCPAgentForScope(t, db)

	id := uuid.New()
	_, err := db.Exec(
		`INSERT INTO mcp_access_requests (id, server_id, agent_id, scope, requested_by)
		 VALUES ($1, $2, $3, 'agent', 'test-requester')`,
		id, serverID, agentID,
	)
	if err != nil {
		t.Fatalf("insert without status: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM mcp_access_requests WHERE id = $1", id) })

	var status string
	if err := db.QueryRow(`SELECT status FROM mcp_access_requests WHERE id = $1`, id).Scan(&status); err != nil {
		t.Fatalf("select status: %v", err)
	}
	if status != "pending" {
		t.Errorf("default status = %q, want %q", status, "pending")
	}
}

// TestMCPAccessRequest_PartialUnique_DuplicatePendingRejected: second pending for same tuple must fail 23505.
func TestMCPAccessRequest_PartialUnique_DuplicatePendingRejected(t *testing.T) {
	db := testDB(t)
	serverID := seedMCPServerForScope(t, db)
	agentID := seedMCPAgentForScope(t, db)

	id1 := uuid.New()
	_, err := db.Exec(
		`INSERT INTO mcp_access_requests (id, server_id, agent_id, user_id, scope, status, requested_by)
		 VALUES ($1, $2, $3, NULL, 'agent', 'pending', 'test-requester')`,
		id1, serverID, agentID,
	)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM mcp_access_requests WHERE id = $1", id1) })

	err = insertAccessRequest(db, serverID, &agentID, nil, "agent", "pending")
	if err == nil {
		t.Fatal("want 23505 for duplicate pending, got nil")
	}
	if !strings.Contains(err.Error(), "23505") {
		t.Errorf("want pg 23505 (partial UNIQUE), got: %v", err)
	}
}

// TestMCPAccessRequest_PartialUnique_AllowsReRequestAfterDeny: re-request after deny allowed.
func TestMCPAccessRequest_PartialUnique_AllowsReRequestAfterDeny(t *testing.T) {
	db := testDB(t)
	serverID := seedMCPServerForScope(t, db)
	agentID := seedMCPAgentForScope(t, db)

	// Insert first pending
	id1 := uuid.New()
	_, err := db.Exec(
		`INSERT INTO mcp_access_requests (id, server_id, agent_id, user_id, scope, status, requested_by)
		 VALUES ($1, $2, $3, NULL, 'agent', 'pending', 'test-requester')`,
		id1, serverID, agentID,
	)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM mcp_access_requests WHERE id = $1", id1) })

	// Deny it
	_, err = db.Exec(`UPDATE mcp_access_requests SET status='denied' WHERE id = $1`, id1)
	if err != nil {
		t.Fatalf("deny: %v", err)
	}

	// Re-request: should succeed (partial UNIQUE excludes denied rows)
	id2 := uuid.New()
	_, err = db.Exec(
		`INSERT INTO mcp_access_requests (id, server_id, agent_id, user_id, scope, status, requested_by)
		 VALUES ($1, $2, $3, NULL, 'agent', 'pending', 'test-requester')`,
		id2, serverID, agentID,
	)
	if err != nil {
		t.Fatalf("re-request after deny: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM mcp_access_requests WHERE id = $1", id2) })
}

// TestMCPAccessRequest_PartialUnique_AllowsReRequestAfterRevoke: re-request after revoke allowed.
func TestMCPAccessRequest_PartialUnique_AllowsReRequestAfterRevoke(t *testing.T) {
	db := testDB(t)
	serverID := seedMCPServerForScope(t, db)
	agentID := seedMCPAgentForScope(t, db)

	id1 := uuid.New()
	_, err := db.Exec(
		`INSERT INTO mcp_access_requests (id, server_id, agent_id, user_id, scope, status, requested_by)
		 VALUES ($1, $2, $3, NULL, 'agent', 'pending', 'test-requester')`,
		id1, serverID, agentID,
	)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM mcp_access_requests WHERE id = $1", id1) })

	// Flip to revoked
	_, err = db.Exec(`UPDATE mcp_access_requests SET status='revoked' WHERE id = $1`, id1)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}

	// Re-request
	id2 := uuid.New()
	_, err = db.Exec(
		`INSERT INTO mcp_access_requests (id, server_id, agent_id, user_id, scope, status, requested_by)
		 VALUES ($1, $2, $3, NULL, 'agent', 'pending', 'test-requester')`,
		id2, serverID, agentID,
	)
	if err != nil {
		t.Fatalf("re-request after revoke: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM mcp_access_requests WHERE id = $1", id2) })
}

// TestMCPAccessRequest_PartialUnique_NullsNotDistinct_AgentScope: two pending rows with same
// server+agent and user_id=NULL both must fail on second insert (23505).
func TestMCPAccessRequest_PartialUnique_NullsNotDistinct_AgentScope(t *testing.T) {
	db := testDB(t)
	serverID := seedMCPServerForScope(t, db)
	agentID := seedMCPAgentForScope(t, db)

	id1 := uuid.New()
	_, err := db.Exec(
		`INSERT INTO mcp_access_requests (id, server_id, agent_id, user_id, scope, status, requested_by)
		 VALUES ($1, $2, $3, NULL, 'agent', 'pending', 'test-requester')`,
		id1, serverID, agentID,
	)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM mcp_access_requests WHERE id = $1", id1) })

	// Second insert same tuple with user_id=NULL both
	err = insertAccessRequest(db, serverID, &agentID, nil, "agent", "pending")
	if err == nil {
		t.Fatal("want 23505 (NULLS NOT DISTINCT), got nil")
	}
	if !strings.Contains(err.Error(), "23505") {
		t.Errorf("want pg 23505, got: %v", err)
	}
}

// TestMCPAccessRequest_RevokeAtomicity: TX deletes mcp_agent_grants + flips access request to 'revoked'.
// Verifies both rows in expected state post-commit.
func TestMCPAccessRequest_RevokeAtomicity(t *testing.T) {
	db := testDB(t)
	serverID := seedMCPServerForScope(t, db)
	agentID := seedMCPAgentForScope(t, db)

	// Insert a granted access request + matching agent grant
	reqID := uuid.New()
	_, err := db.Exec(
		`INSERT INTO mcp_access_requests (id, server_id, agent_id, user_id, scope, status, requested_by)
		 VALUES ($1, $2, $3, NULL, 'agent', 'granted', 'test-requester')`,
		reqID, serverID, agentID,
	)
	if err != nil {
		t.Fatalf("insert access request: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM mcp_access_requests WHERE id = $1", reqID) })

	grantID := uuid.New()
	_, err = db.Exec(
		`INSERT INTO mcp_agent_grants (id, server_id, agent_id, enabled, granted_by, created_at)
		 VALUES ($1, $2, $3, true, 'test-admin', NOW())`,
		grantID, serverID, agentID,
	)
	if err != nil {
		t.Fatalf("insert agent grant: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM mcp_agent_grants WHERE id = $1", grantID) })

	// Atomic revoke: delete grant + flip request status
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(`DELETE FROM mcp_agent_grants WHERE server_id = $1 AND agent_id = $2`, serverID, agentID)
	if err != nil {
		t.Fatalf("delete grant: %v", err)
	}
	_, err = tx.Exec(`UPDATE mcp_access_requests SET status='revoked' WHERE id = $1 AND status='granted'`, reqID)
	if err != nil {
		t.Fatalf("update status: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Assert grant deleted
	var grantCount int
	db.QueryRow(`SELECT COUNT(*) FROM mcp_agent_grants WHERE server_id = $1 AND agent_id = $2`, serverID, agentID).Scan(&grantCount)
	if grantCount != 0 {
		t.Errorf("grant count = %d after revoke, want 0", grantCount)
	}

	// Assert request status = 'revoked'
	var status string
	db.QueryRow(`SELECT status FROM mcp_access_requests WHERE id = $1`, reqID).Scan(&status)
	if status != "revoked" {
		t.Errorf("request status = %q after revoke, want 'revoked'", status)
	}
}
