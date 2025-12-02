package models

type Booking struct {
	BookingID       string            `json:"booking_id"`
	BookingNumber   string            `json:"booking_number"`
	AccessLink      string            `json:"access_link"`
	Status          string            `json:"status"`
	SequenceNumber  int               `json:"sequence_number"`
	BookingDateFrom string            `json:"booking_date_from"`
	BookingDateTo   string            `json:"booking_date_to"`
	PartySize       int               `json:"party_size"`
	CreationDate    string            `json:"creation_date"`
	CreatedBy       string            `json:"created_by"`
	Comment         *string           `json:"comment"`
	StartDate       string            `json:"start_date"`
	EndDate         string            `json:"end_date"`
	Locations       []BookingLocation `json:"locations"`
	Merchant        BookingMerchant   `json:"merchant"`
	Customer        Customer          `json:"customer"`
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
