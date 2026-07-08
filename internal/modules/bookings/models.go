package bookings

import (
	"database/sql"
	"welloresto-api/internal/models"
)

type Booking struct {
	BookingID       string            `json:"booking_id"`
	BookingNumber   string            `json:"booking_number"`
	AccessLink      string            `json:"access_link"`
	Status          string            `json:"status"`
	SequenceNumber  int               `json:"sequence_number"`
	BookingDateFrom int64             `json:"booking_date_from"`
	BookingDateTo   int64             `json:"booking_date_to"`
	PartySize       int               `json:"party_size"`
	CreationDate    int64             `json:"creation_date"`
	CreatedBy       string            `json:"created_by"`
	Comment         *string           `json:"comment"`
	StartDate       string            `json:"start_date"`
	EndDate         string            `json:"end_date"`
	Locations       []BookingLocation `json:"locations"`
	Merchant        BookingMerchant   `json:"merchant"`
	Customer        models.Customer   `json:"customer"`
}

type BookingAvailabilityResponse struct {
	Status     string                 `json:"status"`
	Merchant   *MerchantBookingParams `json:"merchant"`
	Locations  []Location             `json:"locations"`
	TimeRanges []TimeRange            `json:"time_ranges"`
	Slots      []BookingSlot          `json:"booking_slots"`
	Occupation map[string]int         `json:"occupation_by_slot"`
	Date       string                 `json:"requested_date"`
	DayOfWeek  int                    `json:"day_of_week"`
}

type Location struct {
	OrderID      string         `json:"order_id"`
	LocationID   string         `json:"location_id"`
	LocationName string         `json:"location_name"`
	LocationDesc *string        `json:"location_desc"`
	Seats        int            `json:"seats"`
	Order        int            `json:"order"`
	FloorID      string         `json:"floor_id"`
	Shape        sql.NullString `json:"shape"`
	X            sql.NullString `json:"x"`
	Y            sql.NullString `json:"y"`
	W            sql.NullString `json:"w"`
	H            sql.NullString `json:"h"`
	Angle        sql.NullString `json:"angle"`
	OpenOrderID  sql.NullString `json:"open_order_id"`
	Available    string         `json:"available"`
}

type BookingLocation struct {
	BookingID    string `json:"booking_id"`
	LocationID   string `json:"location_id"`
	LocationName string `json:"location_name"`
	LocationDesc string `json:"location_desc"`
}

type BookingMerchant struct {
	BusinessName           string                 `json:"business_name"`
	Timezone               string                 `json:"timezone"`
	LogoURL                string                 `json:"logo_url"`
	DefaultBookingDuration int                    `json:"default_booking_duration"`
	Address                BookingMerchantAddress `json:"address"`
}

type BookingMerchantAddress struct {
	Address string `json:"address"`
}

type BookingCustomer struct {
	CustomerName       string `json:"customer_name"`
	CustomerID         string `json:"customer_id"`
	CustomerTel        string `json:"customer_tel"`
	CustomerEmail      string `json:"customer_email"`
	CustomerNbOrders   int    `json:"customer_nb_orders"`
	CustomerNbBookings int    `json:"customer_nb_bookings"`
}

type BookingObjectRequest struct {
	MerchantID      string          `json:"merchant_id"`
	BookingID       *string         `json:"booking_id"`
	BookingNumber   *string         `json:"booking_number"`
	BookingDateFrom *string         `json:"booking_date_from"`
	BookingDateTo   *string         `json:"booking_date_to"`
	CreatedBy       string          `json:"created_by"`
	Customer        models.Customer `json:"customer"`
	Booking         Booking         `json:"booking"`
}

type BookingListFilters struct {
	Statuses []string
	DateFrom *string
	DateTo   *string
	PartySize *int
	Search   *string
	Source   *string
	SortBy   string
	SortDir  string
	Page     int
	Limit    int
}

type BookingListItem struct {
	BookingID      string   `json:"booking_id"`
	BookingNumber  string   `json:"booking_number"`
	Status         string   `json:"status"`
	Source         string   `json:"source"`
	BookingDateFrom string  `json:"booking_date_from"`
	PartySize      int      `json:"party_size"`
	CustomerName   string   `json:"customer_name"`
	CustomerTel    string   `json:"customer_tel"`
	AssignedTables []string `json:"assigned_tables"`
}

