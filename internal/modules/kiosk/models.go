package kiosk

import (
	"time"

	"welloresto-api/internal/middleware"
)

// AuthenticatedKiosk est un alias du type porté par middleware (voir
// internal/middleware/kiosk_auth.go) — défini là-bas pour que ce middleware
// n'ait jamais besoin d'importer le module kiosk. Depuis l'incrément 2,
// kiosk.Service consomme menuService/ordersService/ordersLifeCycleService,
// qui importent eux-mêmes middleware ; le sens unique de dépendance est donc
// kiosk -> middleware, jamais l'inverse.
type AuthenticatedKiosk = middleware.AuthenticatedKiosk

// KioskRow mappe la table kiosks.
type KioskRow struct {
	ID              int64
	PublicID        string
	MerchantID      string
	Name            string
	LocationID      *string
	Status          string
	AppVersion      *string
	HardwareModel   *string
	OSVersion       *string
	LastHeartbeatAt *time.Time
	LastIP          *string
	LastError       *string
	LastErrorAt     *time.Time
	Enabled         bool
	CreatedAt       time.Time
	UpdatedAt       *time.Time
}

// KioskDeviceTokenRow mappe la table kiosk_device_tokens.
type KioskDeviceTokenRow struct {
	ID         int64
	KioskID    int64
	TokenHash  string
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	LastUsedAt *time.Time
	CreatedAt  time.Time
}

// KioskSettingsRow mappe la table kiosk_settings.
type KioskSettingsRow struct {
	MerchantID           string
	FulfillmentDineIn    bool
	FulfillmentTakeAway  bool
	ForceFulfillmentType *string
	PagerNumberRequired  bool
	ShowAllergens        bool
	InactivityTimeoutSec int
	UpsellEnabled        bool
	PayAtCounterEnabled  bool
	CardPaymentEnabled   bool
	LogoURL              *string
	IdleImageURL         *string
	PrimaryColor         *string
	CreatedAt            time.Time
	UpdatedAt            *time.Time
}

// EnrollmentCodeRow mappe la table kiosk_enrollment_codes.
type EnrollmentCodeRow struct {
	ID              int64
	MerchantID      string
	CodeHash        string
	KioskID         *int64
	ExpiresAt       time.Time
	UsedAt          *time.Time
	CreatedByUserID *string
	CreatedAt       time.Time
}

// ---- Requests / Responses ----

type EnrollRequest struct {
	EnrollmentCode string `json:"enrollment_code"`
	Name           string `json:"name"`
	HardwareModel  string `json:"hardware_model"`
	OSVersion      string `json:"os_version"`
	AppVersion     string `json:"app_version"`
}

