// Package controller is the HTTP layer for app/analytics. Each handler
// returns `error`; the lib/errors.Wrap middleware turns errors into the
// response envelope. Handlers are exposed as http.Handler at the route-mount
// boundary so the router stays flexible (chi accepts http.Handler).
package controller

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"

	"github.com/zrafat80/quickbite/analytics-service/app/analytics"
	"github.com/zrafat80/quickbite/analytics-service/app/analytics/dto"
	"github.com/zrafat80/quickbite/analytics-service/lib/auth"
	apperr "github.com/zrafat80/quickbite/analytics-service/lib/errors"
	libhttp "github.com/zrafat80/quickbite/analytics-service/lib/http"
	"github.com/zrafat80/quickbite/analytics-service/lib/rbac"
)

type AnalyticsController struct {
	svc       analytics.QueryService
	validator *validator.Validate
}

func NewAnalyticsController(svc analytics.QueryService) *AnalyticsController {
	return &AnalyticsController{
		svc:       svc,
		validator: validator.New(),
	}
}

// MountRoutes registers analytics routes under the supplied chi.Router. The
// middleware stack is wired here so each route gets exactly the guards it
// needs. Auth + RBAC middleware are PASSED IN so the controller doesn't
// depend on lib/boot — boot is the only place that knows the concrete
// Verifier / Cache instances.
func (c *AnalyticsController) MountRoutes(r chi.Router, verifier *auth.Verifier, rbacCache *rbac.Cache) {
	r.Route("/analytics", func(rt chi.Router) {
		// Auth applies to every analytics endpoint. RBAC + role gates layer
		// on top per resource group.
		rt.Use(auth.Require(verifier))

		// Per-restaurant — gated by core:restaurant:read. Owner role holds it
		// (seeded by core migration 20260524000001). Service layer additionally
		// asserts claims.RestaurantID matches the path param so an owner of
		// restaurant A can't peek at restaurant B's rollups.
		rt.Group(func(rt chi.Router) {
			rt.Use(rbac.Require(rbacCache, analytics.PermRestaurantRead))
			rt.Method(http.MethodGet, "/restaurants/{restaurantId}/days", apperr.Wrap(c.GetRestaurantDays))
			rt.Method(http.MethodGet, "/restaurants/{restaurantId}/failures", apperr.Wrap(c.GetRestaurantFailures))
			rt.Method(http.MethodGet, "/restaurants/{restaurantId}/delivery-avg", apperr.Wrap(c.GetRestaurantDeliveryAvg))
		})

		// Per-branch — gated by core:branch:read (owner + branch_manager).
		// Service layer asserts branchId is in claims.BranchIDs.
		rt.Group(func(rt chi.Router) {
			rt.Use(rbac.Require(rbacCache, analytics.PermBranchRead))
			rt.Method(http.MethodGet, "/branches/{branchId}/days", apperr.Wrap(c.GetBranchDays))
		})

		// Per-product — gated by core:product:read. No tenant-scoping check
		// here: analytics doesn't store product→restaurant mapping, and the
		// existing seed already grants this to owner / branch_manager / staff.
		rt.Group(func(rt chi.Router) {
			rt.Use(rbac.Require(rbacCache, analytics.PermProductRead))
			rt.Method(http.MethodGet, "/products/{productId}/days", apperr.Wrap(c.GetProductDays))
		})

		// Platform-wide — system_admin only. Bypasses RBAC entirely; the role
		// check alone is sufficient and saves a round-trip to core.
		rt.Group(func(rt chi.Router) {
			rt.Use(auth.RequireRole(rbac.RoleSystemAdmin))
			rt.Method(http.MethodGet, "/platform/days", apperr.Wrap(c.GetPlatformDays))
			rt.Method(http.MethodGet, "/platform/active-restaurants", apperr.Wrap(c.GetActiveRestaurants))
			rt.Method(http.MethodGet, "/restaurants/top", apperr.Wrap(c.GetTopRestaurants))
		})
	})
}

// ─── per-restaurant ────────────────────────────────────────────────────────

func (c *AnalyticsController) GetRestaurantDays(w http.ResponseWriter, r *http.Request) error {
	restaurantID, err := parsePositiveID(r, "restaurantId")
	if err != nil {
		return err
	}
	q := dto.RestaurantDaysQuery{
		From: r.URL.Query().Get("from"),
		To:   r.URL.Query().Get("to"),
	}
	if err := c.validator.Struct(q); err != nil {
		return analytics.ErrValidation.WithCause(err)
	}
	rows, err := c.svc.QueryRestaurantDays(r.Context(), analytics.RestaurantDayQuery{
		RestaurantID: restaurantID, From: q.From, To: q.To,
	})
	if err != nil {
		return err
	}
	libhttp.SendSuccess(w, http.StatusOK, dto.FromRows(rows))
	return nil
}

func (c *AnalyticsController) GetRestaurantFailures(w http.ResponseWriter, r *http.Request) error {
	restaurantID, err := parsePositiveID(r, "restaurantId")
	if err != nil {
		return err
	}
	q := dto.RestaurantDaysQuery{
		From: r.URL.Query().Get("from"),
		To:   r.URL.Query().Get("to"),
	}
	if err := c.validator.Struct(q); err != nil {
		return analytics.ErrValidation.WithCause(err)
	}
	rows, err := c.svc.QueryFailures(r.Context(), analytics.FailureQuery{
		RestaurantID: restaurantID, From: q.From, To: q.To,
	})
	if err != nil {
		return err
	}
	libhttp.SendSuccess(w, http.StatusOK, dto.FromFailureRows(rows))
	return nil
}

