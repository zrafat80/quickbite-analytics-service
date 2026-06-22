package controller

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zrafat80/quickbite/analytics-service/app/analytics"
	. "github.com/zrafat80/quickbite/analytics-service/app/analytics/controller"
	apperr "github.com/zrafat80/quickbite/analytics-service/lib/errors"
)

type queryServiceFake struct {
	restaurantQuery analytics.RestaurantDayQuery
	branchQuery     analytics.BranchDayQuery
	productQuery    analytics.ProductDayQuery
	platformQuery   analytics.DateRangeQuery
	failureQuery    analytics.FailureQuery
	deliveryQuery   analytics.DeliveryAvgQuery
	activeQuery     analytics.DateRangeQuery
	topQuery        analytics.TopRestaurantsQuery
	err             error
}

func (f *queryServiceFake) QueryRestaurantDays(_ context.Context, q analytics.RestaurantDayQuery) ([]analytics.RestaurantDayRow, error) {
	f.restaurantQuery = q
	return []analytics.RestaurantDayRow{{Date: "2026-06-01", OrdersCount: 2, RevenueMinor: 500, Currency: "EGP", AvgOrderMinor: 250}}, f.err
}

func (f *queryServiceFake) QueryBranchDays(_ context.Context, q analytics.BranchDayQuery) ([]analytics.BranchDayRow, error) {
	f.branchQuery = q
	return []analytics.BranchDayRow{{Date: "2026-06-01"}}, f.err
}

func (f *queryServiceFake) QueryProductDays(_ context.Context, q analytics.ProductDayQuery) ([]analytics.ProductDayRow, error) {
	f.productQuery = q
	return []analytics.ProductDayRow{{Date: "2026-06-01"}}, f.err
}

func (f *queryServiceFake) QueryPlatformDays(_ context.Context, q analytics.DateRangeQuery) ([]analytics.PlatformDayRow, error) {
	f.platformQuery = q
	return []analytics.PlatformDayRow{{Date: "2026-06-01"}}, f.err
}

func (f *queryServiceFake) QueryFailures(_ context.Context, q analytics.FailureQuery) ([]analytics.FailureRow, error) {
	f.failureQuery = q
	return []analytics.FailureRow{{Date: "2026-06-01"}}, f.err
}

func (f *queryServiceFake) QueryDeliveryAvg(_ context.Context, q analytics.DeliveryAvgQuery) ([]analytics.DeliveryAvgRow, error) {
	f.deliveryQuery = q
	return []analytics.DeliveryAvgRow{{Date: "2026-06-01"}}, f.err
}

func (f *queryServiceFake) QueryActiveRestaurants(_ context.Context, q analytics.DateRangeQuery) (analytics.ActiveRestaurantsRow, error) {
	f.activeQuery = q
	return analytics.ActiveRestaurantsRow{From: q.From, To: q.To, Count: 3}, f.err
}

func (f *queryServiceFake) QueryTopRestaurants(_ context.Context, q analytics.TopRestaurantsQuery) ([]analytics.TopRestaurantRow, error) {
	f.topQuery = q
	return []analytics.TopRestaurantRow{{RestaurantID: 1}}, f.err
}

func requestWithParam(target, name, value string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, target, nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add(name, value)
	return request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
}

