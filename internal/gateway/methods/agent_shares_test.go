package methods

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// shareCaptureStore records mutations against a single in-memory agent so the
// handler can be exercised without a DB. Methods unrelated to shares fall back
// to no-op defaults via embedded createCaptureStore.
type shareCaptureStore struct {
	createCaptureStore
	agent          *store.AgentData
	createdShares  []store.AgentShareInput
	revokedUserIDs []uuid.UUID
	revokedTeamIDs []uuid.UUID
	listed         []store.AgentShareData
}

func (s *shareCaptureStore) GetByID(_ context.Context, id uuid.UUID) (*store.AgentData, error) {
	if s.agent != nil && s.agent.ID == id {
		return s.agent, nil
	}
	return nil, nil
}

func (s *shareCaptureStore) GetByKey(_ context.Context, key string) (*store.AgentData, error) {
	if s.agent != nil && s.agent.AgentKey == key {
		return s.agent, nil
	}
	return nil, nil
}

func (s *shareCaptureStore) CreateShare(_ context.Context, in store.AgentShareInput) error {
	s.createdShares = append(s.createdShares, in)
	return nil
}

func (s *shareCaptureStore) RevokeShareByUser(_ context.Context, _, userID uuid.UUID) error {
	s.revokedUserIDs = append(s.revokedUserIDs, userID)
	return nil
}

func (s *shareCaptureStore) RevokeShareByTeam(_ context.Context, _, teamID uuid.UUID) error {
	s.revokedTeamIDs = append(s.revokedTeamIDs, teamID)
	return nil
}

func (s *shareCaptureStore) ListShares(_ context.Context, _ uuid.UUID) ([]store.AgentShareData, error) {
	return s.listed, nil
}

func newSharesMethods(stub store.AgentStore) *AgentSharesMethods {
	return NewAgentSharesMethods(stub, nil, &config.Config{})
}

func makeFrame(t *testing.T, method string, params map[string]any) *protocol.RequestFrame {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return &protocol.RequestFrame{ID: "req-1", Method: method, Params: raw}
}

// TestAgentShares_OwnerCanCreate exercises the happy path: caller's UserID
// matches agent.OwnerUserID, payload has exactly one of user/team, store
// receives a populated AgentShareInput with CreatedBy resolved from session.
func TestAgentShares_OwnerCanCreate(t *testing.T) {
	ownerID := uuid.New()
	agentID := uuid.New()
	stub := &shareCaptureStore{
		agent: &store.AgentData{
			BaseModel:   store.BaseModel{ID: agentID},
			AgentKey:    "ops-bot",
			OwnerUserID: &ownerID,
		},
	}
	m := newSharesMethods(stub)

	targetUser := uuid.New()
	client := gateway.NewTestClient(permissions.RoleMember, ownerID.String())
	req := makeFrame(t, protocol.MethodAgentsSharesCreate, map[string]any{
		"agentId":          "ops-bot",
		"sharedWithUserId": targetUser.String(),
		"role":             store.ShareRoleEditor,
	})

	m.handleCreate(context.Background(), client, req)

	if len(stub.createdShares) != 1 {
		t.Fatalf("createdShares = %d, want 1", len(stub.createdShares))
	}
	got := stub.createdShares[0]
	if got.AgentID != agentID {
		t.Errorf("AgentID = %v, want %v", got.AgentID, agentID)
	}
	if got.SharedWithUserID == nil || *got.SharedWithUserID != targetUser {
		t.Errorf("SharedWithUserID = %v, want %v", got.SharedWithUserID, targetUser)
	}
	if got.SharedWithTeamID != nil {
		t.Errorf("SharedWithTeamID should stay nil, got %v", got.SharedWithTeamID)
	}
	if got.Role != store.ShareRoleEditor {
		t.Errorf("Role = %s, want %s", got.Role, store.ShareRoleEditor)
	}
	if got.CreatedBy != ownerID {
		t.Errorf("CreatedBy = %v, want session userID %v", got.CreatedBy, ownerID)
	}
}

