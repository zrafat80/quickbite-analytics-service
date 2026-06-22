package http

import (
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/zrafat80/quickbite/analytics-service/lib/http"
)

func decodeBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	return body
}

func TestResponseWriters(t *testing.T) {
	recorder := httptest.NewRecorder()
	SendSuccess(recorder, nethttp.StatusCreated, map[string]string{"id": "1"})
	assert.Equal(t, nethttp.StatusCreated, recorder.Code)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	assert.Equal(t, true, decodeBody(t, recorder)["success"])

	recorder = httptest.NewRecorder()
	SendPaginated(recorder, nethttp.StatusOK, []string{"a"}, map[string]any{"hasMore": true})
	body := decodeBody(t, recorder)
	assert.NotNil(t, body["data"])
	assert.NotNil(t, body["meta"])

	recorder = httptest.NewRecorder()
	SendError(recorder, nethttp.StatusBadRequest, "BAD", "invalid")
	body = decodeBody(t, recorder)
	assert.Equal(t, false, body["success"])
	assert.Equal(t, "BAD", body["error"].(map[string]any)["code"])
}
