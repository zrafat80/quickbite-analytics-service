package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/zrafat80/quickbite/analytics-service/app/analytics"
	. "github.com/zrafat80/quickbite/analytics-service/app/analytics/dto"
)

func TestResponseFactories(t *testing.T) {
	restaurant := analytics.RestaurantDayRow{
		Date: "2026-06-01", OrdersCount: 2, RevenueMinor: 500, Currency: "EGP", AvgOrderMinor: 250,
	}
	assert.Equal(t, RestaurantDayResponse{
		Date: "2026-06-01", OrdersCount: 2, RevenueMinor: 500, Currency: "EGP", AvgOrderMinor: 250,
	}, FromRow(restaurant))
	assert.Equal(t, []RestaurantDayResponse{FromRow(restaurant)}, FromRows([]analytics.RestaurantDayRow{restaurant}))
	assert.NotNil(t, FromRows(nil))

	branch := analytics.BranchDayRow{
		Date: "2026-06-01", OrdersCount: 3, RevenueMinor: 900, Currency: "SAR", AvgOrderMinor: 300,
	}
	assert.Equal(t, BranchDayResponse{
		Date: "2026-06-01", OrdersCount: 3, RevenueMinor: 900, Currency: "SAR", AvgOrderMinor: 300,
	}, FromBranchRow(branch))
	assert.Equal(t, []BranchDayResponse{FromBranchRow(branch)}, FromBranchRows([]analytics.BranchDayRow{branch}))
	assert.NotNil(t, FromBranchRows(nil))

	product := analytics.ProductDayRow{
		Date: "2026-06-01", OrdersCount: 2, UnitsSold: 4, RevenueMinor: 1200, Currency: "EGP",
	}
	assert.Equal(t, ProductDayResponse{
		Date: "2026-06-01", OrdersCount: 2, UnitsSold: 4, RevenueMinor: 1200, Currency: "EGP",
	}, FromProductRow(product))
	assert.Equal(t, []ProductDayResponse{FromProductRow(product)}, FromProductRows([]analytics.ProductDayRow{product}))
	assert.NotNil(t, FromProductRows(nil))

	platform := analytics.PlatformDayRow{
		Date: "2026-06-01", Currency: "EGP", OrdersCount: 5, RevenueMinor: 2500, AvgOrderMinor: 500,
	}
	assert.Equal(t, PlatformDayResponse{
		Date: "2026-06-01", Currency: "EGP", OrdersCount: 5, RevenueMinor: 2500, AvgOrderMinor: 500,
	}, FromPlatformRow(platform))
	assert.Equal(t, []PlatformDayResponse{FromPlatformRow(platform)}, FromPlatformRows([]analytics.PlatformDayRow{platform}))
	assert.NotNil(t, FromPlatformRows(nil))

	failure := analytics.FailureRow{Date: "2026-06-01", OrdersCount: 4, RejectedCount: 1, FailureRate: 0.25}
	assert.Equal(t, FailureResponse{
		Date: "2026-06-01", OrdersCount: 4, RejectedCount: 1, FailureRate: 0.25,
	}, FromFailureRow(failure))
	assert.Equal(t, []FailureResponse{FromFailureRow(failure)}, FromFailureRows([]analytics.FailureRow{failure}))
	assert.NotNil(t, FromFailureRows(nil))

	delivery := analytics.DeliveryAvgRow{Date: "2026-06-01", DeliveriesCounted: 2, AvgDeliveryMs: 900}
	assert.Equal(t, DeliveryAvgResponse{
		Date: "2026-06-01", DeliveriesCounted: 2, AvgDeliveryMs: 900,
	}, FromDeliveryAvgRow(delivery))
	assert.Equal(t, []DeliveryAvgResponse{FromDeliveryAvgRow(delivery)}, FromDeliveryAvgRows([]analytics.DeliveryAvgRow{delivery}))
	assert.NotNil(t, FromDeliveryAvgRows(nil))

	active := analytics.ActiveRestaurantsRow{From: "2026-06-01", To: "2026-06-30", Count: 7}
	assert.Equal(t, ActiveRestaurantsResponse{From: active.From, To: active.To, Count: 7}, FromActiveRestaurants(active))

	top := analytics.TopRestaurantRow{RestaurantID: 8, OrdersCount: 9, RevenueMinor: 4000, Currency: "EGP"}
	assert.Equal(t, TopRestaurantResponse{
		RestaurantID: 8, OrdersCount: 9, RevenueMinor: 4000, Currency: "EGP",
	}, FromTopRestaurantRow(top))
	assert.Equal(t, []TopRestaurantResponse{FromTopRestaurantRow(top)}, FromTopRestaurantRows([]analytics.TopRestaurantRow{top}))
	assert.NotNil(t, FromTopRestaurantRows(nil))
}