// TestAgentShares_NonOwnerRejected confirms a member who does NOT own the
// agent cannot create a share — the store mutation must be skipped entirely
// and the response must carry an error code.
func TestAgentShares_NonOwnerRejected(t *testing.T) {
	realOwner := uuid.New()
	intruder := uuid.New()
	stub := &shareCaptureStore{
		agent: &store.AgentData{
			BaseModel:   store.BaseModel{ID: uuid.New()},
			AgentKey:    "ops-bot",
			OwnerUserID: &realOwner,
		},
	}
	m := newSharesMethods(stub)

	client, read := gateway.NewTestClientWithCapture(permissions.RoleMember, intruder.String())
	req := makeFrame(t, protocol.MethodAgentsSharesCreate, map[string]any{
		"agentId":          "ops-bot",
		"sharedWithUserId": uuid.NewString(),
		"role":             store.ShareRoleViewer,
	})

	m.handleCreate(context.Background(), client, req)

	if len(stub.createdShares) != 0 {
		t.Fatalf("expected zero CreateShare calls; got %d", len(stub.createdShares))
	}
	resp := read()
	if resp == nil {
		t.Fatalf("expected error response, got none")
	}
	if resp.OK {
		t.Errorf("expected ok=false; got resp=%+v", resp)
	}
}

// TestAgentShares_MutexEnforced mirrors the DB-level CHECK: exactly one of
// sharedWithUserId / sharedWithTeamId must be non-empty. Both-set is rejected
// up front so the user gets a clean 4xx instead of an opaque 500 from PG.
func TestAgentShares_MutexEnforced(t *testing.T) {
	ownerID := uuid.New()
	stub := &shareCaptureStore{
		agent: &store.AgentData{
			BaseModel:   store.BaseModel{ID: uuid.New()},
			AgentKey:    "ops-bot",
			OwnerUserID: &ownerID,
		},
	}
	m := newSharesMethods(stub)
	client := gateway.NewTestClient(permissions.RoleMember, ownerID.String())

	// Both targets set → reject.
	req := makeFrame(t, protocol.MethodAgentsSharesCreate, map[string]any{
		"agentId":          "ops-bot",
		"sharedWithUserId": uuid.NewString(),
		"sharedWithTeamId": uuid.NewString(),
		"role":             store.ShareRoleMember,
	})
	m.handleCreate(context.Background(), client, req)
	if len(stub.createdShares) != 0 {
		t.Errorf("both-set: expected reject, got %d shares written", len(stub.createdShares))
	}

	// Neither target set → reject.
	req2 := makeFrame(t, protocol.MethodAgentsSharesCreate, map[string]any{
		"agentId": "ops-bot",
		"role":    store.ShareRoleMember,
	})
	m.handleCreate(context.Background(), client, req2)
	if len(stub.createdShares) != 0 {
		t.Errorf("neither-set: expected reject, got %d shares written", len(stub.createdShares))
	}
}

// TestAgentShares_DeleteRoutesByTarget confirms RevokeShareByUser is called
// when sharedWithUserId is supplied, and RevokeShareByTeam when
// sharedWithTeamId is supplied — never both, never neither.
func TestAgentShares_DeleteRoutesByTarget(t *testing.T) {
	ownerID := uuid.New()
	stub := &shareCaptureStore{
		agent: &store.AgentData{
			BaseModel:   store.BaseModel{ID: uuid.New()},
			AgentKey:    "ops-bot",
			OwnerUserID: &ownerID,
		},
	}
	m := newSharesMethods(stub)
	client := gateway.NewTestClient(permissions.RoleMember, ownerID.String())

	targetUser := uuid.New()
	m.handleDelete(context.Background(), client, makeFrame(t, protocol.MethodAgentsSharesDelete, map[string]any{
		"agentId":          "ops-bot",
		"sharedWithUserId": targetUser.String(),
	}))
	if len(stub.revokedUserIDs) != 1 || stub.revokedUserIDs[0] != targetUser {
		t.Errorf("RevokeShareByUser not called with target; revokedUserIDs=%v", stub.revokedUserIDs)
	}
	if len(stub.revokedTeamIDs) != 0 {
		t.Errorf("RevokeShareByTeam should not have been called; got %v", stub.revokedTeamIDs)
	}

	targetTeam := uuid.New()
	m.handleDelete(context.Background(), client, makeFrame(t, protocol.MethodAgentsSharesDelete, map[string]any{
		"agentId":          "ops-bot",
		"sharedWithTeamId": targetTeam.String(),
	}))
	if len(stub.revokedTeamIDs) != 1 || stub.revokedTeamIDs[0] != targetTeam {
		t.Errorf("RevokeShareByTeam not called with target; revokedTeamIDs=%v", stub.revokedTeamIDs)
	}
}
