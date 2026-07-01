//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/zrafat80/quickbite/analytics-service/app/analytics"
	"github.com/zrafat80/quickbite/analytics-service/app/analytics/entity"
	"github.com/zrafat80/quickbite/analytics-service/app/analytics/repository"
)

func TestAggregateWriteRepositoriesAndDedupe(t *testing.T) {
	app := setupIntegrationApp(t)
	app.resetDatabase(t)
	ctx := context.Background()

	placedAt := time.Date(2026, 6, 2, 1, 0, 0, 0, time.FixedZone("plus2", 2*60*60))
	require.NoError(t, app.analyticsService.OnOrderPlaced(ctx, analytics.OnOrderPlacedInput{
		OrderID: "order-1", RestaurantID: 10, BranchID: 20, CountryCode: "eg", Currency: "EGP", Total: 1500,
		PlacedAt: placedAt,
		Items: []analytics.OrderItemInput{
			{ProductID: 100, Quantity: 2, LineTotal: 1000},
			{ProductID: 101, Quantity: 1, LineTotal: 500},
		},
	}))
	require.NoError(t, app.analyticsService.OnOrderRejected(ctx, analytics.OnOrderRejectedInput{
		OrderID: "order-1", RestaurantID: 10, BranchID: 20, CountryCode: "eg", Currency: "EGP",
		RejectedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	}))
	require.NoError(t, app.analyticsService.OnOrderDelivered(ctx, analytics.OnOrderDeliveredInput{
		OrderID: "order-1", RestaurantID: 10, BranchID: 20, CountryCode: "eg", Currency: "EGP",
		DeliveredAt: time.Date(2026, 6, 1, 13, 0, 0, 0, time.UTC), DeliveryMs: 900,
	}))

	var restaurant entity.AggRestaurantDay
	require.NoError(t, app.restaurantCollection.FindOne(ctx, bson.M{
		"restaurant_id": int64(10), "date": "2026-06-01",
	}).Decode(&restaurant))
	assert.Equal(t, int64(1), restaurant.OrdersCount)
	assert.Equal(t, int64(1500), restaurant.RevenueSum)
	assert.Equal(t, int64(1), restaurant.RejectedCount)
	assert.Equal(t, int64(900), restaurant.DeliveryMsSum)
	assert.Equal(t, int64(1), restaurant.DeliveryMsCount)

	var branch entity.AggBranchDay
	require.NoError(t, app.branchCollection.FindOne(ctx, bson.M{
		"branch_id": int64(20), "date": "2026-06-01",
	}).Decode(&branch))
	assert.Equal(t, int64(10), branch.RestaurantID)
	assert.Equal(t, int64(1), branch.OrdersCount)
	assert.Equal(t, int64(1), branch.RejectedCount)

	var product entity.AggProductDay
	require.NoError(t, app.productCollection.FindOne(ctx, bson.M{
		"product_id": int64(100), "date": "2026-06-01",
	}).Decode(&product))
	assert.Equal(t, int64(2), product.UnitsSold)
	assert.Equal(t, int64(1000), product.RevenueSum)
	assert.Equal(t, int64(10), product.RestaurantID)

	var platform entity.AggPlatformDay
	require.NoError(t, app.platformCollection.FindOne(ctx, bson.M{
		"date": "2026-06-01", "country_code": "eg",
	}).Decode(&platform))
	assert.Equal(t, int64(1), platform.OrdersCount)
	assert.Equal(t, int64(1), platform.RejectedCount)
	assert.Equal(t, int64(900), platform.DeliveryMsSum)

	fresh, err := app.eventIDsRepo.MarkSeen(ctx, "event-1")
	require.NoError(t, err)
	assert.True(t, fresh)
	fresh, err = app.eventIDsRepo.MarkSeen(ctx, "event-1")
	require.NoError(t, err)
	assert.False(t, fresh)
	assert.Equal(t, int64(1), mustCount(t, app.eventIDsCollection, bson.M{"event_id": "event-1"}))
	require.NoError(t, app.eventIDsRepo.Forget(ctx, "event-1"))
	assert.Equal(t, int64(0), mustCount(t, app.eventIDsCollection, bson.M{"event_id": "event-1"}))
}

func TestPaymentCompletedAndEmptyProductBatch(t *testing.T) {
	app := setupIntegrationApp(t)
	app.resetDatabase(t)
	ctx := context.Background()

	require.NoError(t, app.analyticsService.OnPaymentCompleted(ctx, analytics.OnPaymentCompletedInput{
		OrderID: "online-1", RestaurantID: 11, BranchID: 21, CountryCode: "ksa", Currency: "SAR", Total: 700,
		CompletedAt: time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC),
		Items:       nil,
	}))
	assert.Equal(t, int64(1), mustCount(t, app.restaurantCollection, bson.M{"restaurant_id": int64(11)}))
	assert.Equal(t, int64(1), mustCount(t, app.branchCollection, bson.M{"branch_id": int64(21)}))
	assert.Equal(t, int64(0), mustCount(t, app.productCollection, bson.M{}))
	assert.Equal(t, int64(1), mustCount(t, app.platformCollection, bson.M{"country_code": "ksa"}))
}

func TestIndexDefinitionsAndUniqueness(t *testing.T) {
	app := setupIntegrationApp(t)
	ctx := context.Background()

	expectedNames := map[string][]string{
		repository.CollectionAggRestaurantDay: {"_id_", "uq_restaurant_date", "idx_date_restaurant"},
		repository.CollectionAggBranchDay:     {"_id_", "uq_branch_date", "idx_date_branch", "idx_restaurant_date"},
		repository.CollectionAggProductDay:    {"_id_", "uq_restaurant_product_date", "idx_restaurant_date", "idx_date_product"},
		repository.CollectionAggPlatformDay:   {"_id_", "uq_platform_date_country", "idx_country_date"},
		repository.CollectionEventIDs:         {"_id_", "uq_event_id", "ttl_received_at"},
	}
	for collection, wantNames := range expectedNames {
		specs, err := app.database.Collection(collection).Indexes().ListSpecifications(ctx)
		require.NoError(t, err)
		names := make([]string, 0, len(specs))
		for _, spec := range specs {
			names = append(names, spec.Name)
			if spec.Name == "ttl_received_at" {
				require.NotNil(t, spec.ExpireAfterSeconds)
				assert.Equal(t, int32(7*24*60*60), *spec.ExpireAfterSeconds)
			}
		}
		assert.ElementsMatch(t, wantNames, names)
	}

	seedRestaurantRows(t, app, bson.M{
		"restaurant_id": int64(1), "date": "2026-06-01", "currency": "EGP",
	})
	_, err := app.restaurantCollection.InsertOne(ctx, bson.M{
		"restaurant_id": int64(1), "date": "2026-06-01", "currency": "EGP",
	})
	require.Error(t, err)

	require.NoError(t, repository.EnsureIndexes(ctx, app.database), "index creation must be idempotent")
}

func mustCount(t *testing.T, collection *mongo.Collection, filter any) int64 {
	t.Helper()
	count, err := collection.CountDocuments(context.Background(), filter)
	require.NoError(t, err)
	return count
}
