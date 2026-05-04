//go:build integration

package integration

import (
	"database/sql"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// newKGStore returns a PG knowledge-graph store wired to a mock embedding
// provider so tests don't require live LLM credentials.
func newKGStore(t *testing.T) *pg.PGKnowledgeGraphStore {
	t.Helper()
	db := testDB(t)
	pg.InitSqlx(db)
	s := pg.NewPGKnowledgeGraphStore(db)
	s.SetEmbeddingProvider(newMockEmbedProvider())
	return s
}

// newVaultStore returns a PG vault store wired to a mock embedding provider
// so tests don't require live LLM credentials.
func newVaultStore(db *sql.DB) *pg.PGVaultStore {
	pg.InitSqlx(db)
	vs := pg.NewPGVaultStore(db)
	vs.SetEmbeddingProvider(newMockEmbedProvider())
	return vs
}

// makeVaultDoc builds a personal-scope vault document for an agent.
func makeVaultDoc(_, agentID, path, title string) *store.VaultDocument {
	return &store.VaultDocument{
		AgentID:     &agentID,
		Scope:       "personal",
		Path:        path,
		Title:       title,
		DocType:     "note",
		ContentHash: "abc123",
	}
}

// makeSharedVaultDoc builds a shared (agent_id=NULL, scope='shared') vault
// document. Used to characterize visibility tests for shared docs.
func makeSharedVaultDoc(_, path, title string) *store.VaultDocument {
	return &store.VaultDocument{
		AgentID:     nil,
		TeamID:      nil,
		Scope:       "shared",
		Path:        path,
		Title:       title,
		DocType:     "note",
		ContentHash: "abc123",
	}
}

// makeTeamVaultDoc builds a team-scoped vault document (agent_id=NULL,
// team_id set, scope='team').
func makeTeamVaultDoc(_, teamID, path, title string) *store.VaultDocument {
	return &store.VaultDocument{
		AgentID:     nil,
		TeamID:      &teamID,
		Scope:       "team",
		Path:        path,
		Title:       title,
		DocType:     "note",
		ContentHash: "abc123",
	}
}
