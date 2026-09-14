package subscriptions

import "time"

// Override kind values for subscription_overrides.kind — LOT B B1d
// (docs/WelloResto-Parcours-Client-v2.docx §7.3). Distinct Go type from
// subscription_items' Kind* constants even though the "module" string
// coincides — an override's kind and an item's kind are validated against
// separate closed sets and mean different things (a grant mechanism vs. a
// billed line's category).
const (
	OverrideKindModule     = "module"
	OverrideKindPrice      = "price"
	OverrideKindKioskQuota = "kiosk_quota"
)

// ValidOverrideKinds is the closed set subscription_overrides.kind must
// belong to.
var ValidOverrideKinds = map[string]bool{
	OverrideKindModule:     true,
	OverrideKindPrice:      true,
	OverrideKindKioskQuota: true,
}

// Reason values for subscription_overrides.reason — the exact closed list
// from the brief. "reason obligatoire" — always required, never empty.
const (
	OverrideReasonCommercial = "commercial"
	OverrideReasonTest       = "test"
	OverrideReasonPartenaire = "partenaire"
	OverrideReasonMigration  = "migration"
	OverrideReasonGeste      = "geste"
)

// ValidOverrideReasons is the closed set subscription_overrides.reason must
// belong to.
var ValidOverrideReasons = map[string]bool{
	OverrideReasonCommercial: true,
	OverrideReasonTest:       true,
	OverrideReasonPartenaire: true,
	OverrideReasonMigration:  true,
	OverrideReasonGeste:      true,
}

// Override is one row of subscription_overrides — an internal-only
// commercial dérogation (LOT B B1d), never exposed to the client. See
// migrations/todo/139_subscription_overrides.up.sql's table comment for what
// Target means for each Kind.
type Override struct {
	ID         string     `json:"id"`
	MerchantID string     `json:"merchant_id"`
	Kind       string     `json:"kind"`
	Target     string     `json:"target"`
	Reason     string     `json:"reason"`
	Note       *string    `json:"note,omitempty"`
	CreatedBy  string     `json:"created_by"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	// TrialEndsAt — LOT B B2c-1. Distinct from ExpiresAt above: ExpiresAt
	// (B1d) is a passive marker ListActiveOverrides filters on, with no
	// active behavior ever wired to it. TrialEndsAt drives real behavior at
	// its deadline — reminders, a back-office banner, and (kind='price'
	// specifically) reverting merchant.activation_state to SETUP if no real
	// mandate has taken over by then. See RunTrialExpiryCheck. Flagged in
	// docs/decisions.md as worth unifying with ExpiresAt in a future
	// cleanup — not done here, out of this chantier's scope.
	TrialEndsAt            *time.Time `json:"trial_ends_at,omitempty"`
	TrialReminder7dSentAt  *time.Time `json:"-"`
	TrialReminder1dSentAt  *time.Time `json:"-"`
}

// Kind values for subscription_items.kind — LOT B B1b
// (docs/WelloResto-Parcours-Client-v2.docx §7.2). Not enforced by a DB
// CHECK constraint (same convention as pricing_catalog.kind) — validated
// applicatively by AddItem via ValidKinds.
const (
	KindPlan    = "plan"
	KindModule  = "module"
	KindMetered = "metered"
)

// ValidKinds is the closed set subscription_items.kind must belong to.
var ValidKinds = map[string]bool{
	KindPlan:    true,
	KindModule:  true,
	KindMetered: true,
}

// Code values for subscription_items.code — the exact list from the brief.
const (
	CodeEssentiel        = "essentiel"
	CodePro              = "pro"
	CodeComplet          = "complet"
	CodeReservation      = "reservation"
	CodeHACCP            = "haccp"
	CodePlanning         = "planning"
	CodePlanningEmployee = "planning_employee" // metered — see B1c, recomputed at call time, never read from Quantity
	CodeMarketplaces     = "marketplaces"
	CodeDelivery         = "delivery"
	CodeExtraPOS         = "extra_pos" // metered — see B1c, recomputed at call time, never read from Quantity
	CodeKiosk            = "kiosk"
	CodeSMS              = "sms"
)

// ValidCodes is the closed set subscription_items.code must belong to.
var ValidCodes = map[string]bool{
	CodeEssentiel:        true,
	CodePro:              true,
	CodeComplet:          true,
	CodeReservation:      true,
	CodeHACCP:            true,
	CodePlanning:         true,
	CodePlanningEmployee: true,
	CodeMarketplaces:     true,
	CodeDelivery:         true,
	CodeExtraPOS:         true,
	CodeKiosk:            true,
	CodeSMS:              true,
}

// Item is one row of subscription_items — a billed line, distinct from
// subscriptions.*_enabled (access). See package doc comment in
// repository.go for the §7.1 access-vs-billed split.
type Item struct {
	ID             string     `json:"id"`
	MerchantID     string     `json:"merchant_id"`
	Code           string     `json:"code"`
	Kind           string     `json:"kind"`
	Quantity       int        `json:"quantity"`
	UnitPriceCents int        `json:"unit_price_cents"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	RemovedAt      *time.Time `json:"removed_at,omitempty"`
}
