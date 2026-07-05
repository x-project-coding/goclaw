package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// mockMemoryStore is a minimal in-memory implementation of store.MemoryStore
// for unit testing the MemoryInterceptor.
type mockMemoryStore struct {
	docs map[string]string // key: "agentID|userID|path"
}

func newMockMemoryStore() *mockMemoryStore {
	return &mockMemoryStore{docs: make(map[string]string)}
}

func docKey(agentID, userID, path string) string {
	return agentID + "|" + userID + "|" + path
}

func (m *mockMemoryStore) GetDocument(_ context.Context, agentID, userID, path string) (string, error) {
	if v, ok := m.docs[docKey(agentID, userID, path)]; ok {
		return v, nil
	}
	return "", fmt.Errorf("not found")
}

func (m *mockMemoryStore) PutDocument(_ context.Context, agentID, userID, path, content string) error {
	m.docs[docKey(agentID, userID, path)] = content
	return nil
}

func (m *mockMemoryStore) DeleteDocument(_ context.Context, agentID, userID, path string) error {
	delete(m.docs, docKey(agentID, userID, path))
	return nil
}

func (m *mockMemoryStore) ListDocuments(_ context.Context, agentID, userID string) ([]store.DocumentInfo, error) {
	var out []store.DocumentInfo
	prefix := agentID + "|" + userID + "|"
	for k := range m.docs {
		if after, ok := strings.CutPrefix(k, prefix); ok {
			path := after
			out = append(out, store.DocumentInfo{Path: path})
		}
	}
	return out, nil
}

// Unused interface methods — satisfy store.MemoryStore.
func (m *mockMemoryStore) ListAllDocumentsGlobal(_ context.Context) ([]store.DocumentInfo, error) {
	return nil, nil
}
func (m *mockMemoryStore) ListAllDocuments(_ context.Context, _ string) ([]store.DocumentInfo, error) {
	return nil, nil
}
func (m *mockMemoryStore) GetDocumentDetail(_ context.Context, _, _, _ string) (*store.DocumentDetail, error) {
	return nil, nil
}
func (m *mockMemoryStore) ListChunks(_ context.Context, _, _, _ string) ([]store.ChunkInfo, error) {
	return nil, nil
}
func (m *mockMemoryStore) Search(_ context.Context, _ string, _, _ string, _ store.MemorySearchOptions) ([]store.MemorySearchResult, error) {
	return nil, nil
}
func (m *mockMemoryStore) IndexDocument(_ context.Context, _, _, _ string) error { return nil }
func (m *mockMemoryStore) IndexAll(_ context.Context, _, _ string) error         { return nil }
func (m *mockMemoryStore) SetEmbeddingProvider(_ store.EmbeddingProvider)        {}
func (m *mockMemoryStore) Close() error                                          { return nil }

// --- Test helpers ---

func memCtx(agentID uuid.UUID, userID, leaderID string) context.Context {
	ctx := context.Background()
	ctx = store.WithAgentID(ctx, agentID)
	ctx = store.WithUserID(ctx, userID)
	if leaderID != "" {
		ctx = WithLeaderAgentID(ctx, leaderID)
	}
	return ctx
}

// memCtxTyped builds on memCtx, additionally stamping the agent type — used
// by the shared/private path-routing tests below.
func memCtxTyped(agentID uuid.UUID, userID, leaderID, agentType string) context.Context {
	ctx := memCtx(agentID, userID, leaderID)
	if agentType != "" {
		ctx = store.WithAgentType(ctx, agentType)
	}
	return ctx
}

// --- ReadFile tests ---

func TestReadFile_NoLeader_OwnMemory(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")
	agentID := uuid.New()

	ms.docs[docKey(agentID.String(), "user1", "MEMORY.md")] = "my notes"

	ctx := memCtx(agentID, "user1", "")
	content, handled, err := mi.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatal("expected handled=true")
	}
	if content != "my notes" {
		t.Errorf("expected 'my notes', got %q", content)
	}
}

func TestReadFile_LeaderFallback(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")
	memberID := uuid.New()
	leaderID := uuid.New()

	// Leader has memory, member does not.
	ms.docs[docKey(leaderID.String(), "user1", "MEMORY.md")] = "leader notes"

	ctx := memCtx(memberID, "user1", leaderID.String())
	content, handled, err := mi.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatal("expected handled=true")
	}
	if content != "leader notes" {
		t.Errorf("expected 'leader notes', got %q", content)
	}
}

