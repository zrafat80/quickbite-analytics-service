package entity

import "time"

// AggProductDay rolls per product per day. UnitsSold is the sum of item
// quantities (a product sold 3 times in one order increments UnitsSold by 3
// and OrdersCount by 1). RevenueSum is the sum of line totals — already
// quantity * unit price snapshot, so don't multiply twice.
type AggProductDay struct {
	RestaurantID int64     `bson:"restaurant_id"`
	ProductID    int64     `bson:"product_id"`
	Date         string    `bson:"date"`
	Currency     string    `bson:"currency"`
	OrdersCount  int64     `bson:"orders_count"`
	UnitsSold    int64     `bson:"units_sold"`
	RevenueSum   int64     `bson:"revenue_sum"`
	UpdatedAt    time.Time `bson:"updated_at"`
}
