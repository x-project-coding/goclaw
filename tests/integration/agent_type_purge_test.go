//go:build integration

package integration

import (
	"testing"
)

// TestPGAgentTypeColumnDropped verifies that the agents.agent_type column has
// been removed from the PostgreSQL schema. Predefined-only agents in v4.
//
// RED until Phase 02 lands the schema edit.
func TestPGAgentTypeColumnDropped(t *testing.T) {
	db := testDB(t)

	var count int
	err := db.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM information_schema.columns
		 WHERE table_schema = 'public'
		   AND table_name   = 'agents'
		   AND column_name  = 'agent_type'`,
	).Scan(&count)
	if err != nil {
		t.Fatalf("query information_schema: %v", err)
	}
	if count != 0 {
		t.Fatalf("agents.agent_type column must be absent (got %d)", count)
	}
}
