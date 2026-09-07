//go:build postgres_integration

package analytics

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// TestGetUpsellTotalsWithOrdersTwoPeriods_Postgres and
// TestGetUpsellOrdersTotalTwoPeriods_Postgres are PROMPT 25 Phase 4's
// non-regression tests for the Vente additionnelle tab's fusion.
func TestGetUpsellTotalsWithOrdersTwoPeriods_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID, tvaID20, product int64
	const merchantTZ = "Europe/Paris"
	cleanup := func() {
		if merchantIntID != 0 {
			mid := itoa(merchantIntID)
			_, _ = db.ExecContext(ctx, `DELETE FROM orderitems WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
		if product != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE product_id = $1`, product)
		}
		if tvaID20 != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM tva_categories WHERE tva_id = $1`, tvaID20)
		}
	}
	t.Cleanup(cleanup)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest UpsellMultiPeriod Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-ump', 'https://example.com', '0600000000', 'tok-ump', $1)
		RETURNING id`, merchantTZ).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := itoa(merchantIntID)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO tva_categories (delivery_type, tva_title, tva_desc, tva_rate)
		VALUES ('0', 'ITest UpsellMultiPeriod TVA', 'itest', 20) RETURNING tva_id`).Scan(&tvaID20); err != nil {
		t.Fatalf("seed tva_categories: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, category, price, tva_in_id, tva_take_away_id, tva_delivery_id)
		VALUES ($1, 'ITest UpsellMultiPeriod Product', 'itest-cat', 500, $2, $2, $2) RETURNING product_id`,
		merchantID, tvaID20).Scan(&product); err != nil {
		t.Fatalf("seed product: %v", err)
	}

	loc, err := time.LoadLocation(merchantTZ)
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	current := PeriodWindow{Start: time.Date(2026, 6, 1, 0, 0, 0, 0, loc).UTC(), End: time.Date(2026, 6, 8, 0, 0, 0, 0, loc).UTC()}
	previous := PeriodWindow{Start: time.Date(2026, 5, 25, 0, 0, 0, 0, loc).UTC(), End: time.Date(2026, 6, 1, 0, 0, 0, 0, loc).UTC()}
	channels := Channels

	// Current: one order with an upsell line and a non-upsell line.
	orderCurrent := seedOrder(t, ctx, db, merchantID, 801, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 500, time.Date(2026, 6, 3, 12, 0, 0, 0, loc))
	seedUpsellOrderItem(t, ctx, db, orderCurrent, product, merchantID, 1, 500, true)
	seedUpsellOrderItem(t, ctx, db, orderCurrent, product, merchantID, 1, 500, false)

	// Previous: one order, two upsell lines (same order — OrdersWithUpsellCount must still be 1).
	orderPrevious := seedOrder(t, ctx, db, merchantID, 802, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 500, time.Date(2026, 5, 27, 12, 0, 0, 0, loc))
	seedUpsellOrderItem(t, ctx, db, orderPrevious, product, merchantID, 1, 500, true)
	seedUpsellOrderItem(t, ctx, db, orderPrevious, product, merchantID, 1, 500, true)

	repo := NewRepository(db)

	oldTotC, err := repo.GetUpsellTotals(ctx, []string{merchantID}, channels, current.Start, current.End)
	if err != nil {
		t.Fatalf("GetUpsellTotals (current): %v", err)
	}
	oldOrdC, err := repo.GetOrdersWithUpsellCount(ctx, []string{merchantID}, channels, current.Start, current.End)
	if err != nil {
		t.Fatalf("GetOrdersWithUpsellCount (current): %v", err)
	}
	oldTotP, err := repo.GetUpsellTotals(ctx, []string{merchantID}, channels, previous.Start, previous.End)
	if err != nil {
		t.Fatalf("GetUpsellTotals (previous): %v", err)
	}
	oldOrdP, err := repo.GetOrdersWithUpsellCount(ctx, []string{merchantID}, channels, previous.Start, previous.End)
	if err != nil {
		t.Fatalf("GetOrdersWithUpsellCount (previous): %v", err)
	}

	newC, newP, err := repo.GetUpsellTotalsWithOrdersTwoPeriods(ctx, []string{merchantID}, channels, current, previous)
	if err != nil {
		t.Fatalf("GetUpsellTotalsWithOrdersTwoPeriods: %v", err)
	}

	if newC.UpsellLines != oldTotC.UpsellLines || newC.UpsellRevenueHTCents != oldTotC.UpsellRevenueHTCents || newC.OrdersWithUpsellCount != oldOrdC {
		t.Fatalf("current mismatch — merged=%+v vs unmerged totals=%+v ordersWith=%d", newC, oldTotC, oldOrdC)
	}
	if newP.UpsellLines != oldTotP.UpsellLines || newP.UpsellRevenueHTCents != oldTotP.UpsellRevenueHTCents || newP.OrdersWithUpsellCount != oldOrdP {
		t.Fatalf("previous mismatch — merged=%+v vs unmerged totals=%+v ordersWith=%d", newP, oldTotP, oldOrdP)
	}
	if newC.UpsellLines != 1 || newC.OrdersWithUpsellCount != 1 {
		t.Fatalf("expected current UpsellLines=1/OrdersWithUpsellCount=1 (only the is_upsell=true line counts), got %+v", newC)
	}
	if newP.UpsellLines != 2 || newP.OrdersWithUpsellCount != 1 {
		t.Fatalf("expected previous UpsellLines=2/OrdersWithUpsellCount=1 (two upsell lines, same order), got %+v", newP)
	}
}

