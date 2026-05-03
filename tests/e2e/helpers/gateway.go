//go:build e2e && !windows

package helpers

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Gateway tracks a running goclaw process for the test lifetime.
// Use StartGateway(t) to spawn one; t.Cleanup will tear it down automatically.
type Gateway struct {
	BaseURL string
	Port    string
	cmd     *exec.Cmd
	stop    sync.Once
}

// gatewayBuildOnce caches the binary path so multiple StartGateway calls in one
// test process reuse a single `go build` invocation.
var (
	gatewayBuildOnce sync.Once
	gatewayBinary    string
	gatewayBuildErr  error
)

// StartGateway compiles ./goclaw with the e2e tag-set and starts it.
// Health check polls GET /health (the actual endpoint per cmd/cli_helpers.go)
// until 200 or a 30s timeout fires. Process is killed via SIGTERM on cleanup.
func StartGateway(t *testing.T) *Gateway {
	t.Helper()
	MustLoadEnv()

	binary, err := buildGatewayOnce(t)
	if err != nil {
		t.Fatalf("e2e: build gateway: %v", err)
	}

	port, err := pickFreePort()
	if err != nil {
		t.Fatalf("e2e: pick free port: %v", err)
	}

	repoRoot, err := repoRoot()
	if err != nil {
		t.Fatalf("e2e: locate repo root: %v", err)
	}

	cmd := exec.Command(binary)
	cmd.Dir = repoRoot
	// Bridge env-file values into the env-var names the gateway reads.
	// env.e2e-tests/.env uses GOCLAW_DATABASE_URL; the gateway resolves PG via
	// GOCLAW_POSTGRES_DSN (see cmd/gateway_stores_pg.go).
	cmd.Env = append(os.Environ(),
		"GOCLAW_PORT="+port,
		"GOCLAW_HOST="+GatewayHost(),
		"GOCLAW_POSTGRES_DSN="+DatabaseURL(),
		"GOCLAW_JWT_SECRET="+JWTSecret(),
		"GOCLAW_ENCRYPTION_KEY="+EncryptionKey(),
	)
	// Inherit stdout/stderr so test logs surface gateway diagnostics.
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	// Put gateway in its own process group so we can kill descendants reliably.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		t.Fatalf("e2e: spawn gateway: %v", err)
	}

	gw := &Gateway{
		BaseURL: fmt.Sprintf("http://%s:%s", GatewayHost(), port),
		Port:    port,
		cmd:     cmd,
	}
	t.Cleanup(gw.Stop)

	if err := waitForReady(gw.BaseURL+"/health", 30*time.Second); err != nil {
		gw.Stop()
		t.Fatalf("e2e: gateway not ready: %v", err)
	}
	return gw
}

// Stop sends SIGTERM and waits up to 5s for graceful exit, then SIGKILL.
func (g *Gateway) Stop() {
	g.stop.Do(func() {
		if g.cmd == nil || g.cmd.Process == nil {
			return
		}
		pgid, err := syscall.Getpgid(g.cmd.Process.Pid)
		if err == nil {
			_ = syscall.Kill(-pgid, syscall.SIGTERM)
		} else {
			_ = g.cmd.Process.Signal(syscall.SIGTERM)
		}
		done := make(chan error, 1)
		go func() { done <- g.cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			if pgid, err := syscall.Getpgid(g.cmd.Process.Pid); err == nil {
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
			} else {
				_ = g.cmd.Process.Kill()
			}
			<-done
		}
	})
}

// buildGatewayOnce runs `go build -o /tmp/goclaw-e2e .` from the repo root.
// Reuses the resulting binary across all StartGateway calls in the test process.
func buildGatewayOnce(t *testing.T) (string, error) {
	t.Helper()
	gatewayBuildOnce.Do(func() {
		root, err := repoRoot()
		if err != nil {
			gatewayBuildErr = err
			return
		}
		out := filepath.Join(os.TempDir(), "goclaw-e2e")
		cmd := exec.Command("go", "build", "-o", out, ".")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			gatewayBuildErr = fmt.Errorf("go build: %w", err)
			return
		}
		gatewayBinary = out
	})
	return gatewayBinary, gatewayBuildErr
}

// repoRoot walks up from CWD looking for go.mod (the goclaw module root).
func repoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found walking up from " + cwd)
		}
		dir = parent
	}
}

// pickFreePort asks the kernel for an unused TCP port.
// Race window between Listen.Close and gateway bind is acceptable for e2e.
func pickFreePort() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer l.Close()
	addr := l.Addr().(*net.TCPAddr)
	return fmt.Sprintf("%d", addr.Port), nil
}

// waitForReady polls url every 200ms until 2xx response or timeout.
func waitForReady(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 1 * time.Second}
	var lastErr error
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(200 * time.Millisecond)
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		lastErr = fmt.Errorf("status %d", resp.StatusCode)
		time.Sleep(200 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = errors.New("unknown")
	}
	return fmt.Errorf("waitForReady timeout: %w", lastErr)
}
