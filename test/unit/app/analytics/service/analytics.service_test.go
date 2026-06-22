package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zrafat80/quickbite/analytics-service/app/analytics"
	"github.com/zrafat80/quickbite/analytics-service/app/analytics/entity"
	. "github.com/zrafat80/quickbite/analytics-service/app/analytics/service"
	"github.com/zrafat80/quickbite/analytics-service/lib/appcontext"
	apperr "github.com/zrafat80/quickbite/analytics-service/lib/errors"
)

type restaurantCall struct {
	restaurantID int64
	date         string
	currency     string
	value        int64
}

type restaurantRepoFake struct {
	orderCalls    []restaurantCall
	rejectedCalls []restaurantCall
	deliveryCalls []restaurantCall
	orderErr      error
	rejectedErr   error
	deliveryErr   error
	findRows      []entity.AggRestaurantDay
	findErr       error
	activeCount   int64
	activeErr     error
	topRows       []analytics.TopRestaurantsByRevenueRow
	topErr        error
	lastFrom      string
	lastTo        string
	lastLimit     int64
}

func (f *restaurantRepoFake) IncrementOrderRow(_ context.Context, id int64, date, currency string, value int64) error {
	f.orderCalls = append(f.orderCalls, restaurantCall{id, date, currency, value})
	return f.orderErr
}

func (f *restaurantRepoFake) IncrementRejectedRow(_ context.Context, id int64, date, currency string) error {
	f.rejectedCalls = append(f.rejectedCalls, restaurantCall{id, date, currency, 0})
	return f.rejectedErr
}

func (f *restaurantRepoFake) AddDelivery(_ context.Context, id int64, date, currency string, value int64) error {
	f.deliveryCalls = append(f.deliveryCalls, restaurantCall{id, date, currency, value})
	return f.deliveryErr
}

func (f *restaurantRepoFake) FindByRestaurantInRange(_ context.Context, _ int64, from, to string) ([]entity.AggRestaurantDay, error) {
	f.lastFrom, f.lastTo = from, to
	return f.findRows, f.findErr
}

func (f *restaurantRepoFake) CountActiveRestaurantsInRange(_ context.Context, from, to string) (int64, error) {
	f.lastFrom, f.lastTo = from, to
	return f.activeCount, f.activeErr
}

func (f *restaurantRepoFake) TopRestaurantsByRevenue(_ context.Context, from, to string, limit int64) ([]analytics.TopRestaurantsByRevenueRow, error) {
	f.lastFrom, f.lastTo, f.lastLimit = from, to, limit
	return f.topRows, f.topErr
}

type branchCall struct {
	branchID     int64
	restaurantID int64
	date         string
	currency     string
	value        int64
}

type branchRepoFake struct {
	orderCalls       []branchCall
	rejectedCalls    []branchCall
	deliveryCalls    []branchCall
	orderErr         error
	rejectedErr      error
	deliveryErr      error
	findRows         []entity.AggBranchDay
	findErr          error
	branchRestaurant int64
	branchLookupErr  error
}

func (f *branchRepoFake) IncrementOrderRow(_ context.Context, branchID, restaurantID int64, date, currency string, value int64) error {
	f.orderCalls = append(f.orderCalls, branchCall{branchID, restaurantID, date, currency, value})
	return f.orderErr
}

func (f *branchRepoFake) IncrementRejectedRow(_ context.Context, branchID, restaurantID int64, date, currency string) error {
	f.rejectedCalls = append(f.rejectedCalls, branchCall{branchID, restaurantID, date, currency, 0})
	return f.rejectedErr
}

func (f *branchRepoFake) AddDelivery(_ context.Context, branchID, restaurantID int64, date, currency string, value int64) error {
	f.deliveryCalls = append(f.deliveryCalls, branchCall{branchID, restaurantID, date, currency, value})
	return f.deliveryErr
}

