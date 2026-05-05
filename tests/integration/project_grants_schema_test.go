//go:build integration

package integration

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// ─── Helpers ──────────────────────────────────────────────────────────────

// insertGrant inserts a row into project_grants. userID and teamID are
// mutually exclusive nullable columns — pass nil for the absent one.
func insertGrant(db *sql.DB, projectID uuid.UUID, userID, teamID *uuid.UUID, role string) error {
	_, err := db.Exec(
		`INSERT INTO project_grants (project_id, user_id, team_id, role)
		 VALUES ($1, $2, $3, $4)`,
		projectID, userID, teamID, role)
	return err
}

// seedProjectForGrants inserts a minimal active project and registers
// cleanup. Requires projects table to exist (caller must skip otherwise).
func seedProjectForGrants(t *testing.T, db *sql.DB, ownerID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	slug := "gp-" + id.String()[:8]
	_, err := db.Exec(
		`INSERT INTO projects (id, owner_user_id, slug, status)
		 VALUES ($1, $2, $3, 'active')`,
		id, ownerID, slug)
	if err != nil {
		t.Fatalf("seed project: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM projects WHERE id = $1", id) })
	return id
}

// seedTeamForGrants inserts a minimal agent_teams row.
func seedTeamForGrants(t *testing.T, db *sql.DB, ownerUserID uuid.UUID) uuid.UUID {
	t.Helper()
	// Reuse the agent+team seed from sharing helpers.
	return seedTeamForShares(t, db, ownerUserID)
}

func skipIfGrantsTableMissing(t *testing.T, db *sql.DB) {
	t.Helper()
	if !tableExists(t, db, "project_grants") {
		t.Skip("project_grants table not present — waiting for schema DDL")
	}
}

// ─── XOR constraint ────────────────────────────────────────────────────────

// TestProjectGrantsTargetXOR asserts that (user_id NULL AND team_id NULL)
// and (user_id NOT NULL AND team_id NOT NULL) are both rejected.
func TestProjectGrantsTargetXOR(t *testing.T) {
	db := testDB(t)
	skipIfGrantsTableMissing(t, db)

	owner := seedUserForProjects(t, db)
	project := seedProjectForGrants(t, db, owner)
	team := seedTeamForGrants(t, db, owner)

	// Both NULL → reject.
	if err := insertGrant(db, project, nil, nil, "viewer"); err == nil {
		t.Error("both target columns NULL must be rejected by CHECK constraint")
	}

	// Both set → reject.
	if err := insertGrant(db, project, &owner, &team, "viewer"); err == nil {
		t.Error("both target columns set must be rejected by CHECK constraint")
	}

	// Only user_id → accept.
	if err := insertGrant(db, project, &owner, nil, "viewer"); err != nil {
		t.Errorf("user-only grant must be accepted: %v", err)
	}

	// Only team_id → accept.
	if err := insertGrant(db, project, nil, &team, "member"); err != nil {
		t.Errorf("team-only grant must be accepted: %v", err)
	}
}

// ─── Unique constraint ─────────────────────────────────────────────────────

// TestProjectGrantsUniqueNullsNotDistinct asserts that a second grant for
// the same (project, user) or (project, team) is rejected even when the
// other nullable column is NULL (UNIQUE NULLS NOT DISTINCT semantics).
func TestProjectGrantsUniqueNullsNotDistinct(t *testing.T) {
	db := testDB(t)
	skipIfGrantsTableMissing(t, db)

	owner := seedUserForProjects(t, db)
	project := seedProjectForGrants(t, db, owner)
	team := seedTeamForGrants(t, db, owner)

	// First user grant → ok.
	if err := insertGrant(db, project, &owner, nil, "viewer"); err != nil {
		t.Fatalf("first user grant: %v", err)
	}
	// Duplicate user grant → reject.
	if err := insertGrant(db, project, &owner, nil, "editor"); err == nil {
		t.Error("duplicate (project, user) grant must be rejected")
	}

	// First team grant → ok.
	if err := insertGrant(db, project, nil, &team, "member"); err != nil {
		t.Fatalf("first team grant: %v", err)
	}
	// Duplicate team grant → reject.
	if err := insertGrant(db, project, nil, &team, "viewer"); err == nil {
		t.Error("duplicate (project, team) grant must be rejected")
	}

	// Different user on same project → allowed.
	otherUser := seedUserForProjects(t, db)
	if err := insertGrant(db, project, &otherUser, nil, "viewer"); err != nil {
		t.Errorf("grant to different user must be accepted: %v", err)
	}
}

