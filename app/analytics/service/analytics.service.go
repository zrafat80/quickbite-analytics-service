// Package service holds the AnalyticsService implementation only. Types,
// errors, and enums live in the parent app/analytics package — see
// CLAUDE.md §5 for the rationale.
package service

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/zrafat80/quickbite/analytics-service/app/analytics"
	"github.com/zrafat80/quickbite/analytics-service/lib/appcontext"
)

type AnalyticsService struct {
	log            *slog.Logger
	restaurantRepo analytics.RestaurantDayRepository
	branchRepo     analytics.BranchDayRepository
	productRepo    analytics.ProductDayRepository
	platformRepo   analytics.PlatformDayRepository
}

func NewAnalyticsService(
	log *slog.Logger,
	restaurantRepo analytics.RestaurantDayRepository,
	branchRepo analytics.BranchDayRepository,
	productRepo analytics.ProductDayRepository,
	platformRepo analytics.PlatformDayRepository,
) *AnalyticsService {
	return &AnalyticsService{
		log:            log,
		restaurantRepo: restaurantRepo,
		branchRepo:     branchRepo,
		productRepo:    productRepo,
		platformRepo:   platformRepo,
	}
}

// ─── Write side: one method per event type ─────────────────────────────────

// OnOrderPlaced fans out into restaurant, branch, product, platform aggs.
// Each repo call is a separate Mongo round-trip — that's intentional: each
// aggregate has a different unique key, so a single bulk write can't cover
// all of them. The product call IS a bulk write internally (one round-trip
// for all items). Failures are propagated to the consumer which DLQs the
// message; partial writes can recur after a successful retry, which is fine
// because each upsert is associative.
func (s *AnalyticsService) OnOrderPlaced(ctx context.Context, in analytics.OnOrderPlacedInput) error {
	date := in.PlacedAt.UTC().Format("2006-01-02")
	if err := s.restaurantRepo.IncrementOrderRow(ctx, in.RestaurantID, date, in.Currency, in.Total); err != nil {
		return err
	}
	if err := s.branchRepo.IncrementOrderRow(ctx, in.BranchID, in.RestaurantID, date, in.Currency, in.Total); err != nil {
		return err
	}
	if err := s.productRepo.BulkIncrementOrderItems(ctx, in.RestaurantID, date, in.Currency, in.Items); err != nil {
		return err
	}
	if err := s.platformRepo.IncrementOrderRow(ctx, date, in.CountryCode, in.Currency, in.Total); err != nil {
		return err
	}
	return nil
}

// OnOrderRejected bumps rejected_count on restaurant, branch, platform.
// Products aren't touched: a single rejected order doesn't reverse the
// "this product was sold" signal — the units_sold figure is what they did
// commit to selling at placement, even if the order was later voided.
// (If you want a stricter "net sold" figure, add a rejected_count to
// agg_product_day and decrement here.)
func (s *AnalyticsService) OnOrderRejected(ctx context.Context, in analytics.OnOrderRejectedInput) error {
	date := in.RejectedAt.UTC().Format("2006-01-02")
	if err := s.restaurantRepo.IncrementRejectedRow(ctx, in.RestaurantID, date, in.Currency); err != nil {
		return err
	}
	if err := s.branchRepo.IncrementRejectedRow(ctx, in.BranchID, in.RestaurantID, date, in.Currency); err != nil {
		return err
	}
	if err := s.platformRepo.IncrementRejectedRow(ctx, date, in.CountryCode, in.Currency); err != nil {
		return err
	}
	return nil
}

// OnOrderDelivered records the placed→delivered latency on the three
// time-tracking aggregates. The delivery may arrive before the place event
// has been processed (different queues, transient lag) — that's fine
// because the upsert creates the row if missing.
func (s *AnalyticsService) OnOrderDelivered(ctx context.Context, in analytics.OnOrderDeliveredInput) error {
	date := in.DeliveredAt.UTC().Format("2006-01-02")
	if in.DeliveryMs < 0 {
		// Clock skew between order-service nodes — treat as zero rather than
		// poisoning the average with a negative delta.
		s.log.Warn("analytics: negative delivery_ms clamped to 0",
			"orderId", in.OrderID, "deliveryMs", in.DeliveryMs)
		in.DeliveryMs = 0
	}
	if err := s.restaurantRepo.AddDelivery(ctx, in.RestaurantID, date, in.Currency, in.DeliveryMs); err != nil {
		return err
	}
	if err := s.branchRepo.AddDelivery(ctx, in.BranchID, in.RestaurantID, date, in.Currency, in.DeliveryMs); err != nil {
		return err
	}
	if err := s.platformRepo.AddDelivery(ctx, date, in.CountryCode, in.Currency, in.DeliveryMs); err != nil {
		return err
	}
	return nil
}

