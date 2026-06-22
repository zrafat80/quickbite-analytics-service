// Package dto holds wire-format request/response structs for app/analytics.
// Validation runs via go-playground/validator/v10 — `validate` tags carry
// the same intent class-validator tags do in the Node DTOs.
package dto

// RestaurantDaysQuery is the validated query-string shape for
// GET /restaurants/:restaurantId/days?from=YYYY-MM-DD&to=YYYY-MM-DD.
type RestaurantDaysQuery struct {
	From string `validate:"required,len=10"`
	To   string `validate:"required,len=10"`
}
