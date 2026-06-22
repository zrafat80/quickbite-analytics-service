//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
)

type restaurantDayHTTP struct {
	Date          string `json:"date"`
	OrdersCount   int64  `json:"ordersCount"`
	RevenueMinor  int64  `json:"revenueMinor"`
	Currency      string `json:"currency"`
	AvgOrderMinor int64  `json:"avgOrderMinor"`
}

type failureHTTP struct {
	Date          string  `json:"date"`
	OrdersCount   int64   `json:"ordersCount"`
	RejectedCount int64   `json:"rejectedCount"`
	FailureRate   float64 `json:"failureRate"`
}

type deliveryHTTP struct {
	Date              string `json:"date"`
	DeliveriesCounted int64  `json:"deliveriesCounted"`
	AvgDeliveryMs     int64  `json:"avgDeliveryMs"`
}

func TestRestaurantAnalyticsHTTPCompleteness(t *testing.T) {
	app := setupIntegrationApp(t)
	const restaurantID = int64(100)
	path := func(endpoint string) string {
		return fmt.Sprintf("/api/v1/analytics/restaurants/%d/%s?from=2026-06-01&to=2026-06-30", restaurantID, endpoint)
	}

	t.Run("days golden path is sorted inclusive and uses response DTOs", func(t *testing.T) {
		app.resetDatabase(t)
		seedRestaurantRows(t, app,
			bson.M{"restaurant_id": restaurantID, "date": "2026-06-02", "currency": "EGP", "orders_count": int64(1), "revenue_sum": int64(900)},
			bson.M{"restaurant_id": restaurantID, "date": "2026-06-01", "currency": "EGP", "orders_count": int64(3), "revenue_sum": int64(1000)},
			bson.M{"restaurant_id": restaurantID, "date": "2026-05-31", "currency": "EGP", "orders_count": int64(9), "revenue_sum": int64(9000)},
		)
		response := app.getWithCookie(t, path("days"), ownerToken(t, restaurantID, "owner_days"), "caller-correlation")
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		assert.Equal(t, "caller-correlation", response.Header().Get("x-correlation-id"))
		assert.Equal(t, "application/json", response.Header().Get("Content-Type"))
		body := decodeSuccess[[]restaurantDayHTTP](t, response)
		assert.Equal(t, []restaurantDayHTTP{
			{Date: "2026-06-01", OrdersCount: 3, RevenueMinor: 1000, Currency: "EGP", AvgOrderMinor: 333},
			{Date: "2026-06-02", OrdersCount: 1, RevenueMinor: 900, Currency: "EGP", AvgOrderMinor: 900},
		}, body.Data)
	})

	t.Run("days validation security and empty rules", func(t *testing.T) {
		app.resetDatabase(t)
		validToken := ownerToken(t, restaurantID, "owner_validation")
		assertErrorResponse(t, app.get(t, path("days"), ""), http.StatusUnauthorized, "UNAUTHENTICATED")
		assertErrorResponse(t, app.get(t, path("days"), "garbage"), http.StatusUnauthorized, "UNAUTHENTICATED")
		assertErrorResponse(t, app.get(t, path("days"), mintToken(t, tokenSpec{
			UserID: 1, Role: "restaurant_user", RestaurantID: restaurantID, RestaurantRole: "owner", Expired: true,
		})), http.StatusUnauthorized, "UNAUTHENTICATED")
		assertErrorResponse(t, app.get(t, path("days"), mintToken(t, tokenSpec{
			UserID: 2, Role: "customer",
		})), http.StatusForbidden, "FORBIDDEN")
		assertErrorResponse(t, app.get(t, path("days"), mintToken(t, tokenSpec{
			UserID: 3, Role: "restaurant_user", RestaurantID: restaurantID,
		})), http.StatusForbidden, "FORBIDDEN")
		assertErrorResponse(t, app.get(t, "/api/v1/analytics/restaurants/bad/days?from=2026-06-01&to=2026-06-30", validToken), http.StatusBadRequest, "VALIDATION_ERROR")
		assertErrorResponse(t, app.get(t, fmt.Sprintf("/api/v1/analytics/restaurants/%d/days?to=2026-06-30", restaurantID), validToken), http.StatusBadRequest, "VALIDATION_ERROR")
		assertErrorResponse(t, app.get(t, fmt.Sprintf("/api/v1/analytics/restaurants/%d/days?from=bad-date&to=2026-06-30", restaurantID), validToken), http.StatusBadRequest, "VALIDATION_ERROR")
		assertErrorResponse(t, app.get(t, fmt.Sprintf("/api/v1/analytics/restaurants/%d/days?from=2026-07-01&to=2026-06-30", restaurantID), validToken), http.StatusBadRequest, "ANALYTICS_INVALID_DATE_RANGE")
		assertErrorResponse(t, app.get(t, path("days"), ownerToken(t, restaurantID+1, "foreign_owner")), http.StatusForbidden, "TENANT_MISMATCH")

		response := app.get(t, path("days"), validToken)
		require.Equal(t, http.StatusOK, response.Code)
		assert.Empty(t, decodeSuccess[[]restaurantDayHTTP](t, response).Data)
		require.NotEmpty(t, response.Header().Get("x-correlation-id"))
	})

	t.Run("system admin can inspect a restaurant aggregate", func(t *testing.T) {
		app.resetDatabase(t)
		response := app.get(t, path("days"), adminToken(t))
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	})

	t.Run("permission denial and core dependency failure are distinguished", func(t *testing.T) {
		app.resetDatabase(t)
		app.core.setPermissions("denied_restaurant")
		assertErrorResponse(t, app.get(t, path("days"), ownerToken(t, restaurantID, "denied_restaurant")), http.StatusForbidden, "FORBIDDEN")

		app.core.setFailure(http.StatusServiceUnavailable, false)
		assertErrorResponse(t, app.get(t, path("days"), ownerToken(t, restaurantID, "core_failure_role")), http.StatusBadGateway, "RBAC_LOOKUP_FAILED")
		assert.GreaterOrEqual(t, app.core.callCount("core_failure_role"), 1)

		app.core.setFailure(http.StatusOK, true)
		assertErrorResponse(t, app.get(t, path("days"), ownerToken(t, restaurantID, "core_malformed_role")), http.StatusBadGateway, "RBAC_LOOKUP_FAILED")
	})

	t.Run("failures and delivery averages derive zero-safe values", func(t *testing.T) {
		app.resetDatabase(t)
		seedRestaurantRows(t, app,
			bson.M{
				"restaurant_id": restaurantID, "date": "2026-06-01", "currency": "EGP",
				"orders_count": int64(4), "revenue_sum": int64(2000), "rejected_count": int64(1),
				"delivery_ms_sum": int64(1001), "delivery_ms_count": int64(2),
			},
			bson.M{
				"restaurant_id": restaurantID, "date": "2026-06-02", "currency": "EGP",
				"orders_count": int64(0), "revenue_sum": int64(0), "rejected_count": int64(2),
				"delivery_ms_sum": int64(0), "delivery_ms_count": int64(0),
			},
		)
		token := ownerToken(t, restaurantID, "owner_derived")
		failures := app.get(t, path("failures"), token)
		require.Equal(t, http.StatusOK, failures.Code, failures.Body.String())
		assert.Equal(t, []failureHTTP{
			{Date: "2026-06-01", OrdersCount: 4, RejectedCount: 1, FailureRate: 0.25},
			{Date: "2026-06-02", OrdersCount: 0, RejectedCount: 2, FailureRate: 0},
		}, decodeSuccess[[]failureHTTP](t, failures).Data)

		delivery := app.get(t, path("delivery-avg"), token)
		require.Equal(t, http.StatusOK, delivery.Code, delivery.Body.String())
		assert.Equal(t, []deliveryHTTP{
			{Date: "2026-06-01", DeliveriesCounted: 2, AvgDeliveryMs: 500},
			{Date: "2026-06-02", DeliveriesCounted: 0, AvgDeliveryMs: 0},
		}, decodeSuccess[[]deliveryHTTP](t, delivery).Data)
	})
}
