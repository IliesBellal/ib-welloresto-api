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
	Shape        sql.NullString `json:"location_desc"`
	X            sql.NullString `json:"location_desc"`
	Y            sql.NullString `json:"location_desc"`
	W            sql.NullString `json:"location_desc"`
	H            sql.NullString `json:"location_desc"`
	Angle        sql.NullString `json:"location_desc"`
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

// ----------------------------------------------------
// Merchant params
// ----------------------------------------------------

type MerchantBookingParams struct {
	MerchantID                 int    `json:"merchant_id"`
	Timezone                   string `json:"timezone"`
	LogoURL                    string `json:"logo_url"`
	BusinessName               string `json:"business_name"`
	DefaultBookingDuration     int    `json:"default_booking_duration"`
	SlotIntervalMinutes        int    `json:"slot_interval_minutes"`
	ReserveMaximumPartySize    int    `json:"reserve_maximum_party_size"`
	LastBookingOffsetMinutes   int    `json:"last_booking_offset_minutes"`
	CancelableByCustomer       bool   `json:"cancelable_by_customer"`
	CancelBookingLimitOffsetHr int    `json:"cancel_booking_limit_offset_hours"`
	AutoAccept                 bool   `json:"auto_accept_reserve_bookings"`
}

// ----------------------------------------------------
// Hours of operation
// ----------------------------------------------------

type TimeRange struct {
	ID              int    `json:"id"`
	HourFrom        string `json:"hour_from"`
	HourTo          string `json:"hour_to"`
	BookingCapacity int    `json:"booking_capacity"`
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
	PartySize int
	StartDate string
	EndDate   *string
}
