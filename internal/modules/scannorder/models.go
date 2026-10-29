package scannorder

import (
	"time"
	"welloresto-api/internal/models"
)

type MerchantResponse struct {
	Status   string        `json:"status"`
	Merchant *MerchantData `json:"merchant,omitempty"`
	Error    string        `json:"error,omitempty"`
}

type Address struct {
	Address string  `json:"address"`
	City    string  `json:"city,omitempty"`
	ZipCode string  `json:"zip_code,omitempty"`
	Country string  `json:"country,omitempty"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
}

type MerchantDesign struct {
	PrimaryColor string  `json:"primary_color"`
	TextColor    string  `json:"text_color_on_primary_color"`
	LogoURL      *string `json:"logo_url,omitempty"`
	BannerURL    *string `json:"banner_url,omitempty"`
}

type MerchantFees struct {
	DeliveryFees      float64 `json:"delivery_fees"`
	DeliveryFeesLimit float64 `json:"delivery_fees_limit"`
}

type TimeSlot struct {
	Time      string `json:"time"`
	Available bool   `json:"available"`
}

type SlotsByDate struct {
	Date  string     `json:"date"`
	Slots []TimeSlot `json:"slots"`
}

// MerchantData structure containing merchant information and order settings
type MerchantData struct {
	MerchantID             string          `json:"merchant_id"`
	BusinessName           string          `json:"business_name"`
	Phone                  string          `json:"phone"`
	Currency               string          `json:"currency"`
	Status                 *MerchantStatus `json:"status"`
	Address                Address         `json:"address"`
	Design                 MerchantDesign  `json:"design"`
	Fee                    MerchantFees    `json:"fees"`
	PreparationTime        int             `json:"preparation_time"`
	AverageDeliverySeconds *int            `json:"average_delivery_seconds,omitempty"`
	MinimumOrderAmount     float64         `json:"minimum_order_amount"`

	// Description / CuisineType : scannorder_settings.seo_description /
	// seo_cuisine_type, nil quand vides.
	Description *string      `json:"description,omitempty"`
	CuisineType *string      `json:"cuisine_type,omitempty"`
	SEO         *MerchantSEO `json:"seo,omitempty"`
	// Franchise : nil si l'établissement n'est dans aucune franchise ou masqué
	// sur son hub.
	Franchise *MerchantFranchise `json:"franchise"`

	OrderTypes           OrderTypes   `json:"order_types"`
	PaymentTypes         PaymentTypes `json:"payment_types"`
	AdvanceOrdersEnabled bool         `json:"advance_orders_enabled"`

	// DeliveryHours : créneaux de livraison par jour (delivery_hours), absent
	// quand aucun n'est configuré (livraison sur les horaires d'ouverture).
	DeliveryHours []OpenHoursDay `json:"delivery_hours,omitempty"`
	// DeliveryOpenNow : l'heure actuelle tombe dans un créneau de livraison
	// (true sans créneau configuré). Ne tient pas compte de l'ouverture
	// (status.is_open), à combiner côté front.
	DeliveryOpenNow bool `json:"delivery_open_now"`

	QRCode struct {
		LocationID     *string `json:"location_id"`
		LocationName   *string `json:"location_name"`
		MenuOnly       bool    `json:"menu_only"`
		UserID         *string `json:"user_id"`
		LastWaiterCall *int    `json:"last_waiter_call"`
		OrderID        *string `json:"order_id"`
	} `json:"qr_code"`
}

// MerchantSEO porte ce dont le front a besoin pour le référencement d'une page
// établissement. Seul le QR principal (ni table ni serveur) est référencé : une
// page servie sous un autre code doit pointer son canonical vers CanonicalSlug
// et être exclue de l'index.
type MerchantSEO struct {
	// Title : scannorder_settings.seo_title, nil quand vide.
	Title *string `json:"title,omitempty"`
	// CanonicalSlug : code du QR principal du merchant, nil s'il n'en a pas.
	CanonicalSlug *string `json:"canonical_slug"`
	// Indexable : ScanNOrder activé, abonnement ni suspendu ni résilié, et QR
	// principal existant.
	Indexable bool `json:"indexable"`
}

// MerchantSEORow : lecture brute de GetMerchantSEOInfo.
type MerchantSEORow struct {
	City           string
	ZipCode        string
	Country        string
	SEOTitle       string
	SEODescription string
	SEOCuisineType string
	Activated      bool
	Blocked        bool
	MainSlug       *string
	// FranchiseName / FranchiseSlug : franchise dont le hub affiche
	// l'établissement, nil sinon.
	FranchiseName *string
	FranchiseSlug *string
}

// SitemapEntry : une page établissement référençable.
type SitemapEntry struct {
	Slug string `json:"slug"`
	// UpdatedAt : merchant_parameters.last_menu_update.
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

type SitemapResponse struct {
	Merchants []SitemapEntry `json:"merchants"`
	// Franchises : hubs (/hub/{slug}) affichant au moins un établissement.
	Franchises []SitemapEntry `json:"franchises"`
}

type SlotsResponse struct {
	Status         string        `json:"status"`
	AvailableSlots []SlotsByDate `json:"available_slots,omitempty"`
	Error          string        `json:"error,omitempty"`
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

type OpeningPeriod struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type OpenHoursDay struct {
	DayOfWeek int             `json:"day_of_week"`
	DayName   string          `json:"day_name"`
	Hours     []OpeningPeriod `json:"hours"`
}

type MerchantStatus struct {
	IsOpen    bool           `json:"is_open"`
	OpenHours []OpenHoursDay `json:"open_hours"`
	NextStart string         `json:"next_start,omitempty"`
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

// --- Franchise (hub /hub/{slug}) ---

type FranchiseHubResponse struct {
	Status    string                 `json:"status"`
	Franchise *FranchisePublic       `json:"franchise,omitempty"`
	Merchants []FranchiseHubMerchant `json:"merchants"`
}

type FranchisePublic struct {
	FranchiseID string  `json:"franchise_id"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description *string `json:"description"`
	LogoURL     *string `json:"logo_url"`
	BannerURL   *string `json:"banner_url"`
	// BrandColor : couleur du hub (#RRGGBB), nil = thème par défaut.
	BrandColor *string `json:"brand_color"`
	// Announcement : bandeau d'annonce, seulement quand il est affiché
	// (activé, avec un texte, date de fin non dépassée), D34.
	Announcement *HubAnnouncement `json:"announcement,omitempty"`
}

