package stripeclient

type CheckoutOrderItem struct {
	OrderItemID *int64 `json:"order_item_id,omitempty"`
	Quantity    *int64 `json:"quantity,omitempty"`
}
