package cmd

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// Delegate-result-delivery: when an ops-lead delegation (`system:delegate:*`)
// run completes, RE-INVOKE the Operations Lead into the originating ops-lead↔
// user chat with a HIDDEN review prompt carrying the specialist's result. The
// ops-lead reviews it and produces the user-facing message HERSELF (she may
// request fixes or advance the stage) — we never dump the specialist's raw text
// into the user's chat. This mirrors goclaw's existing subagent/team announce-
// back pattern (a `RunKind:"announce"` + `HideInput:true` run into the leader's
// own session; see processSubagentAnnounceLoop) — we use that primitive
// directly rather than wiring into the subagent announce QUEUE, which is coupled
// to the subagent-tool roster/batching model that does not apply here.
const (
	// delegateSessionPrefix gates the ENTIRE hook. It is LOAD-BEARING: only
	// `system:delegate:*` runs take this path, so the hot normal-chat completion
	// path (and the re-invoked ops-lead run itself, whose key is
	// `agent:…:ws:direct:…`) are completely unaffected.
	delegateSessionPrefix = "system:delegate:"

	// Delegate-session metadata keys. x-api stamps origin*/targetAgent/goal at
	// delegate time; goclaw stamps resultDelivered* on delivery. Names MUST match
	// the x-api manage-operations delegate side byte-for-byte.
	dmMetaOriginSessionKey     = "originSessionKey"
	dmMetaOriginUserID         = "originUserId"
	dmMetaTargetAgent          = "targetAgent"
	dmMetaGoal                 = "goal"
	dmMetaResultDeliveredAt    = "resultDeliveredAt"
	dmMetaResultDeliveredRunID = "resultDeliveredRunID"
)

// wireDelegateResultDeliverySubscriber subscribes to agent run-completion events
// and, for `system:delegate:*` sessions only, delivers the specialist's result
// to the ops-lead for review. It is wired alongside wireChannelStreamingSubscriber
// but under its OWN unique subscriber id (bus.TopicDelegateResultDelivery) —
// Broadcast() fans every event to every subscriber keyed by id, so reusing the
// streaming subscriber's id would silently overwrite it.
func (d *gatewayDeps) wireDelegateResultDeliverySubscriber(sched *scheduler.Scheduler) {
	d.msgBus.Subscribe(bus.TopicDelegateResultDelivery, func(event bus.Event) {
		if event.Name != protocol.EventAgent {
			return
		}
		agentEvent, ok := event.Payload.(agent.AgentEvent)
		if !ok || agentEvent.Type != protocol.AgentEventRunCompleted {
			return
		}
		// Resolve the completed run's session key. Prefer the active-run registry
		// (matches the existing terminal-event handling) and fall back to the
		// key carried on the event itself (immune to an early UnregisterRun).
		sessionKey := d.agentRouter.SessionKeyForRun(agentEvent.RunID)
		if sessionKey == "" {
			sessionKey = agentEvent.SessionKey
		}
		// LOAD-BEARING gate: everything below is delegate-only.
		if !strings.HasPrefix(sessionKey, delegateSessionPrefix) {
			return
		}
		// This handler runs synchronously on the emitting (agent-run) goroutine
		// while Broadcast holds a read lock — so offload ALL I/O (metadata load,
		// the ops-lead re-invocation which is a full LLM turn, the x-api POST)
		// to a goroutine. Never block the bus or the finishing run.
		content := runCompletedContent(agentEvent.Payload)
		go d.deliverDelegateResult(agentEvent.TenantID, sessionKey, agentEvent.RunID, content, sched)
	})
}

// delegateDeliveryDecision is the pure eligibility verdict for a completed run.
type delegateDeliveryDecision struct {
	Deliver          bool
	OriginSessionKey string
	Reason           string
}

