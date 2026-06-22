package dto

import "github.com/zrafat80/quickbite/analytics-service/app/analytics"

type FailureResponse struct {
	Date          string  `json:"date"`
	OrdersCount   int64   `json:"ordersCount"`
	RejectedCount int64   `json:"rejectedCount"`
	FailureRate   float64 `json:"failureRate"` // 0..1
}

func FromFailureRow(r analytics.FailureRow) FailureResponse {
	return FailureResponse{
		Date:          r.Date,
		OrdersCount:   r.OrdersCount,
		RejectedCount: r.RejectedCount,
		FailureRate:   r.FailureRate,
	}
}

func FromFailureRows(rows []analytics.FailureRow) []FailureResponse {
	out := make([]FailureResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, FromFailureRow(r))
	}
	return out
}

type DeliveryAvgResponse struct {
	Date              string `json:"date"`
	DeliveriesCounted int64  `json:"deliveriesCounted"`
	AvgDeliveryMs     int64  `json:"avgDeliveryMs"`
}

func FromDeliveryAvgRow(r analytics.DeliveryAvgRow) DeliveryAvgResponse {
	return DeliveryAvgResponse{
		Date:              r.Date,
		DeliveriesCounted: r.DeliveriesCounted,
		AvgDeliveryMs:     r.AvgDeliveryMs,
	}
}

func FromDeliveryAvgRows(rows []analytics.DeliveryAvgRow) []DeliveryAvgResponse {
	out := make([]DeliveryAvgResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, FromDeliveryAvgRow(r))
	}
	return out
}
