package tools

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// effectiveWorkspace returns the per-user workspace from ctx if available,
// falling back to the interceptor's base workspace.
// This ensures memory path detection and normalization work correctly
// for per-user workspaces (e.g. workspace/channel/userID/).
func effectiveWorkspace(ctx context.Context, baseWorkspace string) string {
	if ws := ToolWorkspaceFromCtx(ctx); ws != "" {
		return ws
	}
	return baseWorkspace
}

// isMemoryDir checks if a path refers to the memory directory itself.
// Handles "memory", "./memory", "/workspace/memory" etc.
func isMemoryDir(path, workspace string) bool {
	clean := filepath.Clean(path)
	if clean == "memory" {
		return true
	}
	if workspace != "" && filepath.IsAbs(clean) {
		expected := filepath.Join(filepath.Clean(workspace), "memory")
		return clean == expected
	}
	return false
}

// sharedMemoryFiles are workspace-wide memory documents for predefined agents.
// Unlike the default per-user-private scoping, these are stored and read at
// global scope (userID="") so every workspace member sees the same facts.
var sharedMemoryFiles = map[string]bool{
	"memory/company.md":          true,
	"memory/company-research.md": true,
	"memory/use-cases.md":        true,
	"memory/decisions.md":        true,
}

// sharedMemoryProjectsPrefix matches any file under memory/projects/ — one
// shared doc per ongoing project or deliverable.
const sharedMemoryProjectsPrefix = "memory/projects/"

// isSharedMemoryPath reports whether relPath (a workspace-relative memory
// path, as returned by normalizeToRelative) is a shared workspace document
// for predefined agents. Everything else under memory/ — MEMORY.md,
// memory/people/*, daily notes (memory/YYYY-MM-DD.md), etc. — stays private
// per-user (the existing/default behavior).
func isSharedMemoryPath(relPath string) bool {
	clean := filepath.ToSlash(filepath.Clean(relPath))
	if sharedMemoryFiles[clean] {
		return true
	}
	return strings.HasPrefix(clean, sharedMemoryProjectsPrefix)
}

// isMemoryPath checks if a path refers to a memory file (MEMORY.md, memory.md, memory/*).
// Handles both relative and absolute paths (when workspace is provided).
func isMemoryPath(path, workspace string) bool {
	clean := filepath.Clean(path)
	base := filepath.Base(clean)

	// Root-level MEMORY.md or memory.md
	dir := filepath.Dir(clean)
	if (dir == "." || dir == "/" || dir == "") && (base == bootstrap.MemoryFile || base == bootstrap.MemoryAltFile) {
		return true
	}

	// Anything under memory/ directory (relative)
	if strings.HasPrefix(clean, "memory/") || strings.HasPrefix(clean, "memory\\") {
		return true
	}

	// Absolute path at workspace root or under workspace/memory/
	if workspace != "" && filepath.IsAbs(clean) {
		cleanWS := filepath.Clean(workspace)
		if filepath.Dir(clean) == cleanWS && (base == bootstrap.MemoryFile || base == bootstrap.MemoryAltFile) {
			return true
		}
		memDir := filepath.Join(cleanWS, "memory")
		if strings.HasPrefix(clean, memDir+string(filepath.Separator)) {
			return true
		}
	}

	return false
}

// KGExtractFunc is a callback invoked after a memory write to extract KG entities.
// agentID, userID, content are passed from the write context.
type KGExtractFunc func(ctx context.Context, agentID, userID, content string)

// MemoryInterceptor routes memory file reads/writes to the MemoryStore.
// Keeps MEMORY.md and memory/* in Postgres.
type MemoryInterceptor struct {
	memStore    store.MemoryStore
	workspace   string
	kgExtractFn KGExtractFunc
}

// NewMemoryInterceptor creates an interceptor backed by the given memory store.
func NewMemoryInterceptor(ms store.MemoryStore, workspace string) *MemoryInterceptor {
	return &MemoryInterceptor{memStore: ms, workspace: workspace}
}

// SetKGExtractFunc sets the callback for KG extraction after memory writes.
func (m *MemoryInterceptor) SetKGExtractFunc(fn KGExtractFunc) {
	m.kgExtractFn = fn
}

