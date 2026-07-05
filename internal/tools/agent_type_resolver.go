package tools

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// AgentTypeResolverFunc returns the agent type ("open"/"predefined") for an
// agent UUID via an authoritative lookup. Returns "" when unknown.
type AgentTypeResolverFunc func(ctx context.Context, agentID uuid.UUID) string

// AgentTypeLookup is the minimal store surface needed to resolve an agent's
// type. Satisfied by store.AgentStore.
type AgentTypeLookup interface {
	GetByIDUnscoped(ctx context.Context, id uuid.UUID) (*store.AgentData, error)
}

// NewCachedAgentTypeResolver returns an AgentTypeResolverFunc backed by the
// agent store with a TTL cache. Agent type is effectively immutable during a
// session (open→predefined transitions recreate the Loop), so a short TTL is
// safe and keeps hot memory paths off the DB.
//
// Lookup errors are not cached — the resolver returns "" (callers fall back
// to the safe per-user-private default) and the next call retries.
func NewCachedAgentTypeResolver(lookup AgentTypeLookup, ttl time.Duration) AgentTypeResolverFunc {
	type entry struct {
		agentType string
		expires   time.Time
	}
	var (
		mu    sync.Mutex
		cache = make(map[uuid.UUID]entry)
	)
	return func(ctx context.Context, agentID uuid.UUID) string {
		if agentID == uuid.Nil || lookup == nil {
			return ""
		}
		now := time.Now()
		mu.Lock()
		if e, ok := cache[agentID]; ok && now.Before(e.expires) {
			mu.Unlock()
			return e.agentType
		}
		mu.Unlock()

		ag, err := lookup.GetByIDUnscoped(ctx, agentID)
		if err != nil || ag == nil {
			return "" // don't cache errors; retry on next call
		}

		mu.Lock()
		cache[agentID] = entry{agentType: ag.AgentType, expires: now.Add(ttl)}
		mu.Unlock()
		return ag.AgentType
	}
}
