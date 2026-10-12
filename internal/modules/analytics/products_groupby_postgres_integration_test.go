//go:build postgres_integration

package analytics

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// TestProductsGroupByMerchant_Postgres covers the group_by=merchant additions
// to the Produits tab (GetProductsTotalsByMerchant,
// GetProductsByProductAndMerchant) against two establishments with a
// hand-computed dataset:
//   - the per-establishment totals sum exactly to GetProductsScopeTotals'
//     quantity and TTC for the same scope ;
//   - each product's split carries only the establishments that actually sold
//     it, and its quantity/TTC sum to the product's own row in GetProductsPage ;
//   - an establishment with no sale in the window is absent, not a zero row.
func TestProductsGroupByMerchant_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantTZ = "Europe/Paris"
	var merchantAIntID, merchantBIntID int64
	var tvaID20 int64
	var productA, productB int64

	t.Cleanup(func() {
		for _, id := range []int64{merchantAIntID, merchantBIntID} {
			if id != 0 {
				_, _ = db.ExecContext(ctx, `DELETE FROM orderitems WHERE merchant_id = $1`, itoa(id))
				_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, itoa(id))
				_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, id)
			}
		}
		for _, id := range []int64{productA, productB} {
			if id != 0 {
				_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE product_id = $1`, id)
			}
		}
		if tvaID20 != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM tva_categories WHERE tva_id = $1`, tvaID20)
		}
	})

	seedMerchant := func(label string, dest *int64) string {
		if err := db.QueryRowContext(ctx, `
			INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
			VALUES ($1, 'addr', '1', 'street', '75001', 'Paris', $2, 'https://example.com', '0600000000', $3, $4)
			RETURNING id`, "ITest Products GroupBy "+label, "siret-pgb-"+label, "tok-pgb-"+label, merchantTZ).Scan(dest); err != nil {
			t.Fatalf("seed merchant %s: %v", label, err)
		}
		return itoa(*dest)
	}
	merchantA := seedMerchant("A", &merchantAIntID)
	merchantB := seedMerchant("B", &merchantBIntID)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO tva_categories (delivery_type, tva_title, tva_desc, tva_rate)
		VALUES ('0', 'ITest Products GroupBy TVA 20', 'itest', 20) RETURNING tva_id`).Scan(&tvaID20); err != nil {
		t.Fatalf("seed tva_categories: %v", err)
	}
	for _, p := range []struct {
		merchantID string
		name       string
		dest       *int64
	}{
		{merchantA, "ITest PGB Burger A", &productA},
		{merchantB, "ITest PGB Burger B", &productB},
	} {
		if err := db.QueryRowContext(ctx, `
			INSERT INTO products (merchant_id, name, category, price, tva_in_id, tva_take_away_id, tva_delivery_id)
			VALUES ($1, $2, 'itest-pgb', 1000, $3, $3, $3) RETURNING product_id`,
			p.merchantID, p.name, tvaID20).Scan(p.dest); err != nil {
			t.Fatalf("seed product %s: %v", p.name, err)
		}
	}

	loc, err := time.LoadLocation(merchantTZ)
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	// A sells 3 units (two orders: 2 + 1), B sells 1 unit.
	orderA1 := seedOrder(t, ctx, db, merchantA, 801, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 2000, time.Date(2026, 1, 15, 12, 0, 0, 0, loc))
	seedOrderItemWithCost(t, ctx, db, orderA1, productA, merchantA, 2, 1000, nil, strPtr("NO_RECIPE"))
	orderA2 := seedOrder(t, ctx, db, merchantA, 802, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 1000, time.Date(2026, 1, 15, 13, 0, 0, 0, loc))
	seedOrderItemWithCost(t, ctx, db, orderA2, productA, merchantA, 1, 1000, nil, strPtr("NO_RECIPE"))
	orderB1 := seedOrder(t, ctx, db, merchantB, 803, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 1000, time.Date(2026, 1, 15, 14, 0, 0, 0, loc))
	seedOrderItemWithCost(t, ctx, db, orderB1, productB, merchantB, 1, 1000, nil, strPtr("NO_RECIPE"))

	repo := NewRepository(db)
	scope := []string{merchantA, merchantB}
	start, end := time.Date(2026, 1, 15, 0, 0, 0, 0, loc).UTC(), time.Date(2026, 1, 17, 0, 0, 0, 0, loc).UTC()

	// ---- Totals by merchant sum to the ungrouped scope totals ----
	byMerchant, err := repo.GetProductsTotalsByMerchant(ctx, scope, "", start, end)
	if err != nil {
		t.Fatalf("GetProductsTotalsByMerchant: %v", err)
	}
	scopeTotals, err := repo.GetProductsScopeTotals(ctx, scope, "", start, end)
	if err != nil {
		t.Fatalf("GetProductsScopeTotals: %v", err)
	}
	if len(byMerchant) != 2 {
		t.Fatalf("expected 2 establishments, got %+v", byMerchant)
	}
	var sumQty, sumTTC int64
	totalsByID := map[string]ProductsMerchantTotal{}
	for _, row := range byMerchant {
		sumQty += row.QuantitySold
		sumTTC += row.RevenueTTCCents
		totalsByID[row.MerchantID] = row
	}
	if sumQty != scopeTotals.QuantitySold || sumTTC != scopeTotals.RevenueTTCCents {
		t.Fatalf("by-merchant sums (qty=%d ttc=%d) disagree with scope totals (qty=%d ttc=%d)", sumQty, sumTTC, scopeTotals.QuantitySold, scopeTotals.RevenueTTCCents)
	}
	if got := totalsByID[merchantA]; got.QuantitySold != 3 || got.RevenueTTCCents != 3000 {
		t.Fatalf("expected A qty=3 ttc=3000, got %+v", got)
	}
	if got := totalsByID[merchantB]; got.QuantitySold != 1 || got.RevenueTTCCents != 1000 {
		t.Fatalf("expected B qty=1 ttc=1000, got %+v", got)
	}

	// ---- Per-product split matches the product's own page row ----
	page, _, err := repo.GetProductsPage(ctx, scope, "", ProductsSortQuantity, "desc", 1, 10, start, end)
	if err != nil {
		t.Fatalf("GetProductsPage: %v", err)
	}
	productIDs := make([]string, len(page))
	for i, row := range page {
		productIDs[i] = row.ProductID
	}
	split, err := repo.GetProductsByProductAndMerchant(ctx, scope, productIDs, start, end)
	if err != nil {
		t.Fatalf("GetProductsByProductAndMerchant: %v", err)
	}
	for _, row := range page {
		parts := split[row.ProductID]
		if len(parts) != 1 {
			t.Fatalf("product %s: expected exactly one selling establishment, got %+v", row.ProductID, parts)
		}
		if parts[0].QuantitySold != row.QuantitySold || parts[0].RevenueTTCCents != row.RevenueTTCCents {
			t.Fatalf("product %s: split %+v disagrees with its page row qty=%d ttc=%d", row.ProductID, parts[0], row.QuantitySold, row.RevenueTTCCents)
		}
	}
	if got := split[itoa(productA)][0].MerchantID; got != merchantA {
		t.Fatalf("expected product A attributed to merchant A (%s), got %s", merchantA, got)
	}

	// ---- Empty product list: no query, empty map ----
	empty, err := repo.GetProductsByProductAndMerchant(ctx, scope, nil, start, end)
	if err != nil || len(empty) != 0 {
		t.Fatalf("expected empty map and no error for no product ids, got %v / %v", empty, err)
	}

	// ---- Window with no sale: absent rows, not zero rows ----
	emptyStart, emptyEnd := time.Date(2020, 1, 1, 0, 0, 0, 0, loc).UTC(), time.Date(2020, 2, 1, 0, 0, 0, 0, loc).UTC()
	none, err := repo.GetProductsTotalsByMerchant(ctx, scope, "", emptyStart, emptyEnd)
	if err != nil {
		t.Fatalf("GetProductsTotalsByMerchant (empty window): %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("expected no rows for a window with no sale, got %+v", none)
	}
}
