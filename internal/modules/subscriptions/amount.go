package subscriptions

import (
	"context"
	"database/sql"

	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/pricing"
)

// annualCycleMonths — LOT B B1c, rule 4 : "cycle annuel : dix mois facturés
// sur douze". A flat multiplier applied to the monthly grid total, uniform
// across every item kind — distinct from pricing_catalog.annual_price_cents
// (chantier 11), which is a separate, already-baked-in rate used only by
// pricing.Service.ResolveCheapestPlan's new-signup quote. The two are not
// combined: this computation always prices from MonthlyPriceCents and
// applies this multiplier itself, so a plan's annual_price_cents column is
// never read here.
const annualCycleMonths = 10

// multiMerchantDiscountPercent — LOT B B1c, rule 5.
const multiMerchantDiscountPercent = 10

// extraPOSCatalogCode bridges subscription_items' "extra_pos" to
// pricing_catalog's "extra_seat" addon — same concept (poste supplémentaire
// / additional cash desk beyond the first), different code string in each
// table. Not a data bug to fix, just two chantiers naming the same thing
// differently — documented here rather than reconciled, since renaming
// either table's code is out of B1c's scope.
const extraPOSCatalogCode = "extra_seat"

// BreakdownLine is one priced component of an Amount.
type BreakdownLine struct {
	Code           string `json:"code"`
	Kind           string `json:"kind"`
	Quantity       int    `json:"quantity"`
	UnitPriceCents int    `json:"unit_price_cents"`
	AmountCents    int    `json:"amount_cents"`
}

// Amount is ComputeSubscriptionAmount's result.
type Amount struct {
	MerchantID   string `json:"merchant_id"`
	BillingCycle string `json:"billing_cycle"`
	// Overridden is true when subscriptions.override_price_cents was
	// non-NULL — TotalCents is then that fixed value, exactly (rule 1: "Le
	// détail est quand même calculé et retourné, pour affichage
	// comparatif"). Breakdown always reflects the grid computation,
	// whether or not it was actually charged.
	Overridden bool            `json:"overridden"`
	TotalCents int             `json:"total_cents"`
	Breakdown  []BreakdownLine `json:"breakdown"`
}

// Service computes what a merchant is actually billed (subscription_items,
// §7.1) — kept separate from Repository, which only owns subscription_items
// CRUD, because this needs pricing.Repository (chantier 11's catalog) as an
// extra dependency. db is used only by the override write paths (B1d, see
// overrides.go), which need dbutils.RunInTx to write subscription_overrides
// and its side-effect column atomically.
type Service struct {
	db          *sql.DB
	repo        *Repository
	pricingRepo *pricing.Repository
	// pricingSvc backs PreviewChange's B1e §7.7 pack-vs-à-la-carte
	// comparison only (pricing.Service.ResolveCheapestPlan) — every other
	// method here reads pricing_catalog directly via pricingRepo.
	pricingSvc *pricing.Service
	// stripeSyncer — LOT B B2c-0. Optional (nil until routes.go calls
	// SetStripeSyncer): billing.Service implements this after
	// subscriptions.Service already exists, so wiring it in eagerly via
	// NewService would need billing constructed first, which itself needs
	// this *Service (see billing.NewService) — a genuine construction-order
	// cycle, resolved with a setter instead of a constructor param.
	stripeSyncer StripeSyncer
	// mailer — LOT B B2c-1's trial-expiry reminders (RunTrialExpiryCheck).
	// Optional for the same construction-order reason as stripeSyncer; a
	// Service without one (most tests) just skips sending.
	mailer mailer.Service
}

// SetMailer wires the trial-reminder email capability in — called once from
// routes.go.
func (s *Service) SetMailer(m mailer.Service) {
	s.mailer = m
}

// StripeSyncer is the one capability billing.Service exposes back to this
// package — deliberately NOT a full import of internal/modules/billing
// (which itself imports subscriptions to resolve Stripe line items; a
// two-way package import would be a real cycle, not just an inconvenient
// construction order).
type StripeSyncer interface {
	SyncSubscriptionItems(ctx context.Context, merchantID string) error
}

// SetStripeSyncer wires the real Stripe-sync capability in — called once
// from routes.go after both services exist.
func (s *Service) SetStripeSyncer(syncer StripeSyncer) {
	s.stripeSyncer = syncer
}

