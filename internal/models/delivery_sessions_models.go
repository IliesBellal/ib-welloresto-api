package models

import "time"

type DeliverySessionRequest struct {
	DeliveryMan struct {
		UserID string `json:"user_id"`
	} `json:"delivery_man"`

	Distance   string              `json:"distance"`
	Duration   string              `json:"duration"`
	MerchantID string              `json:"merchant_id"`
	Orders     []DeliveryOrderItem `json:"orders"`
}

type DeliveryOrderItem struct {
	OrderID string `json:"order_id"`
}

type DeliverySession struct {
	DeliverySessionID string    `json:"delivery_session_id"`
	UserID            string    `json:"user_id"`
	MerchantID        string    `json:"merchant_id"`
	StartDate         string    `json:"start_date"`
	Distance          string    `json:"distance"`
	Duration          string    `json:"duration"`
	Status            string    `json:"status"`
	CurrentOrderID    *string   `json:"current_order_id"`
	Orders            []Order   `json:"orders"`
	DeliveryMan       OrderUser `json:"delivery_man"`
}

// DeliveryStop represents the per-order state of a delivery within a delivery session
// (delivery_session_order columns added by migration 032).
type DeliveryStop struct {
	Priority    *int       `json:"priority"`
	Status      string     `json:"status"`
	ArrivedAt   *time.Time `json:"arrived_at"`
	DeliveredAt *time.Time `json:"delivered_at"`
	FailedAt    *time.Time `json:"failed_at"`
	CanceledAt  *time.Time `json:"canceled_at"`
	FailReason  *string    `json:"fail_reason"`
}
