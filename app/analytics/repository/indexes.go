// Package repository owns ALL mongo-driver usage for app/analytics. The
// indexes module owns this file — pkg/mongo deliberately knows nothing
// about specific collections (CLAUDE.md layering rule).
package repository

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	CollectionAggRestaurantDay = "agg_restaurant_day"
	CollectionAggBranchDay     = "agg_branch_day"
	CollectionAggProductDay    = "agg_product_day"
	CollectionAggPlatformDay   = "agg_platform_day"
	CollectionEventIDs         = "event_ids"
)

// EnsureIndexes is the Mongo analogue of running migrations on a SQL
// service. Idempotent — Mongo silently accepts a re-create that matches the
// existing spec, and errors loudly only if it doesn't.
func EnsureIndexes(ctx context.Context, db *mongo.Database) error {
	// agg_restaurant_day: unique (restaurant_id, date) is the upsert key.
	// Range index (date, restaurant_id) supports "days in window" + top-N reads.
	if _, err := db.Collection(CollectionAggRestaurantDay).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "restaurant_id", Value: 1}, {Key: "date", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("uq_restaurant_date"),
		},
		{
			Keys:    bson.D{{Key: "date", Value: 1}, {Key: "restaurant_id", Value: 1}},
			Options: options.Index().SetName("idx_date_restaurant"),
		},
	}); err != nil {
		return err
	}

	// agg_branch_day: unique (branch_id, date). idx (restaurant_id, date)
	// supports "all branches of restaurant X in window" without scanning.
	if _, err := db.Collection(CollectionAggBranchDay).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "branch_id", Value: 1}, {Key: "date", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("uq_branch_date"),
		},
		{
			Keys:    bson.D{{Key: "restaurant_id", Value: 1}, {Key: "date", Value: 1}},
			Options: options.Index().SetName("idx_restaurant_date"),
		},
	}); err != nil {
		return err
	}

	// agg_product_day: unique (product_id, date). idx (date, product_id)
	// supports "top products in window".
	if _, err := db.Collection(CollectionAggProductDay).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "product_id", Value: 1}, {Key: "date", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("uq_product_date"),
		},
		{
			Keys:    bson.D{{Key: "date", Value: 1}, {Key: "product_id", Value: 1}},
			Options: options.Index().SetName("idx_date_product"),
		},
	}); err != nil {
		return err
	}

	// agg_platform_day: unique (date, currency). Range query is just by date.
	if _, err := db.Collection(CollectionAggPlatformDay).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "date", Value: 1}, {Key: "currency", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("uq_platform_date_currency"),
		},
	}); err != nil {
		return err
	}

	// event_ids: unique on event_id is the dedupe primitive; TTL expires
	// rows after 7 days so we don't store dedupe markers forever.
	if _, err := db.Collection(CollectionEventIDs).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "event_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("uq_event_id"),
		},
		{
			Keys: bson.D{{Key: "received_at", Value: 1}},
			Options: options.Index().
				SetName("ttl_received_at").
				SetExpireAfterSeconds(7 * 24 * 60 * 60),
		},
	}); err != nil {
		return err
	}
	return nil
}
