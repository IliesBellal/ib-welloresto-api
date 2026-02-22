package models

import (
	"fmt"
)

type OrderNotFullyPaidError struct {
	OrderID    string
	PaidAmount int64
	Price      int64
}

func (e *OrderNotFullyPaidError) Error() string {
	return fmt.Sprintf(
		"order %s not fully paid: paid=%d expected=%d",
		e.OrderID, e.PaidAmount, e.Price,
	)
}
