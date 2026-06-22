// Package middleware holds the cross-cutting net/http middleware applied
// globally in lib/boot. Each middleware is the Go analogue of the Node
// CorrelationMiddleware / access-log interceptor.
package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/zrafat80/quickbite/analytics-service/lib/appcontext"
	"github.com/zrafat80/quickbite/analytics-service/lib/logger"
)

// Correlation reads/sets the x-correlation-id header, stuffs it into ctx,
// and attaches a per-request child logger pre-populated with the id.
func Correlation(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("x-correlation-id")
			if id == "" {
				id = uuid.NewString()
			}
			w.Header().Set("x-correlation-id", id)

			reqLog := base.With(slog.String("correlation_id", id))
			ctx := appcontext.WithCorrelationID(r.Context(), id)
			ctx = logger.WithContext(ctx, reqLog)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AccessLog logs one structured line per request (method, path, status,
// duration_ms). Equivalent to LoggingInterceptor in the Node side.
func AccessLog() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: 200}
			next.ServeHTTP(sw, r)
			logger.FromContext(r.Context()).Info("http",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
