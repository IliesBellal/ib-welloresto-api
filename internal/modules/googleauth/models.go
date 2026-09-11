package googleauth

// AuthenticateRequest is the JSON payload for POST /v1/auth/google.
type AuthenticateRequest struct {
	IDToken string `json:"id_token"`
}

// AuthenticateResponse mirrors signup.SignupResponse's shape — the lean set
// of fields any of this module's three successful outcomes (existing
// google_sub, auto-linked account) need. PAS un jeton signé — Token is the
// same opaque users_rights.token every other auth path in this API returns;
// there is no JWT anywhere in this system's own session model.
type AuthenticateResponse struct {
	MerchantID string `json:"merchant_id"`
	UserID     string `json:"user_id"`
	Token      string `json:"token"`
}
