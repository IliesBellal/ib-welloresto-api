package subscriptions

import (
	"context"
	"time"

	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/pricing"
)

// packModuleCodes are the only subscription_items codes ResolveCheapestPlan
// understands (pricing.Cart.Modules) — everything else (kiosk/sms, the plan
// codes themselves, the two metered codes) is excluded from the simulated
// Cart built below.
var packModuleCodes = map[string]bool{
	CodeReservation:  true,
	CodeHACCP:        true,
	CodePlanning:     true,
	CodeMarketplaces: true,
	CodeDelivery:     true,
}

// PackComparison is Preview's optional §7.7 hint: the cheapest packaged plan
// currently beats the (possibly just-changed) à-la-carte composition.
type PackComparison struct {
	PlanCode          string `json:"plan_code"`
	MonthlyTotalCents int    `json:"monthly_total_cents"`
}

// Preview is GET /v1/subscriptions/preview's result (B1e). Read-only —
// nothing here is written.
type Preview struct {
	MerchantID string `json:"merchant_id"`
	// CurrentTotalCents/NewTotalCents are what ComputeSubscriptionAmount
	// would return before/after applying Add/Remove — TotalCents, i.e. the
	// actually-billed figure (an active override_price_cents makes the two
	// equal regardless of Add/Remove: that's expected, not a bug — the
	// override wins over the grid either way).
	CurrentTotalCents int `json:"current_total_cents"`
	NewTotalCents     int `json:"new_total_cents"`
	// ProrataCents is the estimated charge/credit for the remainder of the
	// current billing period — (NewTotalCents-CurrentTotalCents) scaled by
	// the fraction of the period left. See prorataFraction's doc comment for
	// the pre-LIVE fallback (no current_period_end yet).
	ProrataCents int `json:"prorata_cents"`
	// PackComparison is set only when PackCheaper is true.
	PackCheaper    bool            `json:"pack_cheaper"`
	PackComparison *PackComparison `json:"pack_comparison,omitempty"`
	AfterBreakdown []BreakdownLine `json:"after_breakdown"`
}

// PreviewChange implements B1e's GET /v1/subscriptions/preview — "aucune
// écriture" is structural here, not just a promise: every DB call this
// method reaches is a read (ListActive, GetBillingInfo, LoadCatalog, the two
// metered-code counts via computeAmount), never AddItem/RemoveItem.
func (s *Service) PreviewChange(ctx context.Context, merchantID string, add, remove []string) (Preview, error) {
	billing, err := s.repo.GetBillingInfo(ctx, merchantID)
	if err != nil {
		return Preview{}, err
	}
	catalog, err := s.pricingRepo.LoadCatalog(ctx)
	if err != nil {
		return Preview{}, err
	}
	currentItems, err := s.repo.ListActive(ctx, merchantID)
	if err != nil {
		return Preview{}, err
	}

	current, err := s.computeAmount(ctx, merchantID, currentItems, billing, catalog)
	if err != nil {
		return Preview{}, err
	}

	simulatedItems, err := simulateItems(currentItems, add, remove, catalog)
	if err != nil {
		return Preview{}, err
	}
	after, err := s.computeAmount(ctx, merchantID, simulatedItems, billing, catalog)
	if err != nil {
		return Preview{}, err
	}

	preview := Preview{
		MerchantID:        merchantID,
		CurrentTotalCents: current.TotalCents,
		NewTotalCents:     after.TotalCents,
		ProrataCents:      prorataCents(after.TotalCents-current.TotalCents, billing, time.Now()),
		AfterBreakdown:    after.Breakdown,
	}

	// §7.7 pack-vs-à-la-carte comparison — always run on a "monthly" basis
	// on both sides (see the doc comment on monthlyEquivalentCents) since
	// ResolveCheapestPlan's annual rate (pricing_catalog.annual_price_cents)
	// and this package's own annual rule (LOT B B1c rule 4, a flat ×10 on
	// the monthly grid) are two independently-designed, non-interchangeable
	// annual conventions — see annualCycleMonths' doc comment. Comparing one
	// cycle's à-la-carte total against the other cycle's pack total would be
	// meaningless, so both are normalized to monthly here specifically for
	// this hint; the headline New/CurrentTotalCents above stay on the
	// subscription's real billing_cycle throughout.
	if s.pricingSvc != nil {
		afterMonthlyEquivalent := monthlyEquivalentCents(after.TotalCents, billing.BillingCycle)
		cart := buildCart(simulatedItems)
		quote, err := s.pricingSvc.ResolveCheapestPlan(ctx, cart)
		if err == nil && quote.MonthlyTotalCents < afterMonthlyEquivalent {
			preview.PackCheaper = true
			preview.PackComparison = &PackComparison{PlanCode: quote.PlanCode, MonthlyTotalCents: quote.MonthlyTotalCents}
		}
		// A ResolveCheapestPlan error (e.g. an unmapped module code) is not
		// fatal to the preview — the pack hint is a nice-to-have on top of
		// the actually-requested current/new totals, not the point of the
		// endpoint.
	}

	return preview, nil
}

