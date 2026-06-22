//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
)

type productDayHTTP struct {
	Date         string `json:"date"`
	OrdersCount  int64  `json:"ordersCount"`
	UnitsSold    int64  `json:"unitsSold"`
	RevenueMinor int64  `json:"revenueMinor"`
	Currency     string `json:"currency"`
}

func TestProductAnalyticsHTTPCompleteness(t *testing.T) {
	app := setupIntegrationApp(t)
	app.resetDatabase(t)
	seedProductRows(t, app,
		bson.M{"product_id": int64(70), "date": "2026-06-01", "currency": "EGP", "orders_count": int64(2), "units_sold": int64(5), "revenue_sum": int64(2500)},
	)
	path := "/api/v1/analytics/products/70/days?from=2026-06-01&to=2026-06-30"
	response := app.get(t, path, ownerToken(t, 10, "product_reader"))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, []productDayHTTP{{
		Date: "2026-06-01", OrdersCount: 2, UnitsSold: 5, RevenueMinor: 2500, Currency: "EGP",
	}}, decodeSuccess[[]productDayHTTP](t, response).Data)

	app.core.setPermissions("product_denied", "core:branch:read")
	assertErrorResponse(t, app.get(t, path, ownerToken(t, 10, "product_denied")), http.StatusForbidden, "FORBIDDEN")
	assertErrorResponse(t, app.get(t, "/api/v1/analytics/products/0/days?from=2026-06-01&to=2026-06-30", adminToken(t)), http.StatusBadRequest, "VALIDATION_ERROR")
}
