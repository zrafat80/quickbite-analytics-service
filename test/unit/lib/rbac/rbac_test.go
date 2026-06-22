package rbac

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zrafat80/quickbite/analytics-service/lib/appcontext"
	. "github.com/zrafat80/quickbite/analytics-service/lib/rbac"
)

type permissionSourceFake struct {
	mu          sync.Mutex
	permissions []string
	err         error
	calls       int
}

func (f *permissionSourceFake) GetRolePermissions(context.Context, string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return append([]string(nil), f.permissions...), f.err
}

func (f *permissionSourceFake) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestCacheHitExpiryAndInvalidation(t *testing.T) {
	source := &permissionSourceFake{permissions: []string{"a:b", "c:d"}}
	cache := NewCache(source, 15*time.Millisecond)

	has, err := cache.Has(context.Background(), "owner", "a:b")
	require.NoError(t, err)
	assert.True(t, has)
	has, err = cache.Has(context.Background(), "owner", "missing")
	require.NoError(t, err)
	assert.False(t, has)
	assert.Equal(t, 1, source.callCount())

	cache.Invalidate("owner")
	_, err = cache.GetForRole(context.Background(), "owner")
	require.NoError(t, err)
	assert.Equal(t, 2, source.callCount())

	time.Sleep(20 * time.Millisecond)
	_, err = cache.GetForRole(context.Background(), "owner")
	require.NoError(t, err)
	assert.Equal(t, 3, source.callCount())
}

func TestCacheDoesNotStoreFailures(t *testing.T) {
	source := &permissionSourceFake{err: errors.New("core down")}
	cache := NewCache(source, time.Minute)
	_, err := cache.GetForRole(context.Background(), "owner")
	require.Error(t, err)
	_, err = cache.GetForRole(context.Background(), "owner")
	require.Error(t, err)
	assert.Equal(t, 2, source.callCount())
}

func TestRequirePermissionMatrix(t *testing.T) {
	source := &permissionSourceFake{permissions: []string{"core:branch:read"}}
	cache := NewCache(source, time.Minute)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := Require(cache, "core:branch:read")(next)

	role := "owner"
	cases := []struct {
		name   string
		claims *appcontext.Claims
		status int
	}{
		{"missing claims", nil, http.StatusForbidden},
		{"missing role", &appcontext.Claims{}, http.StatusForbidden},
		{"system admin", &appcontext.Claims{Role: RoleSystemAdmin}, http.StatusNoContent},
		{"customer", &appcontext.Claims{Role: "customer"}, http.StatusForbidden},
		{"missing restaurant role", &appcontext.Claims{Role: RoleRestaurantUser}, http.StatusForbidden},
		{"allowed", &appcontext.Claims{Role: RoleRestaurantUser, RestaurantRole: &role}, http.StatusNoContent},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			if test.claims != nil {
				request = request.WithContext(appcontext.WithClaims(request.Context(), test.claims))
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			assert.Equal(t, test.status, recorder.Code)
		})
	}

	source.permissions = nil
	cache.Invalidate(role)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request = request.WithContext(appcontext.WithClaims(request.Context(), &appcontext.Claims{
		Role: RoleRestaurantUser, RestaurantRole: &role,
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusForbidden, recorder.Code)

	source.err = errors.New("core down")
	cache.Invalidate(role)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusBadGateway, recorder.Code)
}
