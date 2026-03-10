package pos

// CreateMerchantRequest is the JSON payload for POST /pos/create.
type CreateMerchantRequest struct {
	FullName     string `json:"full_name"`
	Address      string `json:"address"`
	StreetNumber string `json:"street_number"`
	Street       string `json:"street"`
	ZipCode      string `json:"zip_code"`
	City         string `json:"city"`
	Country      string `json:"country"`
	SIRET        string `json:"siret"`
	Tel          string `json:"tel"`
	WebSite      string `json:"web_site"`
	Email        string `json:"email"`
	// Optional: if set the user is linked to the new merchant in the same transaction.
	UserID string `json:"user_id,omitempty"`
	// Rights to grant when linking. Ignored if UserID is empty.
	Admin bool `json:"admin"`
}

// CreateMerchantResponse is returned on success (201).
type CreateMerchantResponse struct {
	MerchantID string `json:"merchant_id"`
}

// LinkUserRequest is the JSON payload for POST /pos/link-user.
type LinkUserRequest struct {
	UserID     string `json:"user_id"`
	MerchantID string `json:"merchant_id"`
	Admin      bool   `json:"admin"`
}

// LinkUserResponse is returned on success (201).
type LinkUserResponse struct {
	RightsID int    `json:"rights_id"`
	Token    string `json:"token"`
}
