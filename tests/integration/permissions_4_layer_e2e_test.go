//go:build integration

package integration

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	pgstore "github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// --- fixture helpers ---

func seedUserPerm(t *testing.T, db *sql.DB, role string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(`INSERT INTO users (id, email, role, user_key, password_hash)
		VALUES ($1, $2, $3, $4, 'x')`,
		id, "perm-"+id.String()[:8]+"@test.local", role, "pk-"+id.String()[:8])
	if err != nil {
		t.Fatalf("seedUserPerm: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM users WHERE id = $1", id) })
	return id
}

func seedProjectPerm(t *testing.T, db *sql.DB, ownerID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	slug := "prj-" + id.String()[:8]
	_, err := db.Exec(`INSERT INTO projects (id, slug, owner_user_id) VALUES ($1, $2, $3)`,
		id, slug, ownerID)
	if err != nil {
		t.Fatalf("seedProjectPerm: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM projects WHERE id = $1", id) })
	return id
}

func seedTeamPerm(t *testing.T, db *sql.DB, ownerID, leadAgentID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(`INSERT INTO agent_teams (id, team_key, name, owner_user_id, lead_agent_id, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		id, "team-"+id.String()[:8], "Team "+id.String()[:8], ownerID, leadAgentID, ownerID.String())
	if err != nil {
		t.Fatalf("seedTeamPerm: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM agent_teams WHERE id = $1", id) })
	return id
}

func seedAgentPerm(t *testing.T, db *sql.DB, ownerID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	key := "agt-" + id.String()[:8]
	_, err := db.Exec(`INSERT INTO agents (id, agent_key, display_name, owner_id, owner_user_id, model)
		VALUES ($1, $2, $3, $4, $5, 'gpt-4o')`,
		id, key, "Agent "+key, ownerID.String(), ownerID)
	if err != nil {
		t.Fatalf("seedAgentPerm: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM agents WHERE id = $1", id) })
	return id
}

func grantProjectUserPerm(t *testing.T, db *sql.DB, projectID, userID uuid.UUID, role string) {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(`INSERT INTO project_grants (id, project_id, user_id, role) VALUES ($1, $2, $3, $4)`,
		id, projectID, userID, role)
	if err != nil {
		t.Fatalf("grantProjectUserPerm: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM project_grants WHERE id = $1", id) })
}

func grantAgentSharePerm(t *testing.T, db *sql.DB, agentID, userID uuid.UUID, role string, createdBy uuid.UUID) {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(`INSERT INTO agent_shares (id, agent_id, shared_with_user_id, role, created_by)
		VALUES ($1, $2, $3, $4, $5)`,
		id, agentID, userID, role, createdBy)
	if err != nil {
		t.Fatalf("grantAgentSharePerm: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM agent_shares WHERE id = $1", id) })
}

func addTeamUserGrant(t *testing.T, db *sql.DB, teamID, userID uuid.UUID, role string) {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(`INSERT INTO team_user_grants (id, team_id, user_id, role, granted_by)
		VALUES ($1, $2, $3, $4, $5)`,
		id, teamID, userID, role, userID.String())
	if err != nil {
		t.Fatalf("addTeamUserGrant: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM team_user_grants WHERE id = $1", id) })
}

// setAgentTeamOwner sets the owning team on an agent.
func setAgentTeamOwner(t *testing.T, db *sql.DB, agentID, teamID uuid.UUID) {
	t.Helper()
	if _, err := db.Exec(`UPDATE agents SET team_id = $1 WHERE id = $2`, teamID, agentID); err != nil {
		t.Fatalf("setAgentTeamOwner: %v", err)
	}
	t.Cleanup(func() { db.Exec(`UPDATE agents SET team_id = NULL WHERE id = $1`, agentID) })
}

// --- inline resolver wiring ---

// buildPermResolver wires a 4-layer Resolver using inline DB query closures.
// This avoids depending on resolver constructors that may not exist yet.
func buildPermResolver(db *sql.DB) *permissions.Resolver {
	agentAccess := permissions.NewAgentAccessResolver(db)

	return permissions.NewResolver(permissions.ResolverConfig{
		// Layer 1: user's platform role from users.role.
		UserRole: func(ctx context.Context, userID uuid.UUID) string {
			var role string
			db.QueryRowContext(ctx, `SELECT role FROM users WHERE id = $1`, userID).Scan(&role)
			// Map platform role to share-vocabulary admin equivalent.
			if role == store.RoleRoot || role == "admin" {
				return permissions.ShareOwner // admin bypass
			}
			return permissions.ShareMember // default: member-level cap
		},

		// Layer 2: project grant for userID on projectID.
		ProjectGrant: func(ctx context.Context, userID, projectID uuid.UUID) (string, bool) {
			var role string
			err := db.QueryRowContext(ctx, `
				SELECT COALESCE(
					-- direct user grant
					(SELECT role FROM project_grants WHERE project_id = $1 AND user_id = $2 LIMIT 1),
					-- owner
					CASE WHEN (SELECT owner_user_id FROM projects WHERE id = $1) = $2 THEN 'editor' END,
					''
				)`, projectID, userID).Scan(&role)
			if err != nil || role == "" {
				return "", false
			}
			return role, true
		},

		// Layer 3: explicit agent share (also includes implicit team-target share from AgentAccessResolver).
		AgentShare: func(ctx context.Context, userID, agentID uuid.UUID) (string, bool) {
			role, err := agentAccess.ResolveRole(ctx, userID, agentID)
			if err != nil || role == permissions.ShareNone {
				return "", false
			}
			return role, true
		},

		// Layer 4: team membership — user is member of the team that owns the agent.
		TeamMembership: func(ctx context.Context, userID, agentID uuid.UUID) (string, bool) {
			var role string
			err := db.QueryRowContext(ctx, `
				SELECT g.role
				FROM team_user_grants g
				JOIN agents a ON a.team_id = g.team_id
				WHERE a.id = $1 AND g.user_id = $2
				LIMIT 1`, agentID, userID).Scan(&role)
			if err != nil || role == "" {
				return "", false
			}
			return role, true
		},
	})
}

// TestPermissions4LayerE2E exercises the 4-layer AND-intersect resolver with a real PG DB.
//
// Fixture:
//
//	U1 = root  → admin bypass (all actions, all layers skipped)
//	U2 = member + project_grants(P, editor) + agent_shares(A, member)
//	U3 = member + team_user_grants(T, member) — implicit grant via owning team
//	U4 = member — no grants anywhere
func TestPermissions4LayerE2E(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	u1 := seedUserPerm(t, db, store.RoleRoot) // root — admin bypass
	u2 := seedUserPerm(t, db, "member")
	u3 := seedUserPerm(t, db, "member")
	u4 := seedUserPerm(t, db, "member")

	agentA := seedAgentPerm(t, db, u1)
	teamT := seedTeamPerm(t, db, u1, agentA)
	projectP := seedProjectPerm(t, db, u1)
	setAgentTeamOwner(t, db, agentA, teamT)

	grantProjectUserPerm(t, db, projectP, u2, permissions.ShareEditor)
	grantAgentSharePerm(t, db, agentA, u2, permissions.ShareMember, u1)
	addTeamUserGrant(t, db, teamT, u3, permissions.ShareMember)

	r := buildPermResolver(db)
	pid := projectP

	cases := []struct {
		name   string
		userID uuid.UUID
		projID *uuid.UUID
		action permissions.Action
		want   bool
	}{
		// U1 root — admin bypass before any lower layer.
		{"u1 read", u1, nil, permissions.ActionRead, true},
		{"u1 write", u1, nil, permissions.ActionWriteFile, true},
		{"u1 admin", u1, nil, permissions.ActionAdmin, true},

		// U2: project=editor, agent=member — all write-level checks pass when project bound.
		{"u2 read no project", u2, nil, permissions.ActionRead, true},
		{"u2 write no project", u2, nil, permissions.ActionWriteFile, true},
		{"u2 write in project", u2, &pid, permissions.ActionWriteFile, true},

		// U3: team member only — read passes (member≥viewer), write passes (team member=member level).
		{"u3 read no project", u3, nil, permissions.ActionRead, true},
		{"u3 write no project", u3, nil, permissions.ActionWriteFile, true},
		// U3 has no project grant → project layer denies write_file when project is scoped.
		{"u3 write with project", u3, &pid, permissions.ActionWriteFile, false},

		// U4: no grants → all deny.
		{"u4 read", u4, nil, permissions.ActionRead, false},
		{"u4 write", u4, nil, permissions.ActionWriteFile, false},
		{"u4 read with project", u4, &pid, permissions.ActionRead, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := r.CheckAccess(ctx, tc.userID, agentA, tc.projID, tc.action)
			if got != tc.want {
				t.Errorf("CheckAccess(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestSubagentProjectInheritance verifies that project_id is persisted when set
// and NULL when absent — using the real PG subagent_tasks store.
func TestSubagentProjectInheritance(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	u1 := seedUserPerm(t, db, store.RoleRoot)
	projectP := seedProjectPerm(t, db, u1)

	ts := pgstore.NewPGSubagentTaskStore(db)

	// Case 1: subagent with project_id inherited.
	withProj := &store.SubagentTaskData{
		BaseModel:      store.BaseModel{ID: store.GenNewID()},
		ParentAgentKey: "parent-" + uuid.New().String()[:8],
		Subject:        "with project",
		Description:    "desc",
		Status:         "running",
		Depth:          1,
		ProjectID:      &projectP,
	}
	if err := ts.Create(ctx, withProj); err != nil {
		t.Fatalf("create with project: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM subagent_tasks WHERE id = $1", withProj.ID) })

	got, err := ts.Get(ctx, withProj.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.ProjectID == nil {
		t.Fatal("expected project_id set, got nil")
	}
	if *got.ProjectID != projectP {
		t.Errorf("project_id = %v, want %v", *got.ProjectID, projectP)
	}

	// Case 2: subagent without project context.
	noProj := &store.SubagentTaskData{
		BaseModel:      store.BaseModel{ID: store.GenNewID()},
		ParentAgentKey: "parent-" + uuid.New().String()[:8],
		Subject:        "no project",
		Description:    "desc",
		Status:         "running",
		Depth:          1,
	}
	if err := ts.Create(ctx, noProj); err != nil {
		t.Fatalf("create no project: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM subagent_tasks WHERE id = $1", noProj.ID) })

	got2, err := ts.Get(ctx, noProj.ID)
	if err != nil {
		t.Fatalf("get task no project: %v", err)
	}
	if got2.ProjectID != nil {
		t.Errorf("expected project_id=nil, got %v", *got2.ProjectID)
	}
}

// TestTracesContactID verifies contact_id column is nullable and round-trips correctly.
func TestTracesContactID(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	u1 := seedUserPerm(t, db, store.RoleRoot)
	agentA := seedAgentPerm(t, db, u1)

	ts := pgstore.NewPGTracingStore(db)
	now := time.Now().UTC()

	trace := &store.TraceData{
		AgentID:   &agentA,
		UserID:    u1.String(),
		Name:      "contact-id-trace",
		Status:    "running",
		StartTime: now,
		CreatedAt: now,
	}
	if err := ts.CreateTrace(ctx, trace); err != nil {
		t.Fatalf("create trace: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM traces WHERE id = $1", trace.ID) })

	got, err := ts.GetTrace(ctx, trace.ID)
	if err != nil {
		t.Fatalf("get trace: %v", err)
	}
	if got.ContactID != nil {
		t.Errorf("non-channel trace: expected contact_id=nil, got %v", *got.ContactID)
	}
}

// TestConfigTypeConstraint verifies that agent_config_permissions CHECK constraint
// rejects invalid config_type values and accepts all valid ones.
func TestConfigTypeConstraint(t *testing.T) {
	db := testDB(t)

	u1 := seedUserPerm(t, db, store.RoleRoot)
	agentA := seedAgentPerm(t, db, u1)
	userIDStr := u1.String() // user_id is VARCHAR(255), not UUID FK

	// 'file_writer' must be rejected by the CHECK constraint.
	_, err := db.Exec(`INSERT INTO agent_config_permissions
		(id, agent_id, config_type, scope, permission, user_id)
		VALUES ($1, $2, 'file_writer', 'group:tg:-100', 'allow', $3)`,
		uuid.New(), agentA, userIDStr)
	if err == nil {
		t.Fatal("expected CHECK violation for config_type='file_writer', got nil error")
	}

	// All valid config_type values per CHECK constraint: write_file, edit_file, delete_file, cron, heartbeat, *.
	for _, ct := range []string{"write_file", "edit_file", "delete_file", "cron", "heartbeat", "*"} {
		rowID := uuid.New()
		_, err := db.Exec(`INSERT INTO agent_config_permissions
			(id, agent_id, config_type, scope, permission, user_id)
			VALUES ($1, $2, $3, 'group:tg:-100', 'allow', $4)`,
			rowID, agentA, ct, userIDStr)
		if err != nil {
			t.Errorf("config_type=%q should be accepted, got: %v", ct, err)
		} else {
			db.Exec("DELETE FROM agent_config_permissions WHERE id = $1", rowID)
		}
	}
}