type BookingListResponse struct {
	Metadata models.PaginationMetadata `json:"metadata"`
	Bookings []BookingListItem         `json:"bookings"`
}

type BookingSettings struct {
	Enabled                       bool                 `json:"enabled"`
	Code                          string               `json:"code"`
	AutoAcceptReserveBookings     bool                 `json:"auto_accept_reserve_bookings"`
	SlotIntervalMinutes           int                  `json:"slot_interval_minutes"`
	DefaultBookingDuration        int                  `json:"default_booking_duration"`
	ReserveMaximumPartySize       int                  `json:"reserve_maximum_party_size"`
	ReserveMinimumPartySize       int                  `json:"reserve_minimum_party_size"`
	LastBookingOffsetMinutes      int                  `json:"last_booking_offset_minutes"`
	MinBookingNoticeMinutes       int                  `json:"min_booking_notice_minutes"`
	MaxBookingHorizonDays         int                  `json:"max_booking_horizon_days"`
	OverbookingPercent            int                  `json:"overbooking_percent"`
	CancelableByCustomer          bool                 `json:"cancelable_by_customer"`
	CancelBookingLimitOffsetHours int                  `json:"cancel_booking_limit_offset_hours"`
	PendingExpirationHours        int                  `json:"pending_expiration_hours"`
	SMSEnabled                    bool                 `json:"sms_enabled"`
	WaitlistEnabled               bool                 `json:"waitlist_enabled"`
	WaitlistMaxSize               int                  `json:"waitlist_max_size"`
	WaitlistSlotExpiryMinutes     int                  `json:"waitlist_slot_expiry_minutes"`
	DurationRules                 []BookingDurationRule `json:"duration_rules"`
	CapacityWarning               bool                 `json:"capacity_warning"`
	PhysicalCapacity              int                  `json:"physical_capacity"`
}

type PutBookingSettingsRequest struct {
	Enabled                       bool   `json:"enabled"`
	Code                          string `json:"code"`
	AutoAcceptReserveBookings     bool   `json:"auto_accept_reserve_bookings"`
	SlotIntervalMinutes           int    `json:"slot_interval_minutes"`
	DefaultBookingDuration        int    `json:"default_booking_duration"`
	ReserveMaximumPartySize       int    `json:"reserve_maximum_party_size"`
	ReserveMinimumPartySize       int    `json:"reserve_minimum_party_size"`
	LastBookingOffsetMinutes      int    `json:"last_booking_offset_minutes"`
	MinBookingNoticeMinutes       int    `json:"min_booking_notice_minutes"`
	MaxBookingHorizonDays         int    `json:"max_booking_horizon_days"`
	OverbookingPercent            int    `json:"overbooking_percent"`
	CancelableByCustomer          bool   `json:"cancelable_by_customer"`
	CancelBookingLimitOffsetHours int    `json:"cancel_booking_limit_offset_hours"`
	PendingExpirationHours        int    `json:"pending_expiration_hours"`
	SMSEnabled                    bool   `json:"sms_enabled"`
	WaitlistEnabled               bool   `json:"waitlist_enabled"`
	WaitlistMaxSize               int    `json:"waitlist_max_size"`
	WaitlistSlotExpiryMinutes     int    `json:"waitlist_slot_expiry_minutes"`
}

type BookingDurationRule struct {
	RuleID         string `json:"rule_id"`
	MinPartySize   int    `json:"min_party_size"`
	MaxPartySize   int    `json:"max_party_size"`
	DurationMinutes int   `json:"duration_minutes"`
	Enabled        bool   `json:"enabled"`
}

type CreateDurationRuleRequest struct {
	MinPartySize    int `json:"min_party_size"`
	MaxPartySize    int `json:"max_party_size"`
	DurationMinutes int `json:"duration_minutes"`
}

type PatchDurationRuleRequest struct {
	MinPartySize    *int `json:"min_party_size"`
	MaxPartySize    *int `json:"max_party_size"`
	DurationMinutes *int `json:"duration_minutes"`
}

type BookingSettingsHoursResponse struct {
	Hours []models.POSHoursOfOperation `json:"hours"`
}

type PutBookingSettingsHoursRequest struct {
	Hours []models.POSHoursOfOperationPatch `json:"hours"`
}

