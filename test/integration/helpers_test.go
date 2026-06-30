//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/joho/godotenv"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/zrafat80/quickbite/analytics-service/app/analytics/controller"
	"github.com/zrafat80/quickbite/analytics-service/app/analytics/repository"
	"github.com/zrafat80/quickbite/analytics-service/app/analytics/service"
	"github.com/zrafat80/quickbite/analytics-service/lib/auth"
	"github.com/zrafat80/quickbite/analytics-service/lib/boot"
	"github.com/zrafat80/quickbite/analytics-service/lib/coreclient"
	"github.com/zrafat80/quickbite/analytics-service/lib/rbac"
	"github.com/zrafat80/quickbite/analytics-service/pkg/httpclient"
	pkgmongo "github.com/zrafat80/quickbite/analytics-service/pkg/mongo"
)

const (
	testAccessSecret   = "analytics-integration-secret"
	testInternalAPIKey = "analytics-integration-api-key"
)

type integrationApp struct {
	handler              http.Handler
	client               *mongo.Client
	database             *mongo.Database
	restaurantCollection *mongo.Collection
	branchCollection     *mongo.Collection
	productCollection    *mongo.Collection
	platformCollection   *mongo.Collection
	eventIDsCollection   *mongo.Collection
	analyticsService     *service.AnalyticsService
	eventIDsRepo         *repository.EventIDsRepo
	core                 *coreAdapter
}

type tokenSpec struct {
	UserID         int64
	Role           string
	RestaurantID   int64
	RestaurantRole string
	BranchIDs      []int64
	Expired        bool
}

type coreAdapter struct {
	server      *httptest.Server
	mu          sync.Mutex
	permissions map[string][]string
	status      int
	malformed   bool
	calls       map[string]int
}

func setupIntegrationApp(t *testing.T) *integrationApp {
	t.Helper()

	_ = godotenv.Load(filepath.Join("..", "..", ".env"))
	mongoURI := getenv("MONGO_URL", "mongodb://localhost:27017")
	databaseName := getenv("MONGO_TEST_DATABASE", "analytics_service_test")
	require.Contains(
		t,
		strings.ToLower(databaseName),
		"test",
		"refusing integration mutations on non-test database %q",
		databaseName,
	)

	client, database, err := pkgmongo.Connect(context.Background(), pkgmongo.Config{
		URI:       mongoURI,
		Database:  databaseName,
		ConnectTO: 5 * time.Second,
	})
	require.NoError(t, err, "connect to local MongoDB at %s", mongoURI)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, database.Drop(ctx))
	require.NoError(t, repository.EnsureIndexes(ctx, database))

	coreBoundary := newCoreAdapter(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	coreHTTP := httpclient.New(httpclient.Config{
		Timeout:    2 * time.Second,
		MaxRetries: 1,
	})
	coreClient := coreclient.New(coreclient.Config{
		BaseURL:        coreBoundary.server.URL,
		InternalAPIKey: testInternalAPIKey,
	}, coreHTTP)
	rbacCache := rbac.NewCache(coreClient, time.Minute)
	verifier := auth.NewVerifier(testAccessSecret)

	restaurantRepo := repository.NewAggRestaurantDayRepo(database)
	branchRepo := repository.NewAggBranchDayRepo(database)
	productRepo := repository.NewAggProductDayRepo(database)
	platformRepo := repository.NewAggPlatformDayRepo(database)
	eventIDsRepo := repository.NewEventIDsRepo(database)
	analyticsService := service.NewAnalyticsService(
		logger,
		restaurantRepo,
		branchRepo,
		productRepo,
		platformRepo,
	)
	analyticsController := controller.NewAnalyticsController(analyticsService)

	router := boot.NewRouter(logger, verifier, rbacCache, analyticsController)

	app := &integrationApp{
		handler:              router,
		client:               client,
		database:             database,
		restaurantCollection: database.Collection(repository.CollectionAggRestaurantDay),
		branchCollection:     database.Collection(repository.CollectionAggBranchDay),
		productCollection:    database.Collection(repository.CollectionAggProductDay),
		platformCollection:   database.Collection(repository.CollectionAggPlatformDay),
		eventIDsCollection:   database.Collection(repository.CollectionEventIDs),
		analyticsService:     analyticsService,
		eventIDsRepo:         eventIDsRepo,
		core:                 coreBoundary,
	}

	t.Cleanup(func() {
		coreBoundary.server.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		require.NoError(t, database.Drop(cleanupCtx))
		require.NoError(t, pkgmongo.Disconnect(cleanupCtx, client))
	})

	return app
}

