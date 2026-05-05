//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

// TestMemoryRecallScopeFilters covers the 6 canonical 5D scope query patterns.
// Each subtest inserts rows under a specific scope then queries with a filter
// and asserts which row IDs are returned.
// These tests fail (RED) until Phase 02 adds contact_id/project_id columns.
func TestMemoryRecallScopeFilters(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	fx := makeScopeFixtures(t, db)

	// Seed one row per scope variant so we can assert selectivity.
	// path is unique per row to avoid UNIQUE index conflicts.
	rowAgentOnly := insertScopedMemDoc(t, db,
		fx.AgentID, nil, nil, nil, nil,
		"scope/agent-only.md", "h-agent")
	rowAgentUser := insertScopedMemDoc(t, db,
		fx.AgentID, nil, &fx.UserID, nil, nil,
		"scope/agent-user.md", "h-user")
	rowAgentTeam := insertScopedMemDoc(t, db,
		fx.AgentID, &fx.TeamID, nil, nil, nil,
		"scope/agent-team.md", "h-team")
	rowAgentContact := insertScopedMemDoc(t, db,
		fx.AgentID, nil, nil, &fx.ContactID, nil,
		"scope/agent-contact.md", "h-contact")
	rowAgentProject := insertScopedMemDoc(t, db,
		fx.AgentID, nil, nil, nil, &fx.ProjectID,
		"scope/agent-project.md", "h-project")
	rowFull5D := insertScopedMemDoc(t, db,
		fx.AgentID, &fx.TeamID, &fx.UserID, &fx.ContactID, &fx.ProjectID,
		"scope/full-5d.md", "h-full")

	_ = rowAgentOnly
	_ = rowFull5D

	// queryIDs runs a raw SQL scope-filter query and returns the set of returned IDs.
	queryIDs := func(t *testing.T, q string, args ...any) map[uuid.UUID]bool {
		t.Helper()
		rows, err := db.QueryContext(ctx, q, args...)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		defer rows.Close()
		ids := map[uuid.UUID]bool{}
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			ids[id] = true
		}
		return ids
	}

	t.Run("agent_only_sees_all_rows_for_agent", func(t *testing.T) {
		// Pattern 1: agent-only query (no additional scope filter) — should see all rows for this agent.
		ids := queryIDs(t,
			`SELECT id FROM memory_documents WHERE agent_id = $1`,
			fx.AgentID)
		// We inserted 6 rows; all should appear.
		wantCount := 6
		if len(ids) != wantCount {
			t.Errorf("agent-only query: got %d rows, want %d", len(ids), wantCount)
		}
	})

	t.Run("user_scoped_sees_user_and_global_rows", func(t *testing.T) {
		// Pattern 2: agent+user — sees user_id=U OR user_id IS NULL.
		ids := queryIDs(t, `
			SELECT id FROM memory_documents
			WHERE agent_id = $1 AND (user_id = $2 OR user_id IS NULL)`,
			fx.AgentID, fx.UserID)
		// Should include: agent-only (user NULL), agent-user, agent-team (user NULL),
		// agent-contact (user NULL), agent-project (user NULL), full-5D (user=U).
		if !ids[rowAgentUser] {
			t.Error("agent+user filter: missing rowAgentUser")
		}
		// agent-only row has user_id=NULL → should appear
		if !ids[rowAgentOnly] {
			t.Error("agent+user filter: missing rowAgentOnly (user IS NULL should match)")
		}
	})

	t.Run("team_scoped_exact_match", func(t *testing.T) {
		// Pattern 3: agent+team exact match.
		ids := queryIDs(t, `
			SELECT id FROM memory_documents
			WHERE agent_id = $1 AND team_id = $2`,
			fx.AgentID, fx.TeamID)
		if !ids[rowAgentTeam] {
			t.Error("team filter: missing rowAgentTeam")
		}
		// rowFull5D has team_id too — should also appear
		if !ids[rowFull5D] {
			t.Error("team filter: missing rowFull5D (also has team_id)")
		}
		// rows without team_id must not appear
		if ids[rowAgentOnly] {
			t.Error("team filter: rowAgentOnly (no team_id) must not appear")
		}
		if ids[rowAgentUser] {
			t.Error("team filter: rowAgentUser (no team_id) must not appear")
		}
	})

	t.Run("contact_scoped_exact_match", func(t *testing.T) {
		// Pattern 5: agent+contact.
		ids := queryIDs(t, `
			SELECT id FROM memory_documents
			WHERE agent_id = $1 AND contact_id = $2`,
			fx.AgentID, fx.ContactID)
		if !ids[rowAgentContact] {
			t.Error("contact filter: missing rowAgentContact")
		}
		if ids[rowAgentOnly] {
			t.Error("contact filter: rowAgentOnly must not appear")
		}
	})

	t.Run("project_scoped_sees_project_and_null_rows", func(t *testing.T) {
		// Pattern 4: agent+project — project memories + unscoped agent memories.
		ids := queryIDs(t, `
			SELECT id FROM memory_documents
			WHERE agent_id = $1 AND (project_id = $2 OR project_id IS NULL)`,
			fx.AgentID, fx.ProjectID)
		if !ids[rowAgentProject] {
			t.Error("project filter: missing rowAgentProject")
		}
		// rows with NULL project_id should also appear
		if !ids[rowAgentOnly] {
			t.Error("project filter: rowAgentOnly (project NULL) should match")
		}
	})

	t.Run("full_5d_exact_match_most_specific", func(t *testing.T) {
		// Pattern 6: all 5 axes set — most specific row only.
		ids := queryIDs(t, `
			SELECT id FROM memory_documents
			WHERE agent_id = $1
			  AND team_id = $2
			  AND user_id = $3
			  AND contact_id = $4
			  AND project_id = $5`,
			fx.AgentID, fx.TeamID, fx.UserID, fx.ContactID, fx.ProjectID)
		if !ids[rowFull5D] {
			t.Error("full-5D filter: missing rowFull5D")
		}
		if len(ids) != 1 {
			t.Errorf("full-5D filter: expected exactly 1 row, got %d", len(ids))
		}
	})

	_ = fmt.Sprintf // ensure fmt import used
}

// TestMemoryChunksScopeIndexesPresent checks that partial indexes exist on memory_chunks
// for the 5D scope columns.
func TestMemoryChunksScopeIndexesPresent(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	wantIndexes := []string{"idx_memchunk_contact", "idx_memchunk_project"}
	for _, idx := range wantIndexes {
		var found bool
		if err := db.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM pg_indexes
				WHERE tablename = 'memory_chunks' AND indexname = $1
			)`, idx).Scan(&found); err != nil {
			t.Errorf("check index %s: %v", idx, err)
			continue
		}
		if !found {
			t.Errorf("index %s missing on memory_chunks (expected after Phase 02)", idx)
		}
	}
}