func (c *AnalyticsController) GetRestaurantDeliveryAvg(w http.ResponseWriter, r *http.Request) error {
	restaurantID, err := parsePositiveID(r, "restaurantId")
	if err != nil {
		return err
	}
	q := dto.RestaurantDaysQuery{
		From: r.URL.Query().Get("from"),
		To:   r.URL.Query().Get("to"),
	}
	if err := c.validator.Struct(q); err != nil {
		return analytics.ErrValidation.WithCause(err)
	}
	rows, err := c.svc.QueryDeliveryAvg(r.Context(), analytics.DeliveryAvgQuery{
		RestaurantID: restaurantID, From: q.From, To: q.To,
	})
	if err != nil {
		return err
	}
	libhttp.SendSuccess(w, http.StatusOK, dto.FromDeliveryAvgRows(rows))
	return nil
}

// ─── per-branch ────────────────────────────────────────────────────────────

func (c *AnalyticsController) GetBranchDays(w http.ResponseWriter, r *http.Request) error {
	branchID, err := parsePositiveID(r, "branchId")
	if err != nil {
		return err
	}
	q := dto.BranchDaysQuery{
		From: r.URL.Query().Get("from"),
		To:   r.URL.Query().Get("to"),
	}
	if err := c.validator.Struct(q); err != nil {
		return analytics.ErrValidation.WithCause(err)
	}
	rows, err := c.svc.QueryBranchDays(r.Context(), analytics.BranchDayQuery{
		BranchID: branchID, From: q.From, To: q.To,
	})
	if err != nil {
		return err
	}
	libhttp.SendSuccess(w, http.StatusOK, dto.FromBranchRows(rows))
	return nil
}

// ─── per-product ───────────────────────────────────────────────────────────

func (c *AnalyticsController) GetProductDays(w http.ResponseWriter, r *http.Request) error {
	productID, err := parsePositiveID(r, "productId")
	if err != nil {
		return err
	}
	q := dto.ProductDaysQuery{
		From: r.URL.Query().Get("from"),
		To:   r.URL.Query().Get("to"),
	}
	if err := c.validator.Struct(q); err != nil {
		return analytics.ErrValidation.WithCause(err)
	}
	rows, err := c.svc.QueryProductDays(r.Context(), analytics.ProductDayQuery{
		ProductID: productID, From: q.From, To: q.To,
	})
	if err != nil {
		return err
	}
	libhttp.SendSuccess(w, http.StatusOK, dto.FromProductRows(rows))
	return nil
}

// ─── platform-wide ─────────────────────────────────────────────────────────

func (c *AnalyticsController) GetPlatformDays(w http.ResponseWriter, r *http.Request) error {
	q := dto.DateRangeQuery{
		From: r.URL.Query().Get("from"),
		To:   r.URL.Query().Get("to"),
	}
	if err := c.validator.Struct(q); err != nil {
		return analytics.ErrValidation.WithCause(err)
	}
	rows, err := c.svc.QueryPlatformDays(r.Context(), analytics.DateRangeQuery{From: q.From, To: q.To})
	if err != nil {
		return err
	}
	libhttp.SendSuccess(w, http.StatusOK, dto.FromPlatformRows(rows))
	return nil
}

func (c *AnalyticsController) GetActiveRestaurants(w http.ResponseWriter, r *http.Request) error {
	q := dto.DateRangeQuery{
		From: r.URL.Query().Get("from"),
		To:   r.URL.Query().Get("to"),
	}
	if err := c.validator.Struct(q); err != nil {
		return analytics.ErrValidation.WithCause(err)
	}
	row, err := c.svc.QueryActiveRestaurants(r.Context(), analytics.DateRangeQuery{From: q.From, To: q.To})
	if err != nil {
		return err
	}
	libhttp.SendSuccess(w, http.StatusOK, dto.FromActiveRestaurants(row))
	return nil
}

func (c *AnalyticsController) GetTopRestaurants(w http.ResponseWriter, r *http.Request) error {
	var limit int64
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		parsed, err := strconv.ParseInt(rawLimit, 10, 64)
		if err != nil {
			return analytics.ErrValidation.WithCause(err)
		}
		limit = parsed
	}
	q := dto.TopRestaurantsQuery{
		From:  r.URL.Query().Get("from"),
		To:    r.URL.Query().Get("to"),
		Limit: limit,
	}
	if err := c.validator.Struct(q); err != nil {
		return analytics.ErrValidation.WithCause(err)
	}
	rows, err := c.svc.QueryTopRestaurants(r.Context(), analytics.TopRestaurantsQuery{
		From: q.From, To: q.To, Limit: q.Limit,
	})
	if err != nil {
		return err
	}
	libhttp.SendSuccess(w, http.StatusOK, dto.FromTopRestaurantRows(rows))
	return nil
}

// ─── helpers ───────────────────────────────────────────────────────────────

func parsePositiveID(r *http.Request, name string) (int64, error) {
	raw := chi.URLParam(r, name)
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, analytics.ErrBadPathID
	}
	return id, nil
}
