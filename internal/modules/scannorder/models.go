package scannorder

import (
	"welloresto-api/internal/models"
)

type MerchantResponse struct {
	Status   string        `json:"status"`
	Merchant *MerchantData `json:"merchant,omitempty"`
	Error    string        `json:"error,omitempty"`
}

type MerchantData struct {
	MerchantID   string              `json:"merchant_id"`
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
		LocationID     *string `json:"location_id"`
		LocationName   *string `json:"location_name"`
		MenuOnly       bool    `json:"menu_only"`
		UserID         *string `json:"user_id"`
		LastWaiterCall *int    `json:"last_waiter_call"`
		OrderID        *string `json:"order_id"`
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
	Discounts       []Discount               `json:"discounts,omitempty"`
}

type MerchantOpenStatus struct {
	OpenHours  bool   `json:"open_hours"`  // = POS open (procédure stockée)
	OpenStatus bool   `json:"open_status"` // = merchant enabled + scannorder activated
	NextStart  string `json:"next_start"`
}

type SNOProduct struct {
	ProductID int64 `json:"product_id"`
}

type DeliveryZoneResult struct {
	InZone         bool
	DistanceMeters float64
	DistanceKm     float64
	EstimatedFee   int
}

type Discount struct {
	DiscountID         int     `json:"discount_id"`
	DiscountOrderType  string  `json:"discount_order_type"`
	DiscountCode       *string `json:"discount_code"`
	DiscountDesc       string  `json:"discount_desc"`
	DiscountName       string  `json:"discount_name"`
	DiscountValue      int     `json:"discount_value"`
	DiscountUnit       string  `json:"discount_unit"`
	MinOrderValue      int     `json:"min_order_value"`
	MinOrderUnit       string  `json:"min_order_unit"`
	MaxDiscountValue   *int    `json:"max_discount_value"`
	MaxDiscountUnit    *string `json:"max_discount_unit"`
	DiscountedQuantity int     `json:"discounted_quantity"`
	IsCumulative       bool    `json:"is_cumulative"`
	Available          bool    `json:"available"`
}