// OnPaymentCompleted mirrors OnOrderPlaced for online orders — revenue is
// recognized at capture time. The publisher (order-service) is responsible
// for emitting payment.completed only AFTER the capture webhook confirms.
// Items are included so product totals also update for online orders.
func (s *AnalyticsService) OnPaymentCompleted(ctx context.Context, in analytics.OnPaymentCompletedInput) error {
	date := in.CompletedAt.UTC().Format("2006-01-02")
	if err := s.restaurantRepo.IncrementOrderRow(ctx, in.RestaurantID, date, in.Currency, in.Total); err != nil {
		return err
	}
	if err := s.branchRepo.IncrementOrderRow(ctx, in.BranchID, in.RestaurantID, date, in.Currency, in.Total); err != nil {
		return err
	}
	if err := s.productRepo.BulkIncrementOrderItems(ctx, in.RestaurantID, date, in.Currency, in.Items); err != nil {
		return err
	}
	if err := s.platformRepo.IncrementOrderRow(ctx, date, in.CountryCode, in.Currency, in.Total); err != nil {
		return err
	}
	return nil
}

// ─── Read side: one method per endpoint ────────────────────────────────────

func (s *AnalyticsService) QueryRestaurantDays(
	ctx context.Context,
	q analytics.RestaurantDayQuery,
) ([]analytics.RestaurantDayRow, error) {
	if !canAccessRestaurant(ctx, q.RestaurantID) {
		return nil, analytics.ErrTenantMismatch
	}
	if err := validateRange(q.From, q.To); err != nil {
		return nil, err
	}
	rows, err := s.restaurantRepo.FindByRestaurantInRange(ctx, q.RestaurantID, q.From, q.To)
	if err != nil {
		return nil, err
	}
	out := make([]analytics.RestaurantDayRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, analytics.RestaurantDayRow{
			Date:          r.Date,
			OrdersCount:   r.OrdersCount,
			RevenueMinor:  r.RevenueSum,
			Currency:      r.Currency,
			AvgOrderMinor: safeAvg(r.RevenueSum, r.OrdersCount),
		})
	}
	return out, nil
}

func (s *AnalyticsService) QueryBranchDays(
	ctx context.Context,
	q analytics.BranchDayQuery,
) ([]analytics.BranchDayRow, error) {
	if err := s.assertBranchAccess(ctx, q.BranchID); err != nil {
		return nil, err
	}
	if err := validateRange(q.From, q.To); err != nil {
		return nil, err
	}
	rows, err := s.branchRepo.FindByBranchInRange(ctx, q.BranchID, q.From, q.To)
	if err != nil {
		return nil, err
	}
	out := make([]analytics.BranchDayRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, analytics.BranchDayRow{
			Date:          r.Date,
			OrdersCount:   r.OrdersCount,
			RevenueMinor:  r.RevenueSum,
			Currency:      r.Currency,
			AvgOrderMinor: safeAvg(r.RevenueSum, r.OrdersCount),
		})
	}
	return out, nil
}

func (s *AnalyticsService) QueryProductDays(
	ctx context.Context,
	q analytics.ProductDayQuery,
) ([]analytics.ProductDayRow, error) {
	if err := validateRange(q.From, q.To); err != nil {
		return nil, err
	}
	rows, err := s.productRepo.FindByProductInRange(ctx, q.ProductID, q.From, q.To)
	if err != nil {
		return nil, err
	}
	out := make([]analytics.ProductDayRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, analytics.ProductDayRow{
			Date:         r.Date,
			OrdersCount:  r.OrdersCount,
			UnitsSold:    r.UnitsSold,
			RevenueMinor: r.RevenueSum,
			Currency:     r.Currency,
		})
	}
	return out, nil
}

func (s *AnalyticsService) QueryPlatformDays(
	ctx context.Context,
	q analytics.DateRangeQuery,
) ([]analytics.PlatformDayRow, error) {
	if err := validateRange(q.From, q.To); err != nil {
		return nil, err
	}
	rows, err := s.platformRepo.FindInRange(ctx, q.From, q.To)
	if err != nil {
		return nil, err
	}
	out := make([]analytics.PlatformDayRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, analytics.PlatformDayRow{
			Date:          r.Date,
			Currency:      r.Currency,
			OrdersCount:   r.OrdersCount,
			RevenueMinor:  r.RevenueSum,
			AvgOrderMinor: safeAvg(r.RevenueSum, r.OrdersCount),
		})
	}
	return out, nil
}

// QueryFailures projects rejected_count / orders_count per day. Uses the
// same agg_restaurant_day rows the days endpoint reads; no new collection.
func (s *AnalyticsService) QueryFailures(
	ctx context.Context,
	q analytics.FailureQuery,
) ([]analytics.FailureRow, error) {
	if !canAccessRestaurant(ctx, q.RestaurantID) {
		return nil, analytics.ErrTenantMismatch
	}
	if err := validateRange(q.From, q.To); err != nil {
		return nil, err
	}
	rows, err := s.restaurantRepo.FindByRestaurantInRange(ctx, q.RestaurantID, q.From, q.To)
	if err != nil {
		return nil, err
	}
	out := make([]analytics.FailureRow, 0, len(rows))
	for _, r := range rows {
		rate := 0.0
		if r.OrdersCount > 0 {
			rate = float64(r.RejectedCount) / float64(r.OrdersCount)
		}
		out = append(out, analytics.FailureRow{
			Date:          r.Date,
			OrdersCount:   r.OrdersCount,
			RejectedCount: r.RejectedCount,
			FailureRate:   rate,
		})
	}
	return out, nil
}

