package eventhandlers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zrafat80/quickbite/analytics-service/app/analytics"
	. "github.com/zrafat80/quickbite/analytics-service/app/analytics/eventhandlers"
	"github.com/zrafat80/quickbite/analytics-service/lib/coreevents"
)

type eventServiceFake struct {
	placed    []analytics.OnOrderPlacedInput
	rejected  []analytics.OnOrderRejectedInput
	delivered []analytics.OnOrderDeliveredInput
	completed []analytics.OnPaymentCompletedInput
	err       error
}

func (f *eventServiceFake) OnOrderPlaced(_ context.Context, in analytics.OnOrderPlacedInput) error {
	f.placed = append(f.placed, in)
	return f.err
}

func (f *eventServiceFake) OnOrderRejected(_ context.Context, in analytics.OnOrderRejectedInput) error {
	f.rejected = append(f.rejected, in)
	return f.err
}

func (f *eventServiceFake) OnOrderDelivered(_ context.Context, in analytics.OnOrderDeliveredInput) error {
	f.delivered = append(f.delivered, in)
	return f.err
}

func (f *eventServiceFake) OnPaymentCompleted(_ context.Context, in analytics.OnPaymentCompletedInput) error {
	f.completed = append(f.completed, in)
	return f.err
}

func envelope(t *testing.T, payload any) coreevents.Envelope {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	return coreevents.Envelope{EventID: "event-1", Payload: raw}
}

func TestBuildRegistersEverySupportedEvent(t *testing.T) {
	handlers := Build(&eventServiceFake{})
	assert.ElementsMatch(t, []string{
		analytics.EventOrderPlaced,
		analytics.EventOrderRejected,
		analytics.EventOrderDelivered,
		analytics.EventPaymentComplete,
	}, mapKeys(handlers))
}

func mapKeys(in map[string]coreevents.EventHandler) []string {
	out := make([]string, 0, len(in))
	for key := range in {
		out = append(out, key)
	}
	return out
}

func TestOrderPlacedHandlerMapsAndCoalescesItems(t *testing.T) {
	fake := &eventServiceFake{}
	handler := Build(fake)[analytics.EventOrderPlaced]
	err := handler(context.Background(), envelope(t, map[string]any{
		"orderId": "order-1", "restaurantId": 10, "branchId": 20,
		"countryCode": "EG", "currency": "EGP", "total": 1700,
		"placedAt": "2026-06-01T10:00:00+02:00",
		"items": []map[string]any{
			{"productId": 7, "quantity": 1, "lineTotal": 500},
			{"productId": 7, "quantity": 2, "lineTotal": 1000},
			{"productId": 8, "quantity": 1, "lineTotal": 200},
		},
	}))
	require.NoError(t, err)
	require.Len(t, fake.placed, 1)
	assert.Equal(t, int64(10), fake.placed[0].RestaurantID)
	assert.Equal(t, time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC), fake.placed[0].PlacedAt.UTC())
	assert.Equal(t, []analytics.OrderItemInput{
		{ProductID: 7, Quantity: 3, LineTotal: 1500},
		{ProductID: 8, Quantity: 1, LineTotal: 200},
	}, fake.placed[0].Items)
}

func TestOtherHandlersMapPayloads(t *testing.T) {
	fake := &eventServiceFake{}
	handlers := Build(fake)

	require.NoError(t, handlers[analytics.EventOrderRejected](context.Background(), envelope(t, map[string]any{
		"orderId": "order-1", "restaurantId": 10, "branchId": 20,
		"currency": "EGP", "rejectedAt": "2026-06-02T10:00:00Z",
	})))
	require.Len(t, fake.rejected, 1)
	assert.Equal(t, "2026-06-02", fake.rejected[0].RejectedAt.Format("2006-01-02"))

	require.NoError(t, handlers[analytics.EventOrderDelivered](context.Background(), envelope(t, map[string]any{
		"orderId": "order-1", "restaurantId": 10, "branchId": 20, "currency": "EGP",
		"placedAt": "2026-06-02T10:00:00Z", "deliveredAt": "2026-06-02T10:30:00Z",
	})))
	require.Len(t, fake.delivered, 1)
	assert.Equal(t, int64(30*time.Minute/time.Millisecond), fake.delivered[0].DeliveryMs)

	require.NoError(t, handlers[analytics.EventPaymentComplete](context.Background(), envelope(t, map[string]any{
		"orderId": "order-2", "restaurantId": 11, "branchId": 21,
		"currency": "SAR", "total": 800, "completedAt": "2026-06-03T10:00:00Z",
		"items": []map[string]any{{"productId": 9, "quantity": 2, "lineTotal": 800}},
	})))
	require.Len(t, fake.completed, 1)
	assert.Equal(t, int64(800), fake.completed[0].Total)
}

func TestHandlersRejectMalformedPayloadsAndPropagateServiceErrors(t *testing.T) {
	handlers := Build(&eventServiceFake{})
	for eventType, payload := range map[string]json.RawMessage{
		analytics.EventOrderPlaced:     json.RawMessage(`{`),
		analytics.EventOrderRejected:   json.RawMessage(`{`),
		analytics.EventOrderDelivered:  json.RawMessage(`{`),
		analytics.EventPaymentComplete: json.RawMessage(`{`),
	} {
		err := handlers[eventType](context.Background(), coreevents.Envelope{Payload: payload})
		require.Error(t, err, eventType)
	}

	invalidCases := []struct {
		eventType string
		payload   map[string]any
	}{
		{analytics.EventOrderPlaced, map[string]any{"orderId": "", "placedAt": "2026-06-01T00:00:00Z"}},
		{analytics.EventOrderPlaced, map[string]any{
			"orderId": "o", "restaurantId": 1, "branchId": 2, "currency": "EGP",
			"total": 1, "placedAt": "2026-06-01T00:00:00Z",
			"items": []map[string]any{{"productId": 0, "quantity": 1, "lineTotal": 1}},
		}},
		{analytics.EventOrderRejected, map[string]any{"orderId": "o", "rejectedAt": "bad"}},
		{analytics.EventOrderDelivered, map[string]any{
			"orderId": "o", "restaurantId": 1, "branchId": 2, "currency": "EGP",
			"placedAt": "bad", "deliveredAt": "2026-06-01T00:00:00Z",
		}},
		{analytics.EventPaymentComplete, map[string]any{"orderId": "o", "completedAt": "bad"}},
	}
	for _, test := range invalidCases {
		require.Error(t, handlers[test.eventType](context.Background(), envelope(t, test.payload)), test.eventType)
	}

	fake := &eventServiceFake{err: errors.New("write failed")}
	err := Build(fake)[analytics.EventOrderRejected](context.Background(), envelope(t, map[string]any{
		"orderId": "order-1", "restaurantId": 10, "branchId": 20,
		"currency": "EGP", "rejectedAt": "2026-06-02T10:00:00Z",
	}))
	assert.EqualError(t, err, "write failed")
}
