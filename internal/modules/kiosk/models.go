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

// KioskRow mappe la table kiosks. id est un VARCHAR(64) généré côté backend
// (helpers.GeneratePrefixedID(helpers.KioskIDPrefix)) — plus de distinction
// id technique (BIGINT) / public_id séparée, voir docs/KIOSK_DECISIONS.md.
type KioskRow struct {
	ID                string
	MerchantID        string
	Name              string
	LocationID        *string
	Status            string
	AppVersion        *string
	HardwareModel     *string
	AdminPinEncrypted []byte
	OSVersion         *string
	LastHeartbeatAt   *time.Time
	LastIP            *string
	LastError         *string
	LastErrorAt       *time.Time
	Enabled           bool
	CreatedAt         time.Time
	UpdatedAt         *time.Time
}

// KioskDeviceTokenRow mappe la table kiosk_device_tokens.
type KioskDeviceTokenRow struct {
	ID         string
	KioskID    string
	TokenHash  string
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	LastUsedAt *time.Time
	CreatedAt  time.Time
}

// KioskSettingsRow mappe la table kiosk_settings. BusinessName n'est PAS une
// colonne de kiosk_settings : c'est merchant.fullName, attaché à ce struct
// après coup par GetKioskSettings (voir repository.go) pour que l'appelant
// n'ait qu'une seule structure à lire — voir docs/KIOSK_DECISIONS.md.
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
	IdleVideoURL         *string
	PrimaryColor         *string
	BusinessName         *string
	CreatedAt            time.Time
	UpdatedAt            *time.Time
}

// EnrollmentCodeRow mappe la table kiosk_enrollment_codes. KioskID référence
// kiosks.id (VARCHAR(64)) — nil tant que le code n'a pas encore servi.
type EnrollmentCodeRow struct {
	ID              string
	MerchantID      string
	CodeHash        string
	KioskID         *string
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

// EnrollResponse — admin_pin n'est retourné en clair qu'à l'enrôlement et à
// la régénération (voir AdminPinResponse) : entre ces deux moments, seule sa
// forme chiffrée (kiosks.admin_pin_encrypted) est stockée, déchiffrée à la
// demande pour la consultation back-office — voir docs/KIOSK_DECISIONS.md.
type EnrollResponse struct {
	KioskID      string `json:"kiosk_id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    string `json:"expires_at"`
	AdminPin     string `json:"admin_pin"`
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

// HeartbeatResponse — kiosk_status/enabled sont le mécanisme de fallback si
// le WebSocket est coupé : la borne vérifie ces champs à chaque heartbeat
// (toutes les 5 min côté Flutter) pour rattraper un kiosk_status_changed
// manqué pendant la coupure.
type HeartbeatResponse struct {
	Status      string `json:"status"`
	KioskStatus string `json:"kiosk_status"`
	Enabled     bool   `json:"enabled"`
}

// SetKioskStatusRequest — body de POST /pos/kiosk/{kiosk_id}/status (staff,
// depuis l'app POS).
type SetKioskStatusRequest struct {
	Enabled bool `json:"enabled"`
}

// ReportUnavailableRequest — body de POST /kiosk/status/unavailable, envoyé
// par la borne elle-même (device, KioskAuth) quand elle détecte un problème.
type ReportUnavailableRequest struct {
	Reason string `json:"reason"` // connection_lost | app_error | manual
}

// VerifyAdminPinRequest — body de POST /kiosk/auth/verify-admin-pin (device,
// KioskAuth). La borne est déjà authentifiée ; ce PIN ne fait que déverrouiller
// l'écran admin local, voir docs/KIOSK_DECISIONS.md.
type VerifyAdminPinRequest struct {
	Pin string `json:"pin"`
}

type VerifyAdminPinResponse struct {
	Valid bool `json:"valid"`
}

// AdminPinResponse — réponse commune à GET .../admin-pin (consultation,
// déchiffré) et POST .../regenerate-admin-pin (nouveau PIN) côté back-office.
// Le PIN est en clair dans les deux cas : c'est tout l'intérêt du
// chiffrement réversible (admin_pin_encrypted) par rapport à un hash —
// voir docs/KIOSK_DECISIONS.md.
type AdminPinResponse struct {
	AdminPin string `json:"admin_pin"`
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
	LastIP          *string `json:"last_ip"`
	Enabled         bool    `json:"enabled"`
	CreatedAt       string  `json:"created_at"`
}

type ListKioskDevicesResponse struct {
	Devices []KioskDeviceResponse `json:"devices"`
}

// UpdateKioskDeviceRequest porte la mise à jour partielle d'une borne — name
// uniquement pour l'instant (voir docs/KIOSK_DECISIONS.md).
type UpdateKioskDeviceRequest struct {
	Name string `json:"name"`
}

// EnrollmentCodeListItem représente un code d'enrôlement en attente côté
// back-office — jamais le code en clair ni son hash.
type EnrollmentCodeListItem struct {
	ID        string  `json:"id"`
	CreatedAt string  `json:"created_at"`
	ExpiresAt string  `json:"expires_at"`
	UsedAt    *string `json:"used_at"`
}

type ListEnrollmentCodesResponse struct {
	Codes []EnrollmentCodeListItem `json:"codes"`
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
	IdleVideoURL         *string `json:"idle_video_url"`
	PrimaryColor         *string `json:"primary_color"`

	// BusinessName — merchant.fullName, en lecture seule (jamais modifiable
	// via UpdateKioskSettingsRequest : le nom de l'établissement se gère
	// ailleurs, pas dans les paramètres Kiosk) — voir docs/KIOSK_DECISIONS.md.
	BusinessName *string `json:"business_name"`
}

// UpdateKioskSettingsRequest — logo_url et idle_image_url ne sont PAS des
// champs configurables ici : ils passent exclusivement par les endpoints
// d'upload dédiés (/settings/logo, /settings/idle-image), voir
// docs/KIOSK_DECISIONS.md.
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
	ProductID           string   `json:"product_id"`
	Quantity            int      `json:"quantity"`
	SelectedOptionIDs   []string `json:"selected_option_ids,omitempty"`
	Notes               string   `json:"notes,omitempty"`
	WithoutComponentIDs []string `json:"without_component_ids,omitempty"`
}

type CreateKioskOrderRequest struct {
	FulfillmentType string           `json:"fulfillment_type"` // DINE_IN | TAKE_AWAY
	IdempotencyKey  string           `json:"idempotency_key"`
	Items           []KioskOrderItem `json:"items"`
	PaymentMethod   string           `json:"payment_method"` // pay_at_counter uniquement cet incrément
	DiscountCode    *string          `json:"discount_code,omitempty"`
	OrderNotes      string           `json:"order_notes,omitempty"`
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

// KioskDiscount reprend exactement les champs JSON de scannorder.Discount
// (même nommage, voir internal/modules/scannorder/models.go) pour rester
// cohérent entre les deux canaux côté client.
type KioskDiscount struct {
	DiscountID         string  `json:"discount_id"`
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

type KioskDiscountsResponse struct {
	Discounts []KioskDiscount `json:"discounts"`
}
