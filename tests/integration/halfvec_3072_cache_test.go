//go:build integration

package integration

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// TestHalfvec3072EmbeddingCacheRoundTrip upserts a 3072-dim halfvec into the
// embedding_cache table and reads it back, confirming the column stores and
// returns the full 3072 dimensions without truncation.
func TestHalfvec3072EmbeddingCacheRoundTrip(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	const provider = "openai"
	const model = "text-embedding-3-large"
	hash := fmt.Sprintf("testhash-cache-%d", 42)
	vec := generate3072HalfvecLiteral(42)

	_, err := db.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO embedding_cache (hash, provider, model, embedding, dims)
		 VALUES ($1, $2, $3, '%s'::halfvec, 3072)
		 ON CONFLICT (hash, provider, model) DO UPDATE
		   SET embedding = EXCLUDED.embedding, dims = EXCLUDED.dims`, vec),
		hash, provider, model,
	)
	if err != nil {
		t.Fatalf("upsert embedding_cache: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM embedding_cache WHERE hash = $1 AND provider = $2 AND model = $3",
			hash, provider, model)
	})

	// Read back the stored embedding as text and count dimensions.
	var embStr string
	err = db.QueryRowContext(ctx,
		`SELECT embedding::text FROM embedding_cache WHERE hash = $1 AND provider = $2 AND model = $3`,
		hash, provider, model,
	).Scan(&embStr)
	if err != nil {
		t.Fatalf("select embedding_cache: %v", err)
	}

	dims := countVectorDims(embStr)
	if dims != 3072 {
		t.Errorf("embedding_cache round-trip dimension: got %d, want 3072", dims)
	}

	// Also verify the dims column matches.
	var storedDims int
	err = db.QueryRowContext(ctx,
		`SELECT dims FROM embedding_cache WHERE hash = $1 AND provider = $2 AND model = $3`,
		hash, provider, model,
	).Scan(&storedDims)
	if err != nil {
		t.Fatalf("select dims: %v", err)
	}
	if storedDims != 3072 {
		t.Errorf("dims column: got %d, want 3072", storedDims)
	}
	t.Logf("embedding_cache round-trip OK: vector dims=%d dims_col=%d", dims, storedDims)
}

// TestHalfvec3072CacheDimMismatchRejected verifies that inserting a 1536-dim
// vector into the embedding_cache halfvec(3072) column is rejected by
// PostgreSQL with a dimension error.
func TestHalfvec3072CacheDimMismatchRejected(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	vec1536 := generate1536HalfvecLiteral(77)
	_, err := db.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO embedding_cache (hash, provider, model, embedding, dims)
		 VALUES ('mismatch-cache-hash', 'test-prov', 'wrong-model', '%s'::halfvec, 1536)
		 ON CONFLICT (hash, provider, model) DO NOTHING`, vec1536),
	)
	if err == nil {
		// Accepted by DB — verify a row was NOT silently stored with wrong dims.
		var exists bool
		db.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM embedding_cache WHERE hash = 'mismatch-cache-hash' AND provider = 'test-prov')`,
		).Scan(&exists) //nolint:errcheck
		db.Exec("DELETE FROM embedding_cache WHERE hash = 'mismatch-cache-hash' AND provider = 'test-prov'")
		if exists {
			t.Error("embedding_cache accepted a 1536-dim vector into halfvec(3072) — schema constraint missing")
		}
		return
	}
	if !strings.Contains(err.Error(), "dimensions") {
		t.Errorf("expected dimension mismatch error, got: %v", err)
	}
	t.Logf("1536-dim insert correctly rejected: %v", err)
}

// TestHalfvec3072CacheConflictUpdate verifies ON CONFLICT upsert correctly
// updates an existing embedding_cache row, preserving the 3072-dim constraint.
func TestHalfvec3072CacheConflictUpdate(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	const provider = "openai"
	const model = "text-embedding-3-large"
	const hash = "conflict-test-hash"

	vec1 := generate3072HalfvecLiteral(10)
	vec2 := generate3072HalfvecLiteral(20)

	insert := func(vec string) {
		t.Helper()
		_, err := db.ExecContext(ctx,
			fmt.Sprintf(`INSERT INTO embedding_cache (hash, provider, model, embedding, dims)
			 VALUES ($1, $2, $3, '%s'::halfvec, 3072)
			 ON CONFLICT (hash, provider, model) DO UPDATE
			   SET embedding = EXCLUDED.embedding, dims = EXCLUDED.dims`, vec),
			hash, provider, model,
		)
		if err != nil {
			t.Fatalf("upsert embedding_cache: %v", err)
		}
	}

	insert(vec1)
	t.Cleanup(func() {
		db.Exec("DELETE FROM embedding_cache WHERE hash = $1 AND provider = $2 AND model = $3",
			hash, provider, model)
	})
	insert(vec2) // conflict update

	var embStr string
	err := db.QueryRowContext(ctx,
		`SELECT embedding::text FROM embedding_cache WHERE hash = $1 AND provider = $2 AND model = $3`,
		hash, provider, model,
	).Scan(&embStr)
	if err != nil {
		t.Fatalf("select after conflict update: %v", err)
	}
	dims := countVectorDims(embStr)
	if dims != 3072 {
		t.Errorf("conflict update result dimension: got %d, want 3072", dims)
	}
	t.Logf("embedding_cache conflict update OK: dims=%d", dims)
}

// parseHalfvecLiteral parses a pgvector "[f1,f2,...]" string into []float32.
// Used by other test files in this package.
func parseHalfvecLiteral(s string) []float32 {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]float32, 0, len(parts))
	for _, p := range parts {
		f, _ := strconv.ParseFloat(strings.TrimSpace(p), 32)
		out = append(out, float32(f))
	}
	return out
}
