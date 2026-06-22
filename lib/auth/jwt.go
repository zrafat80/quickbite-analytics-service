// Package auth is the Go analogue of order-service's JwtAuthGuard +
// AuthUtilsService. Verify-only: this service never issues tokens.
package auth

import (
	"fmt"
	"strconv"

	"github.com/golang-jwt/jwt/v5"
	"github.com/zrafat80/quickbite/analytics-service/lib/appcontext"
)

// claimAsInt64 normalises the JSON-native variants a numeric claim can arrive
// as. Core-service emits restaurantId/branchIds from a Postgres BIGINT column;
// node-pg serialises bigints as strings, so the JWT contains "12" rather
// than 12. Standard JWTs from a Go publisher emit float64. Accept both —
// silently failing the type assertion (as the previous code did) would mean
// claims.RestaurantID stays nil and every tenant check incorrectly 403s real
// users. Anything unparseable returns (0, false) so the caller can decide.
func claimAsInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case float64:
		return int64(x), true
	case string:
		n, err := strconv.ParseInt(x, 10, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

// Verifier holds the access secret loaded from env at boot. Mirrors the
// `jwt.accessSecret` config key the Node services use.
type Verifier struct {
	accessSecret []byte
}

func NewVerifier(accessSecret string) *Verifier {
	return &Verifier{accessSecret: []byte(accessSecret)}
}

// Verify parses + validates an HS256 token and returns the typed Claims.
// Mirrors core/order: claim names are camelCase userId/role/restaurantId/etc.
func (v *Verifier) Verify(token string) (*appcontext.Claims, error) {
	parsed, err := jwt.Parse(token, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return v.accessSecret, nil
	})
	if err != nil {
		return nil, err
	}
	if !parsed.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	mc, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("unexpected claims type")
	}

	out := &appcontext.Claims{}
	if n, ok := claimAsInt64(mc["userId"]); ok {
		out.UserID = n
	}
	if v, ok := mc["email"].(string); ok {
		out.Email = v
	}
	if v, ok := mc["role"].(string); ok {
		out.Role = v
	}
	if n, ok := claimAsInt64(mc["restaurantId"]); ok {
		out.RestaurantID = &n
	}
	if v, ok := mc["restaurantRole"].(string); ok {
		s := v
		out.RestaurantRole = &s
	}
	if rawIDs, ok := mc["branchIds"].([]any); ok {
		ids := make([]int64, 0, len(rawIDs))
		for _, raw := range rawIDs {
			if n, ok := claimAsInt64(raw); ok {
				ids = append(ids, n)
			}
		}
		out.BranchIDs = ids
	}
	if out.UserID == 0 || out.Role == "" {
		return nil, fmt.Errorf("token missing required claims")
	}
	return out, nil
}
