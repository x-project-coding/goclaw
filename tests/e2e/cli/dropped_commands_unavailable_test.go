//go:build e2e

package cli_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestDroppedCommandsUnavailable runs each dropped CLI verb and asserts
// cobra rejects it with a non-zero exit + "unknown command" stderr.
func TestDroppedCommandsUnavailable(t *testing.T) {
	dropped := []string{
		"onboard",
		"auth",
		"agent",
		"agent-chat",
		"channels",
		"config",
		"cron",
		"pairing",
		"providers",
		"sessions",
		"skills",
		"prompt",
		"setup",
		"tenant-backup",
		"tenant-restore",
	}

	for _, name := range dropped {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(goclawBin, name)
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("expected non-zero exit for dropped %q; got 0\n%s", name, out)
			}
			if !strings.Contains(string(out), "unknown command") {
				t.Errorf("expected %q in stderr for %q; got:\n%s", "unknown command", name, out)
			}
		})
	}
}
