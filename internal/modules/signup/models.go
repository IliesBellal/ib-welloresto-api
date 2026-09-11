package signup

import "time"

// SignupSessionTTL is the idempotent-replay window for a signup attempt —
// a repeated POST /v1/signup with the same Idempotency-Key inside this
// window gets the cached response verbatim, never re-executed. Distinct
// from the 30-day retention the daily purge task enforces (see
// internal/tasks/signup_sessions.go) — a session past this TTL but under 30
// days simply stops being replayable; a *new* attempt with the same key at
// that point is treated as a fresh signup.
const SignupSessionTTL = 24 * time.Hour

// SignupRequest is the JSON payload for POST /v1/signup. provider
// "password" (chantier 6) uses Email/Password; provider "google" (chantier
// 7c) uses IDToken instead — the id_token replaces email + password
// entirely, Email/Password are ignored when set.
type SignupRequest struct {
	Provider     string         `json:"provider"`
	ContextToken string         `json:"context_token,omitempty"`
	Email        string         `json:"email"`
	Password     string         `json:"password"`
	IDToken      string         `json:"id_token,omitempty"`
	FirstName    string         `json:"first_name"`
	LastName     string         `json:"last_name"`
	Tel          string         `json:"tel"`
	PresetCode   string         `json:"preset_code"`
	Merchant     SignupMerchant `json:"merchant"`
}

// SignupMerchant is the establishment half of the signup payload — the
// same identity fields pos.CreateMerchantRequest already takes.
type SignupMerchant struct {
	FullName     string `json:"full_name"`
	SIRET        string `json:"siret"`
	Tel          string `json:"tel"`
	Address      string `json:"address"`
	StreetNumber string `json:"street_number"`
	Street       string `json:"street"`
	ZipCode      string `json:"zip_code"`
	City         string `json:"city"`
	Country      string `json:"country"`
	WebSite      string `json:"web_site"`
	Email        string `json:"email"`
}

// SignupResponse is returned on success (201) — and replayed verbatim for a
// repeated call with the same Idempotency-Key.
type SignupResponse struct {
	MerchantID      string `json:"merchant_id"`
	UserID          string `json:"user_id"`
	Token           string `json:"token"`
	ActivationState string `json:"activation_state"`
}
