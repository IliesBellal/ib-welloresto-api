package models

// Requête principale envoyée par le POS
type CreateOrderRequest struct {
	MerchantID  string       `json:"merchant_id"`
	DeviceID    *int64       `json:"device_id"`
	MerchantLat *float64     `json:"merchant_lat"`
	MerchantLng *float64     `json:"merchant_lng"`
	Order       OrderPayload `json:"order"`
}

type OrderPayload struct {
	OrderID                     *int64                `json:"order_id"`
	OrderNum                    *int64                `json:"order_num"`
	TTC                         float64               `json:"TTC"`
	TVA                         float64               `json:"TVA"`
	HT                          float64               `json:"HT"`
	Products                    []OrderProductPayload `json:"products"`
	Customer                    *CustomerPayload      `json:"customer"`
	OrderType                   string                `json:"order_type"`
	PlacesSettings              int                   `json:"places_settings"`
	CreatedBy                   *string               `json:"created_by"`
	Responsible                 *string               `json:"responsible"`
	Comment                     *string               `json:"comment"`
	Payments                    []PaymentPayload      `json:"payments"`
	Locations                   []OrderLocation       `json:"locations"`
	DeliveryFees                float64               `json:"delivery_fees"`
	EstimatedReady              string                `json:"estimated_ready"`
	IsScheduled                 bool                  `json:"is_scheduled"`
	UseCustomerTemporaryAddress bool                  `json:"use_customer_temporary_address"`
	MerchantApproval            string                `json:"merchant_approval"`
	BrandStatus                 string                `json:"brand_status"`
	DelayID                     *int64                `json:"delay_id"`
	PagerNumber                 *string               `json:"pager_number"`
	OnlinePayment               bool                  `json:"online_payment"`
	BookingID                   *int64                `json:"booking_id"`
}

type CustomerPayload struct {
	CustomerID   *int64   `json:"customer_id"`
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
	ProductID  int64   `json:"product_id"`
	Quantity   float64 `json:"quantity"`
	Price      float64 `json:"price"`
	DiscountID *int64  `json:"discount_id"`
	DelayID    *int64  `json:"delay_id"`

	Extra   []OrderExtraPayload      `json:"extra"`
	Without []OrderWithoutPayload    `json:"without"`
	Config  *OrderConfigPayload      `json:"configuration"`
	Comment *OrderItemCommentPayload `json:"comment"`
}

type OrderExtraPayload struct {
	ComponentID int64   `json:"component_id"`
	Price       float64 `json:"price"`
}

type OrderWithoutPayload struct {
	ComponentID int64 `json:"component_id"`
}

type OrderConfigPayload struct {
	Attributes []ConfigAttribute `json:"attributes"`
}

type ConfigAttribute struct {
	ID      int64                   `json:"id"`
	Options []ConfigAttributeOption `json:"options"`
}

type ConfigAttributeOption struct {
	ID       int64   `json:"id"`
	Quantity float64 `json:"quantity"`
}

type OrderItemCommentPayload struct {
	UserID  int64  `json:"user_id"`
	Content string `json:"content"`
}

type OrderLocation struct {
	LocationID int64 `json:"location_id"`
}

type PaymentPayload struct {
	Amount float64 `json:"amount"`
	MOP    string  `json:"mop"`
}

type CreateOrderResult struct {
	Status     int64      `json:"status"`
	OrderID    int64      `json:"order_id"`
	OrderNum   int64      `json:"order_num"`
	Action     string     `json:"action"`
	OrderItems []UsedItem `json:"order_items"`
}

type UsedItem struct {
	OrderItemID int64   `json:"order_item_id"`
	Quantity    float64 `json:"quantity"`
}