// ReadFile attempts to read a memory file from the DB.
// Returns (content, true, nil) if handled, or ("", false, nil) if not a memory path.
func (m *MemoryInterceptor) ReadFile(ctx context.Context, path string) (string, bool, error) {
	ws := effectiveWorkspace(ctx, m.workspace)
	if !isMemoryPath(path, ws) {
		return "", false, nil
	}

	agentID := store.AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return "", false, nil // no agent context
	}

	// Normalize absolute path to workspace-relative for DB storage
	relPath := normalizeToRelative(path, ws)

	userID := store.MemoryUserID(ctx)
	// Predefined agents: shared workspace-memory paths (memory/company.md,
	// memory/projects/*, memory/decisions.md, etc.) live at global scope —
	// read that scope directly (exact-scope first) instead of the per-user
	// attempt below, so every member finds the same shared doc.
	if store.AgentTypeFromContext(ctx) == store.AgentTypePredefined && isSharedMemoryPath(relPath) {
		userID = ""
	}
	agentStr := agentID.String()

	// Try per-user first, then global
	content, err := m.memStore.GetDocument(ctx, agentStr, userID, relPath)
	if err != nil && userID != "" {
		content, err = m.memStore.GetDocument(ctx, agentStr, "", relPath)
	}
	if err != nil {
		// Not found in own memory — try leader's memory as fallback (team members only).
		if leaderID := LeaderAgentIDFromCtx(ctx); leaderID != "" && leaderID != agentStr {
			leaderContent, lerr := m.memStore.GetDocument(ctx, leaderID, userID, relPath)
			if lerr != nil && userID != "" {
				leaderContent, lerr = m.memStore.GetDocument(ctx, leaderID, "", relPath)
			}
			if lerr == nil && leaderContent != "" {
				return leaderContent, true, nil
			}
		}
		slog.Debug("memory interceptor: document not found", "path", path, "agent", agentStr)
		return "", true, nil
	}

	return content, true, nil
}

// MemoryWriteResult holds the outcome of a memory write operation.
type MemoryWriteResult struct {
	Handled         bool
	KGTriggered     bool
	PreviousContent string // non-empty if an existing document was overwritten (non-append)
}

// WriteFile attempts to write a memory file to the DB (+ re-index chunks for .md files).
// When appendMode is true, new content is appended to the existing document with a separator.
// When appendMode is false and an existing document is overwritten with different content,
// PreviousContent is populated in the result to allow callers to warn the agent.
func (m *MemoryInterceptor) WriteFile(ctx context.Context, path, content string, appendMode bool) (MemoryWriteResult, error) {
	ws := effectiveWorkspace(ctx, m.workspace)
	if !isMemoryPath(path, ws) {
		return MemoryWriteResult{}, nil
	}

	agentID := store.AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return MemoryWriteResult{}, nil // no agent context
	}

	// Block memory writes for team members — memory is leader-only.
	agentStr := agentID.String()
	if leaderID := LeaderAgentIDFromCtx(ctx); leaderID != "" && leaderID != agentStr {
		return MemoryWriteResult{Handled: true}, fmt.Errorf(
			"memory is read-only for team members — only the team leader can save memory files")
	}

	// Normalize absolute path to workspace-relative for DB storage
	relPath := normalizeToRelative(path, ws)

	userID := store.MemoryUserID(ctx)
	// Predefined agents: shared workspace-memory paths (memory/company.md,
	// memory/projects/*, memory/decisions.md, etc.) are written at global
	// scope regardless of the caller's per-user MemoryUserID, so every
	// workspace member benefits from the fact. Everything else under
	// memory/ (MEMORY.md, memory/people/*, daily notes) keeps the existing
	// per-user-private behavior.
	sharedScope := store.AgentTypeFromContext(ctx) == store.AgentTypePredefined && isSharedMemoryPath(relPath)
	if sharedScope {
		userID = ""
	}

	var previousContent string

	if appendMode {
		// Append: read existing content and merge with separator.
		existing, err := m.memStore.GetDocument(ctx, agentStr, userID, relPath)
		if err == nil && existing != "" {
			content = existing + "\n\n---\n\n" + content
		}
	} else {
		// Replace: capture previous content for overwrite warning.
		oldContent, err := m.memStore.GetDocument(ctx, agentStr, userID, relPath)
		if err == nil && oldContent != "" && oldContent != content {
			previousContent = oldContent
		}
	}

	// Write document to DB
	if err := m.memStore.PutDocument(ctx, agentStr, userID, relPath, content); err != nil {
		return MemoryWriteResult{Handled: true}, err
	}

	// Only index .md files (chunk + embed).
	if strings.HasSuffix(relPath, ".md") {
		if err := m.memStore.IndexDocument(ctx, agentStr, userID, relPath); err != nil {
			slog.Warn("memory interceptor: index failed after write", "path", path, "error", err)
			// Non-fatal: document was saved, indexing will catch up
		}
	}

	// Trigger KG extraction in background if configured.
	// Use KGUserID (not MemoryUserID) so shared KG entities go into agent-level scope.
	// Shared memory-path writes always extract at global scope, matching the
	// document scope above, regardless of the ambient KGUserID setting.
	kgTriggered := false
	if m.kgExtractFn != nil && content != "" {
		kgUserID := store.KGUserID(ctx)
		if sharedScope {
			kgUserID = ""
		}
		go m.kgExtractFn(context.WithoutCancel(ctx), agentStr, kgUserID, content)
		kgTriggered = true
	}

	return MemoryWriteResult{Handled: true, KGTriggered: kgTriggered, PreviousContent: previousContent}, nil
}

