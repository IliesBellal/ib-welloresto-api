package discounts

import "errors"

var (
	ErrInvalidDiscountName  = errors.New("invalid discount name")
	ErrInvalidDiscountDesc  = errors.New("invalid discount description")
	ErrInvalidDiscountValue = errors.New("invalid discount value")
	ErrInvalidDiscountUnit  = errors.New("invalid discount unit")
	ErrInvalidSchedules     = errors.New("schedules required for time-limited discounts")
	ErrDiscountNotFound     = errors.New("discount not found")
	ErrAccessDenied         = errors.New("access denied")
	ErrInvalidOrderType     = errors.New("invalid order type")
	ErrInvalidDiscountID    = errors.New("invalid discount id")
)