// evaluateDelegateDelivery decides whether a completed run's result should be
// delivered to the ops-lead for review. Pure (no I/O) so the gate + idempotency
// + skip rules are unit-testable. Rules, in order:
//   - not a `system:delegate:*` session      → skip (load-bearing gate)
//   - no originSessionKey in metadata         → skip (daily backstop reports it)
//   - resultDeliveredRunID == this run's id   → skip (per-run idempotency; a
//     managed-sessions/send FOLLOW-UP is a new run id, so it still delivers)
//   - empty / NO_REPLY final content          → skip (nothing to review)
func evaluateDelegateDelivery(sessionKey string, meta map[string]string, runID, content string) delegateDeliveryDecision {
	if !strings.HasPrefix(sessionKey, delegateSessionPrefix) {
		return delegateDeliveryDecision{Reason: "not-delegate"}
	}
	origin := meta[dmMetaOriginSessionKey]
	if origin == "" {
		return delegateDeliveryDecision{Reason: "no-origin"}
	}
	if runID != "" && meta[dmMetaResultDeliveredRunID] == runID {
		return delegateDeliveryDecision{OriginSessionKey: origin, Reason: "already-delivered"}
	}
	if strings.TrimSpace(content) == "" || agent.IsSilentReply(content) {
		return delegateDeliveryDecision{OriginSessionKey: origin, Reason: "empty-or-silent"}
	}
	return delegateDeliveryDecision{Deliver: true, OriginSessionKey: origin, Reason: "ok"}
}

// deliverDelegateResult loads the delegate session's routing metadata and, when
// the result is worth reporting and not already delivered for THIS run, schedules
// the ops-lead review turn and notifies x-api. Best-effort throughout: it logs
// and returns rather than crashing the run on any failure.
func (d *gatewayDeps) deliverDelegateResult(
	tenantID uuid.UUID,
	delegateSessionKey, runID, content string,
	sched *scheduler.Scheduler,
) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("delegate delivery: panic recovered", "session", delegateSessionKey, "panic", fmt.Sprint(r))
		}
	}()

	sessions := d.pgStores.Sessions
	if sessions == nil || sched == nil {
		return
	}
	ctx := context.Background()
	if tenantID != uuid.Nil {
		ctx = store.WithTenantID(ctx, tenantID)
	}

	meta := sessions.GetSessionMetadata(ctx, delegateSessionKey)
	decision := evaluateDelegateDelivery(delegateSessionKey, meta, runID, content)
	if !decision.Deliver {
		return
	}
	originSessionKey := decision.OriginSessionKey

	label := sessions.GetLabel(ctx, delegateSessionKey)
	if label == "" {
		label = meta[dmMetaGoal]
	}

	// Optimistic per-run guard BEFORE scheduling: prevents a duplicate completion
	// event (or re-entrancy) from double-scheduling the review turn. The
	// resultDeliveredAt timestamp the daily workflow reads is stamped only AFTER
	// a successful review turn, so a failed turn is still reported by the backstop.
	stampDelegateMeta(ctx, sessions, delegateSessionKey, map[string]string{dmMetaResultDeliveredRunID: runID})

	channel, peerKind, chatID := parseSessionRouting(originSessionKey)
	req := agent.RunRequest{
		SessionKey: originSessionKey,
		Message:    buildDelegateReviewPrompt(label, meta[dmMetaTargetAgent], content),
		Channel:    channel,
		ChatID:     chatID,
		PeerKind:   peerKind,
		UserID:     meta[dmMetaOriginUserID],
		RunID:      fmt.Sprintf("delegate-review-%s-%d", shortID(runID), time.Now().UnixNano()),
		RunKind:    "announce",
		HideInput:  true, // the review trigger is hidden from the user
		Stream:     false,
	}

	outcome := <-sched.Schedule(ctx, scheduler.LaneSubagent, req)
	if outcome.Err != nil {
		slog.Error("delegate delivery: ops-lead review run failed",
			"origin", originSessionKey, "delegate", delegateSessionKey, "error", outcome.Err)
		// Leave resultDeliveredAt UNSET → the daily backstop still reports it.
		return
	}

	// Push the ops-lead's user-facing turn to the origin channel. Web (ws) reads
	// the persisted transcript + the x-api realtime refresh below; external
	// channels (Telegram/etc.) need this outbound. Mirrors processSubagentAnnounceLoop.
	if outcome.Result != nil {
		out := outcome.Result.Content
		if agent.IsSilentReply(out) {
			out = ""
		}
		if out != "" || len(outcome.Result.Media) > 0 {
			outMsg := bus.OutboundMessage{Channel: channel, ChatID: chatID, Content: out}
			appendMediaToOutbound(&outMsg, outcome.Result.Media)
			d.msgBus.PublishOutbound(outMsg)
		}
	}

	// Delivery done — stamp the timestamp the daily "Managed work check-in"
	// workflow reads so it never re-reports this run's result.
	stampDelegateMeta(ctx, sessions, delegateSessionKey, map[string]string{
		dmMetaResultDeliveredAt: time.Now().UTC().Format(time.RFC3339),
	})

	// Best-effort: tell x-api to bust + rewarm the ops-lead↔user chat history
	// cache and push a render-neutral realtime refresh, so the ops-lead's new
	// turn appears live in x-ui without the user reloading.
	notifyXAPIDelegateCompleted(delegateSessionKey, originSessionKey, meta[dmMetaOriginUserID])
}

