package cmd

import (
	"context"
	"errors"
	"maps"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/store"
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

// failingScheduler builds a scheduler whose every run fails with err — the
// review-run-failure shape the never-silent fallback guards against.
func failingScheduler(err error) *scheduler.Scheduler {
	runFn := func(_ context.Context, _ agent.RunRequest) (*agent.RunResult, error) {
		return nil, err
	}
	return scheduler.NewScheduler(nil, scheduler.DefaultQueueConfig(), runFn)
}

// fakeSessionStore implements the store.SessionStore surface the review
// delivery paths touch (messages, saves, metadata, labels). Embedding the
// interface satisfies the rest — an unimplemented method panics, flagging an
// unexpected dependency instead of silently passing.
type fakeSessionStore struct {
	store.SessionStore
	mu       sync.Mutex
	messages map[string][]providers.Message
	meta     map[string]map[string]string
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{
		messages: map[string][]providers.Message{},
		meta:     map[string]map[string]string{},
	}
}

func (f *fakeSessionStore) Get(_ context.Context, _ string) *store.SessionData { return nil }

func (f *fakeSessionStore) GetLabel(_ context.Context, _ string) string { return "" }

func (f *fakeSessionStore) AddMessage(_ context.Context, key string, msg providers.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages[key] = append(f.messages[key], msg)
}

func (f *fakeSessionStore) Save(_ context.Context, _ string) error { return nil }

func (f *fakeSessionStore) GetSessionMetadata(_ context.Context, key string) map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	maps.Copy(out, f.meta[key])
	return out
}

func (f *fakeSessionStore) SetSessionMetadata(_ context.Context, key string, md map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.meta[key] == nil {
		f.meta[key] = map[string]string{}
	}
	maps.Copy(f.meta[key], md)
}