func NewService(db *sql.DB, repo *Repository, pricingRepo *pricing.Repository, pricingSvc *pricing.Service) *Service {
	return &Service{db: db, repo: repo, pricingRepo: pricingRepo, pricingSvc: pricingSvc}
}

// resolveUnitPriceCents resolves code's per-unit price from catalog — the one
// place every caller that needs to know "does this code have a real price,
// and what is it" goes through: ComputeSubscriptionAmount's per-item loop,
// AddItem's write-time guard (P3), and preview.go's simulation and
// ApplyItemChanges (B1e).
//
// planning_employee/extra_pos are the only two hardcoded cases — they're
// metered (rule 3), billed under a different underlying catalog entry
// (planning's per-employee rate, extra_seat's addon) than their own code
// name, so there's no pricing_catalog row named "planning_employee" or
// "extra_pos" to look up directly. Every other code is resolved generically
// by looking it up in catalog.Plans then catalog.Modules — so a new
// negotiated plan/module variant (e.g. a discounted "essentiel-xxx" row
// added to pricing_catalog with kind='plan') becomes billable the moment
// it's added to the catalog, with no Go code change. kiosk/sms (and any
// other code pricing_catalog has no row for) fall through to ok=false — see
// models.ErrSubscriptionItemPriceUnavailable's doc comment for why.
func resolveUnitPriceCents(catalog *pricing.Catalog, code string) (cents int, ok bool) {
	switch code {
	case CodePlanningEmployee:
		mod, ok := catalog.Modules[CodePlanning]
		if !ok || mod.PerUnitPriceCents == nil {
			return 0, false
		}
		return *mod.PerUnitPriceCents, true
	case CodeExtraPOS:
		addon, ok := catalog.Addons[extraPOSCatalogCode]
		if !ok {
			return 0, false
		}
		return addon.MonthlyPriceCents, true
	}
	if plan, ok := catalog.Plans[code]; ok {
		return plan.MonthlyPriceCents, true
	}
	if mod, ok := catalog.Modules[code]; ok {
		return mod.MonthlyPriceCents, true
	}
	return 0, false
}

// PriceAvailableForCode reports whether pricing_catalog currently has a
// usable price for code — see resolveUnitPriceCents.
func PriceAvailableForCode(catalog *pricing.Catalog, code string) bool {
	_, ok := resolveUnitPriceCents(catalog, code)
	return ok
}

// AddItem is the guarded entry point real callers (B1d/B1e) use to create a
// billed line — unlike Repository.AddItem, it checks pricing_catalog before
// writing (LOT B PRÉALABLE P3): a subscription_items row must never exist
// for a code the catalog can't yet price, or ComputeSubscriptionAmount would
// immediately fail to read it back. Repository.AddItem itself stays
// unguarded (kind shape validation only, plus its own pricing_catalog
// existence check — see its doc comment) — it is exercised directly by
// TestRepository_Postgres against always-priced codes, and is not itself a
// reachable HTTP path.
func (s *Service) AddItem(ctx context.Context, merchantID, code, kind string, quantity, unitPriceCents int) (Item, error) {
	catalog, err := s.pricingRepo.LoadCatalog(ctx)
	if err != nil {
		return Item{}, err
	}
	if !PriceAvailableForCode(catalog, code) {
		return Item{}, models.ErrSubscriptionItemPriceUnavailable
	}
	return s.repo.AddItem(ctx, merchantID, code, kind, quantity, unitPriceCents)
}

// ComputeSubscriptionAmount implements chantier B1c's five rules, in order.
// Always queries live state (subscription_items, employees, cash_desks,
// users_rights) — nothing is cached, there is no "amount" stored anywhere.
func (s *Service) ComputeSubscriptionAmount(ctx context.Context, merchantID string) (Amount, error) {
	billing, err := s.repo.GetBillingInfo(ctx, merchantID)
	if err != nil {
		return Amount{}, err
	}
	items, err := s.repo.ListActive(ctx, merchantID)
	if err != nil {
		return Amount{}, err
	}
	catalog, err := s.pricingRepo.LoadCatalog(ctx)
	if err != nil {
		return Amount{}, err
	}
	return s.computeAmount(ctx, merchantID, items, billing, catalog)
}

