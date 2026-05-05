//go:build integration

package integration

import (
	"testing"
)

// TestPGMetadataColumnsPresent verifies that all 13 entity tables have a
// metadata column in the PostgreSQL schema. This test must fail (RED) before
// schema edits and pass (GREEN) after.
func TestPGMetadataColumnsPresent(t *testing.T) {
	db := testDB(t)

	tables := []string{
		"agents",
		"agent_teams",
		"agent_shares",
		"agent_links",
		"memory_documents",
		"skills",
		"skill_versions",
		"channel_instances",
		"mcp_servers",
		"cron_jobs",
		"llm_providers",
		"system_configs",
		"user_sessions",
	}

	for _, tbl := range tables {
		tbl := tbl
		t.Run(tbl, func(t *testing.T) {
			var count int
			err := db.QueryRowContext(t.Context(),
				`SELECT COUNT(*) FROM information_schema.columns
				 WHERE table_schema = 'public'
				   AND table_name   = $1
				   AND column_name  = 'metadata'`,
				tbl,
			).Scan(&count)
			if err != nil {
				t.Fatalf("query information_schema: %v", err)
			}
			if count != 1 {
				t.Errorf("table %q: want metadata column, got count=%d", tbl, count)
			}
		})
	}
}