func (f *branchRepoFake) FindRestaurantOfBranch(_ context.Context, _ int64) (int64, error) {
	return f.branchRestaurant, f.branchLookupErr
}

func (f *branchRepoFake) FindByBranchInRange(_ context.Context, _ int64, _, _ string) ([]entity.AggBranchDay, error) {
	return f.findRows, f.findErr
}

type productCall struct {
	date     string
	currency string
	items    []analytics.OrderItemInput
}

type productRepoFake struct {
	calls    []productCall
	writeErr error
	findRows []entity.AggProductDay
	findErr  error
}

func (f *productRepoFake) BulkIncrementOrderItems(_ context.Context, date, currency string, items []analytics.OrderItemInput) error {
	f.calls = append(f.calls, productCall{date, currency, append([]analytics.OrderItemInput(nil), items...)})
	return f.writeErr
}

func (f *productRepoFake) FindByProductInRange(_ context.Context, _ int64, _, _ string) ([]entity.AggProductDay, error) {
	return f.findRows, f.findErr
}

type platformCall struct {
	date     string
	currency string
	value    int64
}

type platformRepoFake struct {
	orderCalls    []platformCall
	rejectedCalls []platformCall
	deliveryCalls []platformCall
	orderErr      error
	rejectedErr   error
	deliveryErr   error
	findRows      []entity.AggPlatformDay
	findErr       error
}

func (f *platformRepoFake) IncrementOrderRow(_ context.Context, date, currency string, value int64) error {
	f.orderCalls = append(f.orderCalls, platformCall{date, currency, value})
	return f.orderErr
}

func (f *platformRepoFake) IncrementRejectedRow(_ context.Context, date, currency string) error {
	f.rejectedCalls = append(f.rejectedCalls, platformCall{date, currency, 0})
	return f.rejectedErr
}

func (f *platformRepoFake) AddDelivery(_ context.Context, date, currency string, value int64) error {
	f.deliveryCalls = append(f.deliveryCalls, platformCall{date, currency, value})
	return f.deliveryErr
}

func (f *platformRepoFake) FindInRange(_ context.Context, _, _ string) ([]entity.AggPlatformDay, error) {
	return f.findRows, f.findErr
}

func newTestService() (*AnalyticsService, *restaurantRepoFake, *branchRepoFake, *productRepoFake, *platformRepoFake) {
	restaurant := &restaurantRepoFake{}
	branch := &branchRepoFake{}
	product := &productRepoFake{}
	platform := &platformRepoFake{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewAnalyticsService(log, restaurant, branch, product, platform), restaurant, branch, product, platform
}

func tenantContext(restaurantID int64, role string, branchIDs ...int64) context.Context {
	restaurantRole := role
	return appcontext.WithClaims(context.Background(), &appcontext.Claims{
		UserID:         1,
		Role:           "restaurant_user",
		RestaurantID:   &restaurantID,
		RestaurantRole: &restaurantRole,
		BranchIDs:      branchIDs,
	})
}

func adminContext() context.Context {
	return appcontext.WithClaims(context.Background(), &appcontext.Claims{
		UserID: 1,
		Role:   "system_admin",
	})
}

func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	var appErr *apperr.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, code, appErr.Code)
}

