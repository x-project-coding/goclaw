//go:build e2e

package e2e_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// TestNoTenantIDColumnsAnywherePG queries information_schema.columns and
// asserts that no live PG table has a `tenant_id` column. v4 schema is
// strictly single-user; any tenant_id column would be a regression from
// pre-v4 multi-tenant residue.
func TestNoTenantIDColumnsAnywherePG(t *testing.T) {
	helpers.MustLoadEnv()
	helpers.MustMigrateClean(t)

	db := helpers.MustDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := db.QueryContext(ctx, `
		SELECT table_name, column_name
		  FROM information_schema.columns
		 WHERE table_schema = 'public'
		   AND column_name = 'tenant_id'
	`)
	if err != nil {
		t.Fatalf("query information_schema.columns: %v", err)
	}
	defer rows.Close()

	var found []string
	for rows.Next() {
		var table, col string
		if err := rows.Scan(&table, &col); err != nil {
			t.Fatalf("scan: %v", err)
		}
		found = append(found, table+"."+col)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("v4 schema must have no tenant_id columns, found: %s",
			strings.Join(found, ", "))
	}
}

// TestNoTenantIDInSQLiteSchemaFile scans the SQLite full-schema file and
// rejects any non-comment line that still defines a `tenant_id` column.
// Comments mentioning "no tenant_id columns" are allowed (and expected).
func TestNoTenantIDInSQLiteSchemaFile(t *testing.T) {
	repoRoot := mustRepoRoot(t)
	path := filepath.Join(repoRoot, "internal", "store", "sqlitestore", "schema.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var bad []string
	for i, line := range strings.Split(string(raw), "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "--") {
			continue
		}
		// reject lines that declare a tenant_id column (CREATE/ALTER definitions).
		// We match only the column-definition shape, not free-text mentions
		// (which would already be caught by the comment guard above).
		lower := strings.ToLower(trim)
		if strings.Contains(lower, "tenant_id ") ||
			strings.HasSuffix(lower, "tenant_id,") ||
			strings.HasSuffix(lower, "tenant_id") {
			bad = append(bad, line+" (line "+strconv.Itoa(i+1)+")")
		}
	}
	if len(bad) != 0 {
		t.Fatalf("SQLite schema must not declare tenant_id columns:\n  %s",
			strings.Join(bad, "\n  "))
	}
}
