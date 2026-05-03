//go:build e2e

package cli_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestBackupUserScope verifies the backup CLI no longer accepts any
// tenant-scoped flag — full-DB scope only (Q-G + Q-8). The kept flags are
// --output, --exclude-db, --exclude-files, --upload-s3.
func TestBackupUserScope(t *testing.T) {
	out, err := exec.Command(goclawBin, "backup", "--help").CombinedOutput()
	if err != nil {
		t.Fatalf("goclaw backup --help: %v\n%s", err, out)
	}
	help := string(out)

	// Acceptable flags must remain.
	for _, flag := range []string{"--output", "--exclude-db", "--exclude-files", "--upload-s3"} {
		if !strings.Contains(help, flag) {
			t.Errorf("missing kept flag %q in backup --help:\n%s", flag, help)
		}
	}

	// Tenant-scope flags must be absent.
	for _, flag := range []string{"--tenant", "--tenant-id", "--tenant-slug", "--scope"} {
		if strings.Contains(help, flag) {
			t.Errorf("dropped flag %q still present in backup --help:\n%s", flag, help)
		}
	}

	// Passing --tenant must error as unknown flag.
	cmd := exec.Command(goclawBin, "backup", "--tenant=foo", "--output=/tmp/x.tar.gz")
	out, err = cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit for backup --tenant; got 0\n%s", out)
	}
	if !strings.Contains(string(out), "unknown flag") {
		t.Errorf("expected %q in stderr; got:\n%s", "unknown flag", out)
	}
}

// TestRestoreUserScope mirrors TestBackupUserScope for the restore CLI.
func TestRestoreUserScope(t *testing.T) {
	out, err := exec.Command(goclawBin, "restore", "--help").CombinedOutput()
	if err != nil {
		t.Fatalf("goclaw restore --help: %v\n%s", err, out)
	}
	help := string(out)

	for _, flag := range []string{"--tenant", "--tenant-id", "--tenant-slug", "--scope"} {
		if strings.Contains(help, flag) {
			t.Errorf("dropped flag %q still present in restore --help:\n%s", flag, help)
		}
	}

	cmd := exec.Command(goclawBin, "restore", "--tenant=foo", "/tmp/x.tar.gz")
	out, err = cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit for restore --tenant; got 0\n%s", out)
	}
	if !strings.Contains(string(out), "unknown flag") {
		t.Errorf("expected %q in stderr; got:\n%s", "unknown flag", out)
	}
}