type DenyBookingRequest struct {
	DeletionReasonID *string `json:"deletion_reason_id"`
}

type AssignBookingLocationsRequest struct {
	Locations []BookingLocation `json:"locations"`
}

// ----------------------------------------------------
// Merchant params
// ----------------------------------------------------

type MerchantBookingParams struct {
	MerchantID                    int    `json:"merchant_id"`
	Timezone                      string `json:"timezone"`
	LogoURL                       string `json:"logo_url"`
	BusinessName                  string `json:"business_name"`
	DefaultBookingDuration        int    `json:"default_booking_duration"`
	SlotIntervalMinutes           int    `json:"slot_interval_minutes"`
	ReserveMaximumPartySize       int    `json:"reserve_maximum_party_size"`
	ReserveMinimumPartySize       int    `json:"reserve_minimum_party_size"`
	FirstBookingOffsetMinutes     int    `json:"first_booking_offset_minutes"`
	LastBookingOffsetMinutes      int    `json:"last_booking_offset_minutes"`
	CancelableByCustomer          bool   `json:"cancelable_by_customer"`
	CancelBookingLimitOffsetHours int    `json:"cancel_booking_limit_offset_hours"`
	AutoAcceptReserveBookings     bool   `json:"auto_accept_reserve_bookings"`
	Enabled                       bool   `json:"enabled"`
	OverbookingPercent            int    `json:"overbooking_percent"`
	MaxBookingHorizonDays         int    `json:"max_booking_horizon_days"`
	PendingExpirationHours        int    `json:"pending_expiration_hours"`
}

// ----------------------------------------------------
// Hours of operation
// ----------------------------------------------------

type TimeRange struct {
	ID               int     `json:"id"`
	HourFrom         string  `json:"hour_from"`
	HourTo           string  `json:"hour_to"`
	BookingCapacity  int     `json:"booking_capacity"`
	FirstBookingTime *string `json:"first_booking_time,omitempty"`
	LastBookingTime  *string `json:"last_booking_time,omitempty"`
}

// ----------------------------------------------------
// Slots
// ----------------------------------------------------

type BookingSlot struct {
	HourOfOperationID int    `json:"hour_of_operation_id"`
	DateFrom          string `json:"date_from"`
	DateTo            string `json:"date_to"`
	Available         bool   `json:"available"`
	Capacity          int    `json:"capacity"`
	RemainingCapacity int    `json:"remaining_capacity"`

	// Debug
	DebugCapacity          int `json:"debug_capacity"`
	DebugMaxBookedInWindow int `json:"debug_max_booked_in_window"`
	DebugRemainingCapacity int `json:"debug_remaining_capacity"`
}

// ----------------------------------------------------
// Existing bookings
// ----------------------------------------------------

type ExistingBooking struct {
	PartySize       int
	StartDate       string
	EndDate         *string
	DurationMinutes *int
	Status          string
}

// ----------------------------------------------------
// Conflit table × créneau
// ----------------------------------------------------

// BookingLocationConflict identifie une affectation existante en collision
// avec le créneau demandé (une ligne par couple booking/table en conflit).
type BookingLocationConflict struct {
	BookingID  string `json:"booking_id"`
	LocationID string `json:"location_id"`
}

// ExpiringBookingContact porte les données de contact d'une réservation
// pending sur le point d'être expirée par le cron, nécessaires à l'envoi
// d'une notification d'annulation avant la bascule de statut.
type ExpiringBookingContact struct {
	BookingID     string
	MerchantID    string
	BookingNumber string
	PartySize     int
	StartDate     string // "2006-01-02 15:04:05" UTC
	MerchantName  string
	MerchantSlug  string
	Timezone      string
	SMSEnabled    bool
	CustomerName  string
	CustomerEmail string
	CustomerPhone string
}

// TableConflictError porte les affectations en collision jusqu'au handler,
// qui les renvoie dans le corps du 409 table_conflict.
type TableConflictError struct {
	Conflicts []BookingLocationConflict
}

func (e *TableConflictError) Error() string { return "table_conflict" }

// Is rattache l'erreur au sentinel models.ErrTableConflict (mapping 409 de
// SendErrorJSON) pour les appelants qui ne la traitent pas spécifiquement.
func (e *TableConflictError) Is(target error) bool { return target == models.ErrTableConflict }
