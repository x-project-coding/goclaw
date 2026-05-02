//go:build e2e

// Package helpers loads env.e2e-tests/.env and exposes typed accessors
// for the v4 e2e harness. Build tag isolates from v3 integration tests.
package helpers

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// envLoaded gates LoadEnv so multiple callers in the same process load once.
var envLoaded sync.Once

// LoadEnv reads env.e2e-tests/.env from the repo root and copies entries
// into os.Environ(). Existing env vars are NOT overwritten — caller env wins.
// Walks up from CWD looking for the env file (tests run inside subpackages).
func LoadEnv() error {
	var err error
	envLoaded.Do(func() {
		path, locateErr := locateEnvFile("env.e2e-tests/.env")
		if locateErr != nil {
			err = locateErr
			return
		}
		err = applyEnvFile(path)
	})
	return err
}

// MustLoadEnv panics on env load failure — convenient for test setup.
func MustLoadEnv() {
	if err := LoadEnv(); err != nil {
		panic(fmt.Sprintf("e2e: load env: %v", err))
	}
}

// locateEnvFile walks parent dirs until it finds rel or hits filesystem root.
func locateEnvFile(rel string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	dir := cwd
	for {
		candidate := filepath.Join(dir, rel)
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("e2e env file not found: walked from %s up looking for %s", cwd, rel)
		}
		dir = parent
	}
}

// applyEnvFile parses KEY=VALUE lines and Setenv-s any key not already set.
// Supports: comments (#), blank lines, quoted values ("..."), unquoted values.
func applyEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if len(val) >= 2 && (val[0] == '"' && val[len(val)-1] == '"') {
			val = val[1 : len(val)-1]
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, val)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", path, err)
	}
	return nil
}

// Typed accessors — these panic if missing, since e2e cannot proceed without them.

func DatabaseURL() string  { return mustEnv("GOCLAW_DATABASE_URL") }
func GatewayPort() string  { return envOr("GOCLAW_PORT", "18790") }
func GatewayHost() string  { return envOr("GOCLAW_HOST", "127.0.0.1") }
func MigrationsDir() string { return envOr("GOCLAW_MIGRATIONS_DIR", "./migrations") }

func EncryptionKey() string { return mustEnv("GOCLAW_ENCRYPTION_KEY") }
func JWTSecret() string     { return mustEnv("GOCLAW_JWT_SECRET") }

func RootEmail() string       { return mustEnv("E2E_ROOT_EMAIL") }
func RootPassword() string    { return mustEnv("E2E_ROOT_PASSWORD") }
func RootDisplayName() string { return envOr("E2E_ROOT_DISPLAY_NAME", "E2E Root") }

func BailianKey() string    { return mustEnv("BAILIAN_API_KEY") }
func OpenRouterKey() string { return mustEnv("OPENROUTER_API_KEY") }

func TestPrefix() string { return envOr("E2E_TEST_PREFIX", "e2e") }

// GatewayBaseURL returns http://host:port for HTTP requests against the e2e gateway.
func GatewayBaseURL() string {
	return fmt.Sprintf("http://%s:%s", GatewayHost(), GatewayPort())
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("e2e: required env var %s is empty (load env.e2e-tests/.env first)", key))
	}
	return v
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
