//go:build sqliteonly

package backup

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"strings"

	_ "modernc.org/sqlite"
)

// RestoreDatabase copies the dump stream directly to the SQLite database file.
// dsn is expected to be a file path or "file:/path/to/db" format.
func RestoreDatabase(_ context.Context, dsn string, dumpReader io.Reader) error {
	dbPath := parseSQLitePath(dsn)
	if dbPath == "" {
		return fmt.Errorf("could not resolve SQLite database path from DSN: %q", dsn)
	}

	f, err := os.OpenFile(dbPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("open SQLite db for restore %q: %w", dbPath, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, dumpReader); err != nil {
		return fmt.Errorf("write SQLite db: %w", err)
	}
	return nil
}

// RevokeAllSessionsPostRestore revokes every still-active row in user_sessions
// after a restore completes. See db_restore.go for full rationale (RFC 6749
// §10.4 implication — restore must force re-auth).
func RevokeAllSessionsPostRestore(ctx context.Context, dsn string) (int64, error) {
	dbPath := parseSQLitePath(dsn)
	if dbPath == "" {
		return 0, fmt.Errorf("could not resolve SQLite database path from DSN: %q", dsn)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return 0, fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	res, err := db.ExecContext(ctx,
		`UPDATE user_sessions SET revoked_at = CURRENT_TIMESTAMP WHERE revoked_at IS NULL`)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return 0, nil
		}
		return 0, fmt.Errorf("revoke sessions: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// CheckActiveConnections always returns 0 for SQLite builds.
// SQLite does not have a server process; concurrent access is handled by file locks.
func CheckActiveConnections(_ context.Context, _ string) (int, error) {
	return 0, nil
}
