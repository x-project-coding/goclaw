//go:build e2e

package helpers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// APIClient is a thin wrapper over net/http for e2e tests against the gateway.
// Auto-injects bearer token when SetToken has been called. Uses a per-instance
// http.Client with a 30s default timeout (overridable via WithTimeout).
type APIClient struct {
	BaseURL string
	hc      *http.Client
	token   string
}

// NewAPIClient builds a client pointing at GatewayBaseURL().
// Loads env on first call so tests can construct it lazily.
func NewAPIClient() *APIClient {
	MustLoadEnv()
	return &APIClient{
		BaseURL: GatewayBaseURL(),
		hc:      &http.Client{Timeout: 30 * time.Second},
	}
}

// SetToken stores the bearer token used in subsequent calls.
// Empty string clears the token (useful for unauth flows).
func (c *APIClient) SetToken(t string) { c.token = t }

// WithTimeout overrides the default 30s HTTP client timeout.
func (c *APIClient) WithTimeout(d time.Duration) *APIClient {
	c.hc.Timeout = d
	return c
}

// APIResponse bundles status + body so tests can assert without re-reading.
type APIResponse struct {
	Status int
	Header http.Header
	Body   []byte
}

// JSON unmarshals the response body into out. Returns the marshal error.
func (r *APIResponse) JSON(out any) error {
	if len(r.Body) == 0 {
		return fmt.Errorf("e2e: empty response body")
	}
	return json.Unmarshal(r.Body, out)
}

// Do executes a request relative to BaseURL. Body, when non-nil, is JSON-encoded.
// Path may be a fully-qualified URL OR a /v1/... relative path.
func (c *APIClient) Do(ctx context.Context, method, path string, body any) (*APIResponse, error) {
	url := c.resolveURL(path)
	var bodyReader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, url, err)
	}
	defer resp.Body.Close()
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return &APIResponse{Status: resp.StatusCode, Header: resp.Header, Body: buf}, nil
}

// GET / POST / PATCH / DELETE convenience wrappers.
func (c *APIClient) GET(ctx context.Context, path string) (*APIResponse, error) {
	return c.Do(ctx, http.MethodGet, path, nil)
}
func (c *APIClient) POST(ctx context.Context, path string, body any) (*APIResponse, error) {
	return c.Do(ctx, http.MethodPost, path, body)
}
func (c *APIClient) PATCH(ctx context.Context, path string, body any) (*APIResponse, error) {
	return c.Do(ctx, http.MethodPatch, path, body)
}
func (c *APIClient) DELETE(ctx context.Context, path string) (*APIResponse, error) {
	return c.Do(ctx, http.MethodDelete, path, nil)
}

func (c *APIClient) resolveURL(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return c.BaseURL + path
}