// ─── Role constraint ───────────────────────────────────────────────────────

// TestProjectGrantsRoleCheck asserts the role CHECK allows only
// "viewer", "member", "editor".
func TestProjectGrantsRoleCheck(t *testing.T) {
	db := testDB(t)
	skipIfGrantsTableMissing(t, db)

	owner := seedUserForProjects(t, db)
	project := seedProjectForGrants(t, db, owner)

	for _, bad := range []string{"owner", "admin", "user", "viewerz", ""} {
		bad := bad
		t.Run("reject_role_"+bad, func(t *testing.T) {
			target := seedUserForProjects(t, db)
			if err := insertGrant(db, project, &target, nil, bad); err == nil {
				t.Errorf("role %q must be rejected", bad)
			}
		})
	}

	for _, good := range []string{"viewer", "member", "editor"} {
		good := good
		t.Run("accept_role_"+good, func(t *testing.T) {
			target := seedUserForProjects(t, db)
			if err := insertGrant(db, project, &target, nil, good); err != nil {
				t.Errorf("role %q must be accepted: %v", good, err)
			}
		})
	}
}

// ─── CASCADE on project delete ─────────────────────────────────────────────

// TestProjectGrantsCascadeOnProjectDelete asserts that deleting a project
// also removes all its grants via ON DELETE CASCADE.
func TestProjectGrantsCascadeOnProjectDelete(t *testing.T) {
	testDB_ := testDB(t)
	skipIfGrantsTableMissing(t, testDB_)

	owner := seedUserForProjects(t, testDB_)
	// Create project without the cleanup registered by seedProjectForGrants
	// so we can delete it manually to trigger CASCADE.
	projID := uuid.New()
	slug := "casc-" + projID.String()[:8]
	if _, err := testDB_.Exec(
		`INSERT INTO projects (id, owner_user_id, slug, status) VALUES ($1, $2, $3, 'active')`,
		projID, owner, slug); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	if err := insertGrant(testDB_, projID, &owner, nil, "viewer"); err != nil {
		t.Fatalf("seed grant: %v", err)
	}

	// Delete the project; CASCADE must drop the grant.
	if _, err := testDB_.Exec("DELETE FROM projects WHERE id = $1", projID); err != nil {
		t.Fatalf("delete project: %v", err)
	}

	var n int
	testDB_.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM project_grants WHERE project_id = $1`, projID).Scan(&n)
	if n != 0 {
		t.Errorf("project delete must cascade grants; got %d remaining", n)
	}
}

// TestProjectGrantsConstraintsHaveDescriptiveErrors asserts the XOR
// violation message references a recognisable constraint name.
func TestProjectGrantsConstraintsHaveDescriptiveErrors(t *testing.T) {
	db := testDB(t)
	skipIfGrantsTableMissing(t, db)

	owner := seedUserForProjects(t, db)
	project := seedProjectForGrants(t, db, owner)

	err := insertGrant(db, project, nil, nil, "viewer")
	if err == nil {
		t.Fatal("expected target-XOR rejection")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "target") && !strings.Contains(msg, "check") {
		t.Errorf("error should mention target/check constraint, got: %q", err)
	}
}

// TestProjectSessionProjectIDSetNullPlaceholder is a placeholder for the
// agent_sessions.project_id SET NULL behaviour (implemented in Phase 04).
// It skips unconditionally until Phase 04 wires the FK.
func TestProjectSessionProjectIDSetNullPlaceholder(t *testing.T) {
	t.Skip("SET NULL on agent_sessions.project_id — deferred to Phase 04")
}
