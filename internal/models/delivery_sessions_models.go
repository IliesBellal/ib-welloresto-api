package models

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
	Orders            []Order   `json:"orders"`
	DeliveryMan       OrderUser `json:"delivery_man"`
}
