// Package coreevents holds the generic inbound consumer wiring and the
// envelope shape every event ships in. Concrete payload structs + handlers
// live in app/<module>/eventhandlers — coreevents is content-blind.
package coreevents

import (
	"context"
	"encoding/json"
	"errors"
)

// Envelope is the outer JSON shape produced by order-service's outbox
// drainer. The `Payload` field is left as raw JSON so each handler can
// decode it into its own typed struct without one global discriminated union.
type Envelope struct {
	EventID       string          `json:"eventId"`
	EventType     string          `json:"eventType"`
	OccurredAt    string          `json:"occurredAt"`
	AggregateType string          `json:"aggregateType"`
	AggregateID   string          `json:"aggregateId"`
	Payload       json.RawMessage `json:"payload"`
}

// EventHandler is the signature handlers register against the consumer.
type EventHandler func(ctx context.Context, env Envelope) error

// EventDeduper abstracts "have I already processed event X?". Implemented
// by app/analytics/repository.EventIDsRepo via duck typing — lib never
// imports app. Mongo with a unique index gives at-least-once → effectively
// once semantics.
type EventDeduper interface {
	MarkSeen(ctx context.Context, eventID string) (fresh bool, err error)
	Forget(ctx context.Context, eventID string) error
}

// PermanentError marks payload/schema failures that cannot succeed on retry.
// The consumer keeps their dedupe marker when routing them to DLQ. Transient
// handler failures release the marker so an operator can safely redrive.
type PermanentError struct {
	cause error
}

func (e *PermanentError) Error() string { return e.cause.Error() }
func (e *PermanentError) Unwrap() error { return e.cause }

func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{cause: err}
}

func IsPermanent(err error) bool {
	var permanent *PermanentError
	return errors.As(err, &permanent)
}
