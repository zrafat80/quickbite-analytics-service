package rbac

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/zrafat80/quickbite/analytics-service/lib/appcontext"
	apperr "github.com/zrafat80/quickbite/analytics-service/lib/errors"
)

// ErrForbidden is the stable AppError for "authenticated but unauthorised".
var ErrForbidden = apperr.New("FORBIDDEN", http.StatusForbidden, "forbidden")

// ErrRBACLookupFailed is surfaced when we couldn't reach core-service to
// resolve the caller's permissions. This is a dependency failure, not a
// permission denial — separate code + 502 so it doesn't get confused with a
// real authorisation problem.
var ErrRBACLookupFailed = apperr.New("RBAC_LOOKUP_FAILED", http.StatusBadGateway, "could not verify caller's permissions")

// RoleSystemAdmin bypasses the permission check entirely. Mirrors the
// super-admin escape hatch in core-service / order-service — a `system_admin`
// JWT is trusted for every protected route in this service without a round
// trip to core's RBAC catalogue.
const RoleSystemAdmin = "system_admin"

// RoleRestaurantUser is the platform-tier role every restaurant-bound user
// carries. The real (permission-bearing) role lives in claims.restaurantRole
// — `owner` / `branch_manager` / `staff` — and that's what core's
// role_permissions table maps. Mirrors order-service PermissionsGuard.
const RoleRestaurantUser = "restaurant_user"

// Require enforces a single permission string (dotted "resource:action").
// Authorisation flow (matches order-service permissions.guard.ts):
//  1. system_admin → bypass.
//  2. restaurant_user with a restaurantRole → look up permissions by that
//     restaurant-scoped role in core's RBAC catalogue.
//  3. Anything else (customer, delivery_agent, restaurant_user w/o restaurantRole)
//     → FORBIDDEN; this service has no surfaces for those callers.
//
// Always run AFTER the JWT middleware — claims must be on ctx.
func Require(cache *Cache, permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := appcontext.ClaimsFrom(r.Context())
			if !ok {
				writeErr(w, http.StatusForbidden, "FORBIDDEN", "missing authenticated claims on request")
				return
			}
			if claims.Role == "" {
				writeErr(w, http.StatusForbidden, "FORBIDDEN", "token has no role claim")
				return
			}
			if claims.Role == RoleSystemAdmin {
				next.ServeHTTP(w, r)
				return
			}
			if claims.Role != RoleRestaurantUser {
				writeErr(w, http.StatusForbidden, "FORBIDDEN",
					fmt.Sprintf("role %q has no analytics surface — log in as a restaurant member or system_admin", claims.Role))
				return
			}
			if claims.RestaurantRole == nil || *claims.RestaurantRole == "" {
				writeErr(w, http.StatusForbidden, "FORBIDDEN",
					"restaurant_user token is missing the restaurantRole claim — re-login from core-service so the JWT is bound to your restaurant role")
				return
			}
			restaurantRole := *claims.RestaurantRole
			has, err := cache.Has(r.Context(), restaurantRole, permission)
			if err != nil {
				writeErr(w, http.StatusBadGateway, "RBAC_LOOKUP_FAILED",
					fmt.Sprintf("could not verify permissions for restaurantRole %q against core-service: %v", restaurantRole, err))
				return
			}
			if !has {
				writeErr(w, http.StatusForbidden, "FORBIDDEN",
					fmt.Sprintf("restaurantRole %q is missing required permission %q — grant it in core-service or call with a role that has it", restaurantRole, permission))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	body, _ := json.Marshal(map[string]any{
		"success": false,
		"error":   map[string]string{"code": code, "message": msg},
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
