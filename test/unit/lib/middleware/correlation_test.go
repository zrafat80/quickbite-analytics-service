package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zrafat80/quickbite/analytics-service/lib/appcontext"
	"github.com/zrafat80/quickbite/analytics-service/lib/logger"
	. "github.com/zrafat80/quickbite/analytics-service/lib/middleware"
)

func TestCorrelationEchoesOrGeneratesIDAndPopulatesContext(t *testing.T) {
	base := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, w.Header().Get("x-correlation-id"), appcontext.CorrelationIDFrom(r.Context()))
		assert.NotSame(t, base, logger.FromContext(r.Context()))
		w.WriteHeader(http.StatusNoContent)
	})
	handler := Correlation(base)(next)

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("x-correlation-id", "provided")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assert.Equal(t, "provided", recorder.Header().Get("x-correlation-id"))

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	require.NotEmpty(t, recorder.Header().Get("x-correlation-id"))
}

func TestAccessLogCapturesStatusAndPath(t *testing.T) {
	var output bytes.Buffer
	log := slog.New(slog.NewTextHandler(&output, nil))
	handler := AccessLog()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	request := httptest.NewRequest(http.MethodPost, "/resource", nil)
	request = request.WithContext(logger.WithContext(request.Context(), log))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusAccepted, recorder.Code)
	text := output.String()
	assert.True(t, strings.Contains(text, "method=POST"))
	assert.True(t, strings.Contains(text, "path=/resource"))
	assert.True(t, strings.Contains(text, "status=202"))
}
