package coreevents

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/zrafat80/quickbite/analytics-service/lib/logger"
	"github.com/zrafat80/quickbite/analytics-service/pkg/messaging"
)

// Consumer wires a Broker to a registry of handlers. Lifecycle:
//  1. Register(eventType, handler) for each handled type.
//  2. Start(ctx) — declares topology, dedupes via EventDeduper, dispatches.
//
// Unknown event types are acked (forward-compat). Handler errors → DLQ.
type Consumer struct {
	broker   messaging.Broker
	deduper  EventDeduper
	topology messaging.ConsumerOptions
	handlers map[string]EventHandler
}

func NewConsumer(broker messaging.Broker, deduper EventDeduper, topology messaging.ConsumerOptions) *Consumer {
	return &Consumer{
		broker:   broker,
		deduper:  deduper,
		topology: topology,
		handlers: make(map[string]EventHandler),
	}
}

// Register binds a handler to an event type. Last registration wins.
func (c *Consumer) Register(eventType string, h EventHandler) {
	c.handlers[eventType] = h
}

// Start declares topology and starts the consume goroutine. Boot is
// best-effort: a broker outage at startup must not crash the API — Connect
// returns the error so the caller (lib/boot) can log+continue.
func (c *Consumer) Start(ctx context.Context) error {
	if err := c.broker.DeclareTopology(ctx, c.topology); err != nil {
		return fmt.Errorf("coreevents declare topology: %w", err)
	}
	return c.broker.Consume(ctx, c.topology, c.dispatch)
}

func (c *Consumer) dispatch(ctx context.Context, d messaging.Delivery) error {
	log := logger.FromContext(ctx)

	var env Envelope
	if err := json.Unmarshal(d.Body, &env); err != nil || env.EventID == "" || env.EventType == "" {
		log.Warn("coreevents: unparseable message, sending to DLQ")
		return d.Nack(false)
	}

	fresh, err := c.deduper.MarkSeen(ctx, env.EventID)
	if err != nil {
		log.Error("coreevents: dedupe failed", "eventId", env.EventID, "err", err.Error())
		return d.Nack(false)
	}
	if !fresh {
		log.Info("coreevents: duplicate, ack-skip", "eventId", env.EventID, "eventType", env.EventType)
		return d.Ack()
	}

	h, ok := c.handlers[env.EventType]
	if !ok {
		log.Warn("coreevents: no handler, ack-skip", "eventType", env.EventType, "eventId", env.EventID)
		return d.Ack()
	}

	if err := h(ctx, env); err != nil {
		log.Error("coreevents: handler failed, sending to DLQ",
			"eventType", env.EventType, "eventId", env.EventID, "err", err.Error())
		if !IsPermanent(err) {
			if releaseErr := c.deduper.Forget(ctx, env.EventID); releaseErr != nil {
				log.Error("coreevents: failed to release dedupe marker after transient handler failure",
					"eventType", env.EventType, "eventId", env.EventID, "err", releaseErr.Error())
			}
		}
		return d.Nack(false)
	}
	return d.Ack()
}
