package dto

// BranchDaysQuery is the validated query-string shape for
// GET /analytics/branches/{id}/days?from=&to=.
type BranchDaysQuery struct {
	From string `validate:"required,len=10"`
	To   string `validate:"required,len=10"`
}