func TestGetUpsellOrdersTotalTwoPeriods_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	const merchantTZ = "Europe/Paris"
	cleanup := func() {
		if merchantIntID != 0 {
			mid := itoa(merchantIntID)
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
	}
	t.Cleanup(cleanup)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest UpsellOrdersMultiPeriod Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-uomp', 'https://example.com', '0600000000', 'tok-uomp', $1)
		RETURNING id`, merchantTZ).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := itoa(merchantIntID)

	loc, err := time.LoadLocation(merchantTZ)
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	current := PeriodWindow{Start: time.Date(2026, 6, 1, 0, 0, 0, 0, loc).UTC(), End: time.Date(2026, 6, 8, 0, 0, 0, 0, loc).UTC()}
	previous := PeriodWindow{Start: time.Date(2026, 5, 25, 0, 0, 0, 0, loc).UTC(), End: time.Date(2026, 6, 1, 0, 0, 0, 0, loc).UTC()}
	channels := Channels

	seedOrder(t, ctx, db, merchantID, 811, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 500, time.Date(2026, 6, 3, 12, 0, 0, 0, loc))
	seedOrder(t, ctx, db, merchantID, 812, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 300, time.Date(2026, 5, 27, 12, 0, 0, 0, loc))

	repo := NewRepository(db)

	oldC, err := repo.GetUpsellOrdersTotal(ctx, []string{merchantID}, channels, current.Start, current.End)
	if err != nil {
		t.Fatalf("GetUpsellOrdersTotal (current): %v", err)
	}
	oldP, err := repo.GetUpsellOrdersTotal(ctx, []string{merchantID}, channels, previous.Start, previous.End)
	if err != nil {
		t.Fatalf("GetUpsellOrdersTotal (previous): %v", err)
	}

	newC, newP, err := repo.GetUpsellOrdersTotalTwoPeriods(ctx, []string{merchantID}, channels, current, previous)
	if err != nil {
		t.Fatalf("GetUpsellOrdersTotalTwoPeriods: %v", err)
	}

	if newC != oldC || newP != oldP {
		t.Fatalf("mismatch — merged current=%d previous=%d vs unmerged current=%d previous=%d", newC, newP, oldC, oldP)
	}
	if newC != 1 || newP != 1 {
		t.Fatalf("expected current=1/previous=1, got current=%d previous=%d", newC, newP)
	}
}

// TestGetProductsScopeTotalsTwoPeriods_Postgres is PROMPT 25 Phase 4's
// non-regression test for the Produits tab's fusion.
func TestGetProductsScopeTotalsTwoPeriods_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID, tvaID20, product int64
	const merchantTZ = "Europe/Paris"
	cleanup := func() {
		if merchantIntID != 0 {
			mid := itoa(merchantIntID)
			_, _ = db.ExecContext(ctx, `DELETE FROM orderitems WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
		if product != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE product_id = $1`, product)
		}
		if tvaID20 != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM tva_categories WHERE tva_id = $1`, tvaID20)
		}
	}
	t.Cleanup(cleanup)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest ProductsMultiPeriod Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-prmp', 'https://example.com', '0600000000', 'tok-prmp', $1)
		RETURNING id`, merchantTZ).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := itoa(merchantIntID)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO tva_categories (delivery_type, tva_title, tva_desc, tva_rate)
		VALUES ('0', 'ITest ProductsMultiPeriod TVA', 'itest', 20) RETURNING tva_id`).Scan(&tvaID20); err != nil {
		t.Fatalf("seed tva_categories: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, category, price, tva_in_id, tva_take_away_id, tva_delivery_id)
		VALUES ($1, 'ITest ProductsMultiPeriod Product', 'itest-cat', 1000, $2, $2, $2) RETURNING product_id`,
		merchantID, tvaID20).Scan(&product); err != nil {
		t.Fatalf("seed product: %v", err)
	}

	loc, err := time.LoadLocation(merchantTZ)
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	current := PeriodWindow{Start: time.Date(2026, 6, 1, 0, 0, 0, 0, loc).UTC(), End: time.Date(2026, 6, 8, 0, 0, 0, 0, loc).UTC()}
	previous := PeriodWindow{Start: time.Date(2026, 5, 25, 0, 0, 0, 0, loc).UTC(), End: time.Date(2026, 6, 1, 0, 0, 0, 0, loc).UTC()}

	orderCurrent := seedOrder(t, ctx, db, merchantID, 901, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 1000, time.Date(2026, 6, 3, 12, 0, 0, 0, loc))
	seedOrderItemWithCost(t, ctx, db, orderCurrent, product, merchantID, 2, 1000, intPtr(400), nil)

	orderPrevious := seedOrder(t, ctx, db, merchantID, 902, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 1000, time.Date(2026, 5, 27, 12, 0, 0, 0, loc))
	seedOrderItemWithCost(t, ctx, db, orderPrevious, product, merchantID, 1, 1000, nil, strPtr("NO_RECIPE"))

	repo := NewRepository(db)

	oldC, err := repo.GetProductsScopeTotals(ctx, []string{merchantID}, "", current.Start, current.End)
	if err != nil {
		t.Fatalf("GetProductsScopeTotals (current): %v", err)
	}
	oldP, err := repo.GetProductsScopeTotals(ctx, []string{merchantID}, "", previous.Start, previous.End)
	if err != nil {
		t.Fatalf("GetProductsScopeTotals (previous): %v", err)
	}

	newC, newP, err := repo.GetProductsScopeTotalsTwoPeriods(ctx, []string{merchantID}, "", current, previous)
	if err != nil {
		t.Fatalf("GetProductsScopeTotalsTwoPeriods: %v", err)
	}

	if newC != oldC {
		t.Fatalf("current mismatch — merged=%+v unmerged=%+v", newC, oldC)
	}
	if newP != oldP {
		t.Fatalf("previous mismatch — merged=%+v unmerged=%+v", newP, oldP)
	}
	if newC.QuantitySold != 2 || newC.RevenueTTCCents != 2000 || newC.CostKnownRevenueTTCCents != 2000 || newC.CostPriceCents != 800 {
		t.Fatalf("expected current QuantitySold=2/RevenueTTC=2000/CostKnownRevenue=2000/CostPrice=800, got %+v", newC)
	}
	if newP.QuantitySold != 1 || newP.NoRecipeQuantity != 1 {
		t.Fatalf("expected previous QuantitySold=1/NoRecipeQuantity=1, got %+v", newP)
	}
}


// TestGetDiscountsScopeTotalsTwoPeriods_Postgres and
// TestGetDiscountsOrdersTotalsTwoPeriods_Postgres are PROMPT 25 Phase 4's
// non-regression tests for the Remises tab's fusion.
func TestGetDiscountsScopeTotalsTwoPeriods_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID, product int64
	var discountID int
	const merchantTZ = "Europe/Paris"
	cleanup := func() {
		if merchantIntID != 0 {
			mid := itoa(merchantIntID)
			_, _ = db.ExecContext(ctx, `DELETE FROM discount_redemptions WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM orderitems WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM discounts_products WHERE discount_id_new IN (SELECT discount_id_new FROM discounts WHERE merchant_id = $1)`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM discounts WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
		if product != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE product_id = $1`, product)
		}
	}
	t.Cleanup(cleanup)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest DiscMultiPeriod Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-dmp', 'https://example.com', '0600000000', 'tok-dmp', $1)
		RETURNING id`, merchantTZ).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := itoa(merchantIntID)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, price, category)
		VALUES ($1, 'ITest DiscMultiPeriod Product', 1000, 'itest-cat') RETURNING product_id`, merchantID).Scan(&product); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO discounts (discount_id, merchant_id, discount_name, discount_desc, discount_value, discount_unit, valid_from, discounted_quantity, is_cumulative, is_time_limited, available, enabled)
		VALUES ('itest-disc-'||gen_random_uuid()::text, $1, 'ITest Promo', 'itest desc', 10, 'PERCENTAGE', now(), 1, false, false, true, true)
		RETURNING discount_id_new`, merchantID).Scan(&discountID); err != nil {
		t.Fatalf("seed discount: %v", err)
	}

	loc, err := time.LoadLocation(merchantTZ)
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	current := PeriodWindow{Start: time.Date(2026, 6, 1, 0, 0, 0, 0, loc).UTC(), End: time.Date(2026, 6, 8, 0, 0, 0, 0, loc).UTC()}
	previous := PeriodWindow{Start: time.Date(2026, 5, 25, 0, 0, 0, 0, loc).UTC(), End: time.Date(2026, 6, 1, 0, 0, 0, 0, loc).UTC()}
	channels := Channels

	orderCurrent := seedOrder(t, ctx, db, merchantID, 1001, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 800, time.Date(2026, 6, 3, 12, 0, 0, 0, loc))
	orderItemCurrent := seedOrderItemReturningID(t, ctx, db, orderCurrent, product, merchantID, 1, 800)
	seedDiscountRedemption(t, ctx, db, discountID, orderCurrent, orderItemCurrent, merchantID, 200, true, time.Date(2026, 6, 3, 12, 0, 0, 0, loc))

	orderPrevious := seedOrder(t, ctx, db, merchantID, 1002, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 600, time.Date(2026, 5, 27, 12, 0, 0, 0, loc))
	orderItemPrevious := seedOrderItemReturningID(t, ctx, db, orderPrevious, product, merchantID, 1, 600)
	seedDiscountRedemption(t, ctx, db, discountID, orderPrevious, orderItemPrevious, merchantID, 150, false, time.Date(2026, 5, 27, 12, 0, 0, 0, loc))

	repo := NewRepository(db)

	oldC, err := repo.GetDiscountsScopeTotals(ctx, []string{merchantID}, channels, current.Start, current.End)
	if err != nil {
		t.Fatalf("GetDiscountsScopeTotals (current): %v", err)
	}
	oldP, err := repo.GetDiscountsScopeTotals(ctx, []string{merchantID}, channels, previous.Start, previous.End)
	if err != nil {
		t.Fatalf("GetDiscountsScopeTotals (previous): %v", err)
	}

	newC, newP, err := repo.GetDiscountsScopeTotalsTwoPeriods(ctx, []string{merchantID}, channels, current, previous)
	if err != nil {
		t.Fatalf("GetDiscountsScopeTotalsTwoPeriods: %v", err)
	}

	if newC != oldC {
		t.Fatalf("current mismatch — merged=%+v unmerged=%+v", newC, oldC)
	}
	if newP != oldP {
		t.Fatalf("previous mismatch — merged=%+v unmerged=%+v", newP, oldP)
	}
	if newC.TotalAmountCents != 200 || newC.ReconstructedRedemptionsCount != 1 {
		t.Fatalf("expected current TotalAmountCents=200/Reconstructed=1, got %+v", newC)
	}
	if newP.TotalAmountCents != 150 || newP.MeasuredRedemptionsCount != 1 {
		t.Fatalf("expected previous TotalAmountCents=150/Measured=1, got %+v", newP)
	}
}

func TestGetDiscountsOrdersTotalsTwoPeriods_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	const merchantTZ = "Europe/Paris"
	cleanup := func() {
		if merchantIntID != 0 {
			mid := itoa(merchantIntID)
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
	}
	t.Cleanup(cleanup)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest DiscOrdersMultiPeriod Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-domp', 'https://example.com', '0600000000', 'tok-domp', $1)
		RETURNING id`, merchantTZ).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := itoa(merchantIntID)

	loc, err := time.LoadLocation(merchantTZ)
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	current := PeriodWindow{Start: time.Date(2026, 6, 1, 0, 0, 0, 0, loc).UTC(), End: time.Date(2026, 6, 8, 0, 0, 0, 0, loc).UTC()}
	previous := PeriodWindow{Start: time.Date(2026, 5, 25, 0, 0, 0, 0, loc).UTC(), End: time.Date(2026, 6, 1, 0, 0, 0, 0, loc).UTC()}
	channels := Channels

	seedOrder(t, ctx, db, merchantID, 1011, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 800, time.Date(2026, 6, 3, 12, 0, 0, 0, loc))
	seedOrder(t, ctx, db, merchantID, 1012, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 600, time.Date(2026, 5, 27, 12, 0, 0, 0, loc))

	repo := NewRepository(db)

	oldC, err := repo.GetDiscountsOrdersTotals(ctx, []string{merchantID}, channels, current.Start, current.End)
	if err != nil {
		t.Fatalf("GetDiscountsOrdersTotals (current): %v", err)
	}
	oldP, err := repo.GetDiscountsOrdersTotals(ctx, []string{merchantID}, channels, previous.Start, previous.End)
	if err != nil {
		t.Fatalf("GetDiscountsOrdersTotals (previous): %v", err)
	}

	newC, newP, err := repo.GetDiscountsOrdersTotalsTwoPeriods(ctx, []string{merchantID}, channels, current, previous)
	if err != nil {
		t.Fatalf("GetDiscountsOrdersTotalsTwoPeriods: %v", err)
	}

	if newC != oldC || newP != oldP {
		t.Fatalf("mismatch — merged current=%+v previous=%+v vs unmerged current=%+v previous=%+v", newC, newP, oldC, oldP)
	}
	if newC.TotalOrdersCount != 1 || newC.ReferenceRevenueTTCCents != 800 {
		t.Fatalf("expected current TotalOrdersCount=1/ReferenceRevenue=800, got %+v", newC)
	}
	if newP.TotalOrdersCount != 1 || newP.ReferenceRevenueTTCCents != 600 {
		t.Fatalf("expected previous TotalOrdersCount=1/ReferenceRevenue=600, got %+v", newP)
	}
}

func seedOrderItemReturningID(t *testing.T, ctx context.Context, db *sql.DB, orderID, productID int64, merchantID string, quantity int, priceCents int64) int64 {
	t.Helper()
	var orderItemID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, base_price, price)
		VALUES ($1, $2, $3, $4, $5, $5)
		RETURNING order_item_id`, orderID, productID, merchantID, quantity, priceCents).Scan(&orderItemID); err != nil {
		t.Fatalf("seed orderitem (returning id) for order %d: %v", orderID, err)
	}
	return orderItemID
}

func seedDiscountRedemption(t *testing.T, ctx context.Context, db *sql.DB, discountID int, orderID, orderItemID int64, merchantID string, amountCents int64, isReconstructed bool, createdAt time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO discount_redemptions (scope, discount_id, order_id, order_item_id, merchant_id, amount_applied_cents, is_reconstructed, created_at)
		VALUES ('PRODUCT_LINE', $1, $2, $3, $4, $5, $6, $7)`,
		discountID, orderID, orderItemID, merchantID, amountCents, isReconstructed, createdAt.UTC(),
	); err != nil {
		t.Fatalf("seed discount_redemptions (order %d): %v", orderID, err)
	}
}
