package base

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// testDialectPG is a minimal PG dialect for testing.
type testDialectPG struct{}

func (testDialectPG) Placeholder(n int) string   { return "$" + itoa(n) }
func (testDialectPG) TransformValue(v any) any    { return v }
func (testDialectPG) SupportsReturning() bool     { return true }

// testDialectSQLite is a minimal SQLite dialect for testing.
type testDialectSQLite struct{}

func (testDialectSQLite) Placeholder(_ int) string { return "?" }
func (testDialectSQLite) TransformValue(v any) any { return v }
func (testDialectSQLite) SupportsReturning() bool  { return false }

func itoa(n int) string { return fmt.Sprintf("%d", n) }

func TestBuildMapUpdate_PG_Placeholder(t *testing.T) {
	id := uuid.New()
	updates := map[string]any{"name": "test"}
	q, args, err := BuildMapUpdate(testDialectPG{}, "skills", id, updates)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q, "$1") || !strings.Contains(q, "$") {
		t.Errorf("PG query missing $N placeholder: %s", q)
	}
	if !strings.HasPrefix(q, "UPDATE skills SET") {
		t.Errorf("unexpected query: %s", q)
	}
	// args: name value + updated_at (skills has it) + id
	if len(args) < 3 {
		t.Errorf("expected >=3 args, got %d", len(args))
	}
}

func TestBuildMapUpdate_SQLite_Placeholder(t *testing.T) {
	id := uuid.New()
	updates := map[string]any{"name": "test"}
	q, _, err := BuildMapUpdate(testDialectSQLite{}, "skills", id, updates)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(q, "$") {
		t.Errorf("SQLite query should use ?, got: %s", q)
	}
	if !strings.Contains(q, "?") {
		t.Errorf("SQLite query missing ? placeholder: %s", q)
	}
}

func TestBuildMapUpdate_EmptyUpdates(t *testing.T) {
	q, args, err := BuildMapUpdate(testDialectPG{}, "agents", uuid.New(), nil)
	if err != nil || q != "" || args != nil {
		t.Errorf("empty updates should return zero values, got q=%q args=%v err=%v", q, args, err)
	}
}

func TestBuildMapUpdate_InvalidColumn(t *testing.T) {
	_, _, err := BuildMapUpdate(testDialectPG{}, "agents", uuid.New(), map[string]any{
		"valid_col":       "ok",
		"bad; DROP TABLE": "injection",
	})
	if err == nil {
		t.Error("expected error for invalid column name")
	}
}

