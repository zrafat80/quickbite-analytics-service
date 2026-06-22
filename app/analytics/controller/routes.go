// Package controller — see analytics.controller.go for the implementation.
// This file is the boot-time provider for the controller, matching the
// "one constructor per type" convention fx wires through fx.Provide.
package controller

import "github.com/zrafat80/quickbite/analytics-service/app/analytics/service"

// New is the fx provider boot uses. We keep it as a separate symbol from
// NewAnalyticsController so future controllers in this package can have
// their own providers without renaming.
func New(svc *service.AnalyticsService) *AnalyticsController {
	return NewAnalyticsController(svc)
}
