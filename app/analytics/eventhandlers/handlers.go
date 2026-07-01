// Package eventhandlers is the event-type → service.Method bridge. Each
// handler is responsible for decoding the JSON payload, mapping to a
// service-layer input struct, and dispatching. The consumer in lib/coreevents
// has already dedupe-marked the eventId, so a handler reaching this code
// means "first observation".
package eventhandlers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zrafat80/quickbite/analytics-service/app/analytics"
	"github.com/zrafat80/quickbite/analytics-service/lib/coreevents"
)

// ─── Payload structs — one per event type, local to this package ───────────

type orderItemPayload struct {
	ProductID         int64 `json:"productId"`
	Quantity          int64 `json:"quantity"`
	UnitPriceSnapshot int64 `json:"unitPriceSnapshot"`
	LineTotal         int64 `json:"lineTotal"`
}

type orderPlacedPayload struct {
	OrderID      string             `json:"orderId"`
	Region       string             `json:"region"`
	CountryCode  string             `json:"countryCode"`
	RestaurantID int64              `json:"restaurantId"`
	BranchID     int64              `json:"branchId"`
	CustomerID   int64              `json:"customerId"`
	Status       string             `json:"status"`
	Subtotal     int64              `json:"subtotal"`
	DeliveryFee  int64              `json:"deliveryFee"`
	ServiceFee   int64              `json:"serviceFee"`
	Total        int64              `json:"total"`
	Currency     string             `json:"currency"`
	Items        []orderItemPayload `json:"items"`
	PlacedAt     string             `json:"placedAt"`
}

type orderRejectedPayload struct {
	OrderID      string `json:"orderId"`
	Region       string `json:"region"`
	CountryCode  string `json:"countryCode"`
	RestaurantID int64  `json:"restaurantId"`
	BranchID     int64  `json:"branchId"`
	Currency     string `json:"currency"`
	RejectedAt   string `json:"rejectedAt"`
}

type orderDeliveredPayload struct {
	OrderID      string `json:"orderId"`
	Region       string `json:"region"`
	CountryCode  string `json:"countryCode"`
	RestaurantID int64  `json:"restaurantId"`
	BranchID     int64  `json:"branchId"`
	Currency     string `json:"currency"`
	PlacedAt     string `json:"placedAt"`
	DeliveredAt  string `json:"deliveredAt"`
}

type paymentCompletedPayload struct {
	OrderID      string             `json:"orderId"`
	Region       string             `json:"region"`
	CountryCode  string             `json:"countryCode"`
	RestaurantID int64              `json:"restaurantId"`
	BranchID     int64              `json:"branchId"`
	Total        int64              `json:"total"`
	Currency     string             `json:"currency"`
	Items        []orderItemPayload `json:"items"`
	CompletedAt  string             `json:"completedAt"`
}

// Build returns the event-type → handler map consumed by lib/coreevents.
func Build(svc analytics.EventService) map[string]coreevents.EventHandler {
	return map[string]coreevents.EventHandler{
		analytics.EventOrderPlaced: func(ctx context.Context, env coreevents.Envelope) error {
			return handleOrderPlaced(ctx, svc, env)
		},
		analytics.EventOrderRejected: func(ctx context.Context, env coreevents.Envelope) error {
			return handleOrderRejected(ctx, svc, env)
		},
		analytics.EventOrderDelivered: func(ctx context.Context, env coreevents.Envelope) error {
			return handleOrderDelivered(ctx, svc, env)
		},
		analytics.EventPaymentComplete: func(ctx context.Context, env coreevents.Envelope) error {
			return handlePaymentCompleted(ctx, svc, env)
		},
	}
}

// ─── handlers ──────────────────────────────────────────────────────────────

func handleOrderPlaced(ctx context.Context, svc analytics.EventService, env coreevents.Envelope) error {
	var p orderPlacedPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return coreevents.Permanent(fmt.Errorf("decode order.placed payload: %w", err))
	}
	if err := validateOrderPlaced(p); err != nil {
		return coreevents.Permanent(err)
	}
	placedAt, err := time.Parse(time.RFC3339, p.PlacedAt)
	if err != nil {
		return coreevents.Permanent(fmt.Errorf("parse placedAt: %w", err))
	}
	return svc.OnOrderPlaced(ctx, analytics.OnOrderPlacedInput{
		OrderID:      p.OrderID,
		RestaurantID: p.RestaurantID,
		BranchID:     p.BranchID,
		CountryCode:  p.CountryCode,
		Currency:     p.Currency,
		Total:        p.Total,
		PlacedAt:     placedAt,
		Items:        toItemInputs(p.Items),
	})
}

