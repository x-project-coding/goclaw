package http

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
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
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
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

	// Canonical session key: agent:{agentID}:{channel}:{peerKind}:{chatID}
	agentID, rest := sessions.ParseSessionKey(req.SessionKey)
	parts := strings.SplitN(rest, ":", 3)
	if agentID == "" || len(parts) < 3 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported sessionKey"})
		return
	}
	channel, peerKind, chatID := parts[0], parts[1], parts[2]

	content := strings.TrimSpace(strings.TrimSpace(req.Title) + "\n\n" + strings.TrimSpace(req.Summary))
	if content == "" {
		content = "A background code job finished."
	}
	if req.JobID != "" {
		content += "\n\n(code job " + req.JobID + ")"
	}

	h.msgBus.PublishInbound(bus.InboundMessage{
		Channel:  channel,
		SenderID: "code-runner",
		ChatID:   chatID,
		Content:  content,
		AgentID:  agentID,
		PeerKind: peerKind,
		TenantID: keyData.TenantID,
		Metadata: map[string]string{"source": "code-skill-callback", "job_id": req.JobID},
	})

	slog.Info("skillcallback.messages delivered to session",
		"agent_id", agentID, "channel", channel, "job_id", req.JobID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "delivered"})
}
