// Package appcontext owns the ctx-keyed pieces of state that flow through the
// request: JWT claims (set by auth middleware), correlation ID (set by the
// correlation middleware). Equivalent to express.d.ts in the Node services.
package appcontext

import "context"

type ctxKey int

const (
	keyClaims ctxKey = iota
	keyCorrelationID
)

// Claims is the JWT payload shape, in sync with order-service/core-service.
type Claims struct {
	UserID         int64   `json:"userId"`
	Email          string  `json:"email,omitempty"`
	Role           string  `json:"role"`
	RestaurantID   *int64  `json:"restaurantId,omitempty"`
	RestaurantRole *string `json:"restaurantRole,omitempty"`
	BranchIDs      []int64 `json:"branchIds,omitempty"`
}

func WithClaims(ctx context.Context, c *Claims) context.Context {
	return context.WithValue(ctx, keyClaims, c)
}

func ClaimsFrom(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(keyClaims).(*Claims)
	return c, ok && c != nil
}

func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyCorrelationID, id)
}

func CorrelationIDFrom(ctx context.Context) string {
	if s, ok := ctx.Value(keyCorrelationID).(string); ok {
		return s
	}
	return ""
}
