//go:build postgres_integration

package analytics

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

// TestProducts_MarketplaceCategory_Postgres covers the marketplace "ghost"
// categories (models.MarketplaceCategoryLabels): a product auto-created from
// an Uber Eats order carries category UBER_EATS, which has no productcateg
// row by design. The Produits tab must still label it "Uber Eats", offer it
// as a filter — and not offer DELIVEROO, which no product of the scope
// carries.
func TestProducts_MarketplaceCategory_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID, tvaID20, productUber int64
	const merchantTZ = "Europe/Paris"
	const categReal = "itest-mkt-c1"

	cleanup := func() {
		if merchantIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM orderitems WHERE merchant_id = $1`, itoa(merchantIntID))
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, itoa(merchantIntID))
			_, _ = db.ExecContext(ctx, `DELETE FROM productcateg WHERE merchant_id = $1`, itoa(merchantIntID))
			_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE merchant_id = $1`, itoa(merchantIntID))
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
		if tvaID20 != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM tva_categories WHERE tva_id = $1`, tvaID20)
		}
	}
	t.Cleanup(cleanup)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Marketplace Categ Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-mkt-categ', 'https://example.com', '0600000000', 'tok-mkt-categ', $1)
		RETURNING id`, merchantTZ).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := itoa(merchantIntID)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO tva_categories (delivery_type, tva_title, tva_desc, tva_rate)
		VALUES ('0', 'ITest Mkt TVA 20', 'itest', 20) RETURNING tva_id`).Scan(&tvaID20); err != nil {
		t.Fatalf("seed tva_categories: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO productcateg (merchant_id, merchant_categ_id, categ_name, categ_order)
		VALUES ($1, $2, 'ITest Plats', 1)`, merchantID, categReal); err != nil {
		t.Fatalf("seed productcateg: %v", err)
	}

	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, category, price, tva_in_id, tva_take_away_id, tva_delivery_id)
		VALUES ($1, 'ITest Uber Product', $2, 1200, $3, $3, $3) RETURNING product_id`,
		merchantID, models.MarketplaceCategoryUberEats, tvaID20).Scan(&productUber); err != nil {
		t.Fatalf("seed uber product: %v", err)
	}

	loc, err := time.LoadLocation(merchantTZ)
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	order := seedOrder(t, ctx, db, merchantID, 401, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 1200, time.Date(2026, 1, 15, 12, 0, 0, 0, loc))
	seedOrderItemWithCost(t, ctx, db, order, productUber, merchantID, 1, 1200, nil, strPtr("NO_RECIPE"))

	repo := NewRepository(db)
	start, end := time.Date(2026, 1, 15, 0, 0, 0, 0, loc).UTC(), time.Date(2026, 1, 16, 0, 0, 0, 0, loc).UTC()

	// ---- Categories: real ones first, then only the ghost ones in use ----
	categories, err := repo.GetProductCategories(ctx, []string{merchantID})
	if err != nil {
		t.Fatalf("GetProductCategories: %v", err)
	}
	if len(categories) != 2 {
		t.Fatalf("expected the real category + Uber Eats only, got %+v", categories)
	}
	if categories[0].CategoryID != categReal {
		t.Fatalf("expected the productcateg category first, got %+v", categories)
	}
	if categories[1].CategoryID != models.MarketplaceCategoryUberEats || categories[1].Name != "Uber Eats" {
		t.Fatalf("expected Uber Eats appended last, got %+v", categories[1])
	}

	// ---- Row label: no productcateg row, still "Uber Eats" ----
	rows, total, err := repo.GetProductsPage(ctx, []string{merchantID}, "", ProductsSortQuantity, "desc", 1, 10, start, end)
	if err != nil {
		t.Fatalf("GetProductsPage: %v", err)
	}
	if total != 1 || len(rows) != 1 {
		t.Fatalf("expected 1 product row, got total=%d rows=%d", total, len(rows))
	}
	if rows[0].CategoryID != models.MarketplaceCategoryUberEats || rows[0].CategoryName != "Uber Eats" {
		t.Fatalf("expected category UBER_EATS labelled \"Uber Eats\", got id=%q name=%q", rows[0].CategoryID, rows[0].CategoryName)
	}

	// ---- Filter on the ghost category ----
	filtered, filteredTotal, err := repo.GetProductsPage(ctx, []string{merchantID}, models.MarketplaceCategoryUberEats, ProductsSortQuantity, "desc", 1, 10, start, end)
	if err != nil {
		t.Fatalf("GetProductsPage (Uber Eats filter): %v", err)
	}
	if filteredTotal != 1 || filtered[0].ProductID != itoa(productUber) {
		t.Fatalf("expected the Uber product under the Uber Eats filter, got total=%d rows=%+v", filteredTotal, filtered)
	}
}
