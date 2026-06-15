package delivery_sessions

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
//
// DeletionReasonID and Comment are optional structured fields (034_delivery_stop_deletion_reason):
// when DeletionReasonID is set, it is stored in delivery_session_order.deletion_reason_id
// (references deletion_reasons.deletion_reason_id) along with Comment in
// deletion_comment, in addition to fail_reason. When absent, only fail_reason is
// written (legacy behavior, unchanged).
type DeliveryStopReasonRequest struct {
	Reason           string  `json:"reason"`
	DeletionReasonID *string `json:"deletion_reason_id,omitempty"`
	Comment          *string `json:"comment,omitempty"`
}