// history returns a copy of the messages persisted under key.
func (f *fakeSessionStore) history(key string) []providers.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]providers.Message(nil), f.messages[key]...)
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
// Without jobName/agentName metadata the prompt keeps its generic wording, and
// a SUCCESSFUL review turn never posts the fallback announce.
func TestScheduleCodeReviewDelivery(t *testing.T) {
	var captured agent.RunRequest
	sched := capturingScheduler("Roman finished the game — here's the recap.", &captured)
	msgBus := bus.New()
	sessStore := newFakeSessionStore()
	deps := &ConsumerDeps{Sched: sched, MsgBus: msgBus, SessStore: sessStore}

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
	// No names on the callback → the prompt's generic fallback wording.
	if !strings.Contains(captured.Message, "the specialist") || !strings.Contains(captured.Message, "the delegated task") {
		t.Errorf("expected generic prompt wording without jobName/agentName, got: %s", captured.Message)
	}
	// A successful review turn NEVER posts the fallback announce.
	if got := sessStore.history("agent:samantha:ws:direct:user-9"); len(got) != 0 {
		t.Errorf("no fallback message should be persisted on a successful review, got %d", len(got))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if out, ok := msgBus.SubscribeOutbound(ctx); !ok || !strings.Contains(out.Content, "Roman finished the game") {
		t.Errorf("expected the ops-lead reply outbound, got ok=%v out=%+v", ok, out)
	}
}

// TestScheduleCodeReviewDeliveryThreadsJobAndAgentNames proves the callback's
// optional jobName/agentName metadata (stamped by skillcallback_messages.go as
// job_name/agent_name) reaches the review prompt: "your task X to Y finished"
// instead of the generic wording.
func TestScheduleCodeReviewDeliveryThreadsJobAndAgentNames(t *testing.T) {
	var captured agent.RunRequest
	sched := capturingScheduler("Reviewed.", &captured)
	msgBus := bus.New()
	deps := &ConsumerDeps{Sched: sched, MsgBus: msgBus, SessStore: newFakeSessionStore()}

	msg := bus.InboundMessage{
		Channel:  "ws",
		PeerKind: "direct",
		ChatID:   "user-9",
		AgentID:  "samantha",
		TenantID: uuid.Nil,
		Content:  "the job result",
		Metadata: map[string]string{
			"source":              "code-skill-callback",
			"announce":            "true",
			bus.MetaCodeReview:    "true",
			"job_id":              "job-xyz",
			bus.MetaCodeJobName:   "Landing page build",
			bus.MetaCodeAgentName: "roman",
		},
	}

	scheduleCodeReviewDelivery(context.Background(), "agent:samantha:ws:direct:user-9", "the job result", msg, deps)
	deps.BgWg.Wait()

	for _, want := range []string{"Landing page build", "roman", "the job result"} {
		if !strings.Contains(captured.Message, want) {
			t.Errorf("review prompt missing %q\nprompt: %s", want, captured.Message)
		}
	}
	for _, generic := range []string{"the specialist", "the delegated task"} {
		if strings.Contains(captured.Message, generic) {
			t.Errorf("review prompt kept generic wording %q despite names\nprompt: %s", generic, captured.Message)
		}
	}

	// Drain the ops-lead reply so the bus is clean for other tests.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	msgBus.SubscribeOutbound(ctx)
}

// TestScheduleCodeReviewDeliveryFallbackOnReviewFailure proves the never-silent
// guarantee on the JOB lane: when the ops-lead review run fails, the raw result
// is delivered as a plain announce (persisted assistant turn + outbound)
// EXACTLY ONCE — a replayed callback for the same job re-runs the review but
// never double-posts the fallback (metaCodeReviewFallbackJobID guard).
func TestScheduleCodeReviewDeliveryFallbackOnReviewFailure(t *testing.T) {
	const origin = "agent:samantha:ws:direct:user-9"
	sched := failingScheduler(errors.New("upstream 400"))
	msgBus := bus.New()
	sessStore := newFakeSessionStore()
	deps := &ConsumerDeps{Sched: sched, MsgBus: msgBus, SessStore: sessStore}

	msg := bus.InboundMessage{
		Channel:  "ws",
		PeerKind: "direct",
		ChatID:   "user-9",
		AgentID:  "samantha",
		TenantID: uuid.Nil,
		Content:  "the job result",
		Metadata: map[string]string{
			"source":              "code-skill-callback",
			"announce":            "true",
			bus.MetaCodeReview:    "true",
			"job_id":              "job-xyz",
			bus.MetaCodeJobName:   "Landing page build",
			bus.MetaCodeAgentName: "roman",
		},
	}

	scheduleCodeReviewDelivery(context.Background(), origin, "the job result", msg, deps)
	deps.BgWg.Wait()

	// The fallback announce is persisted into the origin session with the
	// manager framing and the raw result.
	msgs := sessStore.history(origin)
	if len(msgs) != 1 {
		t.Fatalf("fallback messages persisted = %d, want exactly 1", len(msgs))
	}
	if msgs[0].Role != "assistant" || msgs[0].SenderID != "samantha" {
		t.Errorf("fallback message role/sender = %q/%q, want assistant/samantha", msgs[0].Role, msgs[0].SenderID)
	}
	for _, want := range []string{"roman finished: Landing page build", "the job result"} {
		if !strings.Contains(msgs[0].Content, want) {
			t.Errorf("fallback message missing %q\ncontent: %s", want, msgs[0].Content)
		}
	}
	// ...and pushed to the origin channel.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if out, ok := msgBus.SubscribeOutbound(ctx); !ok || !strings.Contains(out.Content, "the job result") {
		t.Errorf("expected the fallback announce outbound, got ok=%v out=%+v", ok, out)
	}
	// The per-job guard is stamped on the origin session.
	if got := sessStore.GetSessionMetadata(context.Background(), origin)[metaCodeReviewFallbackJobID]; got != "job-xyz" {
		t.Errorf("fallback guard stamp = %q, want job-xyz", got)
	}

	// Replayed callback for the SAME job: the review fails again, but the
	// fallback must NOT be double-posted.
	scheduleCodeReviewDelivery(context.Background(), origin, "the job result", msg, deps)
	deps.BgWg.Wait()
	if got := len(sessStore.history(origin)); got != 1 {
		t.Fatalf("fallback messages after replay = %d, want still exactly 1", got)
	}
	noCtx, noCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer noCancel()
	if out, ok := msgBus.SubscribeOutbound(noCtx); ok {
		t.Errorf("no second fallback outbound expected on replay, got %+v", out)
	}
}