func TestOnOrderPlacedAndPaymentCompletedFanOut(t *testing.T) {
	t.Run("maps UTC date and fans out to every aggregate", func(t *testing.T) {
		svc, restaurant, branch, product, platform := newTestService()
		items := []analytics.OrderItemInput{{ProductID: 7, Quantity: 2, LineTotal: 900}}
		placedAt := time.Date(2026, 6, 2, 0, 30, 0, 0, time.FixedZone("plus2", 2*60*60))

		err := svc.OnOrderPlaced(context.Background(), analytics.OnOrderPlacedInput{
			RestaurantID: 11, BranchID: 22, Currency: "EGP", Total: 1200, PlacedAt: placedAt, Items: items,
		})
		require.NoError(t, err)
		assert.Equal(t, []restaurantCall{{11, "2026-06-01", "EGP", 1200}}, restaurant.orderCalls)
		assert.Equal(t, []branchCall{{22, 11, "2026-06-01", "EGP", 1200}}, branch.orderCalls)
		assert.Equal(t, []productCall{{"2026-06-01", "EGP", items}}, product.calls)
		assert.Equal(t, []platformCall{{"2026-06-01", "EGP", 1200}}, platform.orderCalls)
	})

	t.Run("payment completed uses capture date and the same fanout", func(t *testing.T) {
		svc, restaurant, branch, product, platform := newTestService()
		completedAt := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
		err := svc.OnPaymentCompleted(context.Background(), analytics.OnPaymentCompletedInput{
			RestaurantID: 3, BranchID: 4, Currency: "SAR", Total: 500, CompletedAt: completedAt,
		})
		require.NoError(t, err)
		require.Len(t, restaurant.orderCalls, 1)
		require.Len(t, branch.orderCalls, 1)
		require.Len(t, product.calls, 1)
		require.Len(t, platform.orderCalls, 1)
		assert.Equal(t, "2026-06-03", restaurant.orderCalls[0].date)
	})

	t.Run("stops after the first failing dependency", func(t *testing.T) {
		svc, restaurant, branch, product, platform := newTestService()
		restaurant.orderErr = errors.New("mongo unavailable")
		err := svc.OnOrderPlaced(context.Background(), analytics.OnOrderPlacedInput{PlacedAt: time.Now()})
		require.Error(t, err)
		assert.Empty(t, branch.orderCalls)
		assert.Empty(t, product.calls)
		assert.Empty(t, platform.orderCalls)
	})

	t.Run("propagates branch product and platform failures", func(t *testing.T) {
		for _, test := range []struct {
			name  string
			setup func(*branchRepoFake, *productRepoFake, *platformRepoFake)
		}{
			{"branch", func(b *branchRepoFake, _ *productRepoFake, _ *platformRepoFake) { b.orderErr = errors.New("branch") }},
			{"product", func(_ *branchRepoFake, p *productRepoFake, _ *platformRepoFake) { p.writeErr = errors.New("product") }},
			{"platform", func(_ *branchRepoFake, _ *productRepoFake, p *platformRepoFake) { p.orderErr = errors.New("platform") }},
		} {
			t.Run(test.name, func(t *testing.T) {
				svc, _, branch, product, platform := newTestService()
				test.setup(branch, product, platform)
				require.Error(t, svc.OnPaymentCompleted(context.Background(), analytics.OnPaymentCompletedInput{
					CompletedAt: time.Now(),
				}))
			})
		}
	})
}

