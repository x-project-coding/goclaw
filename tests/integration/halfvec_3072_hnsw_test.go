//go:build integration

package integration

import (
	"testing"
)

// TestHalfvec3072HNSWIndexesExist queries pg_indexes to confirm every table with
// a halfvec(3072) embedding column has an HNSW index using halfvec_cosine_ops.
// A missing or wrong opclass silently falls back to sequential scan, degrading
// search performance in production.
func TestHalfvec3072HNSWIndexesExist(t *testing.T) {
	db := testDB(t)

	// Tables expected to carry halfvec HNSW indexes based on the v4 schema.
	want := []struct {
		table string
		index string
	}{
		{"memory_chunks", "idx_mem_embedding"},
		{"kg_entities", "idx_kg_embedding"},
		{"vault_documents", "idx_vault_docs_embedding"},
		{"skills", "idx_skills_embedding"},
	}

	for _, tc := range want {
		t.Run(tc.table, func(t *testing.T) {
			var found bool
			err := db.QueryRow(`
				SELECT EXISTS (
					SELECT 1 FROM pg_indexes
					WHERE tablename  = $1
					  AND indexname  = $2
					  AND indexdef   LIKE '%halfvec_cosine_ops%'
				)`, tc.table, tc.index,
			).Scan(&found)
			if err != nil {
				t.Fatalf("query pg_indexes for %s/%s: %v", tc.table, tc.index, err)
			}
			if !found {
				t.Errorf("HNSW index %s on %s with halfvec_cosine_ops not found — check migrations",
					tc.index, tc.table)
			} else {
				t.Logf("confirmed: %s.%s uses halfvec_cosine_ops", tc.table, tc.index)
			}
		})
	}
}

// TestHalfvec3072NoVectorCosineOpsIndexes confirms no index anywhere uses the
// legacy vector_cosine_ops opclass. Any such index indicates an incomplete
// migration from vector(1536) to halfvec(3072).
func TestHalfvec3072NoVectorCosineOpsIndexes(t *testing.T) {
	db := testDB(t)

	rows, err := db.Query(`
		SELECT tablename, indexname, indexdef
		FROM pg_indexes
		WHERE schemaname = 'public'
		  AND indexdef LIKE '%vector_cosine_ops%'
	`)
	if err != nil {
		t.Fatalf("query pg_indexes: %v", err)
	}
	defer rows.Close()

	var hits []string
	for rows.Next() {
		var table, index, def string
		if err := rows.Scan(&table, &index, &def); err != nil {
			t.Fatalf("scan: %v", err)
		}
		hits = append(hits, table+"."+index)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}

	if len(hits) > 0 {
		t.Errorf("found %d index(es) still using legacy vector_cosine_ops — migrate to halfvec_cosine_ops: %v",
			len(hits), hits)
	}
}
