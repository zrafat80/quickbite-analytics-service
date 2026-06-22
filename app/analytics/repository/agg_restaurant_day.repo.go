package repository

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/zrafat80/quickbite/analytics-service/app/analytics"
	"github.com/zrafat80/quickbite/analytics-service/app/analytics/entity"
)

// AggRestaurantDayRepo holds the only mongo-driver call sites for the
// agg_restaurant_day collection.
type AggRestaurantDayRepo struct {
	coll *mongo.Collection
}

func NewAggRestaurantDayRepo(db *mongo.Database) *AggRestaurantDayRepo {
	return &AggRestaurantDayRepo{coll: db.Collection(CollectionAggRestaurantDay)}
}

// IncrementOrderRow is called for `order.placed`. Upsert by (restaurant_id,
// date) — one Mongo round-trip regardless of whether the row exists. Shape
// merges associatively so a replay would not violate consistency, but
// dedupe prevents the replay reaching us in the first place.
func (r *AggRestaurantDayRepo) IncrementOrderRow(
	ctx context.Context,
	restaurantID int64,
	date string,
	currency string,
	revenueMinor int64,
) error {
	filter := bson.M{"restaurant_id": restaurantID, "date": date}
	update := bson.M{
		"$inc":         bson.M{"orders_count": 1, "revenue_sum": revenueMinor},
		"$setOnInsert": bson.M{"currency": currency},
		"$set":         bson.M{"updated_at": time.Now().UTC()},
	}
	_, err := r.coll.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

// IncrementRejectedRow is called for `order.rejected`. We track rejects
// independently of orders_count so a late reject never decrements a count
// that has already left the placed bucket.
func (r *AggRestaurantDayRepo) IncrementRejectedRow(
	ctx context.Context,
	restaurantID int64,
	date string,
	currency string,
) error {
	filter := bson.M{"restaurant_id": restaurantID, "date": date}
	update := bson.M{
		"$inc":         bson.M{"rejected_count": 1},
		"$setOnInsert": bson.M{"currency": currency},
		"$set":         bson.M{"updated_at": time.Now().UTC()},
	}
	_, err := r.coll.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

// AddDelivery is called for `order.delivered`. Adds to the delivery_ms
// rolling sum + count; the average is computed at read time.
func (r *AggRestaurantDayRepo) AddDelivery(
	ctx context.Context,
	restaurantID int64,
	date string,
	currency string,
	deliveryMs int64,
) error {
	filter := bson.M{"restaurant_id": restaurantID, "date": date}
	update := bson.M{
		"$inc":         bson.M{"delivery_ms_sum": deliveryMs, "delivery_ms_count": 1},
		"$setOnInsert": bson.M{"currency": currency},
		"$set":         bson.M{"updated_at": time.Now().UTC()},
	}
	_, err := r.coll.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

// FindByRestaurantInRange returns the day rows for a restaurant whose
// `date` falls in [from, to] (inclusive). Lexicographic compare is safe
// because the dates are zero-padded YYYY-MM-DD.
func (r *AggRestaurantDayRepo) FindByRestaurantInRange(
	ctx context.Context,
	restaurantID int64,
	from, to string,
) ([]entity.AggRestaurantDay, error) {
	filter := bson.M{
		"restaurant_id": restaurantID,
		"date":          bson.M{"$gte": from, "$lte": to},
	}
	opts := options.Find().SetSort(bson.D{{Key: "date", Value: 1}})
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	out := make([]entity.AggRestaurantDay, 0)
	for cur.Next(ctx) {
		var row entity.AggRestaurantDay
		if err := cur.Decode(&row); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, cur.Err()
}

// CountActiveRestaurantsInRange returns the number of distinct restaurants
// with ≥ 1 order in [from, to]. Pipeline is $match → $group → $count, all
// of which use the (date, restaurant_id) range index.
func (r *AggRestaurantDayRepo) CountActiveRestaurantsInRange(
	ctx context.Context,
	from, to string,
) (int64, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"date":         bson.M{"$gte": from, "$lte": to},
			"orders_count": bson.M{"$gt": 0},
		}}},
		{{Key: "$group", Value: bson.M{"_id": "$restaurant_id"}}},
		{{Key: "$count", Value: "n"}},
	}
	cur, err := r.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return 0, err
	}
	defer cur.Close(ctx)
	if cur.Next(ctx) {
		var row struct {
			N int64 `bson:"n"`
		}
		if err := cur.Decode(&row); err != nil {
			return 0, err
		}
		return row.N, nil
	}
	return 0, cur.Err()
}

// TopRestaurantsByRevenueRow is the projection $group + $sort returns.
// Defined here (close to the query that produces it) — short-lived row
// type, not part of the public entity package.
type topRestaurantsByRevenueDocument struct {
	RestaurantID int64  `bson:"_id"`
	RevenueSum   int64  `bson:"revenue_sum"`
	OrdersCount  int64  `bson:"orders_count"`
	Currency     string `bson:"currency"`
}

// TopRestaurantsByRevenue returns the top N restaurants by revenue across
// the given date range. Currency is taken from the first row encountered —
// callers wanting per-currency tops should narrow with $match upfront.
func (r *AggRestaurantDayRepo) TopRestaurantsByRevenue(
	ctx context.Context,
	from, to string,
	limit int64,
) ([]analytics.TopRestaurantsByRevenueRow, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"date": bson.M{"$gte": from, "$lte": to}}}},
		{{Key: "$group", Value: bson.M{
			"_id":          "$restaurant_id",
			"revenue_sum":  bson.M{"$sum": "$revenue_sum"},
			"orders_count": bson.M{"$sum": "$orders_count"},
			"currency":     bson.M{"$first": "$currency"},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "revenue_sum", Value: -1}}}},
		{{Key: "$limit", Value: limit}},
	}
	cur, err := r.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]analytics.TopRestaurantsByRevenueRow, 0, limit)
	for cur.Next(ctx) {
		var row topRestaurantsByRevenueDocument
		if err := cur.Decode(&row); err != nil {
			return nil, err
		}
		out = append(out, analytics.TopRestaurantsByRevenueRow{
			RestaurantID: row.RestaurantID,
			RevenueSum:   row.RevenueSum,
			OrdersCount:  row.OrdersCount,
			Currency:     row.Currency,
		})
	}
	return out, cur.Err()
}
