package http

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/workspace"
)

// SkillCallbackHandler serves the /callback/v1/* endpoints used by external
// skill-backing services (e.g. the code-runner behind the `code` skill).
//
// These callbacks authenticate with a workspace API key rather than the
// gateway token, so the handler resolves auth directly against the api-key
// store instead of relying on the generic requireAuth middleware (which would
// also accept the gateway token / no-auth dev mode).
type SkillCallbackHandler struct {
	cfg    *config.Config
	msgBus *bus.MessageBus // for /callback/v1/messages → chat session delivery
}

// NewSkillCallbackHandler creates the skill-callback HTTP handler.
func NewSkillCallbackHandler(cfg *config.Config, msgBus *bus.MessageBus) *SkillCallbackHandler {
	return &SkillCallbackHandler{cfg: cfg, msgBus: msgBus}
}

// RegisterRoutes registers the /callback/v1/* skill-callback routes on the mux.
func (h *SkillCallbackHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /callback/v1/verify-key", h.handleVerifyKey)
	mux.HandleFunc("POST /callback/v1/messages", h.handleMessages)
}

// verifyKeyRequest is the request body for POST /callback/v1/verify-key.
// All fields are optional; the per-user workspace dir is only resolvable when
// both UserID and AgentID are supplied.
type verifyKeyRequest struct {
	UserID  string `json:"userId"`
	AgentID string `json:"agentId"`
}

// verifyKeyResponse is the JSON body returned by a successful verify-key call.
// WorkspaceDir is a pointer so it serializes as JSON null when it cannot be
// resolved — code-runner falls back to an isolated volume in that case.
type verifyKeyResponse struct {
	WorkspaceID  string  `json:"workspaceId"`
	TenantID     string  `json:"tenantId"`
	TenantSlug   string  `json:"tenantSlug"`
	GatewayID    string  `json:"gatewayId"`
	WorkspaceDir *string `json:"workspaceDir"`
}

// handleVerifyKey authenticates a workspace API key and reports the workspace
// identity (tenant, gateway) plus — when resolvable — the absolute per-user
// workspace directory. Before responding it snapshots that directory so the
// code-runner job is recoverable.
func (h *SkillCallbackHandler) handleVerifyKey(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)

	// Auth: require a genuine workspace API key. The gateway token and the
	// no-auth dev fallback are intentionally rejected — code-runner always
	// presents a workspace key.
	token := extractBearerToken(r)
	keyData, role := ResolveAPIKey(r.Context(), token)
	if keyData == nil || role == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": i18n.T(locale, i18n.MsgUnauthorized),
		})
		return
	}

	var req verifyKeyRequest
	if r.Body != nil {
		// Body is optional; tolerate an empty body but reject malformed JSON.
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&req); err != nil && err != io.EOF {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": i18n.T(locale, i18n.MsgInvalidJSON),
			})
			return
		}
	}

	// Resolve the tenant. A tenant-bound API key carries a concrete tenant id;
	// a system-level key (uuid.Nil) maps to the master tenant.
	tenantID := keyData.TenantID
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}

	tenantSlug := ""
	workspaceID := tenantID.String()
	if pkgTenantCache != nil {
		if tenant, err := pkgTenantCache.GetTenant(r.Context(), tenantID); err == nil && tenant != nil {
			tenantSlug = tenant.Slug
			// Prefer an external/x-api workspace id when the tenant record
			// carries one in its settings blob; otherwise fall back to the
			// tenant id as the stable workspace identifier.
			if ext := externalWorkspaceID(tenant.Settings); ext != "" {
				workspaceID = ext
			}
		} else if err != nil {
			slog.Warn("skillcallback.verify_key tenant lookup failed",
				"tenant_id", tenantID.String(), "error", err)
		}
	}

	// Resolve the per-user workspace directory only when BOTH userId and
	// agentId are supplied. Otherwise leave it null so code-runner degrades
	// gracefully to an isolated volume.
	var workspaceDir *string
	if req.UserID != "" && req.AgentID != "" {
		if dir := h.resolveWorkspaceDir(r.Context(), tenantID, tenantSlug, req.AgentID, req.UserID); dir != "" {
			// Pre-job backup: snapshot the workspace before code-runner mutates
			// it, using the container-local path (this process can access it).
			// Best-effort — a backup failure never fails verify-key.
			snapshotWorkspaceDir(dir)
			// Report the HOST path: code-runner's Docker bind-mount needs a
			// host path, not goclaw's container-local one. When the host root
			// is unconfigured this returns the path unchanged and code-runner
			// degrades to an isolated volume.
			hostDir := h.hostWorkspaceDir(dir)
			workspaceDir = &hostDir
		}
	}

	writeJSON(w, http.StatusOK, verifyKeyResponse{
		WorkspaceID:  workspaceID,
		TenantID:     tenantID.String(),
		TenantSlug:   tenantSlug,
		GatewayID:    h.cfg.Gateway.GatewayID,
		WorkspaceDir: workspaceDir,
	})
}