func handleOrderRejected(ctx context.Context, svc analytics.EventService, env coreevents.Envelope) error {
	var p orderRejectedPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return coreevents.Permanent(fmt.Errorf("decode order.rejected payload: %w", err))
	}
	countryCode := countryCodeOrRegion(p.CountryCode, p.Region)
	if p.OrderID == "" || p.RestaurantID <= 0 || p.BranchID <= 0 || countryCode == "" || p.Currency == "" || p.RejectedAt == "" {
		return coreevents.Permanent(fmt.Errorf("validate order.rejected payload: missing or invalid required field"))
	}
	rejectedAt, err := time.Parse(time.RFC3339, p.RejectedAt)
	if err != nil {
		return coreevents.Permanent(fmt.Errorf("parse rejectedAt: %w", err))
	}
	return svc.OnOrderRejected(ctx, analytics.OnOrderRejectedInput{
		OrderID:      p.OrderID,
		RestaurantID: p.RestaurantID,
		BranchID:     p.BranchID,
		CountryCode:  countryCode,
		Currency:     p.Currency,
		RejectedAt:   rejectedAt,
	})
}

func handleOrderDelivered(ctx context.Context, svc analytics.EventService, env coreevents.Envelope) error {
	var p orderDeliveredPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return coreevents.Permanent(fmt.Errorf("decode order.delivered payload: %w", err))
	}
	countryCode := countryCodeOrRegion(p.CountryCode, p.Region)
	if p.OrderID == "" || p.RestaurantID <= 0 || p.BranchID <= 0 || countryCode == "" || p.Currency == "" || p.PlacedAt == "" || p.DeliveredAt == "" {
		return coreevents.Permanent(fmt.Errorf("validate order.delivered payload: missing or invalid required field"))
	}
	placedAt, err := time.Parse(time.RFC3339, p.PlacedAt)
	if err != nil {
		return coreevents.Permanent(fmt.Errorf("parse placedAt: %w", err))
	}
	deliveredAt, err := time.Parse(time.RFC3339, p.DeliveredAt)
	if err != nil {
		return coreevents.Permanent(fmt.Errorf("parse deliveredAt: %w", err))
	}
	return svc.OnOrderDelivered(ctx, analytics.OnOrderDeliveredInput{
		OrderID:      p.OrderID,
		RestaurantID: p.RestaurantID,
		BranchID:     p.BranchID,
		CountryCode:  countryCode,
		Currency:     p.Currency,
		DeliveredAt:  deliveredAt,
		DeliveryMs:   deliveredAt.Sub(placedAt).Milliseconds(),
	})
}

func handlePaymentCompleted(ctx context.Context, svc analytics.EventService, env coreevents.Envelope) error {
	var p paymentCompletedPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return coreevents.Permanent(fmt.Errorf("decode payment.completed payload: %w", err))
	}
	if err := validatePaymentCompleted(p); err != nil {
		return coreevents.Permanent(err)
	}
	completedAt, err := time.Parse(time.RFC3339, p.CompletedAt)
	if err != nil {
		return coreevents.Permanent(fmt.Errorf("parse completedAt: %w", err))
	}
	return svc.OnPaymentCompleted(ctx, analytics.OnPaymentCompletedInput{
		OrderID:      p.OrderID,
		RestaurantID: p.RestaurantID,
		BranchID:     p.BranchID,
		CountryCode:  countryCodeOrRegion(p.CountryCode, p.Region),
		Currency:     p.Currency,
		Total:        p.Total,
		CompletedAt:  completedAt,
		Items:        toItemInputs(p.Items),
	})
}

func toItemInputs(in []orderItemPayload) []analytics.OrderItemInput {
	out := make([]analytics.OrderItemInput, 0, len(in))
	positions := make(map[int64]int, len(in))
	for _, it := range in {
		if position, ok := positions[it.ProductID]; ok {
			out[position].Quantity += it.Quantity
			out[position].LineTotal += it.LineTotal
			continue
		}
		positions[it.ProductID] = len(out)
		out = append(out, analytics.OrderItemInput{
			ProductID: it.ProductID,
			Quantity:  it.Quantity,
			LineTotal: it.LineTotal,
		})
	}
	return out
}

func validateOrderPlaced(p orderPlacedPayload) error {
	if p.OrderID == "" || p.RestaurantID <= 0 || p.BranchID <= 0 || p.CountryCode == "" || p.Currency == "" || p.Total < 0 || p.PlacedAt == "" {
		return fmt.Errorf("validate order.placed payload: missing or invalid required field")
	}
	return validateItems("order.placed", p.Items)
}

func validatePaymentCompleted(p paymentCompletedPayload) error {
	if p.OrderID == "" || p.RestaurantID <= 0 || p.BranchID <= 0 || countryCodeOrRegion(p.CountryCode, p.Region) == "" || p.Currency == "" || p.Total < 0 || p.CompletedAt == "" {
		return fmt.Errorf("validate payment.completed payload: missing or invalid required field")
	}
	return validateItems("payment.completed", p.Items)
}

func countryCodeOrRegion(countryCode, region string) string {
	if countryCode != "" {
		return countryCode
	}
	return region
}

func validateItems(eventType string, items []orderItemPayload) error {
	for _, item := range items {
		if item.ProductID <= 0 || item.Quantity <= 0 || item.LineTotal < 0 {
			return fmt.Errorf("validate %s payload: invalid item", eventType)
		}
	}
	return nil
}
