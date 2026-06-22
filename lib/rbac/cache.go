// Package rbac is the read-through permission cache. Hits an in-process map
// first, falls back to core-service over HTTP, stores result with a TTL.
// Equivalent to PermissionCacheService in order-service.
package rbac

import (
	"context"
	"sync"
	"time"
)

type entry struct {
	perms     map[string]struct{}
	expiresAt time.Time
}

type Cache struct {
	core PermissionSource
	ttl  time.Duration

	mu sync.RWMutex
	m  map[string]entry
}

func NewCache(core PermissionSource, ttl time.Duration) *Cache {
	return &Cache{
		core: core,
		ttl:  ttl,
		m:    make(map[string]entry),
	}
}

// Has returns true when `role` is permitted `permission` (dotted form
// "resource:action"). A cache miss triggers a single round-trip to core; a
// concurrent miss won't dedupe but is harmless (RBAC reads are cheap).
func (c *Cache) Has(ctx context.Context, role, permission string) (bool, error) {
	perms, err := c.GetForRole(ctx, role)
	if err != nil {
		return false, err
	}
	_, ok := perms[permission]
	return ok, nil
}

// GetForRole returns the cached permission set, populating on miss/expiry.
func (c *Cache) GetForRole(ctx context.Context, role string) (map[string]struct{}, error) {
	now := time.Now()
	c.mu.RLock()
	if e, ok := c.m[role]; ok && e.expiresAt.After(now) {
		c.mu.RUnlock()
		return e.perms, nil
	}
	c.mu.RUnlock()

	list, err := c.core.GetRolePermissions(ctx, role)
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, len(list))
	for _, p := range list {
		set[p] = struct{}{}
	}
	c.mu.Lock()
	c.m[role] = entry{perms: set, expiresAt: now.Add(c.ttl)}
	c.mu.Unlock()
	return set, nil
}

// Invalidate drops the cached entry for a role. Future homework hooks the
// `rbac.permissions_changed` consumer up to this method.
func (c *Cache) Invalidate(role string) {
	c.mu.Lock()
	delete(c.m, role)
	c.mu.Unlock()
}
