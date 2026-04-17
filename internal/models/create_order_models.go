package models

// Requête principale envoyée par le POS
type RequestObject struct {
	MerchantID  string       `json:"merchant_id"`
	DeviceID    *string      `json:"device_id"`
	MerchantLat *float64     `json:"merchant_lat"`
	MerchantLng *float64     `json:"merchant_lng"`
	Order       OrderRequest `json:"order"`
}

type OrderRequest struct {
	OrderID                     *string               `json:"order_id,omitempty"`
	Brand                       string                `json:"brand,omitempty"`
	BrandOrderID                *string               `json:"brand_order_id,omitempty"`
	BrandOrderNum               *string               `json:"brand_order_num,omitempty"`
	ParentOrderID               *string               `json:"parent_order_id,omitempty"`
	OrderNum                    *string               `json:"order_num,omitempty"`
	CashRegisterId              *string               `json:"cash_register_id,omitempty"`
	FulfillmentType             *string               `json:"fulfillment_type,omitempty"`
	TTC                         int                   `json:"TTC"`
	TVA                         int                   `json:"TVA"`
	HT                          int                   `json:"HT"`
	Products                    []OrderProductPayload `json:"products"`
	Customer                    *CustomerRequest      `json:"customer"`
	OrderType                   string                `json:"order_type"`
	PlacesSettings              int                   `json:"places_settings"`
	CreatedBy                   *string               `json:"created_by"`
	Responsible                 *string               `json:"responsible,omitempty"`
	Comment                     *string               `json:"comment"`
	Payments                    []PaymentPayload      `json:"payments"`
	Locations                   []OrderLocation       `json:"locations,omitempty"`
	DeliveryFees                int                   `json:"delivery_fees"`
	EstimatedReady              string                `json:"estimated_ready"`
	IsScheduled                 bool                  `json:"is_scheduled"`
	UseCustomerTemporaryAddress bool                  `json:"use_customer_temporary_address"`
	MerchantApproval            string                `json:"merchant_approval"`
	BrandStatus                 string                `json:"brand_status"`
	DelayID                     *string               `json:"delay_id,omitempty"`
	PagerNumber                 *string               `json:"pager_number"`
	OnlinePayment               bool                  `json:"online_payment"`
	IsSNO                       bool                  `json:"is_sno"`
	IsPaid                      bool                  `json:"is_paid"`
	BookingID                   *string               `json:"booking_id,omitempty"`
	Currency                    *string               `json:"currency"`
	UsedRewards                 []*UsedReward         `json:"used_rewards,omitempty"`
}

type CustomerRequest struct {
	CustomerID       *string    `json:"customer_id"`
	BrandCustomerID  *string    `json:"brand_customer_id"`
	CustomerBrand    string     `json:"customer_brand"`
	MerchantID       *string    `json:"merchant_id"`
	Name             *string    `json:"customer_name"`
	Tel              *string    `json:"customer_tel"`
	FirstName        *string    `json:"customer_first_name"`
	LastName         *string    `json:"customer_last_name"`
	Address          *string    `json:"customer_address"`
	Lat              *float64   `json:"customer_lat"`
	Lng              *float64   `json:"customer_lng"`
	DoorNumber       *string    `json:"customer_door_number"`
	FloorNumber      *string    `json:"customer_floor_number"`
	Additional       *string    `json:"customer_additional_address"`
	BusinessName     *string    `json:"customer_business_name"`
	Birthdate        *string    `json:"customer_birthdate"`
	AvailableRewards []DBReward `json:"available_rewards"`

	TemporaryPhone     *string `json:"temporary_phone"`
	TemporaryPhoneCode *string `json:"temporary_phone_code"`
	GooglePlaceID      *string `json:"google_place_id"`
	AdvertisingConsent *bool   `json:"advertising_consent"`
}

type OrderProductPayload struct {
	ProductID       string                   `json:"product_id"`
	Quantity        int                      `json:"quantity"`
	Price           int                      `json:"price"`
	Description     *string                  `json:"description,omitempty"`
	DiscountID      *string                  `json:"discount_id"`
	DiscountName    *string                  `json:"discount_name"`
	DelayID         *string                  `json:"delay_id"`
	ProductName     string                   `json:"product_name"`
	TvaRate         float64                  `json:"tva_rate"`
	DiscountedPrice *int                     `json:"discounted_price"`
	OrderedDate     string                   `json:"ordered_date"`
	Extra           []*OrderExtraPayload     `json:"extra"`
	Without         []*OrderWithoutPayload   `json:"without"`
	Config          *ProductConfiguration    `json:"configuration"`
	Comment         *OrderItemCommentPayload `json:"comment"`
	OrderItemID     *string                  `json:"order_item_id"`
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
	Message         string             `json:"message,omitempty"`
	Status          string             `json:"status"`
	OrderID         string             `json:"order_id,omitempty"`
	OrderNum        *string            `json:"order_num,omitempty"`
	Action          string             `json:"action,omitempty"`
	CheckoutSession *WRCheckoutSession `json:"checkout_session,omitempty"`
}

type WRCheckoutSession struct {
	Status      string `json:"status"`
	RedirectURL string `json:"redirect_url"`
	URL         string `json:"url"`
}

type UsedItem struct {
	OrderItemID string `json:"order_item_id"`
	Quantity    int    `json:"quantity"`
}

type CurrentServiceResponse struct {
	Service      *PerformedService `json:"service"`
	CashRegister *CashRegisterInfo `json:"cash_register"`
	CashDesks    []CashDeskInfo    `json:"cash_desks"`
}

type PerformedService struct {
	ServiceID string  `json:"service_id"`
	StartDate *string `json:"start_date"`
	EndDate   *string `json:"end_date"`
}

type CashRegisterInfo struct {
	DeviceID       string           `json:"device_id"`
	CashRegisterID string           `json:"cash_register_id"`
	CashDesk       CashRegisterDesk `json:"cash_desk"`
}

type CashRegisterDesk struct {
	CashDeskName string `json:"cash_desk_name"`
	CashDeskID   string `json:"cash_desk_id"`
}

type OpenedByInfo struct {
	FirstName *string `json:"first_name"`
	LastName  *string `json:"last_name"`
	UserID    *string `json:"user_id"`
}

type MultipleProductsRequest struct {
	Products []UpdateProductStatusPayload `json:"products"`
}

type UpdateProductStatusPayload struct {
	OrderID          string `json:"order_id"`
	OrderItemID      string `json:"order_item_id"`
	ProductionStatus string `json:"production_status"`
}
