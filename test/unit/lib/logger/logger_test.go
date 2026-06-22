package logger

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"

	. "github.com/zrafat80/quickbite/analytics-service/lib/logger"
)

func TestNewConfiguresLevel(t *testing.T) {
	debug := New("DEBUG")
	assert.True(t, debug.Enabled(context.Background(), slog.LevelDebug))

	info := New("unknown")
	assert.False(t, info.Enabled(context.Background(), slog.LevelDebug))
	assert.True(t, info.Enabled(context.Background(), slog.LevelInfo))
}

func TestContextLogger(t *testing.T) {
	originalDefault := slog.Default()
	t.Cleanup(func() { slog.SetDefault(originalDefault) })
	fallback := slog.New(slog.NewTextHandler(io.Discard, nil))
	slog.SetDefault(fallback)
	assert.Same(t, fallback, FromContext(context.Background()))

	custom := slog.New(slog.NewTextHandler(io.Discard, nil))
	assert.Same(t, custom, FromContext(WithContext(context.Background(), custom)))
	assert.NotNil(t, New("debug"))
}
