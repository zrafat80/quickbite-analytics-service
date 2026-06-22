package coreclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/zrafat80/quickbite/analytics-service/lib/coreclient"
	"github.com/zrafat80/quickbite/analytics-service/pkg/httpclient"
)

func TestGetRolePermissions(t *testing.T) {
	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		assert.Equal(t, "internal", r.Header.Get("x-api-key"))
		_, _ = w.Write([]byte(`{"isSuccess":true,"statusCode":200,"data":{"role":"owner","permissions":["a:b","","c:d"]}}`))
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, InternalAPIKey: "internal"}, httpclient.New(httpclient.Config{
		Timeout: time.Second, MaxRetries: 1,
	}))
	permissions, err := client.GetRolePermissions(context.Background(), "owner")
	require.NoError(t, err)
	assert.Equal(t, "/api/roles/owner/permissions", requestedPath)
	assert.Equal(t, []string{"a:b", "c:d"}, permissions)
}

func TestGetRolePermissionsStatusAndDecodeErrors(t *testing.T) {
	for _, test := range []struct {
		name        string
		status      int
		body        string
		wantEmpty   bool
		wantErrText string
	}{
		{"not found", http.StatusNotFound, `{}`, true, ""},
		{"forbidden", http.StatusForbidden, `{}`, false, "status=403"},
		{"malformed success", http.StatusOK, `{`, false, "decode response"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			client := New(Config{BaseURL: server.URL}, httpclient.New(httpclient.Config{Timeout: time.Second, MaxRetries: 1}))
			permissions, err := client.GetRolePermissions(context.Background(), "role")
			if test.wantErrText != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), test.wantErrText)
				return
			}
			require.NoError(t, err)
			if test.wantEmpty {
				assert.Empty(t, permissions)
			}
		})
	}
}
