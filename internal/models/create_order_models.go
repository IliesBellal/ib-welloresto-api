package models

// Requête principale envoyée par le POS
type CreateOrderRequest struct {
	MerchantID  string       `json:"merchant_id"`
	DeviceID    *string      `json:"device_id"`
	MerchantLat *float64     `json:"merchant_lat"`
	MerchantLng *float64     `json:"merchant_lng"`
	Order       OrderPayload `json:"order"`
}

type OrderPayload struct {
	OrderID                     *string               `json:"order_id"`
	OrderNum                    *int64                `json:"order_num"`
	TTC                         int                   `json:"TTC"`
	TVA                         int                   `json:"TVA"`
	HT                          int                   `json:"HT"`
	Products                    []OrderProductPayload `json:"products"`
	Customer                    *CustomerPayload      `json:"customer"`
	OrderType                   string                `json:"order_type"`
	PlacesSettings              int                   `json:"places_settings"`
	CreatedBy                   *string               `json:"created_by"`
	Responsible                 *string               `json:"responsible"`
	Comment                     *string               `json:"comment"`
	Payments                    []PaymentPayload      `json:"payments"`
	Locations                   []OrderLocation       `json:"locations"`
	DeliveryFees                int                   `json:"delivery_fees"`
	EstimatedReady              string                `json:"estimated_ready"`
	IsScheduled                 bool                  `json:"is_scheduled"`
	UseCustomerTemporaryAddress bool                  `json:"use_customer_temporary_address"`
	MerchantApproval            string                `json:"merchant_approval"`
	BrandStatus                 string                `json:"brand_status"`
	DelayID                     *string               `json:"delay_id"`
	PagerNumber                 *string               `json:"pager_number"`
	OnlinePayment               bool                  `json:"online_payment"`
	BookingID                   *string               `json:"booking_id"`
}

type CustomerPayload struct {
	CustomerID   *string  `json:"customer_id"`
	MerchantID   *string  `json:"merchant_id"`
	Name         *string  `json:"customer_name"`
	Tel          *string  `json:"customer_tel"`
	Address      *string  `json:"customer_address"`
	Lat          *float64 `json:"customer_lat"`
	Lng          *float64 `json:"customer_lng"`
	DoorNumber   *string  `json:"customer_door_number"`
	FloorNumber  *string  `json:"customer_floor_number"`
	Additional   *string  `json:"customer_additional_address"`
	BusinessName *string  `json:"customer_business_name"`
	Birthdate    *string  `json:"customer_birthdate"`
}

type OrderProductPayload struct {
	ProductID  string  `json:"product_id"`
	Quantity   int     `json:"quantity"`
	Price      int     `json:"price"`
	DiscountID *string `json:"discount_id"`
	DelayID    *string `json:"delay_id"`

	Extra   []OrderExtraPayload      `json:"extra"`
	Without []OrderWithoutPayload    `json:"without"`
	Config  *OrderConfigPayload      `json:"configuration"`
	Comment *OrderItemCommentPayload `json:"comment"`
}

type OrderExtraPayload struct {
	ComponentID string `json:"component_id"`
	Price       int    `json:"price"`
}

type OrderWithoutPayload struct {
	ComponentID string `json:"component_id"`
}

type OrderConfigPayload struct {
	Attributes []ConfigAttribute `json:"attributes"`
}

type ConfigAttribute struct {
	ID      string                  `json:"id"`
	Options []ConfigAttributeOption `json:"options"`
}

type ConfigAttributeOption struct {
	ID       string `json:"id"`
	Quantity int    `json:"quantity"`
}

type OrderItemCommentPayload struct {
	UserID  string `json:"user_id"`
	Content string `json:"content"`
}

type OrderLocation struct {
	LocationID string `json:"location_id"`
}

type PaymentPayload struct {
	Amount int    `json:"amount"`
	MOP    string `json:"mop"`
}

type CreateOrderResult struct {
	Status     string     `json:"status"`
	OrderID    string     `json:"order_id"`
	OrderNum   int64      `json:"order_num"`
	Action     string     `json:"action"`
	OrderItems []UsedItem `json:"order_items"`
}

type UsedItem struct {
	OrderItemID string `json:"order_item_id"`
	Quantity    int    `json:"quantity"`
}