func TestBuildMapUpdate_AutoUpdatedAt(t *testing.T) {
	id := uuid.New()
	q, args, err := BuildMapUpdate(testDialectPG{}, "agents", id, map[string]any{"name": "a"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q, "updated_at") {
		t.Error("agents should auto-set updated_at")
	}
	// name + updated_at + id = 3 args
	if len(args) != 3 {
		t.Errorf("expected 3 args, got %d", len(args))
	}
}

func TestBuildMapUpdate_NoAutoUpdatedAt_UnknownTable(t *testing.T) {
	id := uuid.New()
	q, args, err := BuildMapUpdate(testDialectPG{}, "unknown_table", id, map[string]any{"col": "v"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(q, "updated_at") {
		t.Error("unknown table should NOT auto-set updated_at")
	}
	// col + id = 2 args
	if len(args) != 2 {
		t.Errorf("expected 2 args, got %d", len(args))
	}
}

func TestBuildMapUpdateWhereTenant_PG_DropsTenant(t *testing.T) {
	id := uuid.New()
	tid := uuid.New()
	q, args, err := BuildMapUpdateWhereTenant(testDialectPG{}, "agents", map[string]any{"name": "x"}, id, tid)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(q, "tenant_id") {
		t.Errorf("v4: tenant_id should be dropped, got: %s", q)
	}
	// name + updated_at + id = 3 args (tenantID dropped)
	if len(args) != 3 {
		t.Errorf("expected 3 args, got %d", len(args))
	}
	if args[len(args)-1] != id {
		t.Error("last arg should be id")
	}
}

func TestBuildScopeClause_PG_NoTenant(t *testing.T) {
	scope := QueryScope{TenantID: uuid.New()}
	clause, args, next := BuildScopeClause(testDialectPG{}, scope, 3)
	if clause != "" {
		t.Errorf("v4: expected empty clause, got %q", clause)
	}
	if len(args) != 0 {
		t.Errorf("v4: expected no args, got %v", args)
	}
	if next != 3 {
		t.Errorf("next should be unchanged, got %d", next)
	}
}

func TestBuildScopeClause_PG_WithProject(t *testing.T) {
	pid := uuid.New()
	scope := QueryScope{ProjectID: &pid}
	clause, args, next := BuildScopeClause(testDialectPG{}, scope, 1)
	if clause != " AND project_id = $1" {
		t.Errorf("clause = %q, want project_id only", clause)
	}
	if len(args) != 1 {
		t.Errorf("args len = %d, want 1", len(args))
	}
	if next != 2 {
		t.Errorf("next = %d, want 2", next)
	}
}

func TestBuildScopeClause_SQLite_NoTenant(t *testing.T) {
	scope := QueryScope{TenantID: uuid.New()}
	clause, args, next := BuildScopeClause(testDialectSQLite{}, scope, 1)
	if clause != "" || len(args) != 0 || next != 1 {
		t.Errorf("v4: empty expected, got clause=%q args=%v next=%d", clause, args, next)
	}
}

func TestBuildScopeClauseAlias_PG_NoTenant(t *testing.T) {
	scope := QueryScope{TenantID: uuid.New()}
	clause, args, next := BuildScopeClauseAlias(testDialectPG{}, scope, 2, "a")
	if clause != "" || len(args) != 0 || next != 2 {
		t.Errorf("v4: empty expected, got clause=%q args=%v next=%d", clause, args, next)
	}
}

func TestBuildScopeClauseAlias_InvalidAlias_WithProject(t *testing.T) {
	pid := uuid.New()
	scope := QueryScope{ProjectID: &pid}
	clause, _, _ := BuildScopeClauseAlias(testDialectPG{}, scope, 1, "a; DROP")
	if clause != "" {
		t.Error("invalid alias should return empty clause")
	}
}

func TestBuildMapUpdate_InvalidTable(t *testing.T) {
	_, _, err := BuildMapUpdate(testDialectPG{}, "bad; DROP", uuid.New(), map[string]any{"col": "v"})
	if err == nil {
		t.Error("expected error for invalid table name")
	}
}

func TestBuildMapUpdateWhereTenant_InvalidTable(t *testing.T) {
	_, _, err := BuildMapUpdateWhereTenant(testDialectPG{}, "bad; DROP", map[string]any{"col": "v"}, uuid.New(), uuid.New())
	if err == nil {
		t.Error("expected error for invalid table name")
	}
}

func TestBuildMapUpdateWhereTenant_SQLite_DropsTenant(t *testing.T) {
	id := uuid.New()
	tid := uuid.New()
	q, args, err := BuildMapUpdateWhereTenant(testDialectSQLite{}, "agents", map[string]any{"name": "y"}, id, tid)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(q, "$") {
		t.Errorf("SQLite query should use ?, got: %s", q)
	}
	if strings.Contains(q, "tenant_id") {
		t.Errorf("v4: tenant_id should be dropped, got: %s", q)
	}
	if len(args) != 3 {
		t.Errorf("expected 3 args, got %d", len(args))
	}
}

func TestBuildScopeClauseAlias_PG_WithProject(t *testing.T) {
	pid := uuid.New()
	scope := QueryScope{ProjectID: &pid}
	clause, args, next := BuildScopeClauseAlias(testDialectPG{}, scope, 5, "t")
	if clause != " AND t.project_id = $5" {
		t.Errorf("clause = %q", clause)
	}
	if len(args) != 1 {
		t.Errorf("args len = %d, want 1", len(args))
	}
	if next != 6 {
		t.Errorf("next = %d, want 6", next)
	}
}

func TestTenantIDForInsert_NonNil(t *testing.T) {
	tid := uuid.New()
	fallback := uuid.New()
	if got := TenantIDForInsert(tid, fallback); got != tid {
		t.Errorf("got %s, want %s", got, tid)
	}
}

func TestTenantIDForInsert_Nil(t *testing.T) {
	fallback := uuid.New()
	if got := TenantIDForInsert(uuid.Nil, fallback); got != fallback {
		t.Errorf("got %s, want fallback %s", got, fallback)
	}
}

func TestRequireTenantID_Valid(t *testing.T) {
	if err := RequireTenantID(uuid.New()); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRequireTenantID_Nil(t *testing.T) {
	if err := RequireTenantID(uuid.Nil); err == nil {
		t.Error("expected error for nil tenant ID")
	}
}
