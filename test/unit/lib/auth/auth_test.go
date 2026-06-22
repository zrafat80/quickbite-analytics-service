package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zrafat80/quickbite/analytics-service/lib/appcontext"
	. "github.com/zrafat80/quickbite/analytics-service/lib/auth"
)

const unitSecret = "unit-test-secret"

func signToken(t *testing.T, claims jwt.MapClaims, method jwt.SigningMethod) string {
	t.Helper()
	token := jwt.NewWithClaims(method, claims)
	signed, err := token.SignedString([]byte(unitSecret))
	require.NoError(t, err)
	return signed
}

func validClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"userId":         float64(7),
		"email":          "user@example.com",
		"role":           "restaurant_user",
		"restaurantId":   "12",
		"restaurantRole": "owner",
		"branchIds":      []any{"20", float64(21), "invalid"},
		"exp":            time.Now().Add(time.Hour).Unix(),
	}
}

func TestVerifierAcceptsNodeAndGoNumericClaimShapes(t *testing.T) {
	claims, err := NewVerifier(unitSecret).Verify(signToken(t, validClaims(), jwt.SigningMethodHS256))
	require.NoError(t, err)
	assert.Equal(t, int64(7), claims.UserID)
	require.NotNil(t, claims.RestaurantID)
	assert.Equal(t, int64(12), *claims.RestaurantID)
	assert.Equal(t, []int64{20, 21}, claims.BranchIDs)
	assert.Equal(t, "owner", *claims.RestaurantRole)
}

func TestVerifierRejectsInvalidTokens(t *testing.T) {
	verifier := NewVerifier(unitSecret)
	_, err := verifier.Verify("not-a-token")
	require.Error(t, err)

	expired := validClaims()
	expired["exp"] = time.Now().Add(-time.Hour).Unix()
	_, err = verifier.Verify(signToken(t, expired, jwt.SigningMethodHS256))
	require.Error(t, err)

	missing := validClaims()
	delete(missing, "userId")
	_, err = verifier.Verify(signToken(t, missing, jwt.SigningMethodHS256))
	require.Error(t, err)

	wrongSecret := jwt.NewWithClaims(jwt.SigningMethodHS256, validClaims())
	signed, signErr := wrongSecret.SignedString([]byte("wrong"))
	require.NoError(t, signErr)
	_, err = verifier.Verify(signed)
	require.Error(t, err)

	none := jwt.NewWithClaims(jwt.SigningMethodNone, validClaims())
	noneToken, signErr := none.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, signErr)
	_, err = verifier.Verify(noneToken)
	require.Error(t, err)
}

func TestRequireAuthenticationSourcesAndPrecedence(t *testing.T) {
	verifier := NewVerifier(unitSecret)
	valid := signToken(t, validClaims(), jwt.SigningMethodHS256)
	handler := Require(verifier)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := appcontext.ClaimsFrom(r.Context())
		require.True(t, ok)
		_ = json.NewEncoder(w).Encode(claims)
	}))

	for _, test := range []struct {
		name    string
		prepare func(*http.Request)
		status  int
	}{
		{"bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+valid) }, http.StatusOK},
		{"cookie", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "access_token", Value: valid}) }, http.StatusOK},
		{"missing", func(*http.Request) {}, http.StatusUnauthorized},
		{"bad bearer prefix", func(r *http.Request) { r.Header.Set("Authorization", "bearer "+valid) }, http.StatusUnauthorized},
		{"invalid", func(r *http.Request) { r.Header.Set("Authorization", "Bearer invalid") }, http.StatusUnauthorized},
		{"cookie wins over bearer", func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: "access_token", Value: "invalid"})
			r.Header.Set("Authorization", "Bearer "+valid)
		}, http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			test.prepare(request)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			assert.Equal(t, test.status, recorder.Code)
		})
	}
}

func TestRequireRole(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := RequireRole("system_admin", "ops")(next)

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusForbidden, recorder.Code)

	request = request.WithContext(appcontext.WithClaims(context.Background(), &appcontext.Claims{Role: "customer"}))
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusForbidden, recorder.Code)

	request = request.WithContext(appcontext.WithClaims(context.Background(), &appcontext.Claims{Role: "system_admin"}))
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestRequireInternalAPIKey(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	for _, test := range []struct {
		expected string
		actual   string
		status   int
	}{
		{"secret", "secret", http.StatusNoContent},
		{"secret", "wrong", http.StatusUnauthorized},
		{"", "", http.StatusUnauthorized},
	} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("x-api-key", test.actual)
		recorder := httptest.NewRecorder()
		RequireInternalAPIKey(test.expected)(next).ServeHTTP(recorder, request)
		assert.Equal(t, test.status, recorder.Code)
	}
}
