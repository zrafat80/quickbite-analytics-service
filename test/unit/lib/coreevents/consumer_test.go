package coreevents

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/zrafat80/quickbite/analytics-service/lib/coreevents"
	"github.com/zrafat80/quickbite/analytics-service/pkg/messaging"
)

type brokerFake struct {
	declareErr error
	consumeErr error
	options    messaging.ConsumerOptions
	handler    messaging.Handler
}

func (*brokerFake) Connect(context.Context) error { return nil }
func (*brokerFake) Close(context.Context) error   { return nil }
func (b *brokerFake) DeclareTopology(_ context.Context, options messaging.ConsumerOptions) error {
	b.options = options
	return b.declareErr
}
func (b *brokerFake) Consume(_ context.Context, options messaging.ConsumerOptions, handler messaging.Handler) error {
	b.options, b.handler = options, handler
	return b.consumeErr
}
func (*brokerFake) Publish(context.Context, string, string, []byte) error { return nil }

type deduperFake struct {
	fresh     bool
	err       error
	ids       []string
	forgotten []string
	forgetErr error
}

func (d *deduperFake) MarkSeen(_ context.Context, eventID string) (bool, error) {
	d.ids = append(d.ids, eventID)
	return d.fresh, d.err
}

func (d *deduperFake) Forget(_ context.Context, eventID string) error {
	d.forgotten = append(d.forgotten, eventID)
	return d.forgetErr
}

func delivery(body []byte, acked, nacked *int) messaging.Delivery {
	return messaging.Delivery{
		Body: body,
		Ack: func() error {
			*acked++
			return nil
		},
		Nack: func(bool) error {
			*nacked++
			return nil
		},
	}
}

func eventBody(t *testing.T, eventType string) []byte {
	t.Helper()
	body, err := json.Marshal(Envelope{EventID: "event-1", EventType: eventType, Payload: json.RawMessage(`{}`)})
	require.NoError(t, err)
	return body
}

func TestConsumerStart(t *testing.T) {
	topology := messaging.ConsumerOptions{Exchange: "events", Queue: "queue"}
	broker := &brokerFake{}
	consumer := NewConsumer(broker, &deduperFake{}, topology)
	require.NoError(t, consumer.Start(context.Background()))
	assert.Equal(t, topology, broker.options)
	require.NotNil(t, broker.handler)

	broker.declareErr = errors.New("declare")
	assert.Contains(t, consumer.Start(context.Background()).Error(), "declare topology")
	broker.declareErr = nil
	broker.consumeErr = errors.New("consume")
	assert.EqualError(t, consumer.Start(context.Background()), "consume")
}

func TestConsumerDispatchMatrix(t *testing.T) {
	cases := []struct {
		name       string
		body       []byte
		fresh      bool
		dedupeErr  error
		handlerErr error
		permanent  bool
		register   bool
		wantAck    int
		wantNack   int
	}{
		{"malformed", []byte(`{`), true, nil, nil, false, true, 0, 1},
		{"missing envelope fields", []byte(`{}`), true, nil, nil, false, true, 0, 1},
		{"dedupe failure", eventBody(t, "known"), true, errors.New("dedupe"), nil, false, true, 0, 1},
		{"duplicate", eventBody(t, "known"), false, nil, nil, false, true, 1, 0},
		{"unknown event", eventBody(t, "unknown"), true, nil, nil, false, false, 1, 0},
		{"transient handler failure", eventBody(t, "known"), true, nil, errors.New("handler"), false, true, 0, 1},
		{"permanent handler failure", eventBody(t, "known"), true, nil, errors.New("bad payload"), true, true, 0, 1},
		{"success", eventBody(t, "known"), true, nil, nil, false, true, 1, 0},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			deduper := &deduperFake{fresh: test.fresh, err: test.dedupeErr}
			calls := 0
			broker := &brokerFake{}
			consumer := NewConsumer(broker, deduper, messaging.ConsumerOptions{})
			if test.register {
				consumer.Register("known", func(context.Context, Envelope) error {
					calls++
					if test.permanent {
						return Permanent(test.handlerErr)
					}
					return test.handlerErr
				})
			}
			require.NoError(t, consumer.Start(context.Background()))
			acked, nacked := 0, 0
			require.NotNil(t, broker.handler)
			require.NoError(t, broker.handler(context.Background(), delivery(test.body, &acked, &nacked)))
			assert.Equal(t, test.wantAck, acked)
			assert.Equal(t, test.wantNack, nacked)
			if test.name == "success" || test.name == "transient handler failure" || test.name == "permanent handler failure" {
				assert.Equal(t, 1, calls)
			}
			if test.name == "transient handler failure" {
				assert.Equal(t, []string{"event-1"}, deduper.forgotten)
			}
			if test.name == "permanent handler failure" {
				assert.Empty(t, deduper.forgotten)
			}
		})
	}
}

type cacheFake struct {
	roles []string
}

func (c *cacheFake) Invalidate(role string) { c.roles = append(c.roles, role) }

func TestRBACPermissionChangedHandler(t *testing.T) {
	cache := &cacheFake{}
	handler := BuildRBACHandlers(cache)["rbac.permissions_changed"]
	require.NoError(t, handler(context.Background(), Envelope{
		EventID: "event-1", Payload: json.RawMessage(`{"role":"owner"}`),
	}))
	assert.Equal(t, []string{"owner"}, cache.roles)
	require.NoError(t, handler(context.Background(), Envelope{
		EventID: "event-2", Payload: json.RawMessage(`{"role":""}`),
	}))
	assert.Equal(t, []string{"owner"}, cache.roles)
	require.Error(t, handler(context.Background(), Envelope{Payload: json.RawMessage(`{`)}))
}
