//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
)

type branchDayResponse struct {
	Date          string `json:"date"`
	OrdersCount   int64  `json:"ordersCount"`
	RevenueMinor  int64  `json:"revenueMinor"`
	Currency      string `json:"currency"`
	AvgOrderMinor int64  `json:"avgOrderMinor"`
}

type branchDaysEnvelope struct {
	Success bool                `json:"success"`
	Data    []branchDayResponse `json:"data"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func TestBranchDaysCompletenessMatrix(t *testing.T) {
	app := setupIntegrationApp(t)

	const (
		restaurantID = int64(4101)
		branchID     = int64(5101)
		otherBranch  = int64(5102)
		emptyBranch  = int64(5103)
	)

	assignedToken := func(branchIDs ...int64) string {
		return mintToken(t, tokenSpec{
			UserID:         101,
			Role:           "restaurant_user",
			RestaurantID:   restaurantID,
			RestaurantRole: "branch_manager",
			BranchIDs:      branchIDs,
		})
	}

	type testCase struct {
		name       string
		prepare    func(*testing.T)
		path       string
		token      func() string
		wantStatus int
		assertions func(*testing.T, *httptest.ResponseRecorder, branchDaysEnvelope)
	}

	tests := []testCase{
		{
			name: "Zone 1 - Golden Path - returns seeded branch analytics",
			prepare: func(t *testing.T) {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_, err := app.branchCollection.InsertMany(ctx, []any{
					bson.M{
						"branch_id":         branchID,
						"restaurant_id":     restaurantID,
						"date":              "2026-06-01",
						"currency":          "EGP",
						"orders_count":      int64(3),
						"revenue_sum":       int64(4500),
						"rejected_count":    int64(0),
						"delivery_ms_sum":   int64(0),
						"delivery_ms_count": int64(0),
						"updated_at":        time.Now().UTC(),
					},
					bson.M{
						"branch_id":         branchID,
						"restaurant_id":     restaurantID,
						"date":              "2026-06-02",
						"currency":          "EGP",
						"orders_count":      int64(1),
						"revenue_sum":       int64(1200),
						"rejected_count":    int64(0),
						"delivery_ms_sum":   int64(0),
						"delivery_ms_count": int64(0),
						"updated_at":        time.Now().UTC(),
					},
				})
				require.NoError(t, err)
			},
			path:       fmt.Sprintf("/api/v1/analytics/branches/%d/days?from=2026-06-01&to=2026-06-02", branchID),
			token:      func() string { return assignedToken(branchID) },
			wantStatus: http.StatusOK,
			assertions: func(t *testing.T, _ *httptest.ResponseRecorder, body branchDaysEnvelope) {
				t.Helper()
				require.True(t, body.Success)
				require.Equal(t, []branchDayResponse{
					{
						Date:          "2026-06-01",
						OrdersCount:   3,
						RevenueMinor:  4500,
						Currency:      "EGP",
						AvgOrderMinor: 1500,
					},
					{
						Date:          "2026-06-02",
						OrdersCount:   1,
						RevenueMinor:  1200,
						Currency:      "EGP",
						AvgOrderMinor: 1200,
					},
				}, body.Data)

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				count, err := app.branchCollection.CountDocuments(ctx, bson.M{"branch_id": branchID})
				require.NoError(t, err)
				assert.Equal(t, int64(2), count)
			},
		},
		{
			name:       "Zone 2 - Bouncer - rejects malformed dates",
			path:       fmt.Sprintf("/api/v1/analytics/branches/%d/days?from=2026-99-01&to=2026-06-02", branchID),
			token:      func() string { return assignedToken(branchID) },
			wantStatus: http.StatusBadRequest,
			assertions: func(t *testing.T, _ *httptest.ResponseRecorder, body branchDaysEnvelope) {
				t.Helper()
				require.NotNil(t, body.Error)
				assert.Equal(t, "VALIDATION_ERROR", body.Error.Code)
			},
		},
		{
			name:       "Zone 3 - Vault - rejects an unassigned branch",
			path:       fmt.Sprintf("/api/v1/analytics/branches/%d/days?from=2026-06-01&to=2026-06-02", otherBranch),
			token:      func() string { return assignedToken(branchID) },
			wantStatus: http.StatusForbidden,
			assertions: func(t *testing.T, _ *httptest.ResponseRecorder, body branchDaysEnvelope) {
				t.Helper()
				require.NotNil(t, body.Error)
				assert.Equal(t, "TENANT_MISMATCH", body.Error.Code)
			},
		},
		{
			name:       "Zone 4 - Rulebook - returns an empty list when no events exist",
			path:       fmt.Sprintf("/api/v1/analytics/branches/%d/days?from=2026-06-01&to=2026-06-02", emptyBranch),
			token:      func() string { return assignedToken(emptyBranch) },
			wantStatus: http.StatusOK,
			assertions: func(t *testing.T, _ *httptest.ResponseRecorder, body branchDaysEnvelope) {
				t.Helper()
				require.True(t, body.Success)
				assert.Empty(t, body.Data)
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			app.resetDatabase(t)
			if test.prepare != nil {
				test.prepare(t)
			}

			response := app.get(t, test.path, test.token())
			require.Equal(t, test.wantStatus, response.Code, response.Body.String())

			var body branchDaysEnvelope
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			test.assertions(t, response, body)
		})
	}
}

func TestBranchOwnerTenantScope(t *testing.T) {
	app := setupIntegrationApp(t)
	app.resetDatabase(t)
	seedBranchRows(t, app,
		bson.M{"branch_id": int64(20), "restaurant_id": int64(10), "date": "2026-06-01", "currency": "EGP", "orders_count": int64(2), "revenue_sum": int64(600)},
		bson.M{"branch_id": int64(30), "restaurant_id": int64(11), "date": "2026-06-01", "currency": "EGP", "orders_count": int64(1), "revenue_sum": int64(500)},
	)
	ownPath := "/api/v1/analytics/branches/20/days?from=2026-06-01&to=2026-06-30"
	response := app.get(t, ownPath, ownerToken(t, 10, "owner"))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, int64(300), decodeSuccess[[]branchDayResponse](t, response).Data[0].AvgOrderMinor)

	foreignPath := "/api/v1/analytics/branches/30/days?from=2026-06-01&to=2026-06-30"
	assertErrorResponse(t, app.get(t, foreignPath, ownerToken(t, 10, "owner")), http.StatusForbidden, "TENANT_MISMATCH")

	emptyPath := "/api/v1/analytics/branches/40/days?from=2026-06-01&to=2026-06-30"
	empty := app.get(t, emptyPath, ownerToken(t, 10, "owner"))
	require.Equal(t, http.StatusOK, empty.Code)
	assert.Empty(t, decodeSuccess[[]branchDayResponse](t, empty).Data)
}