func TestOnOrderRejectedAndDelivered(t *testing.T) {
	t.Run("rejected increments restaurant branch and platform", func(t *testing.T) {
		svc, restaurant, branch, _, platform := newTestService()
		err := svc.OnOrderRejected(context.Background(), analytics.OnOrderRejectedInput{
			RestaurantID: 1, BranchID: 2, Currency: "EGP",
			RejectedAt: time.Date(2026, 6, 4, 1, 0, 0, 0, time.UTC),
		})
		require.NoError(t, err)
		assert.Equal(t, "2026-06-04", restaurant.rejectedCalls[0].date)
		assert.Equal(t, int64(2), branch.rejectedCalls[0].branchID)
		assert.Len(t, platform.rejectedCalls, 1)
	})

	t.Run("delivered clamps negative duration", func(t *testing.T) {
		svc, restaurant, branch, _, platform := newTestService()
		err := svc.OnOrderDelivered(context.Background(), analytics.OnOrderDeliveredInput{
			OrderID: "o-1", RestaurantID: 1, BranchID: 2, Currency: "EGP",
			DeliveredAt: time.Date(2026, 6, 5, 1, 0, 0, 0, time.UTC), DeliveryMs: -50,
		})
		require.NoError(t, err)
		assert.Zero(t, restaurant.deliveryCalls[0].value)
		assert.Zero(t, branch.deliveryCalls[0].value)
		assert.Zero(t, platform.deliveryCalls[0].value)
	})

	t.Run("write failures stop later aggregate updates", func(t *testing.T) {
		svc, restaurant, branch, _, platform := newTestService()
		restaurant.deliveryErr = errors.New("delivery write")
		require.Error(t, svc.OnOrderDelivered(context.Background(), analytics.OnOrderDeliveredInput{DeliveredAt: time.Now()}))
		assert.Empty(t, branch.deliveryCalls)
		assert.Empty(t, platform.deliveryCalls)

		svc, restaurant, branch, _, platform = newTestService()
		branch.rejectedErr = errors.New("reject write")
		require.Error(t, svc.OnOrderRejected(context.Background(), analytics.OnOrderRejectedInput{RejectedAt: time.Now()}))
		assert.Len(t, restaurant.rejectedCalls, 1)
		assert.Empty(t, platform.rejectedCalls)
	})

	t.Run("later rejected and delivered failures propagate", func(t *testing.T) {
		svc, _, branch, _, platform := newTestService()
		platform.rejectedErr = errors.New("platform reject")
		require.Error(t, svc.OnOrderRejected(context.Background(), analytics.OnOrderRejectedInput{RejectedAt: time.Now()}))
		assert.Len(t, branch.rejectedCalls, 1)

		svc, _, branch, _, platform = newTestService()
		branch.deliveryErr = errors.New("branch delivery")
		require.Error(t, svc.OnOrderDelivered(context.Background(), analytics.OnOrderDeliveredInput{DeliveredAt: time.Now()}))
		assert.Empty(t, platform.deliveryCalls)

		svc, _, _, _, platform = newTestService()
		platform.deliveryErr = errors.New("platform delivery")
		require.Error(t, svc.OnOrderDelivered(context.Background(), analytics.OnOrderDeliveredInput{DeliveredAt: time.Now()}))
	})
}

func TestQueryRestaurantDays(t *testing.T) {
	svc, restaurant, _, _, _ := newTestService()
	restaurant.findRows = []entity.AggRestaurantDay{
		{Date: "2026-06-01", OrdersCount: 3, RevenueSum: 1000, Currency: "EGP"},
		{Date: "2026-06-02", OrdersCount: 0, RevenueSum: 500, Currency: "EGP"},
	}
	rows, err := svc.QueryRestaurantDays(tenantContext(10, "owner"), analytics.RestaurantDayQuery{
		RestaurantID: 10, From: "2026-06-01", To: "2026-06-30",
	})
	require.NoError(t, err)
	assert.Equal(t, []analytics.RestaurantDayRow{
		{Date: "2026-06-01", OrdersCount: 3, RevenueMinor: 1000, Currency: "EGP", AvgOrderMinor: 333},
		{Date: "2026-06-02", OrdersCount: 0, RevenueMinor: 500, Currency: "EGP", AvgOrderMinor: 0},
	}, rows)

	_, err = svc.QueryRestaurantDays(context.Background(), analytics.RestaurantDayQuery{RestaurantID: 10})
	requireCode(t, err, "TENANT_MISMATCH")
	_, err = svc.QueryRestaurantDays(tenantContext(11, "owner"), analytics.RestaurantDayQuery{RestaurantID: 10})
	requireCode(t, err, "TENANT_MISMATCH")

	_, err = svc.QueryRestaurantDays(tenantContext(10, "owner"), analytics.RestaurantDayQuery{
		RestaurantID: 10, From: "bad", To: "2026-06-01",
	})
	requireCode(t, err, "VALIDATION_ERROR")
	_, err = svc.QueryRestaurantDays(tenantContext(10, "owner"), analytics.RestaurantDayQuery{
		RestaurantID: 10, From: "2026-06-02", To: "2026-06-01",
	})
	requireCode(t, err, "ANALYTICS_INVALID_DATE_RANGE")

	restaurant.findErr = errors.New("find")
	_, err = svc.QueryRestaurantDays(tenantContext(10, "owner"), analytics.RestaurantDayQuery{
		RestaurantID: 10, From: "2026-06-01", To: "2026-06-02",
	})
	assert.EqualError(t, err, "find")

	restaurant.findErr = nil
	_, err = svc.QueryRestaurantDays(adminContext(), analytics.RestaurantDayQuery{
		RestaurantID: 999, From: "2026-06-01", To: "2026-06-02",
	})
	require.NoError(t, err)
}

