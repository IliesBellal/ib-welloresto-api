package models

type Booking struct {
	BookingID       string                `json:"booking_id"`
	BookingNumber   string                `json:"booking_number"`
	AccessLink      string                `json:"access_link"`
	Status          string                `json:"status"`
	SequenceNumber  int                   `json:"sequence_number"`
	BookingDateFrom string                `json:"booking_date_from"`
	BookingDateTo   string                `json:"booking_date_to"`
	PartySize       int                   `json:"party_size"`
	CreationDate    string                `json:"creation_date"`
	CreatedBy       string                `json:"created_by"`
	Comment         *string               `json:"comment"`
	StartDate       *string               `json:"start_date"`
	EndDate         *string               `json:"end_date"`
	Locations       []Location            `json:"locations"`
	Merchant        MerchantBookingParams `json:"merchant"`
	Customer        Customer              `json:"customer"`
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
}
