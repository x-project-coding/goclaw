package workspace

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// workspaceRoot returns the resolved workspace root directory.
// Priority: GOCLAW_WORKSPACE_ROOT env var → ~/.goclaw/workspace default.
func workspaceRoot() string {
	if r := os.Getenv("GOCLAW_WORKSPACE_ROOT"); r != "" {
		return r
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".goclaw", "workspace")
}

// projectsDir returns the managed projects sub-folder under workspace root.
func projectsDir() string {
	return filepath.Join(workspaceRoot(), "projects")
}

// ProjectWorkspacePath returns the absolute filesystem path for a project slug.
// Validates slug before any path construction. Applies filepath.Clean and
// asserts the result is strictly under <workspaceRoot>/projects/ as
// defense-in-depth against unexpected path escape.
// Does NOT create the folder — call EnsureProjectFolder for that.
func ProjectWorkspacePath(slug string) (string, error) {
	if err := ValidateProjectSlug(slug); err != nil {
		return "", fmt.Errorf("workspace: invalid project slug: %w", err)
	}

	root := workspaceRoot()
	base := filepath.Join(root, "projects")
	path := filepath.Clean(filepath.Join(base, slug))

	// Defense-in-depth: confirm the cleaned path is still inside projects/.
	prefix := base + string(filepath.Separator)
	if !strings.HasPrefix(path+string(filepath.Separator), prefix) {
		return "", fmt.Errorf("workspace: project path escapes projects directory")
	}

	return path, nil
}

// EnsureProjectFolder creates the project workspace folder with mode 0o755.
// Idempotent — safe to call when the folder already exists.
// Returns the absolute path on success.
// On failure, logs a warning and returns the error; callers in the create flow
// should treat FS errors as non-fatal (DB row is source of truth).
func EnsureProjectFolder(ctx context.Context, slug string) (string, error) {
	path, err := ProjectWorkspacePath(slug)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(path, 0o755); err != nil {
		slog.WarnContext(ctx, "workspace.project_folder_create_failed",
			"slug", slug,
			"path", path,
			"err", err,
		)
		return "", fmt.Errorf("workspace: create project folder %q: %w", path, err)
	}

	return path, nil
}

// ProjectExists reports whether the project workspace folder exists on disk.
func ProjectExists(slug string) bool {
	path, err := ProjectWorkspacePath(slug)
	if err != nil {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
