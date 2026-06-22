package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/zrafat80/quickbite/analytics-service/pkg/httpclient"
)

func TestDoJSONSendsAndDecodesJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "secret", r.Header.Get("x-api-key"))
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	var out struct {
		OK bool `json:"ok"`
	}
	status, err := New(Config{Timeout: time.Second, MaxRetries: 1}).DoJSON(
		context.Background(), http.MethodPost, server.URL,
		map[string]string{"x-api-key": "secret"}, map[string]string{"name": "value"}, &out,
	)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.True(t, out.OK)
}

func TestDoJSONRetriesServerFailures(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) < 3 {
			http.Error(w, "retry", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	var out map[string]bool
	status, err := New(Config{Timeout: time.Second}).DoJSON(
		context.Background(), http.MethodGet, server.URL, nil, nil, &out,
	)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, int32(3), attempts.Load())
	assert.True(t, out["ok"])
}

func TestDoJSONErrorPaths(t *testing.T) {
	client := New(Config{Timeout: 20 * time.Millisecond, MaxRetries: 1})
	_, err := client.DoJSON(context.Background(), http.MethodPost, "http://example.test", nil, func() {}, nil)
	assert.Contains(t, err.Error(), "marshal body")

	_, err = client.DoJSON(context.Background(), http.MethodGet, "://bad-url", nil, nil, nil)
	assert.Contains(t, err.Error(), "build request")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer server.Close()
	var out map[string]any
	status, err := client.DoJSON(context.Background(), http.MethodGet, server.URL, nil, nil, &out)
	assert.Equal(t, http.StatusOK, status)
	assert.Contains(t, err.Error(), "decode response")

	slow := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(100 * time.Millisecond)
	}))
	defer slow.Close()
	_, err = client.DoJSON(context.Background(), http.MethodGet, slow.URL, nil, nil, nil)
	assert.Contains(t, err.Error(), "http do")
}
