package http

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// messagesRequest is the body POSTed by a skill-backing service to deliver an
// async job result back into the originating chat session. It matches the
// code-runner CallbackPayload (callback/client.ts).
type messagesRequest struct {
	SessionKey string `json:"sessionKey"`
	Role       string `json:"role"`
	Title      string `json:"title"`
	Summary    string `json:"summary"`
	JobID      string `json:"jobId"`
	// Announce: when true, the summary is posted DIRECTLY into the session as an
	// assistant message (no LLM relay turn) — see handleCodeAnnounce. The agent
	// already wrote a user-facing summary, so re-running an agent turn to relay
	// it is wasteful and unreliable. When false (default) the legacy path runs:
	// the message is delivered as an inbound and the agent relays it.
	Announce bool `json:"announce"`
	// AgentName is code-runner's `callbackNameFields().agentName`: the job row's
	// launching_agent_id (the X-Goclaw-Agent-Id the job was created with — the
	// key its sandbox workspace is resolved under) when present, else the
	// requested employee label. Used only to decide whether the job ran as
	// this session's own agent (see jobOutputDirForCompletion).
	AgentName string `json:"agentName"`
	// LaunchingAgentID / LaunchingUserID: the exact X-Goclaw-Agent-Id /
	// X-Goclaw-User-Id identity the job was created with. Optional and
	// forward-compatible (code-runner does not send them yet); when both are
	// present they pin the job's workspace dir exactly (delegates run under a
	// synthetic user id the session cannot infer). Absent → the in-chat case is
	// inferred from the callback session (its agent + persisted user id).
	LaunchingAgentID string `json:"launchingAgentId"`
	LaunchingUserID  string `json:"launchingUserId"`
}

// handleMessages receives an async result from a skill-backing service (the
// code-runner behind the `code` skill) and delivers it into the originating
// chat session as an inbound message, so the agent relays it to the user.
// This closes the skill's request → async-job → result feedback loop.
//
// Auth mirrors verify-key: a genuine workspace API key (Bearer). The result is
// published to the inbound bus, scoped to the calling key's tenant.
func (h *SkillCallbackHandler) handleMessages(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)

	token := extractBearerToken(r)
	keyData, role := ResolveAPIKey(r.Context(), token)
	if keyData == nil || role == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": i18n.T(locale, i18n.MsgUnauthorized),
		})
		return
	}

	var req messagesRequest
	if r.Body != nil {
		// Cap the body — a result callback is small (a title + summary); the limit
		// just bounds memory for a misbehaving/replayed authenticated caller and
		// keeps this handler consistent with handleSpend (matches the package convention).
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
		if err := dec.Decode(&req); err != nil && err != io.EOF {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": i18n.T(locale, i18n.MsgInvalidJSON),
			})
			return
		}
	}
	if req.SessionKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sessionKey is required"})
		return
	}
	if h.msgBus == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "message bus unavailable"})
		return
	}
	if h.agents == nil {
		// Fail closed: without the agent store we cannot authorize the target
		// session against the caller, so we must not deliver.
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "agent store unavailable"})
		return
	}

	// Canonical session key: agent:{agentID}:{channel}:{peerKind}:{chatID}
	agentID, rest := sessions.ParseSessionKey(req.SessionKey)
	parts := strings.SplitN(rest, ":", 3)
	if agentID == "" || len(parts) < 3 {
		// A malformed key here usually means the caller's shell never had
		// GOCLAW_SESSION_KEY set (e.g. an unexpanded "$GOCLAW_SESSION_KEY").
		// Log it loudly so the result drop is diagnosable, not silent.
		slog.Warn("skillcallback.messages unsupported sessionKey",
			"session_key", req.SessionKey, "job_id", req.JobID)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported sessionKey"})
		return
	}
	channel, peerKind, chatID := parts[0], parts[1], parts[2]

	// Authorize the caller-supplied sessionKey against the calling key. The whole
	// session key (agentID/channel/chatID) comes from the request body, so without
	// this check any holder of a workspace key — e.g. the SKILL_RUNTIME_TOKEN every
	// skill exec receives — could forge a key for another agent in the tenant and
	// inject assistant messages / drive agent turns / poison job-result links in a
	// co-tenant's session. Tenant is already pinned to keyData.TenantID, so resolve
	// the parsed agent scoped to that tenant: GetByKey filters on tenant_id, so a
	// forged or cross-tenant agentID does not resolve and is rejected with 403.
	authCtx := store.WithTenantID(r.Context(), keyData.TenantID)
	if _, err := h.agents.GetByKey(authCtx, agentID); err != nil {
		slog.Warn("security.skillcallback_messages_agent_unauthorized",
			"agent_id", agentID, "tenant_id", keyData.TenantID.String(),
			"job_id", req.JobID, "error", err)
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": i18n.T(locale, i18n.MsgNoAccess, "agent"),
		})
		return
	}

	meta := map[string]string{"source": "code-skill-callback", "job_id": req.JobID}
	if req.Announce {
		// Stamp the ABSOLUTE directory the job's sandbox workspace was mounted
		// at (tenants/<tenant>/<agentKey>/<userID>, resolved exactly like
		// verify-key did at job start). handleCodeAnnounce persists it on the
		// session so the launching agent can read its own job's output directly
		// instead of hunting for it — the job's own "/workspace/…" report is a
		// sandbox-local path. Best-effort: unresolvable → no stamp.
		if dir := h.jobOutputDirForCompletion(authCtx, keyData.TenantID, agentID, req); dir != "" {
			meta[tools.MetaJobOutputPath] = dir
		}
	}
	var content string
	if req.Announce {
		// Direct-announce: the summary IS the user-facing message. Post it
		// verbatim (no "Code job completed" title, no "(code job <id>)" suffix);
		// handleCodeAnnounce persists it straight into the session.
		content = strings.TrimSpace(req.Summary)
		if content == "" {
			content = strings.TrimSpace(req.Title)
		}
		if content == "" {
			content = "A background job finished."
		}
		meta["announce"] = "true"
	} else {
		// Legacy relay path: the agent rephrases title+summary for the user.
		content = strings.TrimSpace(strings.TrimSpace(req.Title) + "\n\n" + strings.TrimSpace(req.Summary))
		if content == "" {
			content = "A background job finished."
		}
		if req.JobID != "" {
			content += "\n\n(job " + req.JobID + ")"
		}
	}

	h.msgBus.PublishInbound(bus.InboundMessage{
		Channel:  channel,
		SenderID: "code-runner",
		ChatID:   chatID,
		Content:  content,
		AgentID:  agentID,
		PeerKind: peerKind,
		TenantID: keyData.TenantID,
		Metadata: meta,
	})

	slog.Info("skillcallback.messages delivered to session",
		"agent_id", agentID, "channel", channel, "job_id", req.JobID, "announce", req.Announce)
	writeJSON(w, http.StatusOK, map[string]string{"status": "delivered"})
}

