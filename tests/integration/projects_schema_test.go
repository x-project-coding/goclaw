//go:build integration

package integration

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// ─── Helpers ──────────────────────────────────────────────────────────────

// tableExists returns true when the named table is present in public schema.
func tableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var n int
	db.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM information_schema.tables
		  WHERE table_schema = 'public' AND table_name = $1`,
		table).Scan(&n)
	return n > 0
}

// insertProject inserts a row into projects and returns any error.
func insertProject(db *sql.DB, id uuid.UUID, ownerUserID uuid.UUID, slug, status string) error {
	_, err := db.Exec(
		`INSERT INTO projects (id, owner_user_id, slug, status)
		 VALUES ($1, $2, $3, $4)`,
		id, ownerUserID, slug, status)
	return err
}

// seedUserForProjects inserts a minimal users row and registers cleanup.
func seedUserForProjects(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	suffix := id.String()[:8]
	_, err := db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, role, kind, user_key)
		 VALUES ($1, $2, 'x', 'u', 'member', 'human', $3)`,
		id, "proj-"+suffix+"@local", "proj-"+suffix,
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM users WHERE id = $1", id) })
	return id
}

// ─── Table presence ────────────────────────────────────────────────────────

// TestProjectsTableExists skips if the projects table has not been created
// yet (DDL lands in Phase 02). Once DDL lands, this test turns green.
func TestProjectsTableExists(t *testing.T) {
	db := testDB(t)
	if !tableExists(t, db, "projects") {
		t.Skip("projects table not present — waiting for schema DDL")
	}
}

// ─── Slug constraints ──────────────────────────────────────────────────────

// TestProjectSlugUniqueViolation asserts that inserting two projects with
// the same slug fails with a uniqueness error.
func TestProjectSlugUniqueViolation(t *testing.T) {
	db := testDB(t)
	if !tableExists(t, db, "projects") {
		t.Skip("projects table not present — waiting for schema DDL")
	}

	owner := seedUserForProjects(t, db)
	slug := "slug-dup-" + uuid.New().String()[:8]

	id1 := uuid.New()
	if err := insertProject(db, id1, owner, slug, "active"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM projects WHERE id = $1", id1) })

	id2 := uuid.New()
	err := insertProject(db, id2, owner, slug, "active")
	if err == nil {
		db.Exec("DELETE FROM projects WHERE id = $1", id2)
		t.Fatal("duplicate slug must be rejected")
	}
	if !strings.Contains(err.Error(), "23505") {
		t.Errorf("expected unique violation (23505), got: %v", err)
	}
}

// TestProjectSlugCheckRejectsInvalidSlugs asserts the slug CHECK constraint
// blocks each invalid value.
func TestProjectSlugCheckRejectsInvalidSlugs(t *testing.T) {
	db := testDB(t)
	if !tableExists(t, db, "projects") {
		t.Skip("projects table not present — waiting for schema DDL")
	}

	owner := seedUserForProjects(t, db)

	invalid := []string{
		"",
		"-foo",
		"foo-",
		"Foo",
		"foo_bar",
		"foo bar",
		"--",
		"..",
		"/abc",
	}

	for _, slug := range invalid {
		slug := slug
		t.Run("reject_slug_"+slug, func(t *testing.T) {
			id := uuid.New()
			err := insertProject(db, id, owner, slug, "active")
			if err == nil {
				db.Exec("DELETE FROM projects WHERE id = $1", id)
				t.Errorf("slug %q must be rejected by CHECK constraint", slug)
			}
		})
	}
}

// TestProjectSlugCheckAcceptsValidSlugs asserts the slug CHECK constraint
// allows well-formed slugs.
func TestProjectSlugCheckAcceptsValidSlugs(t *testing.T) {
	db := testDB(t)
	if !tableExists(t, db, "projects") {
		t.Skip("projects table not present — waiting for schema DDL")
	}

	owner := seedUserForProjects(t, db)

	valid := []string{
		"my-project",
		"proj-1",
		"a1b",
	}

	for _, slug := range valid {
		slug := slug
		t.Run("accept_slug_"+slug, func(t *testing.T) {
			id := uuid.New()
			err := insertProject(db, id, owner, slug, "active")
			if err != nil {
				t.Errorf("slug %q must be accepted: %v", slug, err)
			}
			t.Cleanup(func() { db.Exec("DELETE FROM projects WHERE id = $1", id) })
		})
	}
}

// ─── Status constraint ─────────────────────────────────────────────────────

// TestProjectStatusCheckConstraint asserts only "active" and "archived" pass.
func TestProjectStatusCheckConstraint(t *testing.T) {
	db := testDB(t)
	if !tableExists(t, db, "projects") {
		t.Skip("projects table not present — waiting for schema DDL")
	}

	owner := seedUserForProjects(t, db)

	for _, bad := range []string{"open", "deleted", "pending", ""} {
		bad := bad
		t.Run("reject_status_"+bad, func(t *testing.T) {
			id := uuid.New()
			err := insertProject(db, id, owner, "status-check-"+uuid.New().String()[:6], bad)
			if err == nil {
				db.Exec("DELETE FROM projects WHERE id = $1", id)
				t.Errorf("status %q must be rejected by CHECK constraint", bad)
			}
		})
	}

	for _, good := range []string{"active", "archived"} {
		good := good
		t.Run("accept_status_"+good, func(t *testing.T) {
			id := uuid.New()
			slug := "s-" + good[:2] + "-" + uuid.New().String()[:6]
			if err := insertProject(db, id, owner, slug, good); err != nil {
				t.Errorf("status %q must be accepted: %v", good, err)
			}
			t.Cleanup(func() { db.Exec("DELETE FROM projects WHERE id = $1", id) })
		})
	}
}
