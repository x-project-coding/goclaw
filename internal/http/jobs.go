package http

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// JobsHandler serves the user-facing /v1/jobs* routes: the chat UI uses them to
// list a workspace's code-runner jobs and to answer a job that paused on a
// question. goclaw is a thin authenticated proxy in front of code-runner — it
// authenticates the chat user, mints a workspace skill key for the tenant, and
// relays the request. The skill-callback channel (code-runner -> goclaw) is
// unchanged; this is the reverse, user-initiated direction.
type JobsHandler struct {
	codeRunnerURL string
	client        *http.Client
}

// NewJobsHandler creates the jobs proxy handler. codeRunnerURL is the base URL
// of the code-runner service (e.g. https://code.42bucks.com).
func NewJobsHandler(codeRunnerURL string) *JobsHandler {
	return &JobsHandler{
		codeRunnerURL: strings.TrimRight(codeRunnerURL, "/"),
		client:        &http.Client{Timeout: 15 * time.Second},
	}
}

// RegisterRoutes registers the /v1/jobs* routes on the mux.
func (h *JobsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/jobs", h.auth(h.handleList))
	mux.HandleFunc("POST /v1/jobs/{id}/answer", h.auth(h.handleAnswer))
}

func (h *JobsHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

// handleList proxies GET /v1/jobs — the caller workspace's recent jobs.
func (h *JobsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	h.proxy(w, r, http.MethodGet, "/v1/jobs", nil)
}

// handleAnswer proxies POST /v1/jobs/{id}/answer — the user's answer to a job
// that paused on a question. The body is relayed verbatim; code-runner
// validates it.
func (h *JobsHandler) handleAnswer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(extractLocale(r), i18n.MsgInvalidJSON))
		return
	}
	h.proxy(w, r, http.MethodPost, "/v1/jobs/"+url.PathEscape(id)+"/answer", body)
}

// proxy authenticates the chat user, mints a workspace skill key for their
// tenant, and relays the request to code-runner — streaming the response back.
func (h *JobsHandler) proxy(w http.ResponseWriter, r *http.Request, method, path string, body []byte) {
	tenantID := store.TenantIDFromContext(r.Context())
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	key := tools.WorkspaceSkillToken(r.Context(), tenantID)
	if key == "" {
		writeJSON(w, http.StatusServiceUnavailable,
			map[string]string{"error": "workspace key unavailable"})
		return
	}

	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(r.Context(), method, h.codeRunnerURL+path, rdr)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError,
			map[string]string{"error": "request build failed"})
		return
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := h.client.Do(req)
	if err != nil {
		slog.Warn("jobs proxy: code-runner call failed", "method", method, "path", path, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "code-runner unavailable"})
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 1<<20))
}
