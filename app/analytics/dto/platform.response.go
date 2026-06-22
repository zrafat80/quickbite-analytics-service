package dto

import "github.com/zrafat80/quickbite/analytics-service/app/analytics"

type PlatformDayResponse struct {
	Date          string `json:"date"`
	Currency      string `json:"currency"`
	OrdersCount   int64  `json:"ordersCount"`
	RevenueMinor  int64  `json:"revenueMinor"`
	AvgOrderMinor int64  `json:"avgOrderMinor"`
}

func FromPlatformRow(r analytics.PlatformDayRow) PlatformDayResponse {
	return PlatformDayResponse{
		Date:          r.Date,
		Currency:      r.Currency,
		OrdersCount:   r.OrdersCount,
		RevenueMinor:  r.RevenueMinor,
		AvgOrderMinor: r.AvgOrderMinor,
	}
}

func FromPlatformRows(rows []analytics.PlatformDayRow) []PlatformDayResponse {
	out := make([]PlatformDayResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, FromPlatformRow(r))
	}
	return out
}

type ActiveRestaurantsResponse struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Count int64  `json:"count"`
}

func FromActiveRestaurants(r analytics.ActiveRestaurantsRow) ActiveRestaurantsResponse {
	return ActiveRestaurantsResponse{From: r.From, To: r.To, Count: r.Count}
}

type TopRestaurantResponse struct {
	RestaurantID int64  `json:"restaurantId"`
	OrdersCount  int64  `json:"ordersCount"`
	RevenueMinor int64  `json:"revenueMinor"`
	Currency     string `json:"currency"`
}

func FromTopRestaurantRow(r analytics.TopRestaurantRow) TopRestaurantResponse {
	return TopRestaurantResponse{
		RestaurantID: r.RestaurantID,
		OrdersCount:  r.OrdersCount,
		RevenueMinor: r.RevenueMinor,
		Currency:     r.Currency,
	}
}

func FromTopRestaurantRows(rows []analytics.TopRestaurantRow) []TopRestaurantResponse {
	out := make([]TopRestaurantResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, FromTopRestaurantRow(r))
	}
	return out
}
