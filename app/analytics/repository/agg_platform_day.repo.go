package repository

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/zrafat80/quickbite/analytics-service/app/analytics/entity"
)

type AggPlatformDayRepo struct {
	coll *mongo.Collection
}

func NewAggPlatformDayRepo(db *mongo.Database) *AggPlatformDayRepo {
	return &AggPlatformDayRepo{coll: db.Collection(CollectionAggPlatformDay)}
}

func (r *AggPlatformDayRepo) IncrementOrderRow(
	ctx context.Context,
	date, countryCode, currency string,
	revenueMinor int64,
) error {
	filter := bson.M{"date": date, "country_code": countryCode, "currency": currency}
	update := bson.M{
		"$inc":         bson.M{"orders_count": 1, "revenue_sum": revenueMinor},
		"$setOnInsert": bson.M{"country_code": countryCode, "currency": currency},
		"$set":         bson.M{"updated_at": time.Now().UTC()},
	}
	_, err := r.coll.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

func (r *AggPlatformDayRepo) IncrementRejectedRow(
	ctx context.Context,
	date, countryCode, currency string,
) error {
	filter := bson.M{"date": date, "country_code": countryCode, "currency": currency}
	update := bson.M{
		"$inc":         bson.M{"rejected_count": 1},
		"$setOnInsert": bson.M{"country_code": countryCode, "currency": currency},
		"$set":         bson.M{"updated_at": time.Now().UTC()},
	}
	_, err := r.coll.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

func (r *AggPlatformDayRepo) AddDelivery(
	ctx context.Context,
	date, countryCode, currency string,
	deliveryMs int64,
) error {
	filter := bson.M{"date": date, "country_code": countryCode, "currency": currency}
	update := bson.M{
		"$inc":         bson.M{"delivery_ms_sum": deliveryMs, "delivery_ms_count": 1},
		"$setOnInsert": bson.M{"country_code": countryCode, "currency": currency},
		"$set":         bson.M{"updated_at": time.Now().UTC()},
	}
	_, err := r.coll.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

func (r *AggPlatformDayRepo) FindInRange(
	ctx context.Context,
	from, to string,
) ([]entity.AggPlatformDay, error) {
	filter := bson.M{"date": bson.M{"$gte": from, "$lte": to}}
	opts := options.Find().SetSort(bson.D{
		{Key: "date", Value: 1},
		{Key: "country_code", Value: 1},
		{Key: "currency", Value: 1},
	})
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]entity.AggPlatformDay, 0)
	for cur.Next(ctx) {
		var row entity.AggPlatformDay
		if err := cur.Decode(&row); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, cur.Err()
}
