//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestSessionListPagedRich_ProjectIDFilter exercises the project_id filter
// path in buildSessionFilter + the project_id column added to the rich
// SELECT. Three sessions are seeded across two projects (one unbound) and
// the filter is expected to return only the matching subset, with the
// returned ProjectID echoed back so the FE can render a per-row chip.
func TestSessionListPagedRich_ProjectIDFilter(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	// Seed an owner user — projects.owner_user_id REFERENCES users(id), and
	// agent_sessions.project_id REFERENCES projects(id), so both must exist
	// before we can insert sessions bound to a project.
	ownerID := "u-owner"
	if _, err := db.Exec(`INSERT INTO users (id, email, password_hash, user_key, role, status, created_at, updated_at)
		VALUES (?, 'owner@example.test', 'x', 'owner', 'admin', 'active', datetime('now'), datetime('now'))`,
		ownerID); err != nil {
		t.Fatalf("INSERT user: %v", err)
	}

	projectA := uuid.New().String()
	projectB := uuid.New().String()
	for i, pid := range []string{projectA, projectB} {
		slug := []string{"alpha-test", "beta-test"}[i]
		if _, err := db.Exec(`INSERT INTO projects (id, slug, owner_user_id, status, created_at, updated_at)
			VALUES (?, ?, ?, 'active', datetime('now'), datetime('now'))`, pid, slug, ownerID); err != nil {
			t.Fatalf("INSERT projects[%d]: %v", i, err)
		}
	}

	// 3 sessions: 2 bound to project A, 1 to project B, 1 unbound.
	type seed struct {
		key       string
		projectID *string
	}
	a1 := projectA
	a2 := projectA
	b1 := projectB
	rows := []seed{
		{key: "agent:x:direct:user-a1", projectID: &a1},
		{key: "agent:x:direct:user-a2", projectID: &a2},
		{key: "agent:x:direct:user-b1", projectID: &b1},
		{key: "agent:x:direct:user-none", projectID: nil},
	}
	for _, r := range rows {
		id := uuid.New().String()
		_, err := db.Exec(`INSERT INTO agent_sessions
			(id, session_key, messages, created_at, updated_at, project_id)
			VALUES (?, ?, '[]', datetime('now'), datetime('now'), ?)`,
			id, r.key, r.projectID)
		if err != nil {
			t.Fatalf("INSERT seed: %v", err)
		}
	}

	sessionStore := NewSQLiteSessionStore(db)
	ctx := context.Background()

	// Filter by project A → expect 2 rows, all carrying ProjectID == projectA.
	got := sessionStore.ListPagedRich(ctx, store.SessionListOpts{
		Limit:     50,
		ProjectID: projectA,
	})
	if got.Total != 2 {
		t.Fatalf("project A Total = %d, want 2", got.Total)
	}
	for _, s := range got.Sessions {
		if s.ProjectID == nil || *s.ProjectID != projectA {
			t.Errorf("session %s: ProjectID = %v, want %s", s.Key, s.ProjectID, projectA)
		}
	}

	// Filter by project B → exactly 1.
	gotB := sessionStore.ListPagedRich(ctx, store.SessionListOpts{
		Limit:     50,
		ProjectID: projectB,
	})
	if gotB.Total != 1 {
		t.Fatalf("project B Total = %d, want 1", gotB.Total)
	}

	// No filter → all 4 rows return; the unbound row carries ProjectID == nil.
	all := sessionStore.ListPagedRich(ctx, store.SessionListOpts{Limit: 50})
	if all.Total != 4 {
		t.Fatalf("no-filter Total = %d, want 4", all.Total)
	}
	var nilSeen bool
	for _, s := range all.Sessions {
		if s.ProjectID == nil {
			nilSeen = true
		}
	}
	if !nilSeen {
		t.Errorf("expected at least one session with ProjectID=nil in unfiltered result")
	}
}
