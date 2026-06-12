package delivery_sessions

import "welloresto-api/internal/models"

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

// DeliveryStopReasonRequest is the body of PATCH .../stops/{order_id}/failed and
// PATCH .../stops/{order_id}/cancel (§3.5/§3.6): a free-text reason, required,
// max 255 chars (stored in delivery_session_order.fail_reason for both transitions).
type DeliveryStopReasonRequest struct {
	Reason string `json:"reason"`
}

type DeliverySession struct {
	DeliverySessionID string           `json:"delivery_session_id"`
	UserID            string           `json:"user_id"`
	MerchantID        string           `json:"merchant_id"`
	StartDate         string           `json:"start_date"`
	Distance          string           `json:"distance"`
	Duration          string           `json:"duration"`
	Status            string           `json:"status"`
	Orders            []models.Order   `json:"orders"`
	DeliveryMan       models.OrderUser `json:"delivery_man"`
}