func TestReadFile_LeaderFallback_SharedScope(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")
	memberID := uuid.New()
	leaderID := uuid.New()

	// Leader has shared (global) memory only.
	ms.docs[docKey(leaderID.String(), "", "MEMORY.md")] = "leader shared"

	ctx := memCtx(memberID, "user1", leaderID.String())
	content, handled, err := mi.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatal("expected handled=true")
	}
	if content != "leader shared" {
		t.Errorf("expected 'leader shared', got %q", content)
	}
}

func TestReadFile_LeaderIsSelf(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")
	agentID := uuid.New()

	ms.docs[docKey(agentID.String(), "user1", "MEMORY.md")] = "own notes"

	// Leader is the same agent — should read own memory, no fallback.
	ctx := memCtx(agentID, "user1", agentID.String())
	content, handled, err := mi.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatal("expected handled=true")
	}
	if content != "own notes" {
		t.Errorf("expected 'own notes', got %q", content)
	}
}

func TestReadFile_NonMemoryPath(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")

	ctx := memCtx(uuid.New(), "user1", "")
	_, handled, err := mi.ReadFile(ctx, "README.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handled {
		t.Error("expected handled=false for non-memory path")
	}
}

func TestReadFile_MemberNoMemory_NoLeader_Empty(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")

	ctx := memCtx(uuid.New(), "user1", "")
	content, handled, err := mi.ReadFile(ctx, "MEMORY.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatal("expected handled=true for memory path")
	}
	if content != "" {
		t.Errorf("expected empty content, got %q", content)
	}
}

// --- WriteFile tests ---

func TestWriteFile_NoLeader_AllowWrite(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")
	agentID := uuid.New()

	ctx := memCtx(agentID, "user1", "")
	result, err := mi.WriteFile(ctx, "MEMORY.md", "new content", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Handled {
		t.Fatal("expected handled=true")
	}

	// Verify content was written.
	got, _ := ms.GetDocument(ctx, agentID.String(), "user1", "MEMORY.md")
	if got != "new content" {
		t.Errorf("expected 'new content', got %q", got)
	}
}

func TestWriteFile_LeaderPresent_BlockWrite(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")
	memberID := uuid.New()
	leaderID := uuid.New()

	ctx := memCtx(memberID, "user1", leaderID.String())
	result, err := mi.WriteFile(ctx, "MEMORY.md", "attempt", false)
	if err == nil {
		t.Fatal("expected error for blocked write")
	}
	if !result.Handled {
		t.Fatal("expected handled=true")
	}
	if !strings.Contains(err.Error(), "read-only") {
		t.Errorf("expected read-only error, got: %v", err)
	}
}

func TestWriteFile_LeaderIsSelf_AllowWrite(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")
	agentID := uuid.New()

	// Leader is the same agent — should allow write.
	ctx := memCtx(agentID, "user1", agentID.String())
	result, err := mi.WriteFile(ctx, "MEMORY.md", "leader writes", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Handled {
		t.Fatal("expected handled=true")
	}

	got, _ := ms.GetDocument(ctx, agentID.String(), "user1", "MEMORY.md")
	if got != "leader writes" {
		t.Errorf("expected 'leader writes', got %q", got)
	}
}

// --- MemoryGetTool leader fallback tests ---

func TestMemoryGet_LeaderFallback(t *testing.T) {
	ms := newMockMemoryStore()
	tool := NewMemoryGetTool()
	tool.SetMemoryStore(ms)

	memberID := uuid.New()
	leaderID := uuid.New()

	ms.docs[docKey(leaderID.String(), "user1", "MEMORY.md")] = "leader get content"

	ctx := memCtx(memberID, "user1", leaderID.String())
	result := tool.Execute(ctx, map[string]any{"path": "MEMORY.md"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "leader get content") {
		t.Errorf("expected leader content in result, got: %s", result.ForLLM)
	}
}

func TestMemoryGet_BlockedByNoLeader(t *testing.T) {
	ms := newMockMemoryStore()
	tool := NewMemoryGetTool()
	tool.SetMemoryStore(ms)

	memberID := uuid.New()
	// No leader, no own memory → error.
	ctx := memCtx(memberID, "user1", "")
	result := tool.Execute(ctx, map[string]any{"path": "MEMORY.md"})
	if !result.IsError {
		t.Fatal("expected error for missing memory")
	}
}

// --- MemorySearchTool leader fallback tests ---

func TestMemorySearch_LeaderFallback(t *testing.T) {
	ms := newMockMemoryStore()
	tool := NewMemorySearchTool()
	tool.SetMemoryStore(ms)

	memberID := uuid.New()
	leaderID := uuid.New()

	// mockMemoryStore.Search returns nil — just verify no crash and correct agent IDs used.
	ctx := memCtx(memberID, "user1", leaderID.String())
	result := tool.Execute(ctx, map[string]any{"query": "test"})
	// With mock returning nil results for both, should get "No memory results found".
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "No memory results found") {
		t.Errorf("expected no results message, got: %s", result.ForLLM)
	}
}

// --- ListFiles tests ---

func TestListFiles_MergeLeaderDocs(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")
	memberID := uuid.New()
	leaderID := uuid.New()

	// Leader has docs, member has none.
	ms.docs[docKey(leaderID.String(), "user1", "MEMORY.md")] = "leader mem"
	ms.docs[docKey(leaderID.String(), "user1", "memory/notes.md")] = "leader notes"

	ctx := memCtx(memberID, "user1", leaderID.String())
	listing, handled, err := mi.ListFiles(ctx, "memory")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatal("expected handled=true")
	}
	if !strings.Contains(listing, "MEMORY.md") {
		t.Errorf("expected MEMORY.md in listing, got: %s", listing)
	}
	if !strings.Contains(listing, "memory/notes.md") {
		t.Errorf("expected memory/notes.md in listing, got: %s", listing)
	}
}

func TestListFiles_LeaderGlobalScopeFallback(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")
	memberID := uuid.New()
	leaderID := uuid.New()

	// Leader has only global-scope docs (userID="").
	ms.docs[docKey(leaderID.String(), "", "MEMORY.md")] = "leader global"

	ctx := memCtx(memberID, "user1", leaderID.String())
	listing, handled, err := mi.ListFiles(ctx, "memory")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatal("expected handled=true")
	}
	if !strings.Contains(listing, "MEMORY.md") {
		t.Errorf("expected MEMORY.md from leader's global scope, got: %s", listing)
	}
}

