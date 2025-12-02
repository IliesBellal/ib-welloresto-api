package models

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
