package bookings

// WaitlistEntry représente une ligne de booking_waitlist.
type WaitlistEntry struct {
	ID            string  `json:"id"`
	MerchantID    string  `json:"merchant_id"`
	CustomerID    *string `json:"customer_id,omitempty"`
	PartySize     int     `json:"party_size"`
	CustomerName  string  `json:"customer_name"`
	CustomerPhone string  `json:"customer_phone"`
	Notes         *string `json:"notes,omitempty"`
	Status        string  `json:"status"`
	NotifiedAt    *string `json:"notified_at,omitempty"`
	ExpiresAt     *string `json:"expires_at,omitempty"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

// CreateWaitlistRequest est le corps d'une inscription (staff ou public).
type CreateWaitlistRequest struct {
	PartySize     int     `json:"party_size"`
	CustomerName  string  `json:"customer_name"`
	CustomerPhone string  `json:"customer_phone"`
	Notes         *string `json:"notes"`
}
