//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"

	"github.com/zrafat80/quickbite/analytics-service/app/analytics/entity"
	"github.com/zrafat80/quickbite/analytics-service/app/analytics/eventhandlers"
	"github.com/zrafat80/quickbite/analytics-service/lib/coreevents"
	"github.com/zrafat80/quickbite/analytics-service/pkg/messaging"
)

type invalidationProbe struct {
	mu    sync.Mutex
	roles []string
}

func (p *invalidationProbe) Invalidate(role string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.roles = append(p.roles, role)
}

func (p *invalidationProbe) contains(role string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, current := range p.roles {
		if current == role {
			return true
		}
	}
	return false
}

func TestRabbitEventConsumersEndToEnd(t *testing.T) {
	app := setupIntegrationApp(t)
	app.resetDatabase(t)

	rabbitURL := getenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
	suffix := uuid.NewString()
	orderTopology := messaging.ConsumerOptions{
		Exchange:           "analytics-test-order-" + suffix,
		Queue:              "analytics-test-order-queue-" + suffix,
		BindingKeys:        []string{"order.#", "payment.#"},
		DeadLetterExchange: "analytics-test-order-dlx-" + suffix,
		DeadLetterQueue:    "analytics-test-order-dlq-" + suffix,
		Prefetch:           4,
	}
	coreTopology := messaging.ConsumerOptions{
		Exchange:           "analytics-test-core-" + suffix,
		Queue:              "analytics-test-core-queue-" + suffix,
		BindingKeys:        []string{"rbac.#"},
		DeadLetterExchange: "analytics-test-core-dlx-" + suffix,
		DeadLetterQueue:    "analytics-test-core-dlq-" + suffix,
		Prefetch:           4,
	}

	broker := messaging.NewAMQPBroker(messaging.Config{URL: rabbitURL})
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, broker.Connect(ctx))
	t.Cleanup(func() {
		cancel()
		_ = broker.Close(context.Background())
		deleteRabbitTopology(t, rabbitURL, orderTopology)
		deleteRabbitTopology(t, rabbitURL, coreTopology)
	})

	orderConsumer := coreevents.NewConsumer(broker, app.eventIDsRepo, orderTopology)
	for eventType, handler := range eventhandlers.Build(app.analyticsService) {
		orderConsumer.Register(eventType, handler)
	}
	orderConsumer.Register("order.transient", func(context.Context, coreevents.Envelope) error {
		return errors.New("temporary mongo failure")
	})
	require.NoError(t, orderConsumer.Start(ctx))

	probe := &invalidationProbe{}
	coreConsumer := coreevents.NewConsumer(broker, app.eventIDsRepo, coreTopology)
	for eventType, handler := range coreevents.BuildRBACHandlers(probe) {
		coreConsumer.Register(eventType, handler)
	}
	require.NoError(t, coreConsumer.Start(ctx))

	placedPayload := map[string]any{
		"orderId": "order-1", "restaurantId": 10, "branchId": 20,
		"countryCode": "EG", "currency": "EGP", "total": 1500,
		"placedAt": "2026-06-10T10:00:00Z",
		"items": []map[string]any{
			{"productId": 100, "quantity": 1, "lineTotal": 500},
			{"productId": 100, "quantity": 2, "lineTotal": 1000},
		},
	}
	publishEnvelope(t, broker, orderTopology.Exchange, "order.placed", "placed-1", "order.placed", placedPayload)
	waitFor(t, func() bool {
		return mustCount(t, app.restaurantCollection, bson.M{"restaurant_id": int64(10)}) == 1 &&
			mustCount(t, app.branchCollection, bson.M{"branch_id": int64(20)}) == 1 &&
			mustCount(t, app.productCollection, bson.M{"product_id": int64(100)}) == 1 &&
			mustCount(t, app.platformCollection, bson.M{"date": "2026-06-10", "currency": "EGP"}) == 1
	})

	var restaurant entity.AggRestaurantDay
	require.NoError(t, app.restaurantCollection.FindOne(context.Background(), bson.M{
		"restaurant_id": int64(10), "date": "2026-06-10",
	}).Decode(&restaurant))
	assert.Equal(t, int64(1), restaurant.OrdersCount)
	assert.Equal(t, int64(1500), restaurant.RevenueSum)

	var branch entity.AggBranchDay
	require.NoError(t, app.branchCollection.FindOne(context.Background(), bson.M{
		"branch_id": int64(20), "date": "2026-06-10",
	}).Decode(&branch))
	assert.Equal(t, int64(10), branch.RestaurantID)
	assert.Equal(t, int64(1), branch.OrdersCount)
	assert.Equal(t, int64(1500), branch.RevenueSum)

	var product entity.AggProductDay
	require.NoError(t, app.productCollection.FindOne(context.Background(), bson.M{
		"product_id": int64(100), "date": "2026-06-10",
	}).Decode(&product))
	assert.Equal(t, int64(1), product.OrdersCount, "duplicate product lines count as one order")
	assert.Equal(t, int64(3), product.UnitsSold)
	assert.Equal(t, int64(1500), product.RevenueSum)

	var platform entity.AggPlatformDay
	require.NoError(t, app.platformCollection.FindOne(context.Background(), bson.M{
		"date": "2026-06-10", "currency": "EGP",
	}).Decode(&platform))
	assert.Equal(t, int64(1), platform.OrdersCount)
	assert.Equal(t, int64(1500), platform.RevenueSum)

	publishEnvelope(t, broker, orderTopology.Exchange, "order.placed", "placed-1", "order.placed", placedPayload)
	time.Sleep(150 * time.Millisecond)
	require.NoError(t, app.restaurantCollection.FindOne(context.Background(), bson.M{
		"restaurant_id": int64(10), "date": "2026-06-10",
	}).Decode(&restaurant))
	assert.Equal(t, int64(1), restaurant.OrdersCount, "duplicate event IDs must be ack-skipped")
	require.NoError(t, app.branchCollection.FindOne(context.Background(), bson.M{
		"branch_id": int64(20), "date": "2026-06-10",
	}).Decode(&branch))
	assert.Equal(t, int64(1), branch.OrdersCount, "duplicate event IDs must not reach branch aggregates")
	require.NoError(t, app.productCollection.FindOne(context.Background(), bson.M{
		"product_id": int64(100), "date": "2026-06-10",
	}).Decode(&product))
	assert.Equal(t, int64(1), product.OrdersCount, "duplicate event IDs must not reach product aggregates")
	assert.Equal(t, int64(3), product.UnitsSold)
	require.NoError(t, app.platformCollection.FindOne(context.Background(), bson.M{
		"date": "2026-06-10", "currency": "EGP",
	}).Decode(&platform))
	assert.Equal(t, int64(1), platform.OrdersCount, "duplicate event IDs must not reach platform aggregates")

	publishEnvelope(t, broker, orderTopology.Exchange, "order.rejected", "rejected-1", "order.rejected", map[string]any{
		"orderId": "order-1", "restaurantId": 10, "branchId": 20,
		"currency": "EGP", "rejectedAt": "2026-06-10T10:05:00Z",
	})
	publishEnvelope(t, broker, orderTopology.Exchange, "order.delivered", "delivered-1", "order.delivered", map[string]any{
		"orderId": "order-1", "restaurantId": 10, "branchId": 20, "currency": "EGP",
		"placedAt": "2026-06-10T10:00:00Z", "deliveredAt": "2026-06-10T10:30:00Z",
	})
	publishEnvelope(t, broker, orderTopology.Exchange, "payment.completed", "payment-1", "payment.completed", map[string]any{
		"orderId": "order-2", "restaurantId": 11, "branchId": 21, "currency": "SAR",
		"total": 700, "completedAt": "2026-06-11T10:00:00Z",
		"items": []map[string]any{{"productId": 101, "quantity": 1, "lineTotal": 700}},
	})
	waitFor(t, func() bool {
		var restaurantRow entity.AggRestaurantDay
		restaurantErr := app.restaurantCollection.FindOne(context.Background(), bson.M{
			"restaurant_id": int64(10), "date": "2026-06-10",
		}).Decode(&restaurantRow)
		var branchRow entity.AggBranchDay
		branchErr := app.branchCollection.FindOne(context.Background(), bson.M{
			"branch_id": int64(20), "date": "2026-06-10",
		}).Decode(&branchRow)
		var platformRow entity.AggPlatformDay
		platformErr := app.platformCollection.FindOne(context.Background(), bson.M{
			"date": "2026-06-10", "currency": "EGP",
		}).Decode(&platformRow)
		return restaurantErr == nil && restaurantRow.RejectedCount == 1 && restaurantRow.DeliveryMsCount == 1 &&
			branchErr == nil && branchRow.RejectedCount == 1 && branchRow.DeliveryMsCount == 1 &&
			platformErr == nil && platformRow.RejectedCount == 1 && platformRow.DeliveryMsCount == 1 &&
			mustCount(t, app.restaurantCollection, bson.M{"restaurant_id": int64(11), "date": "2026-06-11"}) == 1 &&
			mustCount(t, app.branchCollection, bson.M{"branch_id": int64(21), "date": "2026-06-11"}) == 1 &&
			mustCount(t, app.productCollection, bson.M{"product_id": int64(101), "date": "2026-06-11"}) == 1 &&
			mustCount(t, app.platformCollection, bson.M{"date": "2026-06-11", "currency": "SAR"}) == 1
	})
	require.NoError(t, app.restaurantCollection.FindOne(context.Background(), bson.M{
		"restaurant_id": int64(10), "date": "2026-06-10",
	}).Decode(&restaurant))
	assert.Equal(t, int64(1), restaurant.RejectedCount)
	assert.Equal(t, int64(30*time.Minute/time.Millisecond), restaurant.DeliveryMsSum)
	assert.Equal(t, int64(1), restaurant.DeliveryMsCount)

	require.NoError(t, app.branchCollection.FindOne(context.Background(), bson.M{
		"branch_id": int64(20), "date": "2026-06-10",
	}).Decode(&branch))
	assert.Equal(t, int64(1), branch.RejectedCount)
	assert.Equal(t, int64(30*time.Minute/time.Millisecond), branch.DeliveryMsSum)
	assert.Equal(t, int64(1), branch.DeliveryMsCount)

	require.NoError(t, app.platformCollection.FindOne(context.Background(), bson.M{
		"date": "2026-06-10", "currency": "EGP",
	}).Decode(&platform))
	assert.Equal(t, int64(1), platform.RejectedCount)
	assert.Equal(t, int64(30*time.Minute/time.Millisecond), platform.DeliveryMsSum)
	assert.Equal(t, int64(1), platform.DeliveryMsCount)

	var paidRestaurant entity.AggRestaurantDay
	require.NoError(t, app.restaurantCollection.FindOne(context.Background(), bson.M{
		"restaurant_id": int64(11), "date": "2026-06-11",
	}).Decode(&paidRestaurant))
	assert.Equal(t, int64(1), paidRestaurant.OrdersCount)
	assert.Equal(t, int64(700), paidRestaurant.RevenueSum)

	var paidBranch entity.AggBranchDay
	require.NoError(t, app.branchCollection.FindOne(context.Background(), bson.M{
		"branch_id": int64(21), "date": "2026-06-11",
	}).Decode(&paidBranch))
	assert.Equal(t, int64(11), paidBranch.RestaurantID)
	assert.Equal(t, int64(1), paidBranch.OrdersCount)
	assert.Equal(t, int64(700), paidBranch.RevenueSum)

	var paidProduct entity.AggProductDay
	require.NoError(t, app.productCollection.FindOne(context.Background(), bson.M{
		"product_id": int64(101), "date": "2026-06-11",
	}).Decode(&paidProduct))
	assert.Equal(t, int64(1), paidProduct.OrdersCount)
	assert.Equal(t, int64(1), paidProduct.UnitsSold)
	assert.Equal(t, int64(700), paidProduct.RevenueSum)

	var paidPlatform entity.AggPlatformDay
	require.NoError(t, app.platformCollection.FindOne(context.Background(), bson.M{
		"date": "2026-06-11", "currency": "SAR",
	}).Decode(&paidPlatform))
	assert.Equal(t, int64(1), paidPlatform.OrdersCount)
	assert.Equal(t, int64(700), paidPlatform.RevenueSum)

	publishEnvelope(t, broker, orderTopology.Exchange, "order.future", "unknown-1", "order.future", map[string]any{"value": 1})
	waitFor(t, func() bool {
		return mustCount(t, app.eventIDsCollection, bson.M{"event_id": "unknown-1"}) == 1
	})

	publishEnvelope(t, broker, coreTopology.Exchange, "rbac.permissions_changed", "rbac-1", "rbac.permissions_changed", map[string]any{
		"role": "owner",
	})
	waitFor(t, func() bool { return probe.contains("owner") })

	require.NoError(t, broker.Publish(context.Background(), orderTopology.Exchange, "order.placed", []byte(`{`)))
	publishEnvelope(t, broker, orderTopology.Exchange, "order.placed", "bad-payload-1", "order.placed", map[string]any{})
	publishEnvelope(t, broker, orderTopology.Exchange, "order.transient", "transient-1", "order.transient", map[string]any{})
	waitFor(t, func() bool {
		return rabbitQueueMessageCount(t, rabbitURL, orderTopology.DeadLetterQueue) >= 3
	})
	assert.Equal(t, int64(1), mustCount(t, app.eventIDsCollection, bson.M{"event_id": "bad-payload-1"}),
		"permanent payload failures retain their dedupe marker")
	assert.Equal(t, int64(0), mustCount(t, app.eventIDsCollection, bson.M{"event_id": "transient-1"}),
		"transient handler failures release their dedupe marker for redrive")
}

