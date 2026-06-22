package repository

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/zrafat80/quickbite/analytics-service/app/analytics/entity"
)

type AggBranchDayRepo struct {
	coll *mongo.Collection
}

func NewAggBranchDayRepo(db *mongo.Database) *AggBranchDayRepo {
	return &AggBranchDayRepo{coll: db.Collection(CollectionAggBranchDay)}
}

func (r *AggBranchDayRepo) IncrementOrderRow(
	ctx context.Context,
	branchID, restaurantID int64,
	date, currency string,
	revenueMinor int64,
) error {
	filter := bson.M{"branch_id": branchID, "date": date}
	update := bson.M{
		"$inc":         bson.M{"orders_count": 1, "revenue_sum": revenueMinor},
		"$setOnInsert": bson.M{"currency": currency, "restaurant_id": restaurantID},
		"$set":         bson.M{"updated_at": time.Now().UTC()},
	}
	_, err := r.coll.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

func (r *AggBranchDayRepo) IncrementRejectedRow(
	ctx context.Context,
	branchID, restaurantID int64,
	date, currency string,
) error {
	filter := bson.M{"branch_id": branchID, "date": date}
	update := bson.M{
		"$inc":         bson.M{"rejected_count": 1},
		"$setOnInsert": bson.M{"currency": currency, "restaurant_id": restaurantID},
		"$set":         bson.M{"updated_at": time.Now().UTC()},
	}
	_, err := r.coll.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

func (r *AggBranchDayRepo) AddDelivery(
	ctx context.Context,
	branchID, restaurantID int64,
	date, currency string,
	deliveryMs int64,
) error {
	filter := bson.M{"branch_id": branchID, "date": date}
	update := bson.M{
		"$inc":         bson.M{"delivery_ms_sum": deliveryMs, "delivery_ms_count": 1},
		"$setOnInsert": bson.M{"currency": currency, "restaurant_id": restaurantID},
		"$set":         bson.M{"updated_at": time.Now().UTC()},
	}
	_, err := r.coll.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

// FindRestaurantOfBranch returns the restaurant_id this branch's aggregate
// rows are tagged with. Returns (0, nil) when no rows exist yet — the
// caller decides what that means (analytics treats "no rows" as "no data
// to leak", which is safe for the tenant check).
func (r *AggBranchDayRepo) FindRestaurantOfBranch(ctx context.Context, branchID int64) (int64, error) {
	var row entity.AggBranchDay
	err := r.coll.FindOne(ctx,
		bson.M{"branch_id": branchID},
		options.FindOne().SetProjection(bson.M{"restaurant_id": 1}),
	).Decode(&row)
	if err == mongo.ErrNoDocuments {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return row.RestaurantID, nil
}

func (r *AggBranchDayRepo) FindByBranchInRange(
	ctx context.Context,
	branchID int64,
	from, to string,
) ([]entity.AggBranchDay, error) {
	filter := bson.M{
		"branch_id": branchID,
		"date":      bson.M{"$gte": from, "$lte": to},
	}
	opts := options.Find().SetSort(bson.D{{Key: "date", Value: 1}})
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]entity.AggBranchDay, 0)
	for cur.Next(ctx) {
		var row entity.AggBranchDay
		if err := cur.Decode(&row); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, cur.Err()
}
