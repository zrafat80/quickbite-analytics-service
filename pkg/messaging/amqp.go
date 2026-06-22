// Package messaging — amqp091 implementation of Broker.
//
// LAYERING: pkg/. No imports from lib/ or app/. No env, no globals.
package messaging

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Config is the only piece of state the caller must hand us.
type Config struct {
	URL          string
	ReconnectMin time.Duration
}

// AMQPBroker is a single-connection, single-channel client. The implementation
// is deliberately small — reconnect loop on Connect failure (matches the
// "best-effort at boot" semantics on the Node side), single publish channel,
// fresh channel per consumer.
type AMQPBroker struct {
	cfg     Config
	mu      sync.Mutex
	conn    *amqp.Connection
	pubCh   *amqp.Channel
	closeCh chan *amqp.Error
}

func NewAMQPBroker(cfg Config) *AMQPBroker {
	if cfg.ReconnectMin == 0 {
		cfg.ReconnectMin = 500 * time.Millisecond
	}
	return &AMQPBroker{cfg: cfg}
}

// Connect dials once. Callers MAY call Connect repeatedly; subsequent calls
// are no-ops when the underlying connection is alive.
func (b *AMQPBroker) Connect(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn != nil && !b.conn.IsClosed() {
		return nil
	}
	conn, err := amqp.DialConfig(b.cfg.URL, amqp.Config{
		Dial: amqp.DefaultDial(5 * time.Second),
	})
	if err != nil {
		return fmt.Errorf("amqp dial: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("amqp channel: %w", err)
	}
	b.conn = conn
	b.pubCh = ch
	b.closeCh = make(chan *amqp.Error, 1)
	conn.NotifyClose(b.closeCh)
	return nil
}

func (b *AMQPBroker) Close(_ context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	var firstErr error
	if b.pubCh != nil {
		if err := b.pubCh.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if b.conn != nil {
		if err := b.conn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	b.pubCh = nil
	b.conn = nil
	return firstErr
}

// DeclareTopology asserts the exchange, optional DLX/DLQ, queue (with DLX
// argument when configured), and bindings. Idempotent.
func (b *AMQPBroker) DeclareTopology(ctx context.Context, opts ConsumerOptions) error {
	if err := b.Connect(ctx); err != nil {
		return err
	}
	ch, err := b.conn.Channel()
	if err != nil {
		return fmt.Errorf("amqp channel: %w", err)
	}
	defer ch.Close()
	return assertTopology(ch, opts)
}

// Consume opens its own channel, asserts topology, sets prefetch, and starts
// a goroutine that reads deliveries and dispatches them to h. Blocks only
// long enough to register; the goroutine lives for the channel's lifetime.
func (b *AMQPBroker) Consume(ctx context.Context, opts ConsumerOptions, h Handler) error {
	if err := b.Connect(ctx); err != nil {
		return err
	}
	ch, err := b.conn.Channel()
	if err != nil {
		return fmt.Errorf("amqp channel: %w", err)
	}
	if err := assertTopology(ch, opts); err != nil {
		_ = ch.Close()
		return err
	}
	if err := ch.Qos(opts.Prefetch, 0, false); err != nil {
		_ = ch.Close()
		return fmt.Errorf("amqp qos: %w", err)
	}
	deliveries, err := ch.Consume(opts.Queue, "", false /* autoAck */, false, false, false, nil)
	if err != nil {
		_ = ch.Close()
		return fmt.Errorf("amqp consume: %w", err)
	}
	go consumeLoop(ctx, ch, deliveries, h)
	return nil
}

func consumeLoop(ctx context.Context, ch *amqp.Channel, deliveries <-chan amqp.Delivery, h Handler) {
	defer ch.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case d, ok := <-deliveries:
			if !ok {
				return
			}
			dlv := Delivery{
				RoutingKey: d.RoutingKey,
				Body:       d.Body,
				Ack:        func() error { return d.Ack(false) },
				Nack:       func(requeue bool) error { return d.Nack(false, requeue) },
			}
			if err := h(ctx, dlv); err != nil {
				// Handler must already have decided ack/nack; if it returned
				// an error AND didn't ack/nack, the broker re-delivers after
				// channel close. We nack-no-requeue defensively so the
				// message routes to DLQ.
				_ = d.Nack(false, false)
			}
		}
	}
}

// Publish sends a persistent message on the shared publisher channel.
// Confirms are not enabled in this minimal client — analytics-service is the
// CONSUMER side; the only outbound use is /play/publish-test, where lost
// publishes are not catastrophic.
func (b *AMQPBroker) Publish(ctx context.Context, exchange, routingKey string, body []byte) error {
	if err := b.Connect(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	ch := b.pubCh
	b.mu.Unlock()
	if ch == nil {
		return errors.New("amqp publish: channel not initialised")
	}
	return ch.PublishWithContext(ctx, exchange, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		Body:         body,
		DeliveryMode: amqp.Persistent,
	})
}

func assertTopology(ch *amqp.Channel, opts ConsumerOptions) error {
	if err := ch.ExchangeDeclare(opts.Exchange, "topic", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange %q: %w", opts.Exchange, err)
	}

	args := amqp.Table{}
	if opts.DeadLetterExchange != "" && opts.DeadLetterQueue != "" {
		if err := ch.ExchangeDeclare(opts.DeadLetterExchange, "topic", true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare dlx %q: %w", opts.DeadLetterExchange, err)
		}
		if _, err := ch.QueueDeclare(opts.DeadLetterQueue, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare dlq %q: %w", opts.DeadLetterQueue, err)
		}
		if err := ch.QueueBind(opts.DeadLetterQueue, "#", opts.DeadLetterExchange, false, nil); err != nil {
			return fmt.Errorf("bind dlq: %w", err)
		}
		args["x-dead-letter-exchange"] = opts.DeadLetterExchange
	}

	if _, err := ch.QueueDeclare(opts.Queue, true, false, false, false, args); err != nil {
		return fmt.Errorf("declare queue %q: %w", opts.Queue, err)
	}
	for _, k := range opts.BindingKeys {
		if err := ch.QueueBind(opts.Queue, k, opts.Exchange, false, nil); err != nil {
			return fmt.Errorf("bind queue %q -> %q: %w", opts.Queue, k, err)
		}
	}
	return nil
}
