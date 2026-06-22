package boot

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zrafat80/quickbite/analytics-service/app/analytics/controller"
	. "github.com/zrafat80/quickbite/analytics-service/lib/boot"
)

func TestNewRouterHealth(t *testing.T) {
	router := NewRouter(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		nil,
		nil,
		controller.NewAnalyticsController(nil),
	)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	require.NotEmpty(t, recorder.Header().Get("x-correlation-id"))
	assert.JSONEq(t, `{"success":true,"data":{"status":"ok"}}`, recorder.Body.String())
}
