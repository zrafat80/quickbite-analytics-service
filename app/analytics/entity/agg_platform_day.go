package entity

import "time"

// AggPlatformDay is the single-row-per-(date, currency) global rollup.
// Currency is part of the key (not just a label) because multi-region means
// a single date can carry orders in EGP, SAR, etc.; mixing them in one row
// would lose meaning.
//
// Active-restaurant count is NOT a field here — it's computed at read time
// from agg_restaurant_day via an aggregation pipeline, because incrementing
// it per event would double-count restaurants that placed multiple orders.
type AggPlatformDay struct {
	Date            string    `bson:"date"`
	Currency        string    `bson:"currency"`
	OrdersCount     int64     `bson:"orders_count"`
	RevenueSum      int64     `bson:"revenue_sum"`
	RejectedCount   int64     `bson:"rejected_count"`
	DeliveryMsSum   int64     `bson:"delivery_ms_sum"`
	DeliveryMsCount int64     `bson:"delivery_ms_count"`
	UpdatedAt       time.Time `bson:"updated_at"`
}
