package models

import (
	"encoding/json"
	"time"
)

const (
	OrderTypeIn       = "IN"
	OrderTypeTakeAway = "TAKE_AWAY"
	OrderTypeDelivery = "DELIVERY"

	FulfillmentTypeRestaurant = "DELIVERY_BY_RESTAURANT"
	FulfillmentTypeDeliveroo  = "DELIVEROO"

	MerchantApprovalPendingApproval = "PENDING_APPROVAL"

	// MerchantApprovalPendingCardPayment marque une commande borne (Kiosk) créée
	// en paiement carte (Stripe Terminal) dont le paiement n'est pas encore
	// confirmé. Valeur distincte de PENDING_APPROVAL (paiement comptoir) : une
	// commande carte ne doit pas partir en cuisine tant que le webhook Stripe
	// n'a pas confirmé le paiement (payment_intent.succeeded card_present).
	// Le webhook la fait ensuite transiter vers ACCEPTED via SetOrderAccepted,
	// même mécanisme que le paiement comptoir — voir docs/KIOSK_DECISIONS.md.
	MerchantApprovalPendingCardPayment = "PENDING_CARD_PAYMENT"
)

// OrderItemInsert represents an order item insert
type OrderItemInsert struct {
	OrderID         string
	OrderItemID     *string
	ProductID       string
	MerchantID      string
	Quantity        int
	DiscountID      *string
	Price           int  // Final price (discounted_price if provided, otherwise base_price)
	BasePrice       int  // Original price before discounts
	DiscountedPrice *int // Discounted price (optional)
	DelayID         *string
	Comment         *string
	CreatedBy       string
	IsUpsell        bool // true when this line was added from an upsell suggestion
}

type ExtraInsert struct {
	OrderID     string
	OrderItemID string
	ComponentID string
	ProductID   string
	MerchantID  string
	Price       int
}
type WithoutInsert struct {
	OrderID     string
	OrderItemID string
	ComponentID string
	ProductID   string
	MerchantID  string
}
type ConfigInsert struct {
	OrderItemID string
	AttributeID string
	OptionID    string
	Quantity    int
}

type OrderComment struct {
	OrderID      *string    `json:"order_id,omitempty"`
	UserName     *string    `json:"user_name,omitempty"`
	Content      *string    `json:"content,omitempty"`
	CreationDate *time.Time `json:"creation_date,omitempty"`
}

type TableAttributes struct {
	PMR     bool `json:"pmr"`
	Terrace bool `json:"terrace"`
	VIP     bool `json:"vip"`
	Window  bool `json:"window"`
}

type Location struct {
	LocationID    string           `json:"location_id"`
	OrderID       *string          `json:"order_id"`
	BookingID     *string          `json:"booking_id"`
	LocationName  string           `json:"location_name"`
	LocationDesc  *string          `json:"location_desc"`
	Seats         int              `json:"seats"`
	Order         int              `json:"order"`
	FloorID       string           `json:"floor_id,omitempty"`
	Shape         string           `json:"shape,omitempty"`
	X             float64          `json:"x,omitempty"`
	Y             float64          `json:"y,omitempty"`
	W             float64          `json:"width,omitempty"`
	H             float64          `json:"height,omitempty"`
	Angle         float64          `json:"angle,omitempty"`
	OpenOrderID   *string          `json:"open_order_id,omitempty"`
	OrderOpenedAt *string          `json:"order_opened_at,omitempty"` // ISO 8601 UTC, nil si pas de commande ouverte
	Available     bool             `json:"available,omitempty"`
	Bookings      []Booking        `json:"bookings"`
	Booking       *LocationBooking `json:"booking,omitempty"`
	// BookingConflict n'est renseigné que si booking_date_from/booking_date_to
	// sont passés à GET /locations : réservation active (PENDING_APPROVAL,
	// ACCEPTED ou ORDER_OPEN) chevauchant ce créneau sur cette table, calculée
	// avec la même règle que le contrôle serveur de bookings.FindConflictingBookings
	// (qui renverrait 409 table_conflict pour cette table sur ce créneau).
	BookingConflict *LocationBooking `json:"booking_conflict,omitempty"`
	Attributes      *TableAttributes `json:"attributes,omitempty"`
}

