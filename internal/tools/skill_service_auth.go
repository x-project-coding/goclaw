package tools

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
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
//	GOCLAW_WORKSPACE_ID  \
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

// SkillServiceEnv returns the standard skill-service auth env vars for the
// current run context. Returns nil when there is no run context; callers
// append it unconditionally.
func SkillServiceEnv(ctx context.Context) []string {
	rc := store.RunContextFromCtx(ctx)
	if rc == nil {
		return nil
	}
	var env []string
	if tok := workspaceSkillToken(ctx, rc.TenantID); tok != "" {
		env = append(env, "SKILL_RUNTIME_TOKEN="+tok)
	}
	if rc.Workspace != "" {
		env = append(env, "GOCLAW_WORKSPACE_ID="+rc.Workspace)
	}
	if rc.UserID != "" {
		env = append(env, "GOCLAW_USER_ID="+rc.UserID)
	}
	if rc.AgentKey != "" {
		env = append(env, "GOCLAW_AGENT_ID="+rc.AgentKey)
	}
	// Origin session key — lets a skill-backed service post its async result
	// back into the chat via the /callback/v1/messages endpoint.
	if sk := ToolSessionKeyFromCtx(ctx); sk != "" {
		env = append(env, "GOCLAW_SESSION_KEY="+sk)
	}
	return env
}
