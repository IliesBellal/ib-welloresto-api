//go:build postgres_integration

package subscriptions

import (
	"context"
	"errors"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

// TestResolveStripeLineItems_Postgres — B2c-0: resolves subscription_items
// into real Stripe Price/quantity pairs, using the actual test-mode Prices
// cmd/ensure_stripe_prices provisioned in staging (see docs/decisions.md for
// their ids) — not a fake, since the whole point is confirming
// pricing_catalog.stripe_price_id round-trips correctly.
func TestResolveStripeLineItems_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-stripeitems-nominal"
	seedSubscription(t, db, merchantID, "monthly", nil)
	cleanupItems(t, db, merchantID)

	repo := NewRepository(db)
	if _, err := repo.AddItem(ctx, merchantID, CodeEssentiel, KindPlan, 1, 7900); err != nil {
		t.Fatalf("AddItem(essentiel): %v", err)
	}
	if _, err := repo.AddItem(ctx, merchantID, CodeHACCP, KindModule, 1, 3500); err != nil {
		t.Fatalf("AddItem(haccp): %v", err)
	}

	svc := newTestService(db)
	lines, err := svc.ResolveStripeLineItems(ctx, merchantID)
	if err != nil {
		t.Fatalf("ResolveStripeLineItems: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("lines = %+v, want 2", lines)
	}
	byCode := map[string]StripeLineItem{}
	for _, l := range lines {
		byCode[l.Code] = l
	}
	essentiel, ok := byCode[CodeEssentiel]
	if !ok || essentiel.PriceID == "" || essentiel.Quantity != 1 {
		t.Fatalf("essentiel line = %+v, want a real PriceID and quantity 1", essentiel)
	}
	haccp, ok := byCode[CodeHACCP]
	if !ok || haccp.PriceID == "" || haccp.Quantity != 1 {
		t.Fatalf("haccp line = %+v, want a real PriceID and quantity 1", haccp)
	}
	if essentiel.PriceID == haccp.PriceID {
		t.Fatalf("essentiel and haccp resolved to the same Price id %q", essentiel.PriceID)
	}
}

// TestResolveStripeLineItems_UnpriceableCode_Postgres — kiosk/sms have no
// Stripe Price at all (P1/P3) — must fail explicitly, not silently omit the
// line (a subscription missing a line the merchant thinks they're paying
// for is worse than an outright error).
func TestResolveStripeLineItems_UnpriceableCode_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-stripeitems-kiosk"
	seedSubscription(t, db, merchantID, "monthly", nil)
	cleanupItems(t, db, merchantID)

	repo := NewRepository(db)
	if _, err := repo.AddItem(ctx, merchantID, CodeKiosk, KindModule, 1, 18900); err != nil {
		t.Fatalf("AddItem(kiosk): %v", err)
	}

	svc := newTestService(db)
	if _, err := svc.ResolveStripeLineItems(ctx, merchantID); !errors.Is(err, models.ErrStripeCatalogPriceMissing) {
		t.Fatalf("ResolveStripeLineItems(kiosk) = %v, want ErrStripeCatalogPriceMissing", err)
	}
}

// TestResolveStripeLineItems_Annual_Postgres — annual billing_cycle has no
// Stripe Price mapping yet (out of scope, see docs/decisions.md) — must
// fail explicitly rather than silently build a monthly Stripe subscription
// for an annual merchant.
func TestResolveStripeLineItems_Annual_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-stripeitems-annual"
	seedSubscription(t, db, merchantID, "annual", nil)
	cleanupItems(t, db, merchantID)

	repo := NewRepository(db)
	if _, err := repo.AddItem(ctx, merchantID, CodeEssentiel, KindPlan, 1, 7900); err != nil {
		t.Fatalf("AddItem(essentiel): %v", err)
	}

	svc := newTestService(db)
	if _, err := svc.ResolveStripeLineItems(ctx, merchantID); !errors.Is(err, models.ErrAnnualStripeSubscriptionNotSupported) {
		t.Fatalf("ResolveStripeLineItems(annual) = %v, want ErrAnnualStripeSubscriptionNotSupported", err)
	}
}

// TestResolveStripeLineItems_MeteredQuantities_Postgres — rule 3's live
// recount (planning_employee/extra_pos) must also drive the Stripe
// quantity, not just the cents computation (ComputeSubscriptionAmount).
func TestResolveStripeLineItems_MeteredQuantities_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-stripeitems-metered"
	seedSubscription(t, db, merchantID, "monthly", nil)
	cleanupItems(t, db, merchantID)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM cash_desks WHERE merchant_id = $1`, merchantID)
	})

	repo := NewRepository(db)
	if _, err := repo.AddItem(ctx, merchantID, CodeEssentiel, KindPlan, 1, 7900); err != nil {
		t.Fatalf("AddItem(essentiel): %v", err)
	}
	if _, err := repo.AddItem(ctx, merchantID, CodeExtraPOS, KindMetered, 99, 2500); err != nil {
		t.Fatalf("AddItem(extra_pos): %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := db.ExecContext(ctx, `INSERT INTO cash_desks (merchant_id, name) VALUES ($1, $2)`, merchantID, "Caisse "+string(rune('A'+i))); err != nil {
			t.Fatalf("seed cash_desk %d: %v", i, err)
		}
	}

	svc := newTestService(db)
	lines, err := svc.ResolveStripeLineItems(ctx, merchantID)
	if err != nil {
		t.Fatalf("ResolveStripeLineItems: %v", err)
	}
	var extraPOS *StripeLineItem
	for i := range lines {
		if lines[i].Code == CodeExtraPOS {
			extraPOS = &lines[i]
		}
	}
	if extraPOS == nil || extraPOS.Quantity != 2 || extraPOS.PriceID == "" {
		t.Fatalf("extra_pos line = %+v, want quantity=2 (3 cash desks - 1) and a real PriceID", extraPOS)
	}
}