// QueryDeliveryAvg projects delivery_ms_sum / delivery_ms_count per day.
func (s *AnalyticsService) QueryDeliveryAvg(
	ctx context.Context,
	q analytics.DeliveryAvgQuery,
) ([]analytics.DeliveryAvgRow, error) {
	if !canAccessRestaurant(ctx, q.RestaurantID) {
		return nil, analytics.ErrTenantMismatch
	}
	if err := validateRange(q.From, q.To); err != nil {
		return nil, err
	}
	rows, err := s.restaurantRepo.FindByRestaurantInRange(ctx, q.RestaurantID, q.From, q.To)
	if err != nil {
		return nil, err
	}
	out := make([]analytics.DeliveryAvgRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, analytics.DeliveryAvgRow{
			Date:              r.Date,
			DeliveriesCounted: r.DeliveryMsCount,
			AvgDeliveryMs:     safeAvg(r.DeliveryMsSum, r.DeliveryMsCount),
		})
	}
	return out, nil
}

func (s *AnalyticsService) QueryActiveRestaurants(
	ctx context.Context,
	q analytics.DateRangeQuery,
) (analytics.ActiveRestaurantsRow, error) {
	if err := validateRange(q.From, q.To); err != nil {
		return analytics.ActiveRestaurantsRow{}, err
	}
	n, err := s.restaurantRepo.CountActiveRestaurantsInRange(ctx, q.From, q.To)
	if err != nil {
		return analytics.ActiveRestaurantsRow{}, err
	}
	return analytics.ActiveRestaurantsRow{From: q.From, To: q.To, Count: n}, nil
}

func (s *AnalyticsService) QueryTopRestaurants(
	ctx context.Context,
	q analytics.TopRestaurantsQuery,
) ([]analytics.TopRestaurantRow, error) {
	if err := validateRange(q.From, q.To); err != nil {
		return nil, err
	}
	if q.Limit <= 0 || q.Limit > 100 {
		q.Limit = 10
	}
	rows, err := s.restaurantRepo.TopRestaurantsByRevenue(ctx, q.From, q.To, q.Limit)
	if err != nil {
		return nil, err
	}
	out := make([]analytics.TopRestaurantRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, analytics.TopRestaurantRow{
			RestaurantID: r.RestaurantID,
			OrdersCount:  r.OrdersCount,
			RevenueMinor: r.RevenueSum,
			Currency:     r.Currency,
		})
	}
	return out, nil
}

// ─── helpers ───────────────────────────────────────────────────────────────

// assertBranchAccess is the branch-tenancy guard. Two roles can reach a
// branch endpoint after the permission gate clears:
//
//   - branch_manager (and below): only branches explicitly in claims.BranchIDs.
//   - owner: any branch belonging to their restaurant. Core leaves
//     claims.BranchIDs empty for owners (they're not in member_branches —
//     access is implicit), so we look up the branch's restaurant_id from
//     agg_branch_day. If the aggregate hasn't been created yet, allow —
//     there's no data to leak, and the read itself will return [].
func (s *AnalyticsService) assertBranchAccess(ctx context.Context, branchID int64) error {
	claims, ok := appcontext.ClaimsFrom(ctx)
	if !ok {
		return analytics.ErrTenantMismatch
	}
	if claims.Role == "system_admin" {
		return nil
	}
	if claims.RestaurantRole != nil && *claims.RestaurantRole == "owner" {
		if claims.RestaurantID == nil {
			return analytics.ErrTenantMismatch
		}
		ownerRestaurantID := *claims.RestaurantID
		branchRestaurantID, err := s.branchRepo.FindRestaurantOfBranch(ctx, branchID)
		if err != nil {
			return err
		}
		if branchRestaurantID == 0 {
			// No aggregate yet — branch may not exist or just hasn't received
			// an event. Either way nothing to expose; let the read return [].
			return nil
		}
		if branchRestaurantID != ownerRestaurantID {
			return analytics.ErrTenantMismatch
		}
		return nil
	}
	if !slices.Contains(claims.BranchIDs, branchID) {
		return analytics.ErrTenantMismatch
	}
	return nil
}

func validateRange(from, to string) error {
	f, err := time.Parse("2006-01-02", from)
	if err != nil {
		return analytics.ErrValidation.WithCause(err)
	}
	t, err := time.Parse("2006-01-02", to)
	if err != nil {
		return analytics.ErrValidation.WithCause(err)
	}
	if f.After(t) {
		return analytics.ErrInvalidDateRange
	}
	return nil
}

func safeAvg(sum, count int64) int64 {
	if count == 0 {
		return 0
	}
	return sum / count
}

func canAccessRestaurant(ctx context.Context, restaurantID int64) bool {
	claims, ok := appcontext.ClaimsFrom(ctx)
	if !ok {
		return false
	}
	if claims.Role == "system_admin" {
		return true
	}
	return claims.RestaurantID != nil && *claims.RestaurantID == restaurantID
}