func TestQueryBranchDaysAccessMatrix(t *testing.T) {
	t.Run("branch member receives projected rows", func(t *testing.T) {
		svc, _, branch, _, _ := newTestService()
		branch.findRows = []entity.AggBranchDay{{Date: "2026-06-01", OrdersCount: 2, RevenueSum: 900, Currency: "EGP"}}
		rows, err := svc.QueryBranchDays(tenantContext(10, "branch_manager", 20), analytics.BranchDayQuery{
			BranchID: 20, From: "2026-06-01", To: "2026-06-01",
		})
		require.NoError(t, err)
		assert.Equal(t, int64(450), rows[0].AvgOrderMinor)
	})

	t.Run("unassigned member and missing claims are denied", func(t *testing.T) {
		svc, _, _, _, _ := newTestService()
		_, err := svc.QueryBranchDays(tenantContext(10, "staff", 21), analytics.BranchDayQuery{BranchID: 20})
		requireCode(t, err, "TENANT_MISMATCH")
		_, err = svc.QueryBranchDays(context.Background(), analytics.BranchDayQuery{BranchID: 20})
		requireCode(t, err, "TENANT_MISMATCH")
	})

	t.Run("owner may read own or empty branch but not foreign branch", func(t *testing.T) {
		svc, _, branch, _, _ := newTestService()
		branch.branchRestaurant = 10
		_, err := svc.QueryBranchDays(tenantContext(10, "owner"), analytics.BranchDayQuery{
			BranchID: 20, From: "2026-06-01", To: "2026-06-01",
		})
		require.NoError(t, err)

		branch.branchRestaurant = 0
		_, err = svc.QueryBranchDays(tenantContext(10, "owner"), analytics.BranchDayQuery{
			BranchID: 20, From: "2026-06-01", To: "2026-06-01",
		})
		require.NoError(t, err)

		branch.branchRestaurant = 11
		_, err = svc.QueryBranchDays(tenantContext(10, "owner"), analytics.BranchDayQuery{BranchID: 20})
		requireCode(t, err, "TENANT_MISMATCH")

		branch.branchLookupErr = errors.New("lookup")
		_, err = svc.QueryBranchDays(tenantContext(10, "owner"), analytics.BranchDayQuery{BranchID: 20})
		assert.EqualError(t, err, "lookup")
	})

	t.Run("system administrator bypasses tenant lookup", func(t *testing.T) {
		svc, _, branch, _, _ := newTestService()
		branch.branchLookupErr = errors.New("must not be called")
		_, err := svc.QueryBranchDays(adminContext(), analytics.BranchDayQuery{
			BranchID: 20, From: "2026-06-01", To: "2026-06-01",
		})
		require.NoError(t, err)
	})

	t.Run("owner without restaurant and repository failures are rejected", func(t *testing.T) {
		svc, _, branch, _, _ := newTestService()
		owner := "owner"
		ctx := appcontext.WithClaims(context.Background(), &appcontext.Claims{
			Role: "restaurant_user", RestaurantRole: &owner,
		})
		_, err := svc.QueryBranchDays(ctx, analytics.BranchDayQuery{BranchID: 20})
		requireCode(t, err, "TENANT_MISMATCH")

		branch.findErr = errors.New("branch find")
		_, err = svc.QueryBranchDays(tenantContext(10, "branch_manager", 20), analytics.BranchDayQuery{
			BranchID: 20, From: "2026-06-01", To: "2026-06-01",
		})
		assert.EqualError(t, err, "branch find")

		_, err = svc.QueryBranchDays(tenantContext(10, "branch_manager", 20), analytics.BranchDayQuery{
			BranchID: 20, From: "bad", To: "2026-06-01",
		})
		requireCode(t, err, "VALIDATION_ERROR")
	})
}

