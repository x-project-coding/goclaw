package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
)

// TestIsCodeReviewCallback covers the Layer C gate: a code-skill-callback whose
// Metadata carries review=true routes to the ops-lead review run; anything else
// (including a normal announce) keeps the passive path.
func TestIsCodeReviewCallback(t *testing.T) {
	cases := []struct {
		name string
		meta map[string]string
		want bool
	}{
		{"review true → review path", map[string]string{bus.MetaCodeReview: "true"}, true},
		{"review absent → passive announce", map[string]string{"source": "code-skill-callback", "announce": "true"}, false},
		{"review false → passive announce", map[string]string{bus.MetaCodeReview: "false"}, false},
		{"nil metadata → passive announce", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isCodeReviewCallback(tc.meta); got != tc.want {
				t.Fatalf("isCodeReviewCallback(%v) = %v, want %v", tc.meta, got, tc.want)
			}
		})
	}
}

// TestOriginUserIDForReview covers the review-run user scoping: for a ws:direct
// origin (agent:<key>:ws:direct:<userId>) the chatID IS the user id; an explicit
// UserID wins; other shapes yield empty.
func TestOriginUserIDForReview(t *testing.T) {
	cases := []struct {
		name string
		msg  bus.InboundMessage
		want string
	}{
		{"ws direct → chatID", bus.InboundMessage{Channel: "ws", PeerKind: "direct", ChatID: "user-1"}, "user-1"},
		{"explicit UserID wins", bus.InboundMessage{Channel: "ws", PeerKind: "direct", ChatID: "user-1", UserID: "u-explicit"}, "u-explicit"},
		{"telegram direct → empty", bus.InboundMessage{Channel: "telegram", PeerKind: "direct", ChatID: "123"}, ""},
		{"ws group → empty", bus.InboundMessage{Channel: "ws", PeerKind: "group", ChatID: "room"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := originUserIDForReview(tc.msg); got != tc.want {
				t.Fatalf("originUserIDForReview = %q, want %q", got, tc.want)
			}
		})
	}
}

// capturingScheduler builds a scheduler whose runFn records the RunRequest and
// returns the given reply, so tests can assert what was scheduled.
func capturingScheduler(reply string, captured *agent.RunRequest) *scheduler.Scheduler {
	runFn := func(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		*captured = req
		return &agent.RunResult{Content: reply}, nil
	}
	return scheduler.NewScheduler(nil, scheduler.DefaultQueueConfig(), runFn)
}

// TestScheduleOpsLeadReviewRun proves the SHARED helper (used by both the
// delegate-SESSION path and the delegate-JOB path) schedules a HideInput
// "announce" review run into the origin session and pushes the ops-lead's reply
// outbound.
func TestScheduleOpsLeadReviewRun(t *testing.T) {
	var captured agent.RunRequest
	sched := capturingScheduler("Reviewed: it's good — shipping.", &captured)
	msgBus := bus.New()

	err := scheduleOpsLeadReviewRun(context.Background(), sched, msgBus, opsLeadReviewInput{
		OriginSessionKey: "agent:samantha:ws:direct:user-1",
		OriginUserID:     "user-1",
		Label:            "Game build",
		TargetAgent:      "roman",
		Result:           "the raw job result",
		RunIDSeed:        "job-abc",
	})
	if err != nil {
		t.Fatalf("scheduleOpsLeadReviewRun returned error: %v", err)
	}

	// Scheduled as a hidden review turn into the ORIGIN session.
	if captured.SessionKey != "agent:samantha:ws:direct:user-1" {
		t.Errorf("SessionKey = %q, want the origin key", captured.SessionKey)
	}
	if captured.RunKind != "announce" || !captured.HideInput {
		t.Errorf("RunKind/HideInput = %q/%v, want announce/true", captured.RunKind, captured.HideInput)
	}
	if captured.Channel != "ws" || captured.ChatID != "user-1" || captured.UserID != "user-1" {
		t.Errorf("routing = %q/%q/%q, want ws/user-1/user-1", captured.Channel, captured.ChatID, captured.UserID)
	}
	for _, want := range []string{"Game build", "roman", "the raw job result"} {
		if !strings.Contains(captured.Message, want) {
			t.Errorf("review prompt missing %q\nprompt: %s", want, captured.Message)
		}
	}

	// The ops-lead's user-facing reply is pushed to the origin channel.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, ok := msgBus.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("expected an outbound message with the ops-lead reply")
	}
	if out.Channel != "ws" || out.ChatID != "user-1" || out.Content != "Reviewed: it's good — shipping." {
		t.Errorf("outbound = %+v, want ws/user-1 + the reviewed reply", out)
	}
}

// TestScheduleCodeReviewDelivery proves the Layer C TRIGGER (a review:true code
// callback) schedules the ops-lead review run via the shared helper — the
// distinguishing behavior vs the passive announce (which would AddMessage).
func TestScheduleCodeReviewDelivery(t *testing.T) {
	var captured agent.RunRequest
	sched := capturingScheduler("Roman finished the game — here's the recap.", &captured)
	msgBus := bus.New()
	deps := &ConsumerDeps{Sched: sched, MsgBus: msgBus}

	msg := bus.InboundMessage{
		Channel:  "ws",
		PeerKind: "direct",
		ChatID:   "user-9",
		AgentID:  "samantha",
		TenantID: uuid.Nil,
		Content:  "the job result",
		Metadata: map[string]string{
			"source":           "code-skill-callback",
			"announce":         "true",
			bus.MetaCodeReview: "true",
			"job_id":           "job-xyz",
		},
	}

	scheduleCodeReviewDelivery(context.Background(), "agent:samantha:ws:direct:user-9", "the job result", msg, deps)
	deps.BgWg.Wait()

	if captured.RunKind != "announce" || !captured.HideInput {
		t.Errorf("expected a scheduled hidden review run, got RunKind=%q HideInput=%v", captured.RunKind, captured.HideInput)
	}
	if captured.SessionKey != "agent:samantha:ws:direct:user-9" {
		t.Errorf("SessionKey = %q, want the origin key", captured.SessionKey)
	}
	if captured.UserID != "user-9" {
		t.Errorf("UserID = %q, want user-9 (derived from the ws:direct chatID)", captured.UserID)
	}
	if !strings.Contains(captured.Message, "the job result") {
		t.Errorf("review prompt missing the job result: %s", captured.Message)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if out, ok := msgBus.SubscribeOutbound(ctx); !ok || !strings.Contains(out.Content, "Roman finished the game") {
		t.Errorf("expected the ops-lead reply outbound, got ok=%v out=%+v", ok, out)
	}
}
