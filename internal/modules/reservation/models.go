package reservation

import "welloresto-api/internal/modules/bookingcore"

// Design regroupe les paramètres visuels du marchand
type Design struct {
	PrimaryColor            string `json:"primary_color"`
	TextColorOnPrimaryColor string `json:"text_color_on_primary_color"`
}

// Address regroupe les coordonnées
type Address struct {
	StreetNumber string `json:"street_number"`
	Street       string `json:"street"`
	ZipCode      string `json:"zip_code"`
	City         string `json:"city"`
}

// Merchant représente l'entité complète du commerçant
type Merchant struct {
	MerchantID                    string            `json:"merchant_id"`
	Timezone                      string            `json:"timezone"`
	LogoURL                       string            `json:"logo_url"`
	BusinessName                  string            `json:"business_name"`
	HandicapAccess                bool              `json:"handicap_access"`
	Phone                         string            `json:"phone"`
	Design                        Design            `json:"design"`
	Address                       Address           `json:"address"`
	DefaultBookingDuration        int               `json:"default_booking_duration"`
	SlotIntervalMinutes           int               `json:"slot_interval_minutes"`
	ReserveMaximumPartySize       int               `json:"reserve_maximum_party_size"`
	ReserveMinimumPartySize       int               `json:"reserve_minimum_party_size"`
	FirstBookingOffsetMinutes     int               `json:"first_booking_offset_minutes"`
	LastBookingOffsetMinutes      int               `json:"last_booking_offset_minutes"`
	OverbookingPercent            int               `json:"overbooking_percent"`
	MaxBookingHorizonDays         int               `json:"max_booking_horizon_days"`
	PendingExpirationHours        int               `json:"pending_expiration_hours"`
	CancelableByCustomer          bool              `json:"cancelable_by_customer"`
	CancelBookingLimitOffsetHours int               `json:"cancel_booking_limit_offset_hours"`
	AutoAcceptReserveBookings     bool              `json:"auto_accept_reserve_bookings"`
	SMSEnabled                    bool              `json:"sms_enabled"`
	OpenHours                     map[string]string `json:"open_hours,omitempty"`
}

// OperationHour représente une ligne de la table hours_of_operation
type OperationHour struct {
	DayOfWeek int
	HourFrom  string
	HourTo    string
}

// OpenHoursResponse est la structure de réponse pour l'API
type OpenHoursResponse struct {
	Status           string    `json:"status,omitempty"`
	Error            string    `json:"error,omitempty"`
	OpenDays         []int     `json:"open_days,omitempty"`
	MaximumPartySize int       `json:"maximum_party_size,omitempty"`
	Merchant         *Merchant `json:"merchant,omitempty"`
}

// Slot représente un créneau horaire de réservation
type Slot struct {
	Time            string `json:"time"`
	Available       bool   `json:"available"`
	DurationMinutes int    `json:"duration_minutes,omitempty"`
	HOOID           string `json:"hoo_id,omitempty"`
}