func newCoreAdapter(t *testing.T) *coreAdapter {
	t.Helper()

	adapter := &coreAdapter{
		permissions: map[string][]string{},
		status:      http.StatusOK,
		calls:       map[string]int{},
	}
	adapter.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet ||
			!strings.HasPrefix(r.URL.Path, "/api/roles/") ||
			!strings.HasSuffix(r.URL.Path, "/permissions") ||
			r.Header.Get("x-api-key") != testInternalAPIKey {
			http.Error(w, "unexpected core adapter request", http.StatusBadRequest)
			return
		}

		role := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/roles/"), "/permissions")
		adapter.mu.Lock()
		adapter.calls[role]++
		status := adapter.status
		malformed := adapter.malformed
		permissions, configured := adapter.permissions[role]
		adapter.mu.Unlock()
		if !configured {
			permissions = []string{
				"core:restaurant:read",
				"core:branch:read",
				"core:product:read",
			}
		}
		if malformed {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{`))
			return
		}
		if status != http.StatusOK {
			http.Error(w, http.StatusText(status), status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"isSuccess":  true,
			"statusCode": http.StatusOK,
			"data": map[string]any{
				"role":        role,
				"permissions": permissions,
			},
		}))
	}))
	return adapter
}

func (c *coreAdapter) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.permissions = map[string][]string{}
	c.status = http.StatusOK
	c.malformed = false
	c.calls = map[string]int{}
}

func (c *coreAdapter) setPermissions(role string, permissions ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.permissions[role] = append([]string(nil), permissions...)
}

func (c *coreAdapter) setFailure(status int, malformed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status = status
	c.malformed = malformed
}

func (c *coreAdapter) callCount(role string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[role]
}

func (a *integrationApp) resetDatabase(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, collection := range []string{
		repository.CollectionAggRestaurantDay,
		repository.CollectionAggBranchDay,
		repository.CollectionAggProductDay,
		repository.CollectionAggPlatformDay,
		repository.CollectionEventIDs,
	} {
		_, err := a.database.Collection(collection).DeleteMany(ctx, bson.D{})
		require.NoError(t, err)
	}
	a.core.reset()
}

func (a *integrationApp) get(t *testing.T, path, token string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	a.handler.ServeHTTP(response, request)
	return response
}

func (a *integrationApp) getWithCookie(t *testing.T, path, token, correlationID string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		request.AddCookie(&http.Cookie{Name: "access_token", Value: token})
	}
	if correlationID != "" {
		request.Header.Set("x-correlation-id", correlationID)
	}
	response := httptest.NewRecorder()
	a.handler.ServeHTTP(response, request)
	return response
}

func mintToken(t *testing.T, spec tokenSpec) string {
	t.Helper()

	expiresAt := time.Now().Add(time.Hour)
	if spec.Expired {
		expiresAt = time.Now().Add(-time.Hour)
	}
	claims := jwt.MapClaims{
		"userId":         spec.UserID,
		"email":          "analytics-integration@example.com",
		"role":           spec.Role,
		"restaurantId":   spec.RestaurantID,
		"restaurantRole": spec.RestaurantRole,
		"branchIds":      spec.BranchIDs,
		"iat":            time.Now().Add(-time.Minute).Unix(),
		"exp":            expiresAt.Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(testAccessSecret))
	require.NoError(t, err)
	return signed
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
