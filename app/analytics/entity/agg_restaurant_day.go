// Package entity — plain Go structs with `bson` tags, the Go analogue of
// the Node entity classes (which were plain classes with no decorators).
// No DB knowledge, no methods beyond simple invariants.
package entity

import "time"

// AggRestaurantDay is the on-disk shape of one row in `agg_restaurant_day`.
// Averages are stored as sum + count so concurrent replays merge associatively.
// rejected_count is independent of orders_count so a late reject never
// negates a counted order.
type AggRestaurantDay struct {
	RestaurantID    int64     `bson:"restaurant_id"`
	Date            string    `bson:"date"` // YYYY-MM-DD UTC
	Currency        string    `bson:"currency"`
	OrdersCount     int64     `bson:"orders_count"`
	RevenueSum      int64     `bson:"revenue_sum"`
	RejectedCount   int64     `bson:"rejected_count"`
	DeliveryMsSum   int64     `bson:"delivery_ms_sum"`
	DeliveryMsCount int64     `bson:"delivery_ms_count"`
	UpdatedAt       time.Time `bson:"updated_at"`
}