// computeAmount is ComputeSubscriptionAmount's pure calculation core,
// extracted so preview.go's PreviewChange (B1e) can run the exact same five
// rules against a simulated, not-yet-written item list — a code add/remove
// must price identically whether it's a hypothetical preview or the real
// post-write state, or the preview would lie. items/billing/catalog are
// still whatever the caller fetched (live for ComputeSubscriptionAmount,
// simulated for PreviewChange) — this function itself does no I/O beyond the
// two DB reads rule 3's metered codes need (employee/cash-desk counts,
// unavoidable — they're never inputs, always freshly counted).
func (s *Service) computeAmount(ctx context.Context, merchantID string, items []Item, billing BillingInfo, catalog *pricing.Catalog) (Amount, error) {
	isProOrComplet := false
	for _, it := range items {
		if it.Kind == KindPlan && (it.Code == pricing.PlanPro || it.Code == pricing.PlanComplet) {
			isProOrComplet = true
			break
		}
	}

	cycleMultiplier := 1
	if billing.BillingCycle == "annual" {
		cycleMultiplier = annualCycleMonths
	}

	breakdown := make([]BreakdownLine, 0, len(items)+1)
	gridTotal := 0
	for _, it := range items {
		quantity := it.Quantity
		var unitPriceCents int

		switch it.Code {
		case CodePlanningEmployee:
			// Rule 3: recalculated at call time, never read from the
			// stored row — "fiches employés actives, moins 10 si le plan
			// est Pro ou Complet, minimum 0".
			mod, ok := catalog.Modules[CodePlanning]
			if !ok || mod.PerUnitPriceCents == nil {
				return Amount{}, models.ErrSubscriptionItemPriceUnavailable
			}
			unitPriceCents = *mod.PerUnitPriceCents
			count, err := s.repo.activeEmployeeCount(ctx, merchantID)
			if err != nil {
				return Amount{}, err
			}
			quantity = count
			if isProOrComplet {
				quantity -= 10
			}
			if quantity < 0 {
				quantity = 0
			}

		case CodeExtraPOS:
			// Rule 3: "postes actifs moins 1".
			addon, ok := catalog.Addons[extraPOSCatalogCode]
			if !ok {
				return Amount{}, models.ErrSubscriptionItemPriceUnavailable
			}
			unitPriceCents = addon.MonthlyPriceCents
			count, err := s.repo.activeCashDeskCount(ctx, merchantID)
			if err != nil {
				return Amount{}, err
			}
			quantity = count - 1
			if quantity < 0 {
				quantity = 0
			}

		default:
			// Generic plan/module lookup — any code pricing_catalog carries
			// under kind='plan' or kind='module' is billable here with no Go
			// change, including a negotiated variant (e.g. a discounted
			// "essentiel-xxx" row) added straight to the catalog. CodeKiosk,
			// CodeSMS (no usable pricing_catalog entry — kiosk is tiered
			// across 3 addon codes, sms has no row at all) and anything else
			// absent from the catalog fall through to the error. See
			// models.ErrSubscriptionItemPriceUnavailable's doc comment.
			if plan, ok := catalog.Plans[it.Code]; ok {
				unitPriceCents = plan.MonthlyPriceCents
			} else if mod, ok := catalog.Modules[it.Code]; ok {
				unitPriceCents = mod.MonthlyPriceCents
			} else {
				return Amount{}, models.ErrSubscriptionItemPriceUnavailable
			}
		}

		amountCents := quantity * unitPriceCents * cycleMultiplier
		gridTotal += amountCents
		breakdown = append(breakdown, BreakdownLine{
			Code: it.Code, Kind: it.Kind, Quantity: quantity,
			UnitPriceCents: unitPriceCents, AmountCents: amountCents,
		})
	}

	multiMerchant, err := s.repo.hasMultiMerchantOwner(ctx, merchantID)
	if err != nil {
		return Amount{}, err
	}
	if multiMerchant && gridTotal > 0 {
		discountCents := gridTotal * multiMerchantDiscountPercent / 100
		gridTotal -= discountCents
		breakdown = append(breakdown, BreakdownLine{
			Code: "multi_merchant_discount", Kind: "discount",
			Quantity: 1, UnitPriceCents: -discountCents, AmountCents: -discountCents,
		})
	}

	amount := Amount{
		MerchantID:   merchantID,
		BillingCycle: billing.BillingCycle,
		TotalCents:   gridTotal,
		Breakdown:    breakdown,
	}
	if billing.OverridePriceCents != nil {
		amount.Overridden = true
		amount.TotalCents = *billing.OverridePriceCents
	}
	return amount, nil
}
