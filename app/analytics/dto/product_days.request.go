package dto

type ProductDaysQuery struct {
	From string `validate:"required,len=10"`
	To   string `validate:"required,len=10"`
}