func TestProductAndPlatformQueries(t *testing.T) {
	svc, _, _, product, platform := newTestService()
	product.findRows = []entity.AggProductDay{{
		Date: "2026-06-01", OrdersCount: 2, UnitsSold: 5, RevenueSum: 2500, Currency: "EGP",
	}}
	rows, err := svc.QueryProductDays(context.Background(), analytics.ProductDayQuery{
		ProductID: 7, From: "2026-06-01", To: "2026-06-01",
	})
	require.NoError(t, err)
	assert.Equal(t, []analytics.ProductDayRow{{
		Date: "2026-06-01", OrdersCount: 2, UnitsSold: 5, RevenueMinor: 2500, Currency: "EGP",
	}}, rows)

	platform.findRows = []entity.AggPlatformDay{{
		Date: "2026-06-01", Currency: "SAR", OrdersCount: 2, RevenueSum: 1001,
	}}
	platformRows, err := svc.QueryPlatformDays(context.Background(), analytics.DateRangeQuery{
		From: "2026-06-01", To: "2026-06-01",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(500), platformRows[0].AvgOrderMinor)

	product.findErr = errors.New("product find")
	_, err = svc.QueryProductDays(context.Background(), analytics.ProductDayQuery{
		From: "2026-06-01", To: "2026-06-01",
	})
	assert.EqualError(t, err, "product find")
	platform.findErr = errors.New("platform find")
	_, err = svc.QueryPlatformDays(context.Background(), analytics.DateRangeQuery{
		From: "2026-06-01", To: "2026-06-01",
	})
	assert.EqualError(t, err, "platform find")
}

func TestDerivedAndRankingQueries(t *testing.T) {
	svc, restaurant, _, _, _ := newTestService()
	restaurant.findRows = []entity.AggRestaurantDay{
		{Date: "2026-06-01", OrdersCount: 4, RejectedCount: 1, DeliveryMsSum: 1001, DeliveryMsCount: 2},
		{Date: "2026-06-02", OrdersCount: 0, RejectedCount: 2, DeliveryMsSum: 0, DeliveryMsCount: 0},
	}
	ctx := tenantContext(10, "owner")

	failures, err := svc.QueryFailures(ctx, analytics.FailureQuery{
		RestaurantID: 10, From: "2026-06-01", To: "2026-06-02",
	})
	require.NoError(t, err)
	assert.Equal(t, 0.25, failures[0].FailureRate)
	assert.Zero(t, failures[1].FailureRate)

	deliveries, err := svc.QueryDeliveryAvg(ctx, analytics.DeliveryAvgQuery{
		RestaurantID: 10, From: "2026-06-01", To: "2026-06-02",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(500), deliveries[0].AvgDeliveryMs)
	assert.Zero(t, deliveries[1].AvgDeliveryMs)

	restaurant.activeCount = 3
	active, err := svc.QueryActiveRestaurants(context.Background(), analytics.DateRangeQuery{
		From: "2026-06-01", To: "2026-06-30",
	})
	require.NoError(t, err)
	assert.Equal(t, analytics.ActiveRestaurantsRow{From: "2026-06-01", To: "2026-06-30", Count: 3}, active)

	restaurant.topRows = []analytics.TopRestaurantsByRevenueRow{{
		RestaurantID: 9, OrdersCount: 8, RevenueSum: 9000, Currency: "EGP",
	}}
	top, err := svc.QueryTopRestaurants(context.Background(), analytics.TopRestaurantsQuery{
		From: "2026-06-01", To: "2026-06-30", Limit: 5,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(5), restaurant.lastLimit)
	assert.Equal(t, []analytics.TopRestaurantRow{{
		RestaurantID: 9, OrdersCount: 8, RevenueMinor: 9000, Currency: "EGP",
	}}, top)

	for _, limit := range []int64{0, -1, 101} {
		_, err = svc.QueryTopRestaurants(context.Background(), analytics.TopRestaurantsQuery{
			From: "2026-06-01", To: "2026-06-30", Limit: limit,
		})
		require.NoError(t, err)
		assert.Equal(t, int64(10), restaurant.lastLimit)
	}

	restaurant.activeErr = errors.New("active")
	_, err = svc.QueryActiveRestaurants(context.Background(), analytics.DateRangeQuery{
		From: "2026-06-01", To: "2026-06-30",
	})
	assert.EqualError(t, err, "active")
	restaurant.topErr = errors.New("top")
	_, err = svc.QueryTopRestaurants(context.Background(), analytics.TopRestaurantsQuery{
		From: "2026-06-01", To: "2026-06-30",
	})
	assert.EqualError(t, err, "top")
}

func TestDerivedQueryGuardAndErrorBranches(t *testing.T) {
	svc, restaurant, _, _, _ := newTestService()
	validFailure := analytics.FailureQuery{RestaurantID: 10, From: "2026-06-01", To: "2026-06-02"}
	validDelivery := analytics.DeliveryAvgQuery{RestaurantID: 10, From: "2026-06-01", To: "2026-06-02"}

	_, err := svc.QueryFailures(context.Background(), validFailure)
	requireCode(t, err, "TENANT_MISMATCH")
	_, err = svc.QueryDeliveryAvg(tenantContext(11, "owner"), validDelivery)
	requireCode(t, err, "TENANT_MISMATCH")

	_, err = svc.QueryFailures(tenantContext(10, "owner"), analytics.FailureQuery{
		RestaurantID: 10, From: "bad", To: "2026-06-02",
	})
	requireCode(t, err, "VALIDATION_ERROR")
	_, err = svc.QueryDeliveryAvg(tenantContext(10, "owner"), analytics.DeliveryAvgQuery{
		RestaurantID: 10, From: "2026-06-03", To: "2026-06-02",
	})
	requireCode(t, err, "ANALYTICS_INVALID_DATE_RANGE")

	restaurant.findErr = errors.New("derived find")
	_, err = svc.QueryFailures(tenantContext(10, "owner"), validFailure)
	assert.EqualError(t, err, "derived find")
	_, err = svc.QueryDeliveryAvg(tenantContext(10, "owner"), validDelivery)
	assert.EqualError(t, err, "derived find")

	_, err = svc.QueryFailures(adminContext(), analytics.FailureQuery{
		RestaurantID: 999, From: "2026-06-01", To: "2026-06-02",
	})
	assert.EqualError(t, err, "derived find")
}

func TestPlatformAggregateQueryValidation(t *testing.T) {
	svc, _, _, _, _ := newTestService()
	_, err := svc.QueryProductDays(context.Background(), analytics.ProductDayQuery{From: "bad", To: "2026-06-01"})
	requireCode(t, err, "VALIDATION_ERROR")
	_, err = svc.QueryPlatformDays(context.Background(), analytics.DateRangeQuery{From: "2026-06-02", To: "2026-06-01"})
	requireCode(t, err, "ANALYTICS_INVALID_DATE_RANGE")
	_, err = svc.QueryActiveRestaurants(context.Background(), analytics.DateRangeQuery{From: "", To: "2026-06-01"})
	requireCode(t, err, "VALIDATION_ERROR")
	_, err = svc.QueryTopRestaurants(context.Background(), analytics.TopRestaurantsQuery{From: "bad", To: "2026-06-01"})
	requireCode(t, err, "VALIDATION_ERROR")
}