func responseData(t *testing.T, recorder *httptest.ResponseRecorder) any {
	t.Helper()
	var body struct {
		Success bool `json:"success"`
		Data    any  `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.True(t, body.Success)
	return body.Data
}

func TestControllerMapsEveryEndpoint(t *testing.T) {
	fake := &queryServiceFake{}
	ctrl := NewAnalyticsController(fake)
	const dates = "?from=2026-06-01&to=2026-06-30"

	recorder := httptest.NewRecorder()
	require.NoError(t, ctrl.GetRestaurantDays(recorder, requestWithParam("/"+dates, "restaurantId", "10")))
	assert.Equal(t, analytics.RestaurantDayQuery{RestaurantID: 10, From: "2026-06-01", To: "2026-06-30"}, fake.restaurantQuery)
	assert.NotNil(t, responseData(t, recorder))

	recorder = httptest.NewRecorder()
	require.NoError(t, ctrl.GetRestaurantFailures(recorder, requestWithParam("/"+dates, "restaurantId", "10")))
	assert.Equal(t, int64(10), fake.failureQuery.RestaurantID)

	recorder = httptest.NewRecorder()
	require.NoError(t, ctrl.GetRestaurantDeliveryAvg(recorder, requestWithParam("/"+dates, "restaurantId", "10")))
	assert.Equal(t, int64(10), fake.deliveryQuery.RestaurantID)

	recorder = httptest.NewRecorder()
	require.NoError(t, ctrl.GetBranchDays(recorder, requestWithParam("/"+dates, "branchId", "20")))
	assert.Equal(t, analytics.BranchDayQuery{BranchID: 20, From: "2026-06-01", To: "2026-06-30"}, fake.branchQuery)

	recorder = httptest.NewRecorder()
	require.NoError(t, ctrl.GetProductDays(recorder, requestWithParam("/"+dates, "productId", "30")))
	assert.Equal(t, analytics.ProductDayQuery{ProductID: 30, From: "2026-06-01", To: "2026-06-30"}, fake.productQuery)

	request := httptest.NewRequest(http.MethodGet, "/"+dates, nil)
	recorder = httptest.NewRecorder()
	require.NoError(t, ctrl.GetPlatformDays(recorder, request))
	assert.Equal(t, analytics.DateRangeQuery{From: "2026-06-01", To: "2026-06-30"}, fake.platformQuery)

	recorder = httptest.NewRecorder()
	require.NoError(t, ctrl.GetActiveRestaurants(recorder, request))
	assert.Equal(t, fake.platformQuery, fake.activeQuery)

	recorder = httptest.NewRecorder()
	require.NoError(t, ctrl.GetTopRestaurants(recorder, httptest.NewRequest(
		http.MethodGet, "/?from=2026-06-01&to=2026-06-30&limit=7", nil,
	)))
	assert.Equal(t, analytics.TopRestaurantsQuery{From: "2026-06-01", To: "2026-06-30", Limit: 7}, fake.topQuery)
}

func TestControllerRejectsInvalidInputBeforeService(t *testing.T) {
	fake := &queryServiceFake{}
	ctrl := NewAnalyticsController(fake)

	for _, id := range []string{"", "bad", "0", "-1"} {
		err := ctrl.GetRestaurantDays(httptest.NewRecorder(), requestWithParam(
			"/?from=2026-06-01&to=2026-06-30", "restaurantId", id,
		))
		requireAppErrorCode(t, err, "VALIDATION_ERROR")
	}

	err := ctrl.GetBranchDays(httptest.NewRecorder(), requestWithParam(
		"/?from=&to=2026-06-30", "branchId", "1",
	))
	requireAppErrorCode(t, err, "VALIDATION_ERROR")

	err = ctrl.GetPlatformDays(httptest.NewRecorder(), httptest.NewRequest(
		http.MethodGet, "/?from=2026-6-1&to=2026-06-30", nil,
	))
	requireAppErrorCode(t, err, "VALIDATION_ERROR")

	for _, limit := range []string{"text", "-1", "101"} {
		err = ctrl.GetTopRestaurants(httptest.NewRecorder(), httptest.NewRequest(
			http.MethodGet, "/?from=2026-06-01&to=2026-06-30&limit="+limit, nil,
		))
		requireAppErrorCode(t, err, "VALIDATION_ERROR")
	}
}

func TestControllerPropagatesServiceErrorWithoutWritingSuccess(t *testing.T) {
	fake := &queryServiceFake{err: errors.New("service failed")}
	ctrl := NewAnalyticsController(fake)
	recorder := httptest.NewRecorder()
	err := ctrl.GetProductDays(recorder, requestWithParam(
		"/?from=2026-06-01&to=2026-06-30", "productId", "1",
	))
	assert.EqualError(t, err, "service failed")
	assert.Empty(t, recorder.Body.String())
}

func requireAppErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	var appError *apperr.AppError
	require.ErrorAs(t, err, &appError)
	assert.Equal(t, code, appError.Code)
}