// ListFiles lists memory documents from the DB when path is the memory directory.
// Returns (listing, true, nil) if handled, or ("", false, nil) if not a memory path.
func (m *MemoryInterceptor) ListFiles(ctx context.Context, path string) (string, bool, error) {
	ws := effectiveWorkspace(ctx, m.workspace)
	if !isMemoryDir(path, ws) {
		return "", false, nil
	}

	agentID := store.AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return "", false, nil
	}

	userID := store.MemoryUserID(ctx)
	agentStr := agentID.String()
	docs, err := m.memStore.ListDocuments(ctx, agentStr, userID)
	if err != nil {
		return "", true, err
	}

	// Predefined agents: merge in shared (global-scope) workspace documents —
	// memory/company.md, memory/projects/*, memory/decisions.md, etc. — so
	// they show up alongside the caller's private per-user memory files.
	// Only merge docs that are actually routed as shared paths; other
	// global-scope docs (e.g. a fallback template) are unrelated to this
	// routing and stay out of the listing here.
	if store.AgentTypeFromContext(ctx) == store.AgentTypePredefined && userID != "" {
		sharedDocs, serr := m.memStore.ListDocuments(ctx, agentStr, "")
		if serr == nil {
			seen := make(map[string]bool, len(docs))
			for _, d := range docs {
				seen[d.Path] = true
			}
			for _, d := range sharedDocs {
				if !seen[d.Path] && isSharedMemoryPath(d.Path) {
					docs = append(docs, d)
					seen[d.Path] = true
				}
			}
		}
	}

	// Merge leader's documents for team members (fallback, deduplicate by path).
	if leaderID := LeaderAgentIDFromCtx(ctx); leaderID != "" && leaderID != agentStr {
		seen := make(map[string]bool, len(docs))
		for _, d := range docs {
			seen[d.Path] = true
		}
		leaderDocs, _ := m.memStore.ListDocuments(ctx, leaderID, userID)
		// Also try global scope if per-user returned nothing.
		if len(leaderDocs) == 0 && userID != "" {
			leaderDocs, _ = m.memStore.ListDocuments(ctx, leaderID, "")
		}
		for _, d := range leaderDocs {
			if !seen[d.Path] {
				docs = append(docs, d)
			}
		}
	}

	if len(docs) == 0 {
		return "", true, nil
	}

	var sb strings.Builder
	for _, doc := range docs {
		fmt.Fprintf(&sb, "[FILE] %s\n", doc.Path)
	}
	return sb.String(), true, nil
}
