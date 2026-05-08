package methods

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// AgentSharesMethods exposes the explicit grant rows persisted by
// AgentAccessStore over WS so the FE agent-detail "Shares" tab can manage
// per-user / per-team access without needing a direct SQL surface.
//
// Rules of engagement enforced here:
//   - List is allowed for any caller that can already see the agent
//     (admins/owners always; non-admins must be the agent's owner_user_id).
//   - Create/Delete are restricted to the agent owner or admins/owners.
//     We never trust a CreatedBy from the wire — it is resolved from the
//     authenticated session.
//   - Exactly one of sharedWithUserId / sharedWithTeamId must be non-empty
//     on Create. The store enforces the same invariant at the DB CHECK
//     level; we re-check up front for a clean 4xx instead of a 500.
type AgentSharesMethods struct {
	agents   store.AgentStore
	eventBus bus.EventPublisher
	cfg      *config.Config
}

func NewAgentSharesMethods(agents store.AgentStore, eventBus bus.EventPublisher, cfg *config.Config) *AgentSharesMethods {
	return &AgentSharesMethods{agents: agents, eventBus: eventBus, cfg: cfg}
}

func (m *AgentSharesMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodAgentsSharesList, m.handleList)
	router.Register(protocol.MethodAgentsSharesCreate, m.handleCreate)
	router.Register(protocol.MethodAgentsSharesDelete, m.handleDelete)
}

// canManageAgent returns true when the caller is global admin/owner OR is
// the resolved agent's OwnerUserID. Used to gate share mutations.
func (m *AgentSharesMethods) canManageAgent(ag *store.AgentData, client *gateway.Client) bool {
	if canSeeAll(client.Role(), m.cfg.Gateway.OwnerIDs, client.UserID()) {
		return true
	}
	if ag.OwnerUserID == nil {
		return false
	}
	return ag.OwnerUserID.String() == client.UserID()
}

// --- List ---

type sharesListParams struct {
	AgentID string `json:"agentId"` // accepts UUID or agent_key
}

func (m *AgentSharesMethods) handleList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)

	var params sharesListParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}
	if params.AgentID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "agentId")))
		return
	}

	ag, err := resolveAgentInfo(ctx, m.agents, params.AgentID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, err.Error()))
		return
	}

	if !m.canManageAgent(ag, client) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "agent")))
		return
	}

	shares, err := m.agents.ListShares(ctx, ag.ID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"shares": shares,
		"count":  len(shares),
	}))
}

// --- Create ---

type sharesCreateParams struct {
	AgentID            string `json:"agentId"`
	SharedWithUserID   string `json:"sharedWithUserId"`
	SharedWithTeamID   string `json:"sharedWithTeamId"`
	Role               string `json:"role"` // viewer | member | editor
}

func (m *AgentSharesMethods) handleCreate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)

	var params sharesCreateParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}
	if params.AgentID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "agentId")))
		return
	}
	if !store.ValidShareRole(params.Role) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "role (viewer|member|editor)")))
		return
	}

	// Mutex: exactly one of user/team must be set. Mirrors the DB CHECK.
	hasUser := params.SharedWithUserID != ""
	hasTeam := params.SharedWithTeamID != ""
	if hasUser == hasTeam {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "exactly one of sharedWithUserId / sharedWithTeamId")))
		return
	}

	ag, err := resolveAgentInfo(ctx, m.agents, params.AgentID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, err.Error()))
		return
	}
	if !m.canManageAgent(ag, client) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "agent")))
		return
	}

	// CreatedBy must come from the session — never the wire payload.
	createdBy, err := uuid.Parse(client.UserID())
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgInvalidID, "session user")))
		return
	}

	in := store.AgentShareInput{
		AgentID:   ag.ID,
		Role:      params.Role,
		CreatedBy: createdBy,
	}
	if hasUser {
		uid, err := uuid.Parse(params.SharedWithUserID)
		if err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "sharedWithUserId")))
			return
		}
		in.SharedWithUserID = &uid
	}
	if hasTeam {
		tid, err := uuid.Parse(params.SharedWithTeamID)
		if err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "sharedWithTeamId")))
			return
		}
		in.SharedWithTeamID = &tid
	}

	if err := m.agents.CreateShare(ctx, in); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	auditTarget := ag.ID.String()
	emitAudit(m.eventBus, client, "agent_share.created", "agent_share", auditTarget)

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"ok":      true,
		"agentId": ag.ID.String(),
	}))
}

// --- Delete ---

type sharesDeleteParams struct {
	AgentID          string `json:"agentId"`
	SharedWithUserID string `json:"sharedWithUserId"`
	SharedWithTeamID string `json:"sharedWithTeamId"`
}

func (m *AgentSharesMethods) handleDelete(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)

	var params sharesDeleteParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}
	if params.AgentID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "agentId")))
		return
	}

	hasUser := params.SharedWithUserID != ""
	hasTeam := params.SharedWithTeamID != ""
	if hasUser == hasTeam {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "exactly one of sharedWithUserId / sharedWithTeamId")))
		return
	}

	ag, err := resolveAgentInfo(ctx, m.agents, params.AgentID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, err.Error()))
		return
	}
	if !m.canManageAgent(ag, client) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "agent")))
		return
	}

	var revokeErr error
	if hasUser {
		uid, perr := uuid.Parse(params.SharedWithUserID)
		if perr != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "sharedWithUserId")))
			return
		}
		revokeErr = m.agents.RevokeShareByUser(ctx, ag.ID, uid)
	} else {
		tid, perr := uuid.Parse(params.SharedWithTeamID)
		if perr != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "sharedWithTeamId")))
			return
		}
		revokeErr = m.agents.RevokeShareByTeam(ctx, ag.ID, tid)
	}
	if revokeErr != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, revokeErr.Error()))
		return
	}

	target := fmt.Sprintf("%s/%s", ag.ID, firstNonEmpty(params.SharedWithUserID, params.SharedWithTeamID))
	emitAudit(m.eventBus, client, "agent_share.deleted", "agent_share", target)

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"ok":      true,
		"agentId": ag.ID.String(),
	}))
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
