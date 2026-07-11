package tools

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/skillcatalog"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Skill-service auth — the standard way any skill authenticates to a 42bucks
// skill-backed service (the code-runner behind the `code` skill today, and any
// future one).
//
// goclaw injects four env vars into every shell/skill exec:
//
//	SKILL_RUNTIME_TOKEN  — a workspace-scoped goclaw API key for the
//	                       X-Workspace-Key header; the service resolves the
//	                       workspace + gateway from it via /callback/v1/verify-key
//	GOCLAW_WORKSPACE_ID  \  — tenant workspace identifier (the tenant UUID),
//	                       |   sent as the service's workspace id; NOT a path
//	GOCLAW_USER_ID         }— request identity (non-secret)
//	GOCLAW_AGENT_ID        |
//	GOCLAW_SESSION_KEY   /  — origin session, for the service's result callback
//
// A skill's SKILL.md references these by name only — the shell expands them at
// exec time, so the agent never handles the raw token. Tokens are minted per
// tenant, cached, and short-lived. This mirrors the x-router provider's model
// (workspace-anchored bearer key + identity headers) for the skill path.

const skillTokenTTL = 24 * time.Hour

var skillServiceAuth struct {
	mu    sync.Mutex
	keys  store.APIKeyStore
	cache map[string]skillTokenEntry // tenantID string -> token
}

type skillTokenEntry struct {
	token   string
	refresh time.Time // re-mint once past this (kept under the real expiry)
}

// InitSkillServiceAuth wires the api-key store used to mint workspace
// skill-service tokens. Called once at gateway startup; until then (and in
// tests) SkillServiceEnv simply omits the token.
func InitSkillServiceAuth(keys store.APIKeyStore) {
	skillServiceAuth.mu.Lock()
	defer skillServiceAuth.mu.Unlock()
	skillServiceAuth.keys = keys
	skillServiceAuth.cache = map[string]skillTokenEntry{}
}

// workspaceSkillToken returns a cached or freshly-minted skill-service API key
// for the tenant. Returns "" when minting is unavailable.
func workspaceSkillToken(ctx context.Context, tenantID uuid.UUID) string {
	if tenantID == uuid.Nil {
		return ""
	}
	skillServiceAuth.mu.Lock()
	defer skillServiceAuth.mu.Unlock()
	if skillServiceAuth.keys == nil {
		return ""
	}
	tid := tenantID.String()
	if e, ok := skillServiceAuth.cache[tid]; ok && time.Now().Before(e.refresh) {
		return e.token
	}
	raw, hash, prefix, err := crypto.GenerateAPIKey()
	if err != nil {
		return ""
	}
	now := time.Now()
	exp := now.Add(skillTokenTTL)
	key := &store.APIKeyData{
		ID:        store.GenNewID(),
		TenantID:  tenantID,
		Name:      "skill-runtime:" + tid,
		Prefix:    prefix,
		KeyHash:   hash,
		Scopes:    []string{"operator.read", "operator.write"},
		CreatedBy: "skill-service-auth",
		ExpiresAt: &exp,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := skillServiceAuth.keys.Create(ctx, key); err != nil {
		return ""
	}
	// Refresh an hour before the real expiry so a long exec never races it.
	skillServiceAuth.cache[tid] = skillTokenEntry{token: raw, refresh: exp.Add(-time.Hour)}
	return raw
}

// WorkspaceSkillToken returns a workspace-scoped skill-service API key for the
// tenant — the same key SkillServiceEnv injects as SKILL_RUNTIME_TOKEN. Used by
// user-facing HTTP handlers that proxy a request to a 42bucks skill-backed
// service (the code-runner behind the `code` skill). Returns "" when minting
// is unavailable.
func WorkspaceSkillToken(ctx context.Context, tenantID uuid.UUID) string {
	return workspaceSkillToken(ctx, tenantID)
}

// skillServiceEnvMap builds the standard skill-service auth env vars for the
// current run context as a map. Returns nil when there is no run context.
func skillServiceEnvMap(ctx context.Context) map[string]string {
	rc := store.RunContextFromCtx(ctx)
	if rc == nil {
		return nil
	}
	m := map[string]string{}
	if tok := workspaceSkillToken(ctx, rc.TenantID); tok != "" {
		m["SKILL_RUNTIME_TOKEN"] = tok
	}
	// GOCLAW_WORKSPACE_ID is the tenant's stable workspace IDENTIFIER, not its
	// filesystem path. Skill-backed services send it as the workspace id
	// (x-code's `workspaceCuid` → x-api `x-workspace-id`), which x-api resolves
	// against {Workspace.id, gatewayTenantId, verify-key tenantId/workspaceId}
	// and validates as `^[A-Za-z0-9_-]+$`. The tenant UUID always matches the
	// verify-key `tenantId` / `gatewayTenantId`, so it both passes validation
	// and resolves the right workspace. (Historically this was rc.Workspace —
	// the resolved per-user *path*, which contains `/` and fails the regex once
	// user/chat isolation layering is applied, breaking every job launch.)
	if rc.TenantID != uuid.Nil {
		m["GOCLAW_WORKSPACE_ID"] = rc.TenantID.String()
	}
	if rc.UserID != "" {
		m["GOCLAW_USER_ID"] = rc.UserID
	}
	if rc.AgentKey != "" {
		m["GOCLAW_AGENT_ID"] = rc.AgentKey
	}
	// Origin session key — lets a skill-backed service post its async result
	// back into the chat via the /callback/v1/messages endpoint.
	if sk := ToolSessionKeyFromCtx(ctx); sk != "" {
		m["GOCLAW_SESSION_KEY"] = sk
	}
	return m
}

// SkillServiceEnv returns the standard skill-service auth env vars for the
// current run context as KEY=VALUE strings (for exec.Cmd.Env). Returns nil
// when there is no run context; callers append it unconditionally.
//
// HOST-exec only: it also points GOCLAW_SKILL_CATALOG at the runtime-persisted
// catalog the gateway hot-reloads (internal/skillcatalog/reload) so the static
// `skill` CLI in a skill's bash tracks catalog updates without a rebuild. This
// is deliberately NOT part of skillServiceEnvMap (which also feeds the Docker
// sandbox): the sandbox neither mounts that file nor ships the `skill` CLI, so
// the pointer would be dead there.
func SkillServiceEnv(ctx context.Context) []string {
	m := skillServiceEnvMap(ctx)
	if len(m) == 0 {
		return nil
	}
	env := make([]string, 0, len(m)+1)
	for k, v := range m {
		env = append(env, k+"="+v)
	}
	env = append(env, "GOCLAW_SKILL_CATALOG="+skillcatalog.DefaultCatalogPath)
	return env
}
