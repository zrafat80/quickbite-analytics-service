//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthHTTPCompleteness(t *testing.T) {
	app := setupIntegrationApp(t)
	response := app.getWithCookie(t, "/health", "", "health-correlation")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, "health-correlation", response.Header().Get("x-correlation-id"))
	assert.JSONEq(t, `{"success":true,"data":{"status":"ok"}}`, response.Body.String())
}