// stampDelegateMeta merges keys into the delegate session metadata and persists.
// SetSessionMetadata is a merge (maps.Copy), so this never clobbers managedBy /
// originSessionKey / targetAgent etc.
func stampDelegateMeta(ctx context.Context, sessions store.SessionStore, key string, kv map[string]string) {
	sessions.SetSessionMetadata(ctx, key, kv)
	if err := sessions.Save(ctx, key); err != nil {
		slog.Warn("delegate delivery: session metadata save failed", "session", key, "error", err)
	}
}

// buildDelegateReviewPrompt is the HIDDEN input for the ops-lead review turn.
// It hands the specialist's final content to the ops-lead and asks her to decide
// what the user sees — keeping the single-interface principle (everything happens
// in this chat with her).
func buildDelegateReviewPrompt(label, targetAgent, content string) string {
	who := targetAgent
	if who == "" {
		who = "the specialist"
	}
	task := label
	if task == "" {
		task = "the delegated task"
	}
	return fmt.Sprintf(
		"Your delegated task %q to %s just finished. Here is their result:\n\n%s\n\n"+
			"Review it now. If it's good, report it to the user here in your own words — a brief, clear update. "+
			"If it needs changes, note exactly what to fix and relay that back to %s via managed-sessions/send. "+
			"Then decide the next step. Do not send the user to another chat or the app — everything happens here with you.",
		task, who, content, who,
	)
}

// parseSessionRouting splits agent:<key>:<channel>:<peerKind>:<chatID>[:…] into
// its routing parts. Returns empty strings for any non-canonical key.
func parseSessionRouting(sessionKey string) (channel, peerKind, chatID string) {
	parts := strings.Split(sessionKey, ":")
	if len(parts) < 5 || parts[0] != "agent" {
		return "", "", ""
	}
	return parts[2], parts[3], strings.Join(parts[4:], ":")
}

// runCompletedContent pulls the final assistant text out of a run.completed
// payload (map[string]any{"content": …} — see loop_run.go).
func runCompletedContent(payload any) string {
	m, ok := payload.(map[string]any)
	if !ok {
		return ""
	}
	c, _ := m["content"].(string)
	return c
}

func shortID(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// notifyXAPIDelegateCompleted POSTs the internal delegate-completed notify so
// x-api busts + rewarms the ops-lead↔user chat history cache and emits a
// render-neutral realtime refresh. Signed with the SHARED SKILL_RUNTIME_TOKEN
// (goclaw already holds it; it does NOT hold CODE_RUNNER_INTERNAL_SECRET), which
// x-api verifies on POST /internal/workspaces/delegate/completed. Best-effort.
func notifyXAPIDelegateCompleted(delegateSessionKey, originSessionKey, originUserID string) {
	base := strings.TrimRight(os.Getenv("X_API_BASE_URL"), "/")
	secret := os.Getenv("SKILL_RUNTIME_TOKEN")
	if base == "" || secret == "" {
		slog.Warn("delegate delivery: x-api notify skipped (X_API_BASE_URL or SKILL_RUNTIME_TOKEN unset)")
		return
	}
	// workspaceId = the <workspaceId> segment of
	// system:delegate:<workspaceId>:<shortid> (x-api's own workspace id).
	workspaceID := ""
	if parts := strings.Split(delegateSessionKey, ":"); len(parts) >= 3 {
		workspaceID = parts[2]
	}
	payload := map[string]string{
		"workspaceId":        workspaceID,
		"sessionKey":         originSessionKey,
		"delegateSessionKey": delegateSessionKey,
	}
	if originUserID != "" {
		payload["userId"] = originUserID
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequest(http.MethodPost, base+"/internal/workspaces/delegate/completed", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-internal-signature", sig)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("delegate delivery: x-api notify failed", "delegate", delegateSessionKey, "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		slog.Warn("delegate delivery: x-api notify non-2xx", "delegate", delegateSessionKey, "status", resp.StatusCode)
		return
	}
	slog.Info("delegate delivery: delivered to ops-lead + x-api notified",
		"delegate", delegateSessionKey, "origin", originSessionKey)
}
