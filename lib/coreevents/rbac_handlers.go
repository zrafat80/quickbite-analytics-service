package coreevents

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/zrafat80/quickbite/analytics-service/lib/logger"
)

// rbacCache is the minimum surface the handler needs from lib/rbac.Cache.
// Defined here so we don't have to import lib/rbac (which would create no
// cycle today but tightens the seam for future refactors).
type rbacCache interface {
	Invalidate(role string)
}

// rbacPermissionsChangedPayload is the on-wire shape published by
// core-service when an admin grants/revokes a permission on a role. The
// service-level payload may carry more (changed permissions, actor id) —
// we only need the role here because the cache is keyed by role.
type rbacPermissionsChangedPayload struct {
	Role string `json:"role"`
}

// BuildRBACHandlers returns the event-type → handler map for the
// core.events consumer. Single-entry today; adding more cache invalidation
// handlers (product.*, branch.*) is a one-line edit per type.
func BuildRBACHandlers(cache rbacCache) map[string]EventHandler {
	return map[string]EventHandler{
		"rbac.permissions_changed": func(ctx context.Context, env Envelope) error {
			var p rbacPermissionsChangedPayload
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				return fmt.Errorf("decode rbac.permissions_changed: %w", err)
			}
			if p.Role == "" {
				logger.FromContext(ctx).Warn("rbac.permissions_changed: empty role, ack-skip",
					"eventId", env.EventID)
				return nil
			}
			cache.Invalidate(p.Role)
			logger.FromContext(ctx).Info("rbac cache invalidated",
				"role", p.Role, "eventId", env.EventID)
			return nil
		},
	}
}
