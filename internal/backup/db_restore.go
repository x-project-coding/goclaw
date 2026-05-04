//go:build !sqliteonly

package backup

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// RestoreDatabase restores a PostgreSQL database from a plain-SQL dump reader.
// Uses a temporary .pgpass file (0600) to pass credentials securely.
// The child psql process receives only PGPASSFILE, PATH, HOME, LC_ALL=C.
func RestoreDatabase(ctx context.Context, dsn string, dumpReader io.Reader) error {
	creds, err := ParseDSN(dsn)
	if err != nil {
		return fmt.Errorf("parse DSN: %w", err)
	}

	psql, err := exec.LookPath("psql")
	if err != nil {
		return fmt.Errorf("psql not found on PATH: %w", err)
	}

	tempDir, pgpassPath, err := WritePgpass(creds)
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	args := []string{
		"--host", creds.Host,
		"--port", creds.Port,
		"--username", creds.User,
		"--dbname", creds.DBName,
		"--no-password",
	}

	cmd := exec.CommandContext(ctx, psql, args...)
	cmd.Env = CleanEnv(pgpassPath)
	cmd.Stdin = dumpReader

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		// Truncate very long psql error output.
		if len(errMsg) > 512 {
			errMsg = errMsg[:512] + "..."
		}
		return fmt.Errorf("psql restore failed: %s", errMsg)
	}
	return nil
}

// RevokeAllSessionsPostRestore revokes every still-active row in user_sessions
// after a restore completes. Without this step, a backup taken on day-2 and
// restored on day-4 would reactivate refresh tokens that were legitimately
// revoked between days 2 and 4 — defeating refresh-token-rotation safety.
// RFC 6749 §10.4 implication: revocation isn't a delete, so the rows survive
// the round-trip; we force re-auth instead.
//
// Returns the number of rows updated. A missing user_sessions table (fresh DB
// with old-schema backup) is treated as a soft warning, not an error.
func RevokeAllSessionsPostRestore(ctx context.Context, dsn string) (int64, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return 0, fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	res, err := db.ExecContext(ctx,
		`UPDATE user_sessions SET revoked_at = NOW() WHERE revoked_at IS NULL`)
	if err != nil {
		// undefined_table → restored schema predates the user_sessions table; nothing to revoke.
		if strings.Contains(err.Error(), "user_sessions") &&
			strings.Contains(err.Error(), "does not exist") {
			return 0, nil
		}
		return 0, fmt.Errorf("revoke sessions: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// CheckActiveConnections returns the number of active backend connections to the
// database (excluding the current connection). Used as a pre-restore safety check.
func CheckActiveConnections(ctx context.Context, dsn string) (int, error) {
	creds, err := ParseDSN(dsn)
	if err != nil {
		return 0, fmt.Errorf("parse DSN: %w", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return 0, fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	var count int
	query := `SELECT COUNT(*) FROM pg_stat_activity
	           WHERE datname = $1 AND pid <> pg_backend_pid()`
	if err := db.QueryRowContext(ctx, query, creds.DBName).Scan(&count); err != nil {
		return 0, fmt.Errorf("query pg_stat_activity: %w", err)
	}
	return count, nil
}
