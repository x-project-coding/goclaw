//go:build e2e

package stores_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// TestSkillExportNoTenant is a structural assertion: the CustomSkillExport
// JSON representation must not contain a "tenant_id" key, ensuring exported
// skill bundles are tenant-portable.
func TestSkillExportNoTenant(t *testing.T) {
	export := pg.CustomSkillExport{
		ID:          "550e8400-e29b-41d4-a716-446655440000",
		Name:        "test-skill",
		Slug:        "test-skill",
		Visibility:  "public",
		Version:     1,
		Frontmatter: json.RawMessage(`{"author":"e2e"}`),
	}

	b, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal CustomSkillExport: %v", err)
	}

	if bytes.Contains(b, []byte("tenant_id")) {
		t.Fatalf("CustomSkillExport JSON must not contain 'tenant_id', got: %s", b)
	}

	// Also verify the struct via reflection-style check: round-trip into a
	// generic map and assert the key is absent.
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	if _, ok := m["tenant_id"]; ok {
		t.Fatalf("CustomSkillExport map must not have 'tenant_id' key")
	}
}
