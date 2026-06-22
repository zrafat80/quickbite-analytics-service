package analytics

// Permissions consumed by analytics-service. Strings match core-service's
// (resource, action) rows flattened to "resource:action" — see
// lib/coreclient.GetRolePermissions. Each endpoint group is gated by exactly
// one of these.
const (
	PermRestaurantRead = "core:restaurant:read"
	PermBranchRead     = "core:branch:read"
	PermProductRead    = "core:product:read"
)

// Event-type constants — must stay in sync with order-service's
// lib/events/event-types.ts.
const (
	EventOrderPlaced     = "order.placed"
	EventOrderDelivered  = "order.delivered"
	EventOrderRejected   = "order.rejected"
	EventPaymentComplete = "payment.completed"
)