type EnrollResponse struct {
	KioskID      string `json:"kiosk_id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    string `json:"expires_at"`
}

type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type RefreshTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    string `json:"expires_at"`
}

type HeartbeatRequest struct {
	AppVersion string `json:"app_version"`
}

type HeartbeatResponse struct {
	Status string `json:"status"`
}

// ---- Admin (back-office) requests / responses ----

type GenerateEnrollmentCodeResponse struct {
	Code      string `json:"code"`
	ExpiresAt string `json:"expires_at"`
}

type KioskDeviceResponse struct {
	KioskID         string  `json:"kiosk_id"`
	Name            string  `json:"name"`
	Status          string  `json:"status"`
	AppVersion      *string `json:"app_version"`
	HardwareModel   *string `json:"hardware_model"`
	OSVersion       *string `json:"os_version"`
	LastHeartbeatAt *string `json:"last_heartbeat_at"`
	CreatedAt       string  `json:"created_at"`
}

type ListKioskDevicesResponse struct {
	Devices []KioskDeviceResponse `json:"devices"`
}

type KioskSettingsResponse struct {
	FulfillmentDineIn    bool    `json:"fulfillment_dine_in"`
	FulfillmentTakeAway  bool    `json:"fulfillment_take_away"`
	ForceFulfillmentType *string `json:"force_fulfillment_type"`
	PagerNumberRequired  bool    `json:"pager_number_required"`
	ShowAllergens        bool    `json:"show_allergens"`
	InactivityTimeoutSec int     `json:"inactivity_timeout_sec"`
	UpsellEnabled        bool    `json:"upsell_enabled"`
	PayAtCounterEnabled  bool    `json:"pay_at_counter_enabled"`
	CardPaymentEnabled   bool    `json:"card_payment_enabled"`
	LogoURL              *string `json:"logo_url"`
	IdleImageURL         *string `json:"idle_image_url"`
	PrimaryColor         *string `json:"primary_color"`
}

type UpdateKioskSettingsRequest struct {
	FulfillmentDineIn    *bool   `json:"fulfillment_dine_in"`
	FulfillmentTakeAway  *bool   `json:"fulfillment_take_away"`
	ForceFulfillmentType *string `json:"force_fulfillment_type"`
	PagerNumberRequired  *bool   `json:"pager_number_required"`
	ShowAllergens        *bool   `json:"show_allergens"`
	InactivityTimeoutSec *int    `json:"inactivity_timeout_sec"`
	UpsellEnabled        *bool   `json:"upsell_enabled"`
	PayAtCounterEnabled  *bool   `json:"pay_at_counter_enabled"`
	CardPaymentEnabled   *bool   `json:"card_payment_enabled"`
	LogoURL              *string `json:"logo_url"`
	IdleImageURL         *string `json:"idle_image_url"`
	PrimaryColor         *string `json:"primary_color"`
}

// ---- Incrément 2 : menu, upsell, pricing, commandes ----
//
// NOTE produit_id : la colonne products.product_id est un VARCHAR (UUID
// applicatif), pas un entier — voir internal/models/menu_models.go
// (ProductEntry.ProductID string) et docs/ARCHITECTURE_API.md §6.2. Tous les
// identifiants produit de ce module sont donc des string, jamais des int64
// (écart documenté par rapport au brief initial dans KIOSK_DECISIONS.md).

type KioskMenuResponse struct {
	Categories []KioskCategory `json:"categories"`
	ETag       string          `json:"etag"`
}

type KioskCategory struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	SortOrder int            `json:"sort_order"`
	ImageURL  string         `json:"image_url,omitempty"`
	Products  []KioskProduct `json:"products"`
}

type KioskProduct struct {
	ID               string               `json:"id"`
	Name             string               `json:"name"`
	Description      string               `json:"description,omitempty"`
	PriceCents       int64                `json:"price_cents"`
	ImageURL         string               `json:"image_url,omitempty"`
	Available        bool                 `json:"available"`
	AvailableOnKiosk bool                 `json:"available_on_kiosk"`
	Allergens        []string             `json:"allergens,omitempty"`
	Tags             []string             `json:"tags,omitempty"`
	ModifierGroups   []KioskModifierGroup `json:"modifier_groups,omitempty"`
}

type KioskModifierGroup struct {
	ID       string                `json:"id"`
	Name     string                `json:"name"`
	Min      int                   `json:"min"`
	Max      int                   `json:"max"`
	Required bool                  `json:"required"`
	Options  []KioskModifierOption `json:"options"`
}

type KioskModifierOption struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	PriceDeltaCents int    `json:"price_delta_cents"`
}

type KioskUpsellRequest struct {
	CartProductIDs []string `json:"cart_product_ids"`
}

type KioskUpsellResponse struct {
	Suggestions []KioskUpsellSuggestion `json:"suggestions"`
	Source      string                  `json:"source"`
}

type KioskUpsellSuggestion struct {
	ProductID  string `json:"product_id"`
	Name       string `json:"name"`
	PriceCents int64  `json:"price_cents"`
	ImageURL   string `json:"image_url,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type KioskPricingItem struct {
	ProductID         string   `json:"product_id"`
	Quantity          int      `json:"quantity"`
	SelectedOptionIDs []string `json:"selected_option_ids,omitempty"`
	Notes             string   `json:"notes,omitempty"`
}

type KioskPricingRequest struct {
	FulfillmentType string             `json:"fulfillment_type"`
	Items           []KioskPricingItem `json:"items"`
	DiscountCode    *string            `json:"discount_code,omitempty"`
}

type KioskPricingResponse struct {
	ItemsTotalCents int64 `json:"items_total_cents"`
	DiscountCents   int64 `json:"discount_cents"`
	TaxCents        int64 `json:"tax_cents"`
	TotalCents      int64 `json:"total_cents"`
}

type KioskOrderItem struct {
	ProductID         string   `json:"product_id"`
	Quantity          int      `json:"quantity"`
	SelectedOptionIDs []string `json:"selected_option_ids,omitempty"`
	Notes             string   `json:"notes,omitempty"`
}

type CreateKioskOrderRequest struct {
	FulfillmentType string           `json:"fulfillment_type"` // DINE_IN | TAKE_AWAY
	IdempotencyKey  string           `json:"idempotency_key"`
	Items           []KioskOrderItem `json:"items"`
	PaymentMethod   string           `json:"payment_method"` // pay_at_counter uniquement cet incrément
	DiscountCode    *string          `json:"discount_code,omitempty"`
}

type CreateKioskOrderResponse struct {
	OrderID       string `json:"order_id"`
	DisplayNumber string `json:"display_number"`
	Status        string `json:"status"`
	TotalCents    int64  `json:"total_cents"`
}

type CounterPaymentResponse struct {
	OrderID       string `json:"order_id"`
	DisplayNumber string `json:"display_number"`
	Status        string `json:"status"`
	PickupCode    string `json:"pickup_code"`
	QRPayload     string `json:"qr_payload"`
}

type KioskOrderResponse struct {
	OrderID         string `json:"order_id"`
	DisplayNumber   string `json:"display_number"`
	Status          string `json:"status"`
	FulfillmentType string `json:"fulfillment_type,omitempty"`
	TotalCents      int64  `json:"total_cents"`
	CreatedAt       string `json:"created_at,omitempty"`
}
