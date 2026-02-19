package models

import (
	"errors"
	"fmt"
)

var (
	ErrDeliverySessionAlreadyActive = errors.New("delivery_session_already_active")
	ErrInvalidToken                 = errors.New("invalid_token")
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