type HubAnnouncement struct {
	Text string  `json:"text"`
	URL  *string `json:"url,omitempty"`
}

// FranchiseHubMerchant : un établissement du hub. Pas de distance : le
// navigateur la calcule depuis Address.Lat/Lng, la position du client ne
// quitte pas son téléphone et la réponse reste la même pour tous (cachable).
type FranchiseHubMerchant struct {
	MerchantID   string  `json:"merchant_id"`
	BusinessName string  `json:"business_name"`
	Slug         string  `json:"slug"`
	URL          string  `json:"url"`
	LogoURL      *string `json:"logo_url"`
	Address      Address `json:"address"`
	IsOpen       bool    `json:"is_open"`
	// NextOpening : prochaine ouverture ("2006-01-02 15:04:05", heure locale de
	// l'établissement), nil si ouvert ou inconnue.
	NextOpening     *string    `json:"next_opening"`
	PreparationTime int        `json:"preparation_time"`
	OrderTypes      OrderTypes `json:"order_types"`
	// ReservationURL : page de réservation en ligne, nil si l'établissement ne
	// prend pas de réservation.
	ReservationURL *string `json:"reservation_url"`
	// AdvanceOrdersEnabled : commande programmée possible (le hub propose
	// « Précommander » quand l'établissement est fermé).
	AdvanceOrdersEnabled bool `json:"advance_orders_enabled"`
	// Discounts : remises actives maintenant, en commande en ligne (à emporter
	// ou livraison) — même forme que GET /scannorder/{slug}/discounts.
	Discounts []Discount `json:"discounts"`
}