// resolveWorkspaceDir computes the absolute per-user workspace directory for a
// personal ("open") agent via the shared workspace resolver. Returns "" when
// the resolver fails.
//
// The resolved path is {workspaceBase}/tenants/{tenantSlug}/{agentID}/{userID}
// (the master tenant omits the tenants/{slug} prefix).
func (h *SkillCallbackHandler) resolveWorkspaceDir(ctx context.Context, tenantID uuid.UUID, tenantSlug, agentID, userID string) string {
	resolver := workspace.NewResolver()
	wc, err := resolver.Resolve(ctx, workspace.ResolveParams{
		AgentID:    agentID,
		AgentType:  "open", // personal, per-user workspace isolation
		UserID:     userID,
		TenantID:   tenantID.String(),
		TenantSlug: tenantSlug,
		PeerKind:   "direct",
		BaseDir:    h.cfg.WorkspacePath(),
	})
	if err != nil || wc == nil {
		slog.Warn("skillcallback.verify_key workspace resolution failed",
			"agent_id", agentID, "user_id", userID, "error", err)
		return ""
	}
	abs, err := filepath.Abs(wc.ActivePath)
	if err != nil {
		return wc.ActivePath
	}
	return abs
}

// hostWorkspaceDir rewrites a container-local workspace path onto the
// configured host root, so an external service (code-runner) can bind-mount
// it. GoClaw's workspace volume is mounted at cfg.WorkspacePath() inside this
// container but lives at WorkspaceHostRoot on the Docker host. When no host
// root is configured, or the path is not under the workspace base, the path
// is returned unchanged.
func (h *SkillCallbackHandler) hostWorkspaceDir(containerPath string) string {
	hostRoot := h.cfg.Gateway.WorkspaceHostRoot
	if hostRoot == "" {
		return containerPath
	}
	rel, err := filepath.Rel(h.cfg.WorkspacePath(), containerPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return containerPath
	}
	return filepath.Join(hostRoot, rel)
}

// externalWorkspaceID extracts an external/x-api workspace identifier from a
// tenant settings blob, if one is present. Recognises a few common key spellings.
// Returns "" when no usable id is found.
func externalWorkspaceID(settings json.RawMessage) string {
	if len(settings) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(settings, &m); err != nil {
		return ""
	}
	for _, key := range []string{"workspace_id", "workspaceId", "x_api_workspace_id", "external_workspace_id"} {
		if v, ok := m[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// snapshotWorkspaceDir takes a best-effort recoverability snapshot of the
// workspace directory before a code-runner job mutates it:
//   - if the directory is a git repository, it stages and commits everything;
//   - otherwise it writes a timestamped tar archive under a sibling
//     .code-backups/ directory.
//
// Any failure is logged and swallowed — backup must never block verify-key.
func snapshotWorkspaceDir(dir string) {
	if dir == "" {
		return
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return
	}

	if isGitRepo(dir) {
		if err := gitSnapshot(dir); err != nil {
			slog.Warn("skillcallback.verify_key git backup failed", "dir", dir, "error", err)
		} else {
			slog.Info("skillcallback.verify_key git backup committed", "dir", dir)
		}
		return
	}

	if path, err := tarSnapshot(dir); err != nil {
		slog.Warn("skillcallback.verify_key tar backup failed", "dir", dir, "error", err)
	} else {
		slog.Info("skillcallback.verify_key tar backup written", "dir", dir, "archive", path)
	}
}

// isGitRepo reports whether dir contains a .git directory or file (worktree).
func isGitRepo(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return true
	}
	return false
}

// gitSnapshot stages and commits the working tree of a git repo. A no-op
// commit (nothing changed) is treated as success.
func gitSnapshot(dir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if out, err := runGit(ctx, dir, "add", "-A"); err != nil {
		return fmt.Errorf("git add: %w (%s)", err, out)
	}

	// Nothing to commit → success, skip the commit.
	if out, err := runGit(ctx, dir, "status", "--porcelain"); err != nil {
		return fmt.Errorf("git status: %w (%s)", err, out)
	} else if len(out) == 0 {
		return nil
	}

	msg := "code-runner pre-job snapshot " + time.Now().UTC().Format(time.RFC3339)
	if out, err := runGitWithIdentity(ctx, dir, "commit", "-m", msg); err != nil {
		return fmt.Errorf("git commit: %w (%s)", err, out)
	}
	return nil
}

// runGit runs a git subcommand inside dir and returns trimmed combined output.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// runGitWithIdentity runs git with a committer identity injected via env, so
// the commit succeeds even when the repo (or the running user) has no
// configured git identity.
func runGitWithIdentity(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=goclaw", "GIT_AUTHOR_EMAIL=goclaw@localhost",
		"GIT_COMMITTER_NAME=goclaw", "GIT_COMMITTER_EMAIL=goclaw@localhost",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// tarSnapshot writes a timestamped tar archive of dir into a sibling
// .code-backups/ directory and returns the archive path.
func tarSnapshot(dir string) (string, error) {
	backupRoot := filepath.Join(filepath.Dir(dir), ".code-backups")
	if err := os.MkdirAll(backupRoot, 0o755); err != nil {
		return "", fmt.Errorf("mkdir backups: %w", err)
	}

	name := fmt.Sprintf("%s-%s.tar", filepath.Base(dir), time.Now().UTC().Format("20060102T150405Z"))
	archivePath := filepath.Join(backupRoot, name)

	f, err := os.Create(archivePath)
	if err != nil {
		return "", fmt.Errorf("create archive: %w", err)
	}
	defer f.Close()

	tw := tar.NewWriter(f)
	defer tw.Close()

	walkErr := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Skip the backups directory itself if it happens to live under dir.
		if info.IsDir() && path == backupRoot {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		// Only regular files and directories — skip symlinks/devices.
		if !info.Mode().IsRegular() && !info.IsDir() {
			return nil
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if info.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(tw, src)
		return err
	})
	if walkErr != nil {
		return "", fmt.Errorf("walk: %w", walkErr)
	}
	return archivePath, nil
}
