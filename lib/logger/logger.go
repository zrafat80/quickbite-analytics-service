// Package logger wraps log/slog with: (a) structured JSON output, (b) per
// request "child logger" stored on the request context, and (c) a
// FromContext accessor used everywhere else. Equivalent to the Pino-style
// logger in the Node services.
package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

type ctxKey struct{}

// New builds the root logger. Level comes from config; the handler writes
// JSON to stdout. JSON because the Node services log JSON too, and downstream
// log infra (loki, vector) parses the same shape across the platform.
func New(levelStr string) *slog.Logger {
	level := parseLevel(levelStr)
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	return slog.New(h)
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// WithContext stores l on ctx. Used by the correlation middleware to attach
// a request-scoped child logger (with correlation_id baked in).
func WithContext(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext returns the request logger, falling back to slog.Default() when
// none is attached (e.g. background workers).
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}
