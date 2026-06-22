package dto

import "github.com/zrafat80/quickbite/analytics-service/app/analytics"

type BranchDayResponse struct {
	Date          string `json:"date"`
	OrdersCount   int64  `json:"ordersCount"`
	RevenueMinor  int64  `json:"revenueMinor"`
	Currency      string `json:"currency"`
	AvgOrderMinor int64  `json:"avgOrderMinor"`
}

func FromBranchRow(r analytics.BranchDayRow) BranchDayResponse {
	return BranchDayResponse{
		Date:          r.Date,
		OrdersCount:   r.OrdersCount,
		RevenueMinor:  r.RevenueMinor,
		Currency:      r.Currency,
		AvgOrderMinor: r.AvgOrderMinor,
	}
}

func FromBranchRows(rows []analytics.BranchDayRow) []BranchDayResponse {
	out := make([]BranchDayResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, FromBranchRow(r))
	}
	return out
}
