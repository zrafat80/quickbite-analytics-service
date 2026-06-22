package dto

import "github.com/zrafat80/quickbite/analytics-service/app/analytics"

type ProductDayResponse struct {
	Date         string `json:"date"`
	OrdersCount  int64  `json:"ordersCount"`
	UnitsSold    int64  `json:"unitsSold"`
	RevenueMinor int64  `json:"revenueMinor"`
	Currency     string `json:"currency"`
}

func FromProductRow(r analytics.ProductDayRow) ProductDayResponse {
	return ProductDayResponse{
		Date:         r.Date,
		OrdersCount:  r.OrdersCount,
		UnitsSold:    r.UnitsSold,
		RevenueMinor: r.RevenueMinor,
		Currency:     r.Currency,
	}
}

func FromProductRows(rows []analytics.ProductDayRow) []ProductDayResponse {
	out := make([]ProductDayResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, FromProductRow(r))
	}
	return out
}