// LocationBooking résume, pour une table du plan de salle, la prochaine
// réservation ACCEPTED sur le créneau actif (dérivée de Location.Bookings,
// déjà chargées via la jointure booked_location de GetLocations). Réutilisé
// tel quel pour Location.BookingConflict (même shape, sémantique différente).
type LocationBooking struct {
	BookingID     string `json:"booking_id"`
	BookingNumber string `json:"booking_number"`
	PartySize     int    `json:"party_size"`
	StartsAt      string `json:"starts_at"` // ISO 8601 UTC
	EndsAt        string `json:"ends_at"`   // ISO 8601 UTC
	CustomerName  string `json:"customer_name"`
}

type LocationResponse struct {
	Locations []Location `json:"locations"`
	Floors    []Floor    `json:"floors"`
	Areas     []Area     `json:"areas"`
	Obstacles []Obstacle `json:"obstacles"`
}

type Floor struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Area struct {
	ID          string          `json:"id"`
	FloorID     string          `json:"floor_id"`
	Name        string          `json:"name"`
	Points      json.RawMessage `json:"points"` // Utilisation de RawMessage pour le JSON brut
	X           float64         `json:"x"`
	Y           float64         `json:"y"`
	Angle       float64         `json:"angle"`
	StrokeColor string          `json:"stroke_color"`
	Color       string          `json:"color"`
}

type Obstacle struct {
	ID        string   `json:"id"`
	FloorID   string   `json:"floor_id"`
	Type      string   `json:"type"`
	X         float64  `json:"x"`
	Y         float64  `json:"y"`
	Width     float64  `json:"width"`
	Height    float64  `json:"height"`
	Angle     float64  `json:"angle"`
	Direction *float64 `json:"direction"`
	Enabled   bool     `json:"enabled"`
}

type PaymentItem struct {
	OrderItemID string `json:"order_item_id"`
	Quantity    int    `json:"quantity"`
}

type Customer struct {
	CustomerID                         *string  `json:"customer_id"`
	CustomerCode                       *string  `json:"customer_code"`
	CustomerName                       *string  `json:"customer_name"`
	CustomerFirstName                  *string  `json:"customer_first_name"`
	CustomerLastName                   *string  `json:"customer_last_name"`
	CustomerTel                        *string  `json:"customer_tel"`
	CustomerEmail                      *string  `json:"customer_email"`
	CustomerTemporaryPhone             *string  `json:"customer_temporary_phone"`
	CustomerTemporaryPhoneCode         *string  `json:"customer_temporary_phone_code"`
	CustomerNbOrders                   *int     `json:"customer_nb_orders"`
	CustomerNbBookings                 *int     `json:"customer_nb_bookings"`
	CustomerTotalSpent                 *int     `json:"customer_total_spent"`
	MatchScore                         *int     `json:"match_score"`
	CustomerAdditionalInfo             *string  `json:"customer_additional_info"`
	CustomerZoneCode                   *string  `json:"customer_zone_code"`
	CustomerAddress                    *string  `json:"customer_address"`
	CustomerLat                        *float64 `json:"customer_lat"`
	CustomerLng                        *float64 `json:"customer_lng"`
	CustomerFloorNumber                *string  `json:"customer_floor_number"`
	CustomerDoorNumber                 *string  `json:"customer_door_number"`
	CustomerAdditionalAddress          *string  `json:"customer_additional_address"`
	MerchantID                         string   `json:"merchant_id"`
	CustomerBusinessName               *string  `json:"customer_business_name"`
	CustomerBirthdate                  *string  `json:"customer_birthdate"`
	CustomerTemporaryAddress           *string  `json:"customer_temporary_address"`
	CustomerTemporaryLat               *string  `json:"customer_temporary_lat"`
	CustomerTemporaryLng               *string  `json:"customer_temporary_lng"`
	CustomerTemporaryDoorNumber        *string  `json:"customer_temporary_door_number"`
	CustomerTemporaryFloorNumber       *string  `json:"customer_temporary_floor_number"`
	CustomerTemporaryAdditionalAddress *string  `json:"customer_temporary_additional_address"`
	CreationDate                       *string  `json:"creation_date"`
	AdvertisingConsent                 *bool    `json:"advertising_consent"`
	CustomerBrand                      *string  `json:"customer_brand"`
	CustomerDeliveryNotes              *string  `json:"customer_delivery_notes"`
}