// AvailabilityResponse est la réponse renvoyée au client
type AvailabilityResponse struct {
	Status string `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
	Slots  []Slot `json:"slots,omitempty"`
}

// OperationRange représente une plage horaire étendue avec capacités
type OperationRange struct {
	ID               string
	HourFrom         string
	HourTo           string
	BookingCapacity  int
	FirstBookingTime *string
	LastBookingTime  *string
}

type BookingRequest struct {
	MerchantID string        `json:"merchant_id"`
	Booking    *BookingData  `json:"booking"`
	Customer   *CustomerData `json:"customer"`
	CreatedBy  string        `json:"created_by"`
}

type BookingData struct {
	BookingID       string  `json:"booking_id"`
	BookingNumber   string  `json:"booking_number"`
	MerchantID      string  `json:"merchant_id"`
	StartDate       string  `json:"start_date"`
	EndDate         string  `json:"end_date"`
	DurationMinutes int     `json:"duration_minutes,omitempty"`
	PartySize       int     `json:"party_size"`
	Comment         *string `json:"comment,omitempty"`
	Status          string  `json:"status"`
	SequenceNumber  int     `json:"sequence_number"` // Ajouté pour la limite de modif
	Cancelable      bool    `json:"cancelable"`      // Champ calculé
}

type CustomerData struct {
	CustomerID        string `json:"customer_id"`
	MerchantID        string `json:"merchant_id"`
	CustomerFirstName string `json:"customer_first_name"`
	CustomerLastName  string `json:"customer_last_name"`
	// CustomerName est déduit de CustomerFirstName + CustomerLastName par le
	// service (CreateReservation) ; toute valeur envoyée par le client public
	// est ignorée. Conservé pour l'upsert client et les messages de
	// confirmation qui l'utilisent déjà.
	CustomerName     string   `json:"customer_name"`
	CustomerTel      string   `json:"customer_tel"`
	CustomerEmail    string   `json:"customer_email"`
	AvailableRewards []Reward `json:"available_rewards,omitempty"`
}

type Reward struct {
	RewardID         int     `json:"reward_id"`
	LoyaltyProgramID int     `json:"loyalty_program_id"`
	CreationDate     string  `json:"creation_date"`
	RewardType       string  `json:"reward_type"`
	RewardValue      float64 `json:"reward_value"`
}

type CreateBookingResponse struct {
	Status  string       `json:"status"`
	Error   string       `json:"error,omitempty"`
	Booking *BookingData `json:"booking,omitempty"`
}

type MerchantPublic struct {
	BusinessName string  `json:"business_name"`
	Phone        string  `json:"phone"`
	Address      Address `json:"address"`
	LogoURL      string  `json:"logo_url"`
	Design       Design  `json:"design"`
	Timezone     string  `json:"timezone"`
}

type BookingPublic struct {
	BookingNumber    string         `json:"booking_number"`
	Status           string         `json:"status"`
	PartySize        int            `json:"party_size"`
	DateFrom         string         `json:"date_from"`
	DurationMinutes  int            `json:"duration_minutes"`
	Comment          *string        `json:"comment,omitempty"`
	Cancelable       bool           `json:"cancelable"`
	Modifiable       bool           `json:"modifiable"`
	RemainingUpdates int            `json:"remaining_updates"`
	Merchant         MerchantPublic `json:"merchant"`
}

type PublicBookingResponse struct {
	Status  string         `json:"status"`
	Error   string         `json:"error,omitempty"`
	Warning string         `json:"warning,omitempty"`
	Booking *BookingPublic `json:"booking,omitempty"`
}

func NewBookingSettingsFromMerchant(merchant *Merchant) bookingcore.BookingSettings {
	if merchant == nil {
		return bookingcore.DefaultBookingSettings()
	}

	return bookingcore.BookingSettings{
		DefaultBookingDuration:        merchant.DefaultBookingDuration,
		AutoAcceptReserveBookings:     merchant.AutoAcceptReserveBookings,
		ReserveMaximumPartySize:       merchant.ReserveMaximumPartySize,
		ReserveMinimumPartySize:       merchant.ReserveMinimumPartySize,
		FirstBookingOffsetMinutes:     merchant.FirstBookingOffsetMinutes,
		LastBookingOffsetMinutes:      merchant.LastBookingOffsetMinutes,
		CancelBookingLimitOffsetHours: merchant.CancelBookingLimitOffsetHours,
		SlotIntervalMinutes:           merchant.SlotIntervalMinutes,
		CancelableByCustomer:          merchant.CancelableByCustomer,
		Enabled:                       true,
		OverbookingPercent:            merchant.OverbookingPercent,
		MaxBookingHorizonDays:         merchant.MaxBookingHorizonDays,
		PendingExpirationHours:        merchant.PendingExpirationHours,
	}
}

type GenericResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}
