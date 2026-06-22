package dto

import "github.com/zrafat80/quickbite/analytics-service/app/analytics"

// RestaurantDayResponse is the wire shape. Money fields are integer minor
// units (CLAUDE.md §6); `avgOrderMinor` is derived in the service layer.
type RestaurantDayResponse struct {
	Date          string `json:"date"`
	OrdersCount   int64  `json:"ordersCount"`
	RevenueMinor  int64  `json:"revenueMinor"`
	Currency      string `json:"currency"`
	AvgOrderMinor int64  `json:"avgOrderMinor"`
}

// FromRow builds the wire DTO from a service-layer row. We never return raw
// entity fields; everything flows through this conversion.
func FromRow(r analytics.RestaurantDayRow) RestaurantDayResponse {
	return RestaurantDayResponse{
		Date:          r.Date,
		OrdersCount:   r.OrdersCount,
		RevenueMinor:  r.RevenueMinor,
		Currency:      r.Currency,
		AvgOrderMinor: r.AvgOrderMinor,
	}
}

func FromRows(rows []analytics.RestaurantDayRow) []RestaurantDayResponse {
	out := make([]RestaurantDayResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, FromRow(r))
	}
	return out
}