func TestReadFile_LeaderFallback_MemorySubpath(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")
	memberID := uuid.New()
	leaderID := uuid.New()

	// Leader has a memory subpath file.
	ms.docs[docKey(leaderID.String(), "user1", "memory/notes.md")] = "leader subpath"

	ctx := memCtx(memberID, "user1", leaderID.String())
	content, handled, err := mi.ReadFile(ctx, "memory/notes.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatal("expected handled=true")
	}
	if content != "leader subpath" {
		t.Errorf("expected 'leader subpath', got %q", content)
	}
}

func TestListFiles_LeaderIsSelf_NoDuplication(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")
	agentID := uuid.New()

	ms.docs[docKey(agentID.String(), "user1", "MEMORY.md")] = "own mem"

	ctx := memCtx(agentID, "user1", agentID.String())
	listing, handled, err := mi.ListFiles(ctx, "memory")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatal("expected handled=true")
	}
	// Should appear exactly once.
	count := strings.Count(listing, "MEMORY.md")
	if count != 1 {
		t.Errorf("expected MEMORY.md once, got %d times in: %s", count, listing)
	}
}

// --- Path-based shared/private memory scoping (predefined agents) ---

func TestIsSharedMemoryPath(t *testing.T) {
	cases := []struct {
		path   string
		shared bool
	}{
		{"memory/company.md", true},
		{"memory/company-research.md", true},
		{"memory/use-cases.md", true},
		{"memory/decisions.md", true},
		{"memory/projects/falcon.md", true},
		{"memory/projects/falcon/notes.md", true},
		{"MEMORY.md", false},
		{"memory/people/mark.md", false},
		{"memory/2026-07-05.md", false},
		{"memory/notes.md", false},
		{"memory/projects", false}, // the directory itself, not a file under it
	}
	for _, c := range cases {
		if got := isSharedMemoryPath(c.path); got != c.shared {
			t.Errorf("isSharedMemoryPath(%q) = %v, want %v", c.path, got, c.shared)
		}
	}
}

