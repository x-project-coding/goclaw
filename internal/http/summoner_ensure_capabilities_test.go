package http

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// backfillStubStore is a minimal AgentStore stub for ensureBackfillFiles tests.
// Only GetAgentContextFiles and SetAgentContextFile are exercised.
type backfillStubStore struct {
	store.AgentStore // embed to satisfy interface; unused methods panic
	files            []store.AgentContextFileData
	setCalls         atomic.Int32
	setFiles         []string
}

func (s *backfillStubStore) GetAgentContextFiles(_ context.Context, _ uuid.UUID) ([]store.AgentContextFileData, error) {
	return s.files, nil
}
func (s *backfillStubStore) SetAgentContextFile(_ context.Context, _ uuid.UUID, fileName, _ string) error {
	s.setCalls.Add(1)
	s.setFiles = append(s.setFiles, fileName)
	return nil
}

func TestEnsureBackfillFiles_SeedsCapabilitiesWhenMissing(t *testing.T) {
	stub := &backfillStubStore{
		files: []store.AgentContextFileData{
			{FileName: "SOUL.md", Content: "style info"},
		},
	}
	s := &AgentSummoner{agents: stub}

	s.ensureBackfillFiles(context.Background(), uuid.New())

	// USER_PREDEFINED.md is no longer backfilled — only CAPABILITIES.md.
	if n := stub.setCalls.Load(); n != 1 {
		t.Fatalf("expected 1 SetAgentContextFile call, got %d", n)
	}
	if stub.setFiles[0] != bootstrap.CapabilitiesFile {
		t.Fatalf("expected %q seeded, got %q", bootstrap.CapabilitiesFile, stub.setFiles[0])
	}
}

func TestEnsureBackfillFiles_SkipsWhenAllExist(t *testing.T) {
	stub := &backfillStubStore{
		files: []store.AgentContextFileData{
			{FileName: "SOUL.md", Content: "style info"},
			{FileName: bootstrap.CapabilitiesFile, Content: "existing capabilities"},
		},
	}
	s := &AgentSummoner{agents: stub}

	s.ensureBackfillFiles(context.Background(), uuid.New())

	if n := stub.setCalls.Load(); n != 0 {
		t.Fatalf("expected 0 SetAgentContextFile calls (all exist), got %d", n)
	}
}

func TestEnsureBackfillFiles_SeedsOnlyMissing(t *testing.T) {
	stub := &backfillStubStore{
		files: []store.AgentContextFileData{
			{FileName: "SOUL.md", Content: "style info"},
		},
	}
	s := &AgentSummoner{agents: stub}

	s.ensureBackfillFiles(context.Background(), uuid.New())

	if n := stub.setCalls.Load(); n != 1 {
		t.Fatalf("expected 1 SetAgentContextFile call, got %d", n)
	}
	if stub.setFiles[0] != bootstrap.CapabilitiesFile {
		t.Fatalf("expected %q seeded, got %q", bootstrap.CapabilitiesFile, stub.setFiles[0])
	}
}
