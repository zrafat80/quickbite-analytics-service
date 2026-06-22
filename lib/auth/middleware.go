package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/zrafat80/quickbite/analytics-service/lib/appcontext"
	apperr "github.com/zrafat80/quickbite/analytics-service/lib/errors"
)

// ErrUnauthenticated is the single stable AppError returned by Require.
// Mirrors order-service's GUARD_ERRORS.UNAUTHENTICATED — declared as a
// package-level var (the Go equivalent of <module>.constants.ts entries) so
// call sites never compose ad-hoc strings.
var ErrUnauthenticated = apperr.New("UNAUTHENTICATED", http.StatusUnauthorized, "unauthenticated")

// ErrForbiddenRole is returned by RequireRole when the caller authenticated
// but holds a role that isn't on the allow-list. Distinct code from RBAC's
// FORBIDDEN so dashboards can tell role-gating apart from permission-gating.
var ErrForbiddenRole = apperr.New("FORBIDDEN_ROLE", http.StatusForbidden, "forbidden role")

// Require is a chi-compatible middleware that:
//  1. Reads the access token from `access_token` cookie OR
//     `Authorization: Bearer <token>` header (cookie wins).
//  2. Verifies it with the Verifier wired in lib/boot.
//  3. Stashes claims on ctx via appcontext.WithClaims.
//
// On any failure it writes the unauthenticated envelope and stops the chain.
func Require(v *Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tok := extractToken(r)
			if tok == "" {
				writeUnauth(w)
				return
			}
			claims, err := v.Verify(tok)
			if err != nil {
				writeUnauth(w)
				return
			}
			ctx := appcontext.WithClaims(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extractToken(r *http.Request) string {
	if c, err := r.Cookie("access_token"); err == nil && c.Value != "" {
		return c.Value
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return ""
}

func writeUnauth(w http.ResponseWriter) {
	// Bypass our standard handler middleware — auth failures happen before
	// any application handler runs, so we render the envelope inline.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	// Hand-rolled JSON to avoid an import cycle with lib/http.
	_, _ = w.Write([]byte(`{"success":false,"error":{"code":"UNAUTHENTICATED","message":"unauthenticated"}}`))
}

// RequireRole gates a route to one of the supplied roles by string-match
// against claims.Role. Use for endpoints whose authorisation rule is "role X
// only" rather than "any role with permission P" — saves the RBAC round-trip
// and makes platform-admin-only routes obvious from the mount site. Must run
// AFTER Require so claims are present on ctx.
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	allow := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		allow[r] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := appcontext.ClaimsFrom(r.Context())
			if !ok {
				writeForbiddenRole(w, "missing authenticated claims on request")
				return
			}
			if _, ok := allow[claims.Role]; !ok {
				writeForbiddenRole(w, fmt.Sprintf("role %q is not permitted on this route", claims.Role))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeForbiddenRole(w http.ResponseWriter, msg string) {
	body, _ := json.Marshal(map[string]any{
		"success": false,
		"error":   map[string]string{"code": "FORBIDDEN_ROLE", "message": msg},
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write(body)
}
