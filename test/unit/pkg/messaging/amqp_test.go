package messaging

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	. "github.com/zrafat80/quickbite/analytics-service/pkg/messaging"
)

func TestBrokerDefaultsAndDisconnectedOperations(t *testing.T) {
	broker := NewAMQPBroker(Config{URL: "://invalid"})
	require.Error(t, broker.Connect(context.Background()))
	require.Error(t, broker.DeclareTopology(context.Background(), ConsumerOptions{}))
	require.Error(t, broker.Consume(context.Background(), ConsumerOptions{}, func(context.Context, Delivery) error { return nil }))
	require.Error(t, broker.Publish(context.Background(), "exchange", "key", []byte("{}")))
	require.NoError(t, broker.Close(context.Background()))
}
