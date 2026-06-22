package auth

import "net/http"

// RequireInternalAPIKey is the Go analogue of order-service's
// RequireInternalApiKeyGuard. Plain shared-secret equality on `x-api-key`.
// Not used by the public analytics endpoints in this milestone — provided
// for parity so future /internal/* routes have an obvious mount point.
func RequireInternalAPIKey(expected string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("x-api-key") != expected || expected == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"error":{"code":"UNAUTHENTICATED","message":"invalid api key"}}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
