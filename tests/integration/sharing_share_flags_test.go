//go:build integration

package integration

import (
	"testing"

	"github.com/google/uuid"
)

// TestPGAgentShareFlagColumns asserts the v4 sharing-model split has
// produced two distinct BOOLEAN columns on `agents`: share_workspace and
// share_memory. Both must be NOT NULL with a default of false.
//
// RED until Phase 02 schema lands.
func TestPGAgentShareFlagColumns(t *testing.T) {
	db := testDB(t)

	type colInfo struct {
		dataType   string
		isNullable string
		colDefault *string
	}

	want := []string{"share_workspace", "share_memory"}
	for _, col := range want {
		col := col
		t.Run(col, func(t *testing.T) {
			var info colInfo
			err := db.QueryRowContext(t.Context(),
				`SELECT data_type, is_nullable, column_default
				   FROM information_schema.columns
				  WHERE table_schema = 'public'
				    AND table_name   = 'agents'
				    AND column_name  = $1`,
				col,
			).Scan(&info.dataType, &info.isNullable, &info.colDefault)
			if err != nil {
				t.Fatalf("agents.%s missing: %v", col, err)
			}
			if info.dataType != "boolean" {
				t.Errorf("agents.%s: want data_type=boolean, got %q", col, info.dataType)
			}
			if info.isNullable != "NO" {
				t.Errorf("agents.%s: want NOT NULL, got is_nullable=%q", col, info.isNullable)
			}
			if info.colDefault == nil {
				t.Errorf("agents.%s: want default false, got NULL", col)
			} else if *info.colDefault != "false" {
				t.Errorf("agents.%s: want default 'false', got %q", col, *info.colDefault)
			}
		})
	}
}

// TestPGWorkspaceSharingBlobRemoved asserts the legacy JSONB blob
// `agents.workspace_sharing` no longer exists post-migration.
//
// RED until Phase 02 schema lands.
func TestPGWorkspaceSharingBlobRemoved(t *testing.T) {
	db := testDB(t)

	var count int
	err := db.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM information_schema.columns
		  WHERE table_schema = 'public'
		    AND table_name   = 'agents'
		    AND column_name  = 'workspace_sharing'`,
	).Scan(&count)
	if err != nil {
		t.Fatalf("query information_schema: %v", err)
	}
	if count != 0 {
		t.Errorf("agents.workspace_sharing must be removed, got count=%d", count)
	}
}

// TestPGGranularToggleNoSideEffect verifies that toggling one share flag
// does NOT mutate the other. Insert agent with both false, flip share_memory,
// re-read, assert share_workspace untouched.
//
// RED until Phase 02 schema lands.
func TestPGGranularToggleNoSideEffect(t *testing.T) {
	db := testDB(t)

	agentID := uuid.New()
	agentKey := "share-flags-" + agentID.String()[:8]

	t.Cleanup(func() {
		db.Exec("DELETE FROM agents WHERE id = $1", agentID)
	})

	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO agents (id, agent_key, status, provider, model, owner_id, share_workspace, share_memory)
		 VALUES ($1, $2, 'active', 'test', 'test-model', 'test-owner', FALSE, FALSE)`,
		agentID, agentKey,
	); err != nil {
		t.Fatalf("insert agent: %v", err)
	}

	if _, err := db.ExecContext(t.Context(),
		`UPDATE agents SET share_memory = TRUE WHERE id = $1`, agentID,
	); err != nil {
		t.Fatalf("update share_memory: %v", err)
	}

	var shareWorkspace, shareMemory bool
	if err := db.QueryRowContext(t.Context(),
		`SELECT share_workspace, share_memory FROM agents WHERE id = $1`, agentID,
	).Scan(&shareWorkspace, &shareMemory); err != nil {
		t.Fatalf("select: %v", err)
	}
	if shareWorkspace {
		t.Errorf("share_workspace must remain false after toggling share_memory; got true")
	}
	if !shareMemory {
		t.Errorf("share_memory must be true after explicit set; got false")
	}
}
