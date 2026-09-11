package signup

import (
	"errors"
	"time"

	"welloresto-api/internal/modules/pricing"
)

// SignupSessionTTL is the idempotent-replay window for a signup attempt —
// a repeated POST /v1/signup with the same Idempotency-Key inside this
// window gets the cached response verbatim, never re-executed. Distinct
// from the 30-day retention the daily purge task enforces (see
// internal/tasks/signup_sessions.go) — a session past this TTL but under 30
// days simply stops being replayable; a *new* attempt with the same key at
// that point is treated as a fresh signup.
const SignupSessionTTL = 24 * time.Hour

// errContextTokenInvalid is contextTokenSigner.verify's internal sentinel —
// translated to models.ErrContextNotFound at the service boundary, never
// exposed past it.
var errContextTokenInvalid = errors.New("signup: context token invalid, expired, or missing")

// SignupIdentity is the "identity" half of POST /v1/signup's payload —
// docs/WelloResto-Parcours-Client-v2.docx §5.6. provider "password" uses
// Email/Password; provider "google" uses IDToken instead (Email/Password
// ignored when set).
type SignupIdentity struct {
	Provider  string `json:"provider"`
	IDToken   string `json:"id_token,omitempty"`
	Email     string `json:"email,omitempty"`
	Password  string `json:"password,omitempty"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

// SignupMerchantPayload is the "merchant" half of POST /v1/signup's payload
// (§5.6). Address is a single formatted string (no separate street/street_number
// — the vitrine's Google Places selection doesn't split them out); Lat/Lng/PlaceID
// are captured silently (§5.4.3 — "Oui (silencieux)"), never surfaced for editing.
type SignupMerchantPayload struct {
	FullName string  `json:"full_name"`
	Address  string  `json:"address"`
	ZipCode  string  `json:"zip_code"`
	City     string  `json:"city"`
	Country  string  `json:"country"`
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
	Tel      string  `json:"tel"`
	Email    string  `json:"email"`
	SIRET    string  `json:"siret"`
	PlaceID  string  `json:"place_id"`
}

// SignupRequest is the JSON payload for POST /v1/signup — §5.6's exact
// shape (nested identity/merchant, accepts_terms/accepts_marketing). This
// replaced a flat shape this chantier had originally shipped before
// docs/WelloResto-Parcours-Client-v2.docx was found — see docs/decisions.md.
type SignupRequest struct {
	ContextToken     string                `json:"context_token,omitempty"`
	Identity         SignupIdentity        `json:"identity"`
	Merchant         SignupMerchantPayload `json:"merchant"`
	PresetCode       string                `json:"preset_code"`
	AcceptsTerms     bool                  `json:"accepts_terms"`
	AcceptsMarketing bool                  `json:"accepts_marketing"`
}

// SignupResponse is returned on success (201) — and replayed verbatim for a
// repeated call with the same Idempotency-Key.
type SignupResponse struct {
	MerchantID      string `json:"merchant_id"`
	UserID          string `json:"user_id"`
	Token           string `json:"token"`
	ActivationState string `json:"activation_state"`
}

// CreateContextRequest is POST /v1/public/signup-context's payload — §4.4's
// exact shape.
type CreateContextRequest struct {
	Segment     string       `json:"segment,omitempty"`
	Cart        pricing.Cart `json:"cart"`
	Attribution Attribution  `json:"attribution"`
}

// CreateContextResponse is POST /v1/public/signup-context's response —
// §4.4's exact shape (context_token / resolved_plan / recommended_channel).
type CreateContextResponse struct {
	ContextToken       string        `json:"context_token"`
	ResolvedPlan       pricing.Quote `json:"resolved_plan"`
	RecommendedChannel string        `json:"recommended_channel"`
}

// GetContextResponse is GET /v1/public/signup-context/{token}'s response —
// the tunnel restitutes the cart and its server-computed price from this,
// never from anything the client itself carried (§4.4).
type GetContextResponse struct {
	Segment            string        `json:"segment,omitempty"`
	Cart               pricing.Cart  `json:"cart"`
	ResolvedPlan       pricing.Quote `json:"resolved_plan"`
	RecommendedChannel string        `json:"recommended_channel"`
}
