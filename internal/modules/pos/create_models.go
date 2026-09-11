package pos

import "encoding/json"

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
	// Lat/Lng/PlaceID: LOT A Semaine 3, Chantier 14 — the establishment's
	// Google Places location, silently captured (never shown for editing —
	// see docs/WelloResto-Parcours-Client-v2.docx §5.4.3). Zero value (0, "")
	// for a merchant created without a resolved place (e.g. /pos/create).
	Lat     float64 `json:"lat,omitempty"`
	Lng     float64 `json:"lng,omitempty"`
	PlaceID string  `json:"place_id,omitempty"`
	// SignupChannel/SignupSource: LOT A Semaine 3, Chantier 14 — populated
	// only by /v1/signup ("self_signup", plus the vitrine's utm/landing/referrer
	// attribution as raw JSON when present). Left empty by /pos/create,
	// which has no such context — merchant.signup_channel/signup_source
	// (migration 125) stay NULL for a staff-created merchant, as before.
	SignupChannel string          `json:"signup_channel,omitempty"`
	SignupSource  json.RawMessage `json:"signup_source,omitempty"`
	PackageID     string          `json:"package_id"`
	// Optional: if set the user is linked to the new merchant in the same transaction.
	UserID string `json:"user_id,omitempty"`
	// Rights to grant when linking. Ignored if UserID is empty.
	Admin bool `json:"admin"`
}

// CreateMerchantResponse is returned on success (201).
type CreateMerchantResponse struct {
	MerchantID string `json:"merchant_id"`
	// OwnerRightsToken is the new users_rights.token for req.UserID, set only
	// when UserID was provided (empty otherwise). Additive field — LOT A
	// Semaine 2, Chantier 6b needs it to hand back an opaque session token
	// from POST /v1/signup without a second query; existing /pos/create
	// callers that ignore it are unaffected.
	OwnerRightsToken string `json:"owner_rights_token,omitempty"`
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
