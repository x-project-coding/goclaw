package pg

import (
	"context"
	"sync"
	"time"
)

const contactResolveCacheTTL = 60 * time.Second

// contactResolveEntry holds a cached user resolution result.
type contactResolveEntry struct {
	tenantUserID string // empty = not merged
	fetched      time.Time
}

// contactResolveCache is a TTL cache for contact→user resolution.
type contactResolveCache struct {
	mu    sync.RWMutex
	items map[string]contactResolveEntry // key: "channelType:senderID"
}

func newContactResolveCache() *contactResolveCache {
	return &contactResolveCache{items: make(map[string]contactResolveEntry)}
}

func (c *contactResolveCache) get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if entry, ok := c.items[key]; ok && time.Since(entry.fetched) < contactResolveCacheTTL {
		return entry.tenantUserID, true
	}
	return "", false
}

func (c *contactResolveCache) set(key, tenantUserID string) {
	c.mu.Lock()
	c.items[key] = contactResolveEntry{tenantUserID: tenantUserID, fetched: time.Now()}
	c.mu.Unlock()
}

// InvalidateContactResolveCache clears all cached contact→user resolutions.
// Call after merge/unmerge operations.
func (s *PGContactStore) InvalidateContactResolveCache() {
	if s.resolveCache == nil {
		return
	}
	s.resolveCache.mu.Lock()
	s.resolveCache.items = make(map[string]contactResolveEntry)
	s.resolveCache.mu.Unlock()
}

// ResolveTenantUserID looks up a contact's merged user identity.
// v4 has no tenant_users table — returns empty string (no merged identity).
func (s *PGContactStore) ResolveTenantUserID(_ context.Context, _, _ string) (string, error) {
	return "", nil
}
