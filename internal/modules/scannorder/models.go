package scannorder

import (
	"welloresto-api/internal/models"
)

type MerchantResponse struct {
	Status   string        `json:"status"`
	Merchant *MerchantData `json:"merchant,omitempty"`
	Error    string        `json:"error,omitempty"`
}

type Address struct {
	Address string  `json:"address"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
}

type MerchantDesign struct {
	PrimaryColor string `json:"primary_color"`
	TextColor    string `json:"text_color_on_primary_color"`
}

type MerchantFees struct {
	DeliveryFees      float64 `json:"delivery_fees"`
	DeliveryFeesLimit float64 `json:"delivery_fees_limit"`
}

type TimeSlot struct {
	Time      string `json:"time"`
	Available bool   `json:"available"`
}

type AdvanceOrder struct {
	EnableAdvanceOrders bool                  `json:"enable_advance_orders"`
	AvailableSlots      map[string][]TimeSlot `json:"available_slots"` // On change le type ici
}

// Ta structure MerchantData mise à jour
type MerchantData struct {
	MerchantID      string              `json:"merchant_id"`
	BusinessName    string              `json:"business_name"`
	Phone           string              `json:"phone"`
	Currency        string              `json:"currency"`
	IsOpen          bool                `json:"is_open"`
	Status          *MerchantOpenStatus `json:"status"`
	Address         Address             `json:"address"`
	Design          MerchantDesign      `json:"design"`
	Fee             MerchantFees        `json:"fees"`
	PreparationTime int                 `json:"preparation_time"`

	OrderTypes   OrderTypes   `json:"order_types"`
	PaymentTypes PaymentTypes `json:"payment_types"`

	QRCode struct {
		LocationID     *string `json:"location_id"`
		LocationName   *string `json:"location_name"`
		MenuOnly       bool    `json:"menu_only"`
		UserID         *string `json:"user_id"`
		LastWaiterCall *int    `json:"last_waiter_call"`
		OrderID        *string `json:"order_id"`
	} `json:"qr_code"`
	AdvanceOrder AdvanceOrder `json:"advance_order"`
}

type OrderTypes struct {
	TakeawayEnabled   bool `json:"takeaway_enabled"`
	TakeawayAvailable bool `json:"takeaway_available"`
	DeliveryEnabled   bool `json:"delivery_enabled"`
	DeliveryAvailable bool `json:"delivery_available"`
	InEnabled         bool `json:"in_enabled"`
	InAvailable       bool `json:"in_available"`
}

type PaymentTypes struct {
	Online bool `json:"online"`
	Cash   bool `json:"cash"`
}

type MenuResponse struct {
	Status string    `json:"status"`
	Menu   *MenuData `json:"menu,omitempty"`
	Error  string    `json:"error,omitempty"`
}

type MenuData struct {
	OrderType       string                   `json:"order_type"`
	ProductTypes    []models.ProductCategory `json:"products_types"`
	LoyaltyPrograms []LoyaltyProgram         `json:"loyalty_programs,omitempty"`
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

// --- Brand ---

type BrandResponse struct {
	Status    string            `json:"status"`
	Brand     *BrandData        `json:"brand,omitempty"`
	Merchants []MerchantSummary `json:"merchants"`
	Error     string            `json:"error,omitempty"`
}

type BrandData struct {
	BrandID     string  `json:"brand_id"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	LogoURL     *string `json:"logo_url"`
	BannerURL   *string `json:"banner_url"`
	Description *string `json:"description"`
}

type MerchantSummary struct {
	MerchantID      string   `json:"merchant_id"`
	BusinessName    string   `json:"business_name"`
	IsOpen          bool     `json:"is_open"`
	PreparationTime int      `json:"preparation_time"`
	DistanceKm      *float64 `json:"distance_km"`
	Address         Address  `json:"address"`
	TakeawayEnabled bool     `json:"takeaway_enabled"`
	DeliveryEnabled bool     `json:"delivery_enabled"`
}

// BrandMerchantRow is used internally to scan merchant rows from the DB.
// BrandMerchantRow is used internally to scan merchant rows from the DB.
type BrandMerchantRow struct {
	MerchantID      string
	FullName        string
	Address         string
	Lat             float64
	Lng             float64
	Timezone        string
	LogoURL         *string
	BannerURL       *string
	TakeawayEnabled bool
	DeliveryEnabled bool
	PrepTimeMode    string
	PrepTime        int
	DistanceKm      *float64
}

// --- Delivery Zone Check ---

type DeliveryCheckRequest struct {
	Address string  `json:"address"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
}

type DeliveryCheckResponse struct {
	Status                  string  `json:"status"`
	MinOrderAmount          float64 `json:"min_order_amount,omitempty"`
	DeliveryFee             int     `json:"delivery_fee,omitempty"`
	Message                 string  `json:"message,omitempty"`
	DistanceKm              float64 `json:"distance_km,omitempty"`
	DeliveryDistanceLimitKm float64 `json:"delivery_distance_limit_km,omitempty"`
}

// --- Loyalty Programs & Discounts ---

type LoyaltyProgramsResponse struct {
	LoyaltyPrograms []LoyaltyProgram `json:"loyalty_programs"`
}

type LoyaltyProgram struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type DiscountsResponse struct {
	Discounts []Discount `json:"discounts"`
}

// --- Upsell ---

type UpsellProduct struct {
	ProductID   string  `json:"product_id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Price       int64   `json:"price"`
	ImageURL    *string `json:"image_url,omitempty"`
}

type UpsellResponse struct {
	Products []UpsellProduct `json:"products"`
}
