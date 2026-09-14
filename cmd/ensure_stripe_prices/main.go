// Command ensure_stripe_prices provisions the real Stripe Prices LOT B
// B2c-0's recurring subscription construction needs — one per billable
// pricing_catalog row (plan/module + planning's per-employee rate + the
// extra_seat addon), created once and written back to
// pricing_catalog.stripe_price_id/per_unit_stripe_price_id.
//
// Idempotent: a row that already has a stripe_price_id is left untouched —
// this is deliberately NOT something request-serving code ever does on the
// fly (see docs/decisions.md, B2c-0) — kiosk/sms are never included here,
// they have no cents price at all yet (P1/P3) and stay unresolvable to
// Stripe for the same reason.
//
// Usage:
//
//	DB_DIALECT=postgres POSTGRES_URL=... STRIPE_API_KEY=sk_test_... \
//	  GOOGLE_API_KEY=x R2_PRIVATE_BUCKET=x PIN_PEPPER=x \
//	  go run ./cmd/ensure_stripe_prices
package main

import (
	"context"
	"database/sql"
	"log"

	"welloresto-api/internal/config"
	"welloresto-api/internal/database"
	"welloresto-api/internal/database/dbx"
	stripeclient "welloresto-api/internal/infrastructure/stripe"
	"welloresto-api/internal/modules/pricing"
)

func main() {
	cfg := config.Load()

	var db *sql.DB
	var err error
	if dbx.ActiveDialect() == dbx.Postgres {
		db, err = database.NewPostgres(cfg.Database)
	} else {
		db, err = database.NewMySQL(cfg.Database)
	}
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer db.Close()

	if cfg.Stripe.APIKey == "" {
		log.Fatal("STRIPE_API_KEY not set")
	}
	stripeMgr := stripeclient.NewStripeManager(cfg.Stripe.APIKey)

	ctx := context.Background()
	repo := pricing.NewRepository(db)
	catalog, err := repo.LoadCatalog(ctx)
	if err != nil {
		log.Fatalf("load catalog: %v", err)
	}

	ensure := func(kind, code, label string, cents int, existing string) {
		if existing != "" {
			log.Printf("%s/%s: already has %s, skipping", kind, code, existing)
			return
		}
		price, err := stripeMgr.CreateRecurringPrice("WelloResto — "+label, cents, code)
		if err != nil {
			log.Fatalf("%s/%s: CreateRecurringPrice: %v", kind, code, err)
		}
		if err := repo.SetStripePriceID(ctx, kind, code, price.ID); err != nil {
			log.Fatalf("%s/%s: SetStripePriceID: %v", kind, code, err)
		}
		log.Printf("%s/%s: created %s (%d cents/month)", kind, code, price.ID, cents)
	}

	for code, plan := range catalog.Plans {
		ensure("plan", code, plan.PackageName, plan.MonthlyPriceCents, plan.StripePriceID)
	}
	for code, mod := range catalog.Modules {
		ensure("module", code, mod.Label, mod.MonthlyPriceCents, mod.StripePriceID)
		if mod.PerUnitPriceCents != nil {
			if mod.PerUnitStripePriceID != "" {
				log.Printf("module/%s (per-unit): already has %s, skipping", code, mod.PerUnitStripePriceID)
				continue
			}
			price, err := stripeMgr.CreateRecurringPrice("WelloResto — "+mod.Label+" ("+mod.PerUnitLabel+")", *mod.PerUnitPriceCents, code+"_per_unit")
			if err != nil {
				log.Fatalf("module/%s (per-unit): CreateRecurringPrice: %v", code, err)
			}
			if err := repo.SetPerUnitStripePriceID(ctx, code, price.ID); err != nil {
				log.Fatalf("module/%s (per-unit): SetPerUnitStripePriceID: %v", code, err)
			}
			log.Printf("module/%s (per-unit): created %s (%d cents/employee/month)", code, price.ID, *mod.PerUnitPriceCents)
		}
	}
	// extra_seat is the only addon a real billed subscription_items code
	// (extra_pos) resolves through today — the kiosk tiers/others are
	// reference data only (see docs/decisions.md, chantier 11).
	if addon, ok := catalog.Addons["extra_seat"]; ok {
		ensure("addon", "extra_seat", addon.Label, addon.MonthlyPriceCents, addon.StripePriceID)
	}

	log.Println("done")
}