// jobOutputDirForCompletion resolves the container-local workspace directory a
// completed code-runner job actually wrote to, i.e. the same dir verify-key
// handed the runner as the sandbox bind-mount for the job's identity headers:
//
//	{workspaceBase}/tenants/{tenantSlug}/{agentKey}/{userID}
//
// Identity resolution, most to least explicit:
//   - LaunchingAgentID + LaunchingUserID both present → use them verbatim
//     (the agent key must resolve in the caller's tenant).
//   - otherwise, the job must have run as THIS session's agent (AgentName /
//     LaunchingAgentID absent or equal to the session agent — the in-chat
//     `jobs` skill sends its own GOCLAW_AGENT_ID / GOCLAW_USER_ID); its user
//     is the session's persisted user id. A job that ran as a DIFFERENT agent
//     (a session-lane delegate under a synthetic system:workflow:* user we
//     cannot infer) yields "" rather than a plausible-but-wrong path.
//
// Returns "" whenever any input is missing or the resolver fails.
func (h *SkillCallbackHandler) jobOutputDirForCompletion(ctx context.Context, tenantID uuid.UUID, sessionAgentID string, req messagesRequest) string {
	agentKey := strings.TrimSpace(req.LaunchingAgentID)
	userID := strings.TrimSpace(req.LaunchingUserID)
	if agentKey == "" || userID == "" {
		// Inference path: only when the job ran as the session's own agent.
		ranAs := agentKey
		if ranAs == "" {
			ranAs = strings.TrimSpace(req.AgentName)
		}
		if ranAs != "" && ranAs != sessionAgentID {
			return ""
		}
		if h.sessions == nil || req.SessionKey == "" {
			return ""
		}
		sd := h.sessions.Get(ctx, req.SessionKey)
		if sd == nil || strings.TrimSpace(sd.UserID) == "" {
			return ""
		}
		agentKey = sessionAgentID
		userID = strings.TrimSpace(sd.UserID)
	} else if agentKey != sessionAgentID {
		// Explicit identity naming another agent: it must exist in the caller's
		// tenant (GetByKey is tenant-scoped via ctx), else refuse to resolve.
		if h.agents == nil {
			return ""
		}
		if _, err := h.agents.GetByKey(ctx, agentKey); err != nil {
			return ""
		}
	}

	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	tenantSlug := ""
	if pkgTenantCache != nil {
		if tenant, err := pkgTenantCache.GetTenant(ctx, tenantID); err == nil && tenant != nil {
			tenantSlug = tenant.Slug
		}
	}
	return h.resolveWorkspaceDir(ctx, tenantID, tenantSlug, agentKey, userID)
}
