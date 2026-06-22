package dto

// DateRangeQuery is shared by the platform endpoints (platform/days,
// platform/active-restaurants).
type DateRangeQuery struct {
	From string `validate:"required,len=10"`
	To   string `validate:"required,len=10"`
}

// TopRestaurantsQuery extends DateRangeQuery with a Limit. We default Limit
// in the service when zero, so it stays optional on the wire.
type TopRestaurantsQuery struct {
	From  string `validate:"required,len=10"`
	To    string `validate:"required,len=10"`
	Limit int64  `validate:"omitempty,min=1,max=100"`
}