// ApplyItemChanges implements B1e's POST /v1/subscriptions/items — applies
// add/remove for real (unlike PreviewChange) and returns the resulting
// Amount. Never touches subscriptions.override_price_cents: AddItem/RemoveItem
// only ever write subscription_items, so a pre-existing override is
// preserved by construction, not by a special case here.
func (s *Service) ApplyItemChanges(ctx context.Context, merchantID string, add, remove []string) (Amount, error) {
	currentItems, err := s.repo.ListActive(ctx, merchantID)
	if err != nil {
		return Amount{}, err
	}
	active := make(map[string]Item, len(currentItems))
	for _, it := range currentItems {
		active[it.Code] = it
	}

	catalog, err := s.pricingRepo.LoadCatalog(ctx)
	if err != nil {
		return Amount{}, err
	}

	for _, code := range remove {
		it, ok := active[code]
		if !ok {
			continue // not active — nothing to remove, not an error
		}
		if err := s.repo.RemoveItem(ctx, it.ID); err != nil {
			return Amount{}, err
		}
		delete(active, code)
	}

	for _, code := range add {
		if _, ok := active[code]; ok {
			continue // already active — a no-op, not a duplicate-row error
		}
		if !ValidCodes[code] {
			return Amount{}, models.ErrInvalidSubscriptionItemCode
		}
		unitPriceCents, ok := resolveUnitPriceCents(catalog, code)
		if !ok {
			return Amount{}, models.ErrSubscriptionItemPriceUnavailable
		}
		kind := itemKindForCode(code)
		if _, err := s.AddItem(ctx, merchantID, code, kind, 1, unitPriceCents); err != nil {
			return Amount{}, err
		}
	}

	// LOT B B2c-0 : un changement de module doit mettre à jour l'abonnement
	// Stripe existant, pas seulement subscription_items localement — sinon
	// invoice.created continuerait de facturer l'ancienne composition.
	// s.stripeSyncer est nil tant que routes.go ne l'a pas branché (voir
	// SetStripeSyncer) — les tests qui n'en ont pas besoin n'ont rien à
	// fournir. Une erreur ici signale une vraie divergence
	// local/Stripe : remontée telle quelle plutôt qu'avalée, même si
	// l'écriture locale, elle, a déjà eu lieu (pas de transaction couvrant
	// les deux systèmes — voir docs/decisions.md).
	if s.stripeSyncer != nil {
		if err := s.stripeSyncer.SyncSubscriptionItems(ctx, merchantID); err != nil {
			return Amount{}, err
		}
	}

	return s.ComputeSubscriptionAmount(ctx, merchantID)
}

// simulateItems applies add/remove to currentItems in memory — never
// touches the database. Added codes get a freshly-resolved catalog price
// (0/Quantity=1 placeholder for metered codes, whose quantity computeAmount
// always recomputes from live counts regardless — see
// resolveUnitPriceCents' callers). An unpriceable add (kiosk/sms, or a code
// pricing_catalog can't resolve right now) fails the whole preview, matching
// AddItem's real behavior: the preview must not promise a change the real
// endpoint would reject.
func simulateItems(currentItems []Item, add, remove []string, catalog *pricing.Catalog) ([]Item, error) {
	active := make(map[string]Item, len(currentItems))
	for _, it := range currentItems {
		active[it.Code] = it
	}
	for _, code := range remove {
		delete(active, code)
	}
	for _, code := range add {
		if _, ok := active[code]; ok {
			continue
		}
		if !ValidCodes[code] {
			return nil, models.ErrInvalidSubscriptionItemCode
		}
		unitPriceCents, ok := resolveUnitPriceCents(catalog, code)
		if !ok {
			return nil, models.ErrSubscriptionItemPriceUnavailable
		}
		active[code] = Item{Code: code, Kind: itemKindForCode(code), Quantity: 1, UnitPriceCents: unitPriceCents}
	}

	items := make([]Item, 0, len(active))
	for _, it := range active {
		items = append(items, it)
	}
	return items, nil
}

func itemKindForCode(code string) string {
	switch code {
	case CodeEssentiel, CodePro, CodeComplet:
		return KindPlan
	case CodePlanningEmployee, CodeExtraPOS:
		return KindMetered
	default:
		return KindModule
	}
}

func buildCart(items []Item) pricing.Cart {
	cart := pricing.Cart{BillingCycle: "monthly"}
	for _, it := range items {
		if packModuleCodes[it.Code] {
			cart.Modules = append(cart.Modules, it.Code)
		}
	}
	return cart
}

// monthlyEquivalentCents undoes B1c rule 4's flat ×annualCycleMonths so an
// annual-cycle grid total can be compared against ResolveCheapestPlan's
// always-monthly Cart — see PreviewChange's doc comment on why the two
// annual conventions can't be compared directly.
func monthlyEquivalentCents(totalCents int, billingCycle string) int {
	if billingCycle == "annual" && annualCycleMonths > 0 {
		return totalCents / annualCycleMonths
	}
	return totalCents
}

// prorataCents estimates the charge/credit for the rest of the current
// billing period. Uses subscriptions.current_period_end when set; that
// column stays NULL until a merchant's first real Stripe billing cycle
// starts (LOT B2's SEPA mandate) — most of staging today — so this falls
// back to a nominal 30-day month rather than refusing to estimate at all.
// Flagged in this chantier's report as a simplification to revisit once B2
// wires real invoice periods.
func prorataCents(deltaCents int, billing BillingInfo, now time.Time) int {
	const nominalPeriodDays = 30
	periodEnd := billing.CurrentPeriodEnd
	if periodEnd == nil {
		return deltaCents
	}
	daysLeft := int(periodEnd.Sub(now).Hours() / 24)
	if daysLeft < 0 {
		daysLeft = 0
	}
	if daysLeft > nominalPeriodDays {
		daysLeft = nominalPeriodDays
	}
	return deltaCents * daysLeft / nominalPeriodDays
}
