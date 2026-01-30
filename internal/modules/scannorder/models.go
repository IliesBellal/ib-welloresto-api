package scannorder

import (
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/customers"
	"welloresto-api/internal/modules/orders"
	stripeclient "welloresto-api/internal/modules/stripe"
)

type MerchantResponse struct {
	Status   string        `json:"status"`
	Merchant *MerchantData `json:"merchant,omitempty"`
	Error    string        `json:"error,omitempty"`
}

type MerchantData struct {
	MerchantID   int64               `json:"merchant_id"`
	BusinessName string              `json:"business_name"`
	Phone        string              `json:"phone"`
	Currency     string              `json:"currency"`
	IsOpen       bool                `json:"is_open"`
	Status       *MerchantOpenStatus `json:"status"`
	Address      struct {
		Address string  `json:"address"`
		Lat     float64 `json:"lat"`
		Lng     float64 `json:"lng"`
	} `json:"address"`

	Design struct {
		PrimaryColor string `json:"primary_color"`
		TextColor    string `json:"text_color_on_primary_color"`
	} `json:"design"`

	Fee struct {
		DeliveryFees      float64 `json:"delivery_fees"`
		DeliveryFeesLimit float64 `json:"delivery_fees_limit"`
	} `json:"fees"`

	QRCode struct {
		LocationID     *int64  `json:"location_id"`
		LocationName   *string `json:"location_name"`
		MenuOnly       bool    `json:"menu_only"`
		UserID         *int64  `json:"user_id"`
		LastWaiterCall *string `json:"last_waiter_call"`
		OrderID        *int64  `json:"order_id"`
	} `json:"qr_code"`

	AdvanceOrder struct {
		EnableAdvanceOrders bool                `json:"enable_advance_orders"`
		AvailableSlots      map[string][]string `json:"available_slots"`
	} `json:"advance_order"`
}

type MenuResponse struct {
	Status string    `json:"status"`
	Menu   *MenuData `json:"menu,omitempty"`
	Error  string    `json:"error,omitempty"`
}

type MenuData struct {
	OrderType       string                   `json:"order_type"`
	ProductTypes    []models.ProductCategory `json:"products_types"`
	LoyaltyPrograms []map[string]interface{} `json:"loyalty_programs,omitempty"`
	Discounts       []map[string]interface{} `json:"discounts,omitempty"`
}

type MerchantOpenStatus struct {
	OpenHours  bool   `json:"open_hours"`  // = POS open (procédure stockée)
	OpenStatus bool   `json:"open_status"` // = merchant enabled + scannorder activated
	NextStart  string `json:"next_start"`
}

type PricingSNORequest struct {
	QRCode string    `json:"qr_code"`
	Order  *SNOOrder `json:"order"`

	// Champs enrichis par le service
	MerchantID       string
	IsSNO            bool
	DayOfWeek        int
	Time             string
	IsInDeliveryZone bool
}

type SNOOrder struct {
	OrderType string              `json:"order_type"` // DELIVERY / IN / TAKE_AWAY
	Customer  *customers.Customer `json:"customer,omitempty"`
	Products  []SNOProduct        `json:"products"`
}

type SNOCustomer struct {
	CustomerLat float64 `json:"customer_lat"`
	CustomerLng float64 `json:"customer_lng"`
	Phone       string  `json:"phone"`
	MerchantID  int64   `json:"-"`
}

type SNOProduct struct {
	ProductID int64 `json:"product_id"`
}

type Dependencies struct {
	Repo            *Repository
	OrderingService orders.OrdersService // ton module Ordering
	StripeClient    *stripeclient.Client
}

type CreateOrderRequest struct {
	QRCode              string                 `json:"qr_code"`
	CheckoutSessionType string                 `json:"checkout_session_type,omitempty"`
	Order               map[string]interface{} `json:"order"`
	Merchant            map[string]interface{} `json:"merchant,omitempty"`

	MerchantID int64  `json:"-"`
	DayOfWeek  int    `json:"-"`
	Time       string `json:"-"`
	IsSNO      bool   `json:"-"`
}
