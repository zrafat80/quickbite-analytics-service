// Package analytics is the parent module package. The package-level types
// here are visible to subpackages (service, controller, eventhandlers) so
// service/ can stay focused on the service implementation only — mirrors
// CLAUDE.md §5's directive to keep <Module>.constants.ts at the module
// root rather than buried in service/.
package analytics

import (
	"context"
	"time"

	"github.com/zrafat80/quickbite/analytics-service/app/analytics/entity"
)

type RestaurantDayRepository interface {
	IncrementOrderRow(ctx context.Context, restaurantID int64, date, currency string, revenueMinor int64) error
	IncrementRejectedRow(ctx context.Context, restaurantID int64, date, currency string) error
	AddDelivery(ctx context.Context, restaurantID int64, date, currency string, deliveryMs int64) error
	FindByRestaurantInRange(ctx context.Context, restaurantID int64, from, to string) ([]entity.AggRestaurantDay, error)
	CountActiveRestaurantsInRange(ctx context.Context, from, to string) (int64, error)
	TopRestaurantsByRevenue(ctx context.Context, from, to string, limit int64) ([]TopRestaurantsByRevenueRow, error)
}

type BranchDayRepository interface {
	IncrementOrderRow(ctx context.Context, branchID, restaurantID int64, date, currency string, revenueMinor int64) error
	IncrementRejectedRow(ctx context.Context, branchID, restaurantID int64, date, currency string) error
	AddDelivery(ctx context.Context, branchID, restaurantID int64, date, currency string, deliveryMs int64) error
	FindRestaurantOfBranch(ctx context.Context, branchID int64) (int64, error)
	FindByBranchInRange(ctx context.Context, branchID int64, from, to string) ([]entity.AggBranchDay, error)
}

type ProductDayRepository interface {
	BulkIncrementOrderItems(ctx context.Context, date, currency string, items []OrderItemInput) error
	FindByProductInRange(ctx context.Context, productID int64, from, to string) ([]entity.AggProductDay, error)
}

type PlatformDayRepository interface {
	IncrementOrderRow(ctx context.Context, date, currency string, revenueMinor int64) error
	IncrementRejectedRow(ctx context.Context, date, currency string) error
	AddDelivery(ctx context.Context, date, currency string, deliveryMs int64) error
	FindInRange(ctx context.Context, from, to string) ([]entity.AggPlatformDay, error)
}

type QueryService interface {
	QueryRestaurantDays(ctx context.Context, q RestaurantDayQuery) ([]RestaurantDayRow, error)
	QueryBranchDays(ctx context.Context, q BranchDayQuery) ([]BranchDayRow, error)
	QueryProductDays(ctx context.Context, q ProductDayQuery) ([]ProductDayRow, error)
	QueryPlatformDays(ctx context.Context, q DateRangeQuery) ([]PlatformDayRow, error)
	QueryFailures(ctx context.Context, q FailureQuery) ([]FailureRow, error)
	QueryDeliveryAvg(ctx context.Context, q DeliveryAvgQuery) ([]DeliveryAvgRow, error)
	QueryActiveRestaurants(ctx context.Context, q DateRangeQuery) (ActiveRestaurantsRow, error)
	QueryTopRestaurants(ctx context.Context, q TopRestaurantsQuery) ([]TopRestaurantRow, error)
}

type EventService interface {
	OnOrderPlaced(ctx context.Context, in OnOrderPlacedInput) error
	OnOrderRejected(ctx context.Context, in OnOrderRejectedInput) error
	OnOrderDelivered(ctx context.Context, in OnOrderDeliveredInput) error
	OnPaymentCompleted(ctx context.Context, in OnPaymentCompletedInput) error
}

// ─── Event handler inputs (service-layer view of the event payload) ────────

// OrderItemInput is the per-line view passed to the product aggregator.
// LineTotal is already quantity * unit price — don't multiply twice.
type OrderItemInput struct {
	ProductID int64
	Quantity  int64
	LineTotal int64
}

// OnOrderPlacedInput is the service-layer shape derived from the
// `order.placed` event payload. Money fields are integer minor units;
// PlacedAt is the placement timestamp from order-service. Items carry
// per-product line totals so agg_product_day can be rolled in the same call.
type OnOrderPlacedInput struct {
	OrderID      string
	RestaurantID int64
	BranchID     int64
	CountryCode  string
	Currency     string
	Total        int64
	PlacedAt     time.Time
	Items        []OrderItemInput
}

