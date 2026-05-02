//go:build e2e

package schema_test

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// TestPgUpDown verifies that running migrate up → down → up produces a
// schema identical to the first up. Comparison is done via pg_dump --schema-only
// with lines sorted and whitespace normalised, so column/index ordering
// differences in INFORMATION_SCHEMA do not cause false failures.
func TestPgUpDown(t *testing.T) {
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Skip("pg_dump not in PATH — skipping round-trip test")
	}

	helpers.MustLoadEnv()

	// First: apply all migrations.
	helpers.MustMigrateClean(t)

	dump1 := schemaDump(t)

	// Down then up.
	helpers.MigrateDown(t)
	helpers.MigrateUp(t)

	dump2 := schemaDump(t)

	if dump1 != dump2 {
		// Surface a diff-friendly view.
		lines1 := strings.Split(dump1, "\n")
		lines2 := strings.Split(dump2, "\n")
		t.Logf("round-trip dump1 lines: %d, dump2 lines: %d", len(lines1), len(lines2))
		for i, l := range lines1 {
			if i >= len(lines2) {
				t.Logf("line %d only in dump1: %q", i, l)
				continue
			}
			if lines2[i] != l {
				t.Logf("diff at line %d:\n  want: %q\n  got:  %q", i, l, lines2[i])
			}
		}
		for i := len(lines1); i < len(lines2); i++ {
			t.Logf("line %d only in dump2: %q", i, lines2[i])
		}
		t.Fatal("schema dump differs after up→down→up round-trip")
	}
}

// schemaDump runs pg_dump --schema-only on the e2e database and returns a
// normalised, sorted representation suitable for equality comparison.
func schemaDump(t *testing.T) string {
	t.Helper()

	dsn := helpers.DatabaseURL()
	// pg_dump accepts standard libpq env vars; easier to set them than parse DSN.
	host, port, user, password, dbname := parseDSN(t, dsn)

	repoRoot := findRepoRoot(t)

	cmd := exec.Command("pg_dump",
		"--schema-only",
		"--no-owner",
		"--no-privileges",
		"--no-comments",
		"-h", host,
		"-p", port,
		"-U", user,
		"-d", dbname,
	)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "PGPASSWORD="+password)

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("pg_dump failed: %v", err)
	}

	return normalise(out)
}

// normalise strips blank lines, comment lines, SET lines, and then sorts the
// remaining lines so statement ordering differences don't cause false failures.
func normalise(raw []byte) string {
	sc := bufio.NewScanner(bytes.NewReader(raw))
	var lines []string
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "--") {
			continue
		}
		if strings.HasPrefix(line, "SET ") {
			continue
		}
		if strings.HasPrefix(line, "SELECT pg_catalog") {
			continue
		}
		// Strip pg_dump ACL / privilege lines — they vary by session/role and are
		// not part of the structural schema we want to compare.
		if strings.HasPrefix(line, "GRANT ") || strings.HasPrefix(line, "REVOKE ") ||
			strings.HasPrefix(line, "\\connect ") || strings.HasPrefix(line, "\\restrict ") ||
			strings.HasPrefix(line, "\\unrestrict ") {
			continue
		}
		lines = append(lines, line)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// parseDSN extracts connection parameters from a postgres:// DSN.
// Supports: postgres://user:pass@host:port/dbname?params
func parseDSN(t *testing.T, dsn string) (host, port, user, password, dbname string) {
	t.Helper()
	// Strip scheme.
	s := strings.TrimPrefix(dsn, "postgres://")
	s = strings.TrimPrefix(s, "postgresql://")

	// Strip query params.
	if idx := strings.Index(s, "?"); idx != -1 {
		s = s[:idx]
	}

	// user:pass@host:port/dbname
	atIdx := strings.LastIndex(s, "@")
	if atIdx < 0 {
		t.Fatalf("parseDSN: no @ in %q", dsn)
	}
	userPass := s[:atIdx]
	hostPathPart := s[atIdx+1:]

	if idx := strings.Index(userPass, ":"); idx >= 0 {
		user = userPass[:idx]
		password = userPass[idx+1:]
	} else {
		user = userPass
	}

	slashIdx := strings.Index(hostPathPart, "/")
	if slashIdx < 0 {
		t.Fatalf("parseDSN: no / after host in %q", dsn)
	}
	hostPort := hostPathPart[:slashIdx]
	dbname = hostPathPart[slashIdx+1:]

	if idx := strings.LastIndex(hostPort, ":"); idx >= 0 {
		host = hostPort[:idx]
		port = hostPort[idx+1:]
	} else {
		host = hostPort
		port = "5432"
	}
	return
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found walking up from %s", cwd)
		}
		dir = parent
	}
}
