//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
)

type platformDayHTTP struct {
	Date          string `json:"date"`
	Currency      string `json:"currency"`
	OrdersCount   int64  `json:"ordersCount"`
	RevenueMinor  int64  `json:"revenueMinor"`
	AvgOrderMinor int64  `json:"avgOrderMinor"`
}

type activeRestaurantsHTTP struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Count int64  `json:"count"`
}

type topRestaurantHTTP struct {
	RestaurantID int64  `json:"restaurantId"`
	OrdersCount  int64  `json:"ordersCount"`
	RevenueMinor int64  `json:"revenueMinor"`
	Currency     string `json:"currency"`
}

func TestPlatformAnalyticsHTTPCompleteness(t *testing.T) {
	app := setupIntegrationApp(t)
	admin := adminToken(t)

	t.Run("platform days returns sorted currency rows and is admin only", func(t *testing.T) {
		app.resetDatabase(t)
		seedPlatformRows(t, app,
			bson.M{"date": "2026-06-02", "currency": "EGP", "orders_count": int64(1), "revenue_sum": int64(100)},
			bson.M{"date": "2026-06-01", "currency": "SAR", "orders_count": int64(2), "revenue_sum": int64(501)},
			bson.M{"date": "2026-06-01", "currency": "EGP", "orders_count": int64(3), "revenue_sum": int64(900)},
		)
		path := "/api/v1/analytics/platform/days?from=2026-06-01&to=2026-06-30"
		response := app.get(t, path, admin)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		assert.Equal(t, []platformDayHTTP{
			{Date: "2026-06-01", Currency: "EGP", OrdersCount: 3, RevenueMinor: 900, AvgOrderMinor: 300},
			{Date: "2026-06-01", Currency: "SAR", OrdersCount: 2, RevenueMinor: 501, AvgOrderMinor: 250},
			{Date: "2026-06-02", Currency: "EGP", OrdersCount: 1, RevenueMinor: 100, AvgOrderMinor: 100},
		}, decodeSuccess[[]platformDayHTTP](t, response).Data)
		assertErrorResponse(t, app.get(t, path, ""), http.StatusUnauthorized, "UNAUTHENTICATED")
		assertErrorResponse(t, app.get(t, path, ownerToken(t, 10, "owner_platform")), http.StatusForbidden, "FORBIDDEN_ROLE")
	})

	t.Run("active restaurants counts distinct restaurants with orders", func(t *testing.T) {
		app.resetDatabase(t)
		seedRestaurantRows(t, app,
			bson.M{"restaurant_id": int64(1), "date": "2026-06-01", "currency": "EGP", "orders_count": int64(2), "revenue_sum": int64(100)},
			bson.M{"restaurant_id": int64(1), "date": "2026-06-02", "currency": "EGP", "orders_count": int64(1), "revenue_sum": int64(100)},
			bson.M{"restaurant_id": int64(2), "date": "2026-06-02", "currency": "EGP", "orders_count": int64(3), "revenue_sum": int64(100)},
			bson.M{"restaurant_id": int64(3), "date": "2026-06-02", "currency": "EGP", "orders_count": int64(0), "revenue_sum": int64(0)},
			bson.M{"restaurant_id": int64(4), "date": "2026-05-01", "currency": "EGP", "orders_count": int64(5), "revenue_sum": int64(100)},
		)
		path := "/api/v1/analytics/platform/active-restaurants?from=2026-06-01&to=2026-06-30"
		response := app.get(t, path, admin)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		assert.Equal(t, activeRestaurantsHTTP{From: "2026-06-01", To: "2026-06-30", Count: 2},
			decodeSuccess[activeRestaurantsHTTP](t, response).Data)
	})

	t.Run("top restaurants groups sorts limits and rejects malformed limit", func(t *testing.T) {
		app.resetDatabase(t)
		seedRestaurantRows(t, app,
			bson.M{"restaurant_id": int64(1), "date": "2026-06-01", "currency": "EGP", "orders_count": int64(1), "revenue_sum": int64(500)},
			bson.M{"restaurant_id": int64(1), "date": "2026-06-02", "currency": "EGP", "orders_count": int64(2), "revenue_sum": int64(700)},
			bson.M{"restaurant_id": int64(2), "date": "2026-06-01", "currency": "EGP", "orders_count": int64(4), "revenue_sum": int64(2000)},
		)
		path := "/api/v1/analytics/restaurants/top?from=2026-06-01&to=2026-06-30&limit=1"
		response := app.get(t, path, admin)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		assert.Equal(t, []topRestaurantHTTP{{
			RestaurantID: 2, OrdersCount: 4, RevenueMinor: 2000, Currency: "EGP",
		}}, decodeSuccess[[]topRestaurantHTTP](t, response).Data)

		assertErrorResponse(t, app.get(t,
			"/api/v1/analytics/restaurants/top?from=2026-06-01&to=2026-06-30&limit=not-a-number",
			admin,
		), http.StatusBadRequest, "VALIDATION_ERROR")
		assertErrorResponse(t, app.get(t,
			"/api/v1/analytics/restaurants/top?from=2026-06-01&to=2026-06-30&limit=101",
			admin,
		), http.StatusBadRequest, "VALIDATION_ERROR")
	})
}