// OnOrderRejectedInput — `order.rejected`. Date taken from RejectedAt; we
// don't need order total because rejected_count is independent of revenue.
type OnOrderRejectedInput struct {
	OrderID      string
	RestaurantID int64
	BranchID     int64
	Currency     string
	RejectedAt   time.Time
}

// OnOrderDeliveredInput — `order.delivered`. DeliveryMs is the millis between
// PlacedAt and DeliveredAt; computing it on the publisher side keeps the
// handler simple and avoids losing precision on JSON round-trips.
type OnOrderDeliveredInput struct {
	OrderID      string
	RestaurantID int64
	BranchID     int64
	Currency     string
	DeliveredAt  time.Time
	DeliveryMs   int64
}

// OnPaymentCompletedInput — `payment.completed`. For online orders the
// revenue is recognized at capture, not at placement. The handler fans
// the increment across restaurant/branch/platform aggregates. Items are
// included so product aggregates also get bumped for online orders.
type OnPaymentCompletedInput struct {
	OrderID      string
	RestaurantID int64
	BranchID     int64
	Currency     string
	Total        int64
	CompletedAt  time.Time
	Items        []OrderItemInput
}

// ─── Read-side query inputs / projection rows ──────────────────────────────

// RestaurantDayRow is the projection produced by service.QueryRestaurantDays
// — the unit the response DTO is built from. Storage fields like
// delivery_ms_sum / delivery_ms_count never leak this far; derived fields
// (avg) are computed by the service.
type RestaurantDayRow struct {
	Date          string // YYYY-MM-DD UTC
	OrdersCount   int64
	RevenueMinor  int64
	Currency      string
	AvgOrderMinor int64
}

type RestaurantDayQuery struct {
	RestaurantID int64
	From         string
	To           string
}

// BranchDayRow mirrors RestaurantDayRow for the branch-level endpoint.
type BranchDayRow struct {
	Date          string
	OrdersCount   int64
	RevenueMinor  int64
	Currency      string
	AvgOrderMinor int64
}

type BranchDayQuery struct {
	BranchID int64
	From     string
	To       string
}

// ProductDayRow is one row per product per day with units sold and revenue.
type ProductDayRow struct {
	Date         string
	OrdersCount  int64
	UnitsSold    int64
	RevenueMinor int64
	Currency     string
}

type ProductDayQuery struct {
	ProductID int64
	From      string
	To        string
}

// PlatformDayRow — one row per (date, currency) for the platform/days endpoint.
type PlatformDayRow struct {
	Date          string
	Currency      string
	OrdersCount   int64
	RevenueMinor  int64
	AvgOrderMinor int64
}

type DateRangeQuery struct {
	From string
	To   string
}

// FailureRow is the projection for the per-restaurant failures endpoint —
// one row per UTC day with the rejected count and a derived ratio.
type FailureRow struct {
	Date          string
	OrdersCount   int64
	RejectedCount int64
	FailureRate   float64 // 0..1 — rejected / orders. 0 when orders == 0.
}

type FailureQuery struct {
	RestaurantID int64
	From         string
	To           string
}

// DeliveryAvgRow — per-day average delivery time in millis (delivery_ms_sum
// / delivery_ms_count). DeliveriesCounted lets the caller distinguish
// "no deliveries on this day" from "deliveries with zero average".
type DeliveryAvgRow struct {
	Date              string
	DeliveriesCounted int64
	AvgDeliveryMs     int64
}

type DeliveryAvgQuery struct {
	RestaurantID int64
	From         string
	To           string
}

// TopRestaurantRow is the projection for GET /restaurants/top.
type TopRestaurantRow struct {
	RestaurantID int64
	OrdersCount  int64
	RevenueMinor int64
	Currency     string
}

type TopRestaurantsQuery struct {
	From  string
	To    string
	Limit int64
}

// ActiveRestaurantsRow returns the single number callers asked for plus
// the window they asked about so the wire payload is self-describing.
type ActiveRestaurantsRow struct {
	From  string
	To    string
	Count int64
}

type TopRestaurantsByRevenueRow struct {
	RestaurantID int64
	RevenueSum   int64
	OrdersCount  int64
	Currency     string
}