// FranchiseHubRow : lecture brute de GetFranchiseHub.
type FranchiseHubRow struct {
	MerchantID        string
	FullName          string
	Address           string
	ZipCode           string
	City              string
	Lat               float64
	Lng               float64
	Timezone          string
	LogoURL           *string
	TakeawayEnabled   bool
	TakeawayAvailable bool
	DeliveryEnabled   bool
	DeliveryAvailable bool
	InEnabled         bool
	InAvailable       bool
	PrepTimeMode      string
	PrepTime          int
	// ExtraPrepMinutes : temps d'attente supplémentaire temporaire, déjà filtré
	// par son échéance côté SQL (cf. snoActiveExtraPrepMinutes).
	ExtraPrepMinutes int
	Slug             string
	// ReservationCode : bookings_settings.code quand la réservation en ligne est
	// ouverte.
	ReservationCode      *string
	AdvanceOrdersEnabled bool
}

// MerchantFranchise : franchise d'un établissement visible sur son hub (lien
// « autres établissements » de la page restaurant).
type MerchantFranchise struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
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

// --- Public delivery tracking (GDPR-safe) ---

// PublicSNOOrder is the response returned by GET /scannorder/{slug}/orders/{order_id}.
// It embeds the caller's own order (their own customer data is legitimate) but overrides
// the delivery_session field with a filtered, GDPR-safe projection. The embedded
// models.Order.DeliverySession (json:"delivery_session") is shadowed by the outer field
// of the same JSON name, so the internal session (which lists every other customer of the
// tour) is never serialized on this public endpoint.
//
// The internal sequential order_id is shadowed the same way (always nil, so omitted):
// the public_id is the only id a client may hold (docs/SCANNORDER_PUBLIC_ORDER_ID.md,
// phase 2). The Go field name differs on purpose, so server code reading
// publicOrder.OrderID still gets the embedded internal id.
type PublicSNOOrder struct {
	*models.Order
	DeliverySession *PublicDeliverySession `json:"delivery_session"`
	HiddenOrderID   *struct{}              `json:"order_id,omitempty"`
}

// PublicDeliverySession is the GDPR-safe projection of a delivery session exposed to the
// unauthenticated ScanNOrder client tracking their own order. It deliberately omits every
// other customer's order (names, addresses, phones, delivery notes) and the delivery man's
// full identity / GPS of other stops. It is kept separate from models.DeliverySession (used
// by authenticated merchant endpoints) so a new field added to the internal model can never
// re-leak automatically through this public endpoint.
type PublicDeliverySession struct {
	DeliverySessionID string            `json:"delivery_session_id"`
	Status            string            `json:"status"`
	DeliveryMan       PublicDeliveryMan `json:"delivery_man"`
	// StopsBeforeYou is the number of stops still to be served ahead of the caller's own
	// order in the tour (non-terminal stops with a lower priority). Non-identifying: it is a
	// count only, never the underlying orders. Nil when the caller's order is not found in
	// the tour.
	StopsBeforeYou *int `json:"stops_before_you,omitempty"`
	// TotalStops is the total number of stops in the tour (a count, not the orders).
	TotalStops *int `json:"total_stops,omitempty"`
}

// PublicDeliveryMan exposes only the minimal, non-identifying delivery man info a tracking
// client legitimately needs: first name and live position. No last name, user id, phone.
type PublicDeliveryMan struct {
	FirstName *string  `json:"first_name"`
	Lat       *float64 `json:"lat,omitempty"`
	Lng       *float64 `json:"lng,omitempty"`
	Status    *string  `json:"status,omitempty"`
}

// SNOConfigurationOption holds the official (database) values of a configuration option,
// used to overwrite what the client sent in the order payload.
type SNOConfigurationOption struct {
	ExtraPrice int
	Title      string
}

// --- Upsell ---

// UpsellResponse carries fully-configured products (same shape as the product detail
// endpoint), so the frontend can open the configuration modal directly from an upsell suggestion.
// SuggestionID is only populated by PostUpsell (cart-aware, tracked) — the deprecated
// GetUpsell (static is_popular) leaves it empty since it never creates a tracked suggestion.
type UpsellResponse struct {
	Products     []models.ProductEntry `json:"products"`
	SuggestionID string                `json:"suggestion_id,omitempty"`
}
