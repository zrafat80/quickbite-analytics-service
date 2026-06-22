package analytics

import (
	"net/http"

	apperr "github.com/zrafat80/quickbite/analytics-service/lib/errors"
)

// Module-level error catalogue. Go analogue of `<module>.constants.ts`'s
// MODULE_ERRORS dictionary — every stable failure mode gets a named var
// here, with the HTTP status it surfaces as. Call sites NEVER compose
// ad-hoc strings or apperr.New(...) for these cases.
var (
	ErrInvalidDateRange = apperr.New(
		"ANALYTICS_INVALID_DATE_RANGE",
		http.StatusBadRequest,
		"`from` must be on or before `to`",
	)
	ErrValidation = apperr.New(
		"VALIDATION_ERROR",
		http.StatusBadRequest,
		"invalid request",
	)
	ErrBadPathID = apperr.New(
		"VALIDATION_ERROR",
		http.StatusBadRequest,
		"invalid path parameter",
	)
	ErrTenantMismatch = apperr.New(
		"TENANT_MISMATCH",
		http.StatusForbidden,
		"cannot view analytics for resources outside your tenant",
	)
)
