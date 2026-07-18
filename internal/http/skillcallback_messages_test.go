package http

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// stubCallbackAgentStore satisfies the callback handler's target-agent
// authorization; only GetByKey is exercised (embedding covers the rest — an
// unimplemented method panics, flagging an unexpected dependency).
type stubCallbackAgentStore struct{ store.AgentStore }

func (stubCallbackAgentStore) GetByKey(_ context.Context, agentKey string) (*store.AgentData, error) {
	return &store.AgentData{AgentKey: agentKey}, nil
}

// TestHandleMessagesOptionalJobAndAgentNames pins the /callback/v1/messages
// wire contract for the OPTIONAL jobName/agentName fields: when sent they are
// stamped into the inbound metadata (job_name/agent_name — read by the
// consumer's review-delivery path); when absent the metadata stays clean so
// the review prompt keeps its generic wording. Senders that predate the
// fields (the current x-code callback) are the "absent" case.
func TestHandleMessagesOptionalJobAndAgentNames(t *testing.T) {
	prevCache := pkgAPIKeyCache
	t.Cleanup(func() { pkgAPIKeyCache = prevCache })

	const token = "gcw_test_callback_key"
	keyStore := newMockAPIKeyStore()
	keyStore.keys[crypto.HashAPIKey(token)] = &store.APIKeyData{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		Scopes:   []string{"operator.write"},
	}
	pkgAPIKeyCache = newAPIKeyCache(keyStore, time.Minute)

	msgBus := bus.New()
	h := NewSkillCallbackHandler(nil, msgBus, stubCallbackAgentStore{})

	post := func(t *testing.T, body string) bus.InboundMessage {
		t.Helper()
		req := httptest.NewRequest("POST", "/callback/v1/messages", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.handleMessages(w, req)
		if w.Code != 200 {
			t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		msg, ok := msgBus.ConsumeInbound(ctx)
		if !ok {
			t.Fatal("no inbound message published")
		}
		return msg
	}

	t.Run("jobName and agentName stamped when sent", func(t *testing.T) {
		msg := post(t, `{"sessionKey":"agent:samantha:ws:direct:user-1","summary":"done",`+
			`"jobId":"job-1","announce":true,"review":true,`+
			`"jobName":"Landing page build","agentName":"roman"}`)
		if got := msg.Metadata[bus.MetaCodeJobName]; got != "Landing page build" {
			t.Errorf("metadata[%s] = %q, want the job name", bus.MetaCodeJobName, got)
		}
		if got := msg.Metadata[bus.MetaCodeAgentName]; got != "roman" {
			t.Errorf("metadata[%s] = %q, want the agent name", bus.MetaCodeAgentName, got)
		}
	})

	t.Run("absent fields leave metadata unstamped", func(t *testing.T) {
		msg := post(t, `{"sessionKey":"agent:samantha:ws:direct:user-1","summary":"done",`+
			`"jobId":"job-1","announce":true,"review":true}`)
		if _, ok := msg.Metadata[bus.MetaCodeJobName]; ok {
			t.Errorf("metadata[%s] stamped despite absent jobName", bus.MetaCodeJobName)
		}
		if _, ok := msg.Metadata[bus.MetaCodeAgentName]; ok {
			t.Errorf("metadata[%s] stamped despite absent agentName", bus.MetaCodeAgentName)
		}
	})

	t.Run("whitespace-only names are treated as absent", func(t *testing.T) {
		msg := post(t, `{"sessionKey":"agent:samantha:ws:direct:user-1","summary":"done",`+
			`"jobId":"job-1","announce":true,"review":true,"jobName":"  ","agentName":" "}`)
		if _, ok := msg.Metadata[bus.MetaCodeJobName]; ok {
			t.Errorf("metadata[%s] stamped despite whitespace-only jobName", bus.MetaCodeJobName)
		}
		if _, ok := msg.Metadata[bus.MetaCodeAgentName]; ok {
			t.Errorf("metadata[%s] stamped despite whitespace-only agentName", bus.MetaCodeAgentName)
		}
	})
}