func TestWriteFile_PredefinedAgent_SharedPath_StoresGlobal(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")
	agentID := uuid.New()

	ctx := memCtxTyped(agentID, "user1", "", store.AgentTypePredefined)
	result, err := mi.WriteFile(ctx, "memory/projects/falcon.md", "Falcon launches Sept 15", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Handled {
		t.Fatal("expected handled=true")
	}

	// Stored at global scope (userID=""), not per-user.
	got, gerr := ms.GetDocument(ctx, agentID.String(), "", "memory/projects/falcon.md")
	if gerr != nil || got != "Falcon launches Sept 15" {
		t.Errorf("expected shared doc at global scope, got %q, err %v", got, gerr)
	}
	if _, err := ms.GetDocument(ctx, agentID.String(), "user1", "memory/projects/falcon.md"); err == nil {
		t.Error("did not expect shared path to also be stored per-user")
	}
}

func TestWriteFile_PredefinedAgent_PrivatePath_StoresPerUser(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")
	agentID := uuid.New()

	ctx := memCtxTyped(agentID, "user1", "", store.AgentTypePredefined)
	result, err := mi.WriteFile(ctx, "memory/people/mark.md", "Mark prefers async updates", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Handled {
		t.Fatal("expected handled=true")
	}

	// Stored per-user (existing/private behavior), not at global scope.
	got, gerr := ms.GetDocument(ctx, agentID.String(), "user1", "memory/people/mark.md")
	if gerr != nil || got != "Mark prefers async updates" {
		t.Errorf("expected private doc per-user, got %q, err %v", got, gerr)
	}
	if _, err := ms.GetDocument(ctx, agentID.String(), "", "memory/people/mark.md"); err == nil {
		t.Error("did not expect private path to be stored at global scope")
	}
}

func TestReadFile_PredefinedAgent_SharedPath_ReadsGlobal(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")
	agentID := uuid.New()

	// Doc lives at global scope only — never written per-user.
	ms.docs[docKey(agentID.String(), "", "memory/company.md")] = "Acme Corp, B2B SaaS"

	ctx := memCtxTyped(agentID, "user2", "", store.AgentTypePredefined)
	content, handled, err := mi.ReadFile(ctx, "memory/company.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatal("expected handled=true")
	}
	if content != "Acme Corp, B2B SaaS" {
		t.Errorf("expected shared doc content, got %q", content)
	}
}

func TestReadFile_PredefinedAgent_PrivatePath_ReadsPerUser(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")
	agentID := uuid.New()

	ms.docs[docKey(agentID.String(), "user1", "memory/people/mark.md")] = "Mark: prefers async"

	ctx := memCtxTyped(agentID, "user1", "", store.AgentTypePredefined)
	content, handled, err := mi.ReadFile(ctx, "memory/people/mark.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatal("expected handled=true")
	}
	if content != "Mark: prefers async" {
		t.Errorf("expected private per-user doc, got %q", content)
	}
}

func TestWriteFile_OpenAgent_ProjectsPath_StaysPerUser(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")
	agentID := uuid.New()

	// Open agents are unaffected by shared-path routing — always per-user.
	ctx := memCtxTyped(agentID, "user1", "", store.AgentTypeOpen)
	_, err := mi.WriteFile(ctx, "memory/projects/falcon.md", "personal project notes", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, gerr := ms.GetDocument(ctx, agentID.String(), "user1", "memory/projects/falcon.md")
	if gerr != nil || got != "personal project notes" {
		t.Errorf("expected open agent to store per-user, got %q, err %v", got, gerr)
	}
	if _, err := ms.GetDocument(ctx, agentID.String(), "", "memory/projects/falcon.md"); err == nil {
		t.Error("open agent should not route memory/projects/* to global scope")
	}
}

func TestWriteFile_NoAgentType_ProjectsPath_StaysPerUser(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")
	agentID := uuid.New()

	// No agent type set at all (agentType == "") — same as open, unaffected.
	ctx := memCtx(agentID, "user1", "")
	_, err := mi.WriteFile(ctx, "memory/projects/falcon.md", "notes", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, gerr := ms.GetDocument(ctx, agentID.String(), "user1", "memory/projects/falcon.md"); gerr != nil || got != "notes" {
		t.Errorf("expected per-user storage when agent type unset, got %q, err %v", got, gerr)
	}
}

func TestWriteFile_PredefinedAgent_SharedPath_KGExtractGlobalScope(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")
	agentID := uuid.New()

	gotUserID := "unset"
	done := make(chan struct{})
	mi.SetKGExtractFunc(func(_ context.Context, _, userID, _ string) {
		gotUserID = userID
		close(done)
	})

	ctx := memCtxTyped(agentID, "user1", "", store.AgentTypePredefined)
	if _, err := mi.WriteFile(ctx, "memory/decisions.md", "Decided to ship v2 in Q3", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	<-done
	if gotUserID != "" {
		t.Errorf("expected KG extraction at global scope (userID=\"\"), got %q", gotUserID)
	}
}

func TestWriteFile_PredefinedAgent_PrivatePath_KGExtractPerUserScope(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")
	agentID := uuid.New()

	gotUserID := "unset"
	done := make(chan struct{})
	mi.SetKGExtractFunc(func(_ context.Context, _, userID, _ string) {
		gotUserID = userID
		close(done)
	})

	ctx := memCtxTyped(agentID, "user1", "", store.AgentTypePredefined)
	if _, err := mi.WriteFile(ctx, "memory/people/mark.md", "Mark: likes concise updates", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	<-done
	if gotUserID != "user1" {
		t.Errorf("expected KG extraction at per-user scope, got %q", gotUserID)
	}
}

func TestListFiles_PredefinedAgent_MergesSharedDocs(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")
	agentID := uuid.New()

	// Private per-user doc.
	ms.docs[docKey(agentID.String(), "user1", "memory/people/mark.md")] = "Mark notes"
	// Shared workspace docs at global scope.
	ms.docs[docKey(agentID.String(), "", "memory/company.md")] = "Acme Corp"
	ms.docs[docKey(agentID.String(), "", "memory/projects/falcon.md")] = "Falcon project"

	ctx := memCtxTyped(agentID, "user1", "", store.AgentTypePredefined)
	listing, handled, err := mi.ListFiles(ctx, "memory")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatal("expected handled=true")
	}
	for _, want := range []string{"memory/people/mark.md", "memory/company.md", "memory/projects/falcon.md"} {
		if !strings.Contains(listing, want) {
			t.Errorf("expected %q in listing, got: %s", want, listing)
		}
	}
}

func TestListFiles_OpenAgent_DoesNotMergeGlobalScope(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")
	agentID := uuid.New()

	ms.docs[docKey(agentID.String(), "user1", "MEMORY.md")] = "own mem"
	// Some unrelated global-scope doc — open agents must not see it via merge.
	ms.docs[docKey(agentID.String(), "", "memory/company.md")] = "Acme Corp"

	ctx := memCtxTyped(agentID, "user1", "", store.AgentTypeOpen)
	listing, handled, err := mi.ListFiles(ctx, "memory")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatal("expected handled=true")
	}
	if strings.Contains(listing, "memory/company.md") {
		t.Errorf("open agent listing should not merge global-scope docs, got: %s", listing)
	}
}

// --- Agent-type resolver fallback (ctx without agent type, e.g. MCP bridge path) ---

func TestWriteFile_NoCtxType_ResolverPredefined_StoresGlobal(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")
	agentID := uuid.New()

	// Simulates the MCP bridge / background paths: ctx carries agent ID + user
	// ID but NOT the agent type. The authoritative resolver must supply it.
	mi.SetAgentTypeResolver(func(_ context.Context, id uuid.UUID) string {
		if id == agentID {
			return store.AgentTypePredefined
		}
		return ""
	})

	ctx := memCtx(agentID, "user1", "") // no WithAgentType
	if _, err := mi.WriteFile(ctx, "memory/decisions.md", "Launch is Sept 15", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, gerr := ms.GetDocument(ctx, agentID.String(), "", "memory/decisions.md")
	if gerr != nil || got != "Launch is Sept 15" {
		t.Errorf("expected shared doc at global scope via resolver, got %q, err %v", got, gerr)
	}
	if _, err := ms.GetDocument(ctx, agentID.String(), "user1", "memory/decisions.md"); err == nil {
		t.Error("did not expect per-user storage when resolver reports predefined")
	}
}

func TestReadFile_NoCtxType_ResolverPredefined_ReadsGlobal(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")
	agentID := uuid.New()

	ms.docs[docKey(agentID.String(), "", "memory/company.md")] = "Acme Corp"
	mi.SetAgentTypeResolver(func(_ context.Context, _ uuid.UUID) string {
		return store.AgentTypePredefined
	})

	ctx := memCtx(agentID, "user2", "") // no WithAgentType
	content, handled, err := mi.ReadFile(ctx, "memory/company.md")
	if err != nil || !handled {
		t.Fatalf("unexpected: handled=%v err=%v", handled, err)
	}
	if content != "Acme Corp" {
		t.Errorf("expected shared doc via resolver, got %q", content)
	}
}

func TestWriteFile_NoCtxType_NoResolver_StaysPerUser(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")
	agentID := uuid.New()

	// Neither ctx nor resolver knows the type → safe per-user default.
	ctx := memCtx(agentID, "user1", "")
	if _, err := mi.WriteFile(ctx, "memory/decisions.md", "notes", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, gerr := ms.GetDocument(ctx, agentID.String(), "user1", "memory/decisions.md"); gerr != nil || got != "notes" {
		t.Errorf("expected per-user storage without type info, got %q, err %v", got, gerr)
	}
}

func TestWriteFile_CtxTypeWinsOverResolver(t *testing.T) {
	ms := newMockMemoryStore()
	mi := NewMemoryInterceptor(ms, "/workspace")
	agentID := uuid.New()

	// ctx says open; resolver would say predefined. ctx is the fast path and wins.
	mi.SetAgentTypeResolver(func(_ context.Context, _ uuid.UUID) string {
		return store.AgentTypePredefined
	})
	ctx := memCtxTyped(agentID, "user1", "", store.AgentTypeOpen)
	if _, err := mi.WriteFile(ctx, "memory/decisions.md", "open agent notes", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := ms.GetDocument(ctx, agentID.String(), "user1", "memory/decisions.md"); err != nil {
		t.Error("expected per-user storage when ctx type is open")
	}
}

// --- NewCachedAgentTypeResolver ---

type stubAgentTypeLookup struct {
	agentType string
	err       error
	calls     int
}

func (s *stubAgentTypeLookup) GetByIDUnscoped(_ context.Context, _ uuid.UUID) (*store.AgentData, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return &store.AgentData{AgentType: s.agentType}, nil
}

func TestCachedAgentTypeResolver_LooksUpAndCaches(t *testing.T) {
	lookup := &stubAgentTypeLookup{agentType: store.AgentTypePredefined}
	resolver := NewCachedAgentTypeResolver(lookup, time.Minute)
	agentID := uuid.New()

	for range 3 {
		if got := resolver(context.Background(), agentID); got != store.AgentTypePredefined {
			t.Fatalf("resolver = %q, want predefined", got)
		}
	}
	if lookup.calls != 1 {
		t.Errorf("expected 1 store call (cached afterwards), got %d", lookup.calls)
	}
}

func TestCachedAgentTypeResolver_ErrorNotCached(t *testing.T) {
	lookup := &stubAgentTypeLookup{err: fmt.Errorf("db down")}
	resolver := NewCachedAgentTypeResolver(lookup, time.Minute)
	agentID := uuid.New()

	if got := resolver(context.Background(), agentID); got != "" {
		t.Errorf("expected empty type on error, got %q", got)
	}
	// Error must not be cached — recovery on next call.
	lookup.err = nil
	lookup.agentType = store.AgentTypeOpen
	if got := resolver(context.Background(), agentID); got != store.AgentTypeOpen {
		t.Errorf("expected retry after error to succeed, got %q", got)
	}
}

func TestCachedAgentTypeResolver_NilAgentID(t *testing.T) {
	lookup := &stubAgentTypeLookup{agentType: store.AgentTypePredefined}
	resolver := NewCachedAgentTypeResolver(lookup, time.Minute)
	if got := resolver(context.Background(), uuid.Nil); got != "" {
		t.Errorf("expected empty type for nil agent ID, got %q", got)
	}
	if lookup.calls != 0 {
		t.Errorf("expected no store call for nil agent ID, got %d", lookup.calls)
	}
}
