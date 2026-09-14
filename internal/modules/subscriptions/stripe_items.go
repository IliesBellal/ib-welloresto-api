package subscriptions

import (
	"context"

	"welloresto-api/internal/models"
)

// StripeLineItem is one line ResolveStripeLineItems produces — a real
// Stripe Price + quantity pair, ready for a Subscription's Items (LOT B
// B2c-0). Code is carried through so billing.Service can diff against an
// existing Stripe subscription's items by code (via each Stripe
// SubscriptionItem's own metadata), not by Price ID alone.
type StripeLineItem struct {
	Code     string
	PriceID  string
	Quantity int
}

// ResolveStripeLineItems turns merchantID's active subscription_items into
// real Stripe Price/quantity pairs — "PAS un montant recalculé à la main :
// Stripe doit porter les vrais objets Price" (B2c-0). Mirrors computeAmount's
// per-code switch and quantity rules (rule 3: planning_employee/extra_pos
// recomputed from live counts) but resolves pricing_catalog.stripe_price_id
// instead of monthly_price_cents.
//
// Annual billing_cycle is out of scope here (see docs/decisions.md) —
// pricing_catalog carries no distinct Stripe Price for a yearly interval,
// and B1c's own "×10 months" annual rule doesn't map onto a single Stripe
// Price either. Returns ErrAnnualStripeSubscriptionNotSupported rather than
// silently building a monthly Stripe subscription for a merchant who chose
// annual billing.
func (s *Service) ResolveStripeLineItems(ctx context.Context, merchantID string) ([]StripeLineItem, error) {
	billing, err := s.repo.GetBillingInfo(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	if billing.BillingCycle == "annual" {
		return nil, models.ErrAnnualStripeSubscriptionNotSupported
	}

	items, err := s.repo.ListActive(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	catalog, err := s.pricingRepo.LoadCatalog(ctx)
	if err != nil {
		return nil, err
	}

	isProOrComplet := false
	for _, it := range items {
		if it.Kind == KindPlan && (it.Code == "pro" || it.Code == "complet") {
			isProOrComplet = true
			break
		}
	}

	lines := make([]StripeLineItem, 0, len(items))
	for _, it := range items {
		var priceID string
		quantity := it.Quantity

		switch it.Code {
		case CodePlanningEmployee:
			mod, ok := catalog.Modules[CodePlanning]
			if !ok || mod.PerUnitStripePriceID == "" {
				return nil, models.ErrStripeCatalogPriceMissing
			}
			priceID = mod.PerUnitStripePriceID
			count, err := s.repo.activeEmployeeCount(ctx, merchantID)
			if err != nil {
				return nil, err
			}
			quantity = count
			if isProOrComplet {
				quantity -= 10
			}
			if quantity < 0 {
				quantity = 0
			}

		case CodeExtraPOS:
			addon, ok := catalog.Addons[extraPOSCatalogCode]
			if !ok || addon.StripePriceID == "" {
				return nil, models.ErrStripeCatalogPriceMissing
			}
			priceID = addon.StripePriceID
			count, err := s.repo.activeCashDeskCount(ctx, merchantID)
			if err != nil {
				return nil, err
			}
			quantity = count - 1
			if quantity < 0 {
				quantity = 0
			}

		case CodeEssentiel, CodePro, CodeComplet:
			plan, ok := catalog.Plans[it.Code]
			if !ok || plan.StripePriceID == "" {
				return nil, models.ErrStripeCatalogPriceMissing
			}
			priceID = plan.StripePriceID

		case CodeReservation, CodeHACCP, CodePlanning, CodeMarketplaces, CodeDelivery:
			mod, ok := catalog.Modules[it.Code]
			if !ok || mod.StripePriceID == "" {
				return nil, models.ErrStripeCatalogPriceMissing
			}
			priceID = mod.StripePriceID

		default:
			// CodeKiosk, CodeSMS — never resolvable, same as the cents-price
			// switch in computeAmount.
			return nil, models.ErrStripeCatalogPriceMissing
		}

		// A quantity-0 metered line (e.g. no extra POS beyond the first)
		// isn't billed at all — Stripe rejects a zero-quantity subscription
		// item outright, and there is nothing to charge for anyway.
		if quantity == 0 {
			continue
		}

		lines = append(lines, StripeLineItem{Code: it.Code, PriceID: priceID, Quantity: quantity})
	}

	return lines, nil
}
