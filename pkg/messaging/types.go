package messaging

import "context"

// Delivery is a single inbound message handed to a consumer. Ack/Nack are
// channel methods on the underlying amqp091.Delivery — wrapped here so the
// consumer side doesn't depend on amqp091 directly.
type Delivery struct {
	RoutingKey string
	Body       []byte
	Ack        func() error
	Nack       func(requeue bool) error
}

// ConsumerOptions describes the topology a consumer wants the broker to set
// up (idempotently) before binding. Mirrors order-service's ConsumerOptions.
type ConsumerOptions struct {
	Exchange           string
	Queue              string
	BindingKeys        []string
	DeadLetterExchange string
	DeadLetterQueue    string
	Prefetch           int
}

// Handler is the user-supplied callback. It MUST ack or nack via the
// Delivery's helpers — the broker never auto-acks.
type Handler func(ctx context.Context, d Delivery) error

// Broker is the framework-agnostic interface implemented by pkg/messaging
// AMQP code. lib/ wires it; app/ never touches it directly.
type Broker interface {
	Connect(ctx context.Context) error
	Close(ctx context.Context) error
	DeclareTopology(ctx context.Context, opts ConsumerOptions) error
	Consume(ctx context.Context, opts ConsumerOptions, h Handler) error
	Publish(ctx context.Context, exchange, routingKey string, body []byte) error
}
