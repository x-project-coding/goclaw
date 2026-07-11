// Package reload polls x-api for a fresh skill-service catalog and hot-swaps it
// into internal/skillcatalog at runtime. It lives outside the stdlib-only
// skillcatalog package (it uses log/slog and is compiled only into the gateway)
// so the `skill` CLI stays a small static binary.
//
// Contract with x-api (GET {BaseURL}/api/skill-services/catalog):
//
//	200 → {"version":"<sha256 hex>","operations":[ ...Operation JSON... ]}
//	      with ETag: "<version>". We send If-None-Match: <last version> so an
//	      unchanged catalog returns:
//	304 → keep the current snapshot.
//
// On a valid 200 the loader calls skillcatalog.Load (which validates + atomically
// swaps) and best-effort persists the raw operations JSON to the on-disk catalog
// file so the static `skill` CLI (a separate process) picks up the same set. Any
// error — network, timeout, bad JSON, non-2xx — keeps the current snapshot and
// logs; the loader never crashes the gateway. The embedded catalog is always the
// floor.
package reload

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/skillcatalog"
)

const (
	// defaultInterval is how often the catalog is re-polled after boot.
	defaultInterval = 10 * time.Minute
	// fetchTimeout caps a single catalog fetch.
	fetchTimeout = 10 * time.Second
	// maxCatalogBytes caps the response body (the embedded catalog is ~34 KiB).
	maxCatalogBytes = 4 << 20 // 4 MiB
	// catalogPathSuffix is the endpoint path under BaseURL.
	catalogPathSuffix = "/api/skill-services/catalog"
)

// Options configures the reloader. The zero value is valid: it polls the live
// x-api origin every 10 minutes and persists to skillcatalog.DefaultCatalogPath.
type Options struct {
	// URL is the full catalog endpoint. Empty → BaseURL() + catalogPathSuffix,
	// re-resolved per fetch so an X_API_BASE_URL change is honoured.
	URL string
	// FilePath is where the raw operations JSON is persisted for the `skill` CLI.
	// Empty → skillcatalog.DefaultCatalogPath. A blank path disables persistence.
	FilePath string
	// Interval overrides the poll interval. Zero → defaultInterval.
	Interval time.Duration
	// Client overrides the HTTP client. Nil → a client with a 10s timeout.
	Client *http.Client
}

type reloader struct {
	url      string // empty means resolve per fetch from BaseURL()
	filePath string
	client   *http.Client
}

func newReloader(opts Options) *reloader {
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: fetchTimeout}
	}
	filePath := opts.FilePath
	if filePath == "" && opts.URL == "" {
		// Only default the persistence path in real (non-test) wiring, i.e. when
		// the caller did not point us at a test server. A test that sets URL but
		// leaves FilePath empty gets no disk writes.
		filePath = skillcatalog.DefaultCatalogPath
	}
	return &reloader{url: opts.URL, filePath: filePath, client: client}
}

func (r *reloader) endpoint() string {
	if r.url != "" {
		return r.url
	}
	return skillcatalog.BaseURL() + catalogPathSuffix
}

// Start launches the reloader: an immediate (non-blocking) fetch on boot, then a
// poll every interval. It returns a stop func the caller defers to halt the
// poller and wait for the in-flight goroutine to exit on shutdown.
func Start(ctx context.Context, opts Options) (stop func()) {
	interval := opts.Interval
	if interval <= 0 {
		interval = defaultInterval
	}
	r := newReloader(opts)

	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Boot fetch first (non-blocking on the caller — this runs in the
		// goroutine), then poll on the ticker. The embedded floor covers the
		// window before the first fetch returns.
		r.fetchOnce(loopCtx)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-loopCtx.Done():
				return
			case <-t.C:
				r.fetchOnce(loopCtx)
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

// catalogResponse is the x-api endpoint envelope. Operations is kept raw so it
// can be handed to skillcatalog.Load (which expects a []Operation array) and
// persisted verbatim without a re-marshal round-trip.
type catalogResponse struct {
	Version    string          `json:"version"`
	Operations json.RawMessage `json:"operations"`
}

// fetchOnce performs a single conditional GET and, on a valid 200, swaps the
// catalog and persists it. Every failure path keeps the current snapshot.
func (r *reloader) fetchOnce(ctx context.Context) {
	endpoint := r.endpoint()
	prev := skillcatalog.Version()

	reqCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		slog.Warn("skillcatalog reload: build request failed", "url", endpoint, "error", err)
		return
	}
	if prev != "" {
		req.Header.Set("If-None-Match", prev)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		slog.Warn("skillcatalog reload: fetch failed", "url", endpoint, "error", err)
		return
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotModified:
		slog.Debug("skillcatalog reload: catalog unchanged (304)", "version", prev)
		return
	case http.StatusOK:
		// fall through
	default:
		slog.Warn("skillcatalog reload: unexpected status, keeping current", "status", resp.StatusCode, "url", endpoint, "version", prev)
		return
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogBytes))
	if err != nil {
		slog.Warn("skillcatalog reload: read body failed, keeping current", "url", endpoint, "error", err)
		return
	}

	var payload catalogResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		slog.Warn("skillcatalog reload: response envelope invalid, keeping current", "url", endpoint, "error", err)
		return
	}
	version := strings.TrimSpace(payload.Version)
	if version == "" {
		// Fall back to the ETag if the body omitted the version field.
		version = strings.Trim(strings.TrimSpace(resp.Header.Get("ETag")), `"`)
	}
	if version == "" {
		slog.Warn("skillcatalog reload: 200 without a version, keeping current", "url", endpoint)
		return
	}

	if err := skillcatalog.Load(payload.Operations, version); err != nil {
		slog.Warn("skillcatalog reload: catalog invalid, keeping current", "url", endpoint, "version", version, "error", err)
		return
	}
	slog.Info("skillcatalog reload: catalog updated", "old", prev, "new", version, "operations", len(skillcatalog.Catalog()))

	// Best-effort: persist the raw operations JSON so the static `skill` CLI
	// (a separate process) reads the same set. A write failure does not undo
	// the in-memory swap.
	if r.filePath != "" {
		if err := writeCatalogFile(r.filePath, payload.Operations); err != nil {
			slog.Warn("skillcatalog reload: persist to disk failed (in-memory catalog updated anyway)", "path", r.filePath, "error", err)
		}
	}
}

// writeCatalogFile atomically writes data to path (0644) via a temp file in the
// same directory + rename, so a concurrent CLI reader never sees a partial file.
func writeCatalogFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".skill-catalog-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Remove the temp file if we bail before the rename (no-op once renamed).
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