type Order struct {
	OrderID            string           `json:"order_id"`
	MerchantID         *string          `json:"merchant_id,omitempty"`
	OrderNum           *string          `json:"order_num"`
	DeliverySessionID  *string          `json:"delivery_session_id"`
	DeliveryPriority   *int             `json:"delivery_priority"`
	Brand              *string          `json:"brand"`
	BrandOrderID       *string          `json:"brand_order_id"`
	BrandOrderNum      *string          `json:"brand_order_num"`
	BrandStatus        *string          `json:"brand_status"`
	OrderType          *string          `json:"order_type"`
	CutleryNotes       *string          `json:"cutlery_notes"`
	State              *string          `json:"state"`
	Scheduled          bool             `json:"scheduled"`
	TTC                int64            `json:"TTC"`
	TVA                *int64           `json:"TVA"`
	HT                 *int64           `json:"HT"`
	CartDiscountID     *string          `json:"cart_discount_id,omitempty"`
	CartDiscountCode   *string          `json:"cart_discount_code,omitempty"`
	CartDiscountAmount int64            `json:"cart_discount_amount"`
	PlacesSettings     *int64           `json:"places_settings"`
	PagerNumber        *string          `json:"pager_number"`
	IsPaid             bool             `json:"isPaid"`
	IsDistributed      bool             `json:"isDistributed"`
	IsSNO              bool             `json:"isSNO"`
	CallHour           *string          `json:"callHour"`
	EstimatedReady     *int             `json:"estimated_ready"`
	MerchantApproval   string           `json:"merchant_approval"`
	DeliveryFees       *int64           `json:"delivery_fees"`
	Customer           *Customer        `json:"customer"`
	Comment            *string          `json:"comment,omitempty"`
	Payments           []Payment        `json:"payments"`
	Responsible        *OrderUser       `json:"responsible"`
	Location           []Location       `json:"location"`
	Products           []ProductEntry   `json:"products"`
	Priority           *int             `json:"priority"`
	CreationDate       int64            `json:"creation_date"`
	FulfillmentType    *string          `json:"fulfillment_type"`
	LastUpdate         int64            `json:"last_update"`
	DeliverySession    *DeliverySession `json:"delivery_session"`
	CashRegister       *CashRegister    `json:"cash_register"`
	DeliveryStop       *DeliveryStop    `json:"delivery_stop,omitempty"`
}

// OrderUser Can be used as Responsible, OrderedBy, DeliveryMan, etc...
type OrderUser struct {
	UserID         string   `json:"user_id"`
	FirstName      *string  `json:"first_name"`
	LastName       *string  `json:"last_name"`
	ProfilePicture *string  `json:"profile_picture,omitempty"`
	Lat            *float64 `json:"lat,omitempty"`
	Lng            *float64 `json:"lng,omitempty"`
	PlanningColor  *string  `json:"planning_color,omitempty"`
	Status         *string  `json:"status,omitempty"`
}

type PendingOrdersResponse struct {
	Orders           []Order           `json:"orders"`
	DeliverySessions []DeliverySession `json:"delivery_sessions"`
	Timings          interface{}       `json:"timings,omitempty"`
}

type DeliverySessionsResponse struct {
	DeliverySessions []DeliverySession `json:"delivery_sessions"`
}

type DistributedProduct struct {
	OrderItemID string `json:"order_item_id"`
	Quantity    int    `json:"quantity"`
}

type SetDistributedProductsRequest struct {
	OrderID  string               `json:"order_id"`
	Products []DistributedProduct `json:"products"`
}