func publishEnvelope(
	t *testing.T,
	broker messaging.Broker,
	exchange, routingKey, eventID, eventType string,
	payload any,
) {
	t.Helper()
	rawPayload, err := json.Marshal(payload)
	require.NoError(t, err)
	body, err := json.Marshal(coreevents.Envelope{
		EventID: eventID, EventType: eventType, OccurredAt: "2026-06-10T10:00:00Z",
		AggregateType: "order", AggregateID: eventID, Payload: rawPayload,
	})
	require.NoError(t, err)
	require.NoError(t, broker.Publish(context.Background(), exchange, routingKey, body))
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	require.FailNow(t, "condition was not met before timeout")
}

func rabbitQueueMessageCount(t *testing.T, rabbitURL, queue string) int {
	t.Helper()
	connection, err := amqp.Dial(rabbitURL)
	require.NoError(t, err)
	defer connection.Close()
	channel, err := connection.Channel()
	require.NoError(t, err)
	defer channel.Close()
	inspection, err := channel.QueueInspect(queue)
	require.NoError(t, err)
	return inspection.Messages
}

func deleteRabbitTopology(t *testing.T, rabbitURL string, topology messaging.ConsumerOptions) {
	t.Helper()
	connection, err := amqp.Dial(rabbitURL)
	if err != nil {
		t.Logf("rabbit cleanup connection failed: %v", err)
		return
	}
	defer connection.Close()
	channel, err := connection.Channel()
	if err != nil {
		t.Logf("rabbit cleanup channel failed: %v", err)
		return
	}
	defer channel.Close()
	_, _ = channel.QueueDelete(topology.Queue, false, false, false)
	_, _ = channel.QueueDelete(topology.DeadLetterQueue, false, false, false)
	_ = channel.ExchangeDelete(topology.Exchange, false, false)
	_ = channel.ExchangeDelete(topology.DeadLetterExchange, false, false)
}
