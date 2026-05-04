//go:build e2e

package e2e_test

import (
	"strings"
	"testing"
)

// TestNoTenantIDInSQLStatements asserts no production Go file contains a raw
// `tenant_id` SQL token in INSERT / UPDATE / SELECT / WHERE clauses. v4 has
// no `tenant_id` column anywhere in the schema; any leftover SQL touching
// such a column would crash at runtime with `column "tenant_id" does not
// exist` (PG SQLSTATE 42703).
//
// Allowlisted: the STT proxy column `stt_tenant_id` is a 3rd-party API
// parameter, NOT goclaw's tenant concept — it stays.
func TestNoTenantIDInSQLStatements(t *testing.T) {
	repoRoot := mustRepoRoot(t)

	// Grep production Go (excluding tests, docs, plans, .claude) for the
	// literal "tenant_id" token. We grep the whole token and then post-filter
	// out the STT 3rd-party allowlist.
	out, _ := runGrep(t, repoRoot,
		"-rnEw", "tenant_id", "--include=*.go",
		"--exclude=*_test.go",
		"--exclude-dir=node_modules",
		"--exclude-dir=plans",
		"--exclude-dir=docs",
		"--exclude-dir=.claude",
		"--exclude-dir=.git")

	var hits []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		// Allowlist: STT proxy 3rd-party param, NOT goclaw tenant concept.
		if strings.Contains(line, "stt_tenant_id") {
			continue
		}
		hits = append(hits, line)
	}

	if len(hits) > 0 {
		t.Fatalf("found %d production references to tenant_id (v4 schema has no such column — would crash at runtime):\n  %s",
			len(hits), strings.Join(hits, "\n  "))
	}
}

// TestNoTenantIDDBTagInStructs asserts no production struct uses the
// `db:"tenant_id"` field tag. v4 stores must not declare a tenant_id column
// mapping; any sqlx StructScan against such a tag would either fail or
// silently leave the field zero, masking real schema bugs.
func TestNoTenantIDDBTagInStructs(t *testing.T) {
	repoRoot := mustRepoRoot(t)

	out, _ := runGrep(t, repoRoot,
		"-rnE", `db:"tenant_id`, "--include=*.go",
		"--exclude=*_test.go",
		"--exclude-dir=node_modules",
		"--exclude-dir=plans",
		"--exclude-dir=docs",
		"--exclude-dir=.claude",
		"--exclude-dir=.git")

	if strings.TrimSpace(out) != "" {
		t.Fatalf("found production struct fields with db:\"tenant_id\" tag (v4 schema has no such column):\n%s", out)
	}
}
