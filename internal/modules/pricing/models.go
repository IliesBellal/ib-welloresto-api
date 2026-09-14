package pricing

// Plan codes — kind='plan' rows in pricing_catalog (migration 133).
const (
	PlanEssentiel = "essentiel" // "à la carte" in the reference doc (§4.4) — every module billed individually
	PlanPro       = "pro"
	PlanComplet   = "complet"
)

// nonPlanningModuleCodes are the only codes that compete for Pro's "2
// modules au choix" free slots (§1.3/§4.4) — planning is a separate,
// always-partially-included line (base included, only the >10-employee
// surcharge is ever billed on pro/complet), not one of the two choices.
var nonPlanningModuleCodes = map[string]bool{
	"reservation":  true,
	"haccp":        true,
	"marketplaces": true,
	"delivery":     true,
}

const planningModuleCode = "planning"

// planningFreeEmployeesOnPlans is how many salariés' planning cost pro and
// complet absorb into their base price before the per-employee surcharge
// starts (§1.3: "planning jusqu'à 10 salariés" ; §4.4: "max(0 ; 2,50 ×
// (salariés − 10))"). Essentiel (à la carte) has no such floor — it bills
// every employee from the first (§4.4: "29 + 2,50 × salariés").
const planningFreeEmployeesOnPlans = 10

// BreakdownLine is one line of Quote.Breakdown — a priced component of the
// resolved plan, shown to the caller for transparency (§4.4's example
// response).
type BreakdownLine struct {
	Code        string `json:"code"`
	Label       string `json:"label"`
	AmountCents int    `json:"amount_cents"`
}

// Cart is the pricing input — mirrors docs' §4.4 POST
// /v1/public/signup-context request body exactly (field names included).
type Cart struct {
	Modules      []string `json:"modules"`
	Employees    int      `json:"employees"`
	Kiosks       int      `json:"kiosks"` // captured for attribution/display only — no cost formula includes kiosks (bornes have their own tiered, non-plan-choice pricing, see docs/decisions.md)
	ExtraPOS     int      `json:"extra_pos"`
	BillingCycle string   `json:"billing_cycle"` // "monthly" | "annual" ; defaults to "monthly"
}

// Quote is ResolveCheapestPlan's result — matches §4.4's resolved_plan
// response shape (plan_code / monthly_total_cents / breakdown) plus the
// real packages.id this chantier's callers (signup) need internally.
type Quote struct {
	PlanCode          string          `json:"plan_code"`
	PackageID         string          `json:"-"`
	MonthlyTotalCents int             `json:"monthly_total_cents"`
	Breakdown         []BreakdownLine `json:"breakdown"`
}

// CatalogPlan is one kind='plan' row.
type CatalogPlan struct {
	Code                 string
	PackageName          string
	MonthlyPriceCents    int
	AnnualPriceCents     int
	IncludedModulesCount *int // nil = unlimited (complet)
	// StripePriceID — LOT B B2c-0: the real Stripe Price this plan's
	// monthly amount is billed through. Empty until
	// cmd/ensure_stripe_prices provisions one (see docs/decisions.md) —
	// never created on the fly by request-serving code.
	StripePriceID string
}

// CatalogModule is one kind='module' row.
type CatalogModule struct {
	Code              string
	Label             string
	MonthlyPriceCents int
	PerUnitPriceCents *int
	PerUnitLabel      string
	// StripePriceID — the flat monthly Price for this module (B2c-0).
	StripePriceID string
	// PerUnitStripePriceID — planning's own per-employee Price, distinct
	// from StripePriceID above (planning's flat monthly amount) — see
	// subscriptions.CodePlanningEmployee, a separate subscription_items
	// line billed at this rate, not at StripePriceID's.
	PerUnitStripePriceID string
}

// CatalogAddon is one kind='addon' row (poste supplémentaire, bornes —
// reference data only in this chantier, see docs/decisions.md).
type CatalogAddon struct {
	Code              string
	Label             string
	MonthlyPriceCents int
	// StripePriceID — extra_seat's Price, consumed by
	// subscriptions.CodeExtraPOS (B2c-0).
	StripePriceID string
}

// Catalog is the whole pricing_catalog table, bucketed by kind.
type Catalog struct {
	Plans   map[string]CatalogPlan
	Modules map[string]CatalogModule
	Addons  map[string]CatalogAddon
}

// planPrice returns plan's monthly-or-annual-equivalent rate per billing.
func planPrice(plan CatalogPlan, billing string) int {
	if billing == "annual" {
		return plan.AnnualPriceCents
	}
	return plan.MonthlyPriceCents
}
