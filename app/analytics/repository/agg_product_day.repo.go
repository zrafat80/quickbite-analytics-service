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

type AggProductDayRepo struct {
	coll *mongo.Collection
}

func NewAggProductDayRepo(db *mongo.Database) *AggProductDayRepo {
	return &AggProductDayRepo{coll: db.Collection(CollectionAggProductDay)}
}

// BulkIncrementOrderItems is the order.placed write — one Mongo round-trip
// for the full items array. CLAUDE.md §9 #1 forbids per-iteration writes;
// looping UpdateOne over N items would be N round-trips.
func (r *AggProductDayRepo) BulkIncrementOrderItems(
	ctx context.Context,
	restaurantID int64,
	date, currency string,
	items []analytics.OrderItemInput,
) error {
	if len(items) == 0 {
		return nil
	}
	models := make([]mongo.WriteModel, 0, len(items))
	now := time.Now().UTC()
	for _, it := range items {
		filter := bson.M{"restaurant_id": restaurantID, "product_id": it.ProductID, "date": date}
		update := bson.M{
			"$inc": bson.M{
				"orders_count": 1,
				"units_sold":   it.Quantity,
				"revenue_sum":  it.LineTotal,
			},
			"$setOnInsert": bson.M{"currency": currency},
			"$set":         bson.M{"updated_at": now},
		}
		models = append(models,
			mongo.NewUpdateOneModel().
				SetFilter(filter).
				SetUpdate(update).
				SetUpsert(true),
		)
	}
	_, err := r.coll.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false))
	return err
}

func (r *AggProductDayRepo) FindByProductInRange(
	ctx context.Context,
	productID int64,
	from, to string,
) ([]entity.AggProductDay, error) {
	filter := bson.M{
		"product_id": productID,
		"date":       bson.M{"$gte": from, "$lte": to},
	}
	opts := options.Find().SetSort(bson.D{{Key: "date", Value: 1}})
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]entity.AggProductDay, 0)
	for cur.Next(ctx) {
		var row entity.AggProductDay
		if err := cur.Decode(&row); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, cur.Err()
}
