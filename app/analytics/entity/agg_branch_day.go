package entity

import "time"

// AggBranchDay mirrors AggRestaurantDay but rolled per branch. A restaurant
// with N branches yields N rows per day. RestaurantID is denormalized so a
// "branches under restaurant X" read can be served by one query.
type AggBranchDay struct {
	BranchID        int64     `bson:"branch_id"`
	RestaurantID    int64     `bson:"restaurant_id"`
	Date            string    `bson:"date"`
	Currency        string    `bson:"currency"`
	OrdersCount     int64     `bson:"orders_count"`
	RevenueSum      int64     `bson:"revenue_sum"`
	RejectedCount   int64     `bson:"rejected_count"`
	DeliveryMsSum   int64     `bson:"delivery_ms_sum"`
	DeliveryMsCount int64     `bson:"delivery_ms_count"`
	UpdatedAt       time.Time `bson:"updated_at"`
}
