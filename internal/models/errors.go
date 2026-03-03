package models

import (
	"fmt"
)

type OrderNotFullyPaidError struct {
	OrderID    string
	PaidAmount int
	Price      int
}

func (e *OrderNotFullyPaidError) Error() string {
	return fmt.Sprintf(
		"order %s not fully paid: paid=%d expected=%d",
		e.OrderID, e.PaidAmount, e.Price,
	)
}

type contextKey string

const (
	ContextUserID     contextKey = "user_id"
	ContextMerchantID contextKey = "merchant_id"
)
