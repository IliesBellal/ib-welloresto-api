//go:build postgres_integration

package analytics

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// TestOptions_Postgres seeds a known, hand-computed dataset and checks every
// GetOptions* query against it — PROMPT 17's mandatory accuracy test. Covers:
//   - quantity_sold (CA-driving instance count, oic.quantity * oi.quantity)
//     DIVERGING from the adoption numerator (oi.quantity alone) whenever a
//     line selects the same option more than once ("Extra Bacon x2" on a
//     single product unit) — see options.go's doc comment;
//   - adoption_rate against the product's own total unit sales, including a
//     product with sales outside any option/removal (denominator > numerator,
//     never a spurious >100%);
//   - NULL, never 0, for a cost-known-empty option (NO_RECIPE/
//     INCOMPLETE_RECIPE) AND for every "removed" row (no cost snapshot exists
//     for `without` at all — structurally not applicable, not merely
//     unresolved);
//   - revenue_ttc_cents is a real 0 (never nil) for a free option and for
//     every removed-ingredient row;
//   - option_types filtering actually restricts the result set (paid-only,
//     removed-only, combined);
//   - server-side pagination/sort, margin sort NULLS LAST;
//   - basket-impact shares (GetOptionsBasketShares/GetOptionsBasketSharesRemoved)
//     against hand-counted distinct orders;
//   - a zero-order period returns zero, not an error.
func TestOptions_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	var productP1, productP2 int64
	var attrID string
	const merchantTZ = "Europe/Paris"

	cleanup := func() {
		if merchantIntID != 0 {
			mid := itoa(merchantIntID)
			_, _ = db.ExecContext(ctx, `DELETE FROM without WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM order_item_configuration WHERE order_item_id IN (SELECT order_item_id FROM orderitems WHERE merchant_id = $1)`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM orderitems WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
		if productP1 != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE product_id = $1`, productP1)
		}
		if productP2 != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE product_id = $1`, productP2)
		}
		if attrID != "" {
			_, _ = db.ExecContext(ctx, `DELETE FROM configurable_attribute_options WHERE configurable_attribute_id = $1`, attrID)
			_, _ = db.ExecContext(ctx, `DELETE FROM configurable_attributes WHERE id = $1`, attrID)
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM components WHERE name = 'ITest Options Olives'`)
	}
	t.Cleanup(cleanup)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Options Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-options', 'https://example.com', '0600000000', 'tok-options', $1)
		RETURNING id`, merchantTZ).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := itoa(merchantIntID)

	const categID = "itest-options-categ"
	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, price, category)
		VALUES ($1, 'ITest Options Burger', 1000, $2) RETURNING product_id`, merchantID, categID).Scan(&productP1); err != nil {
		t.Fatalf("seed product P1: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, price, category)
		VALUES ($1, 'ITest Options Pizza', 1200, $2) RETURNING product_id`, merchantID, categID).Scan(&productP2); err != nil {
		t.Fatalf("seed product P2: %v", err)
	}

	attrID = "itest-attr-options"
	if _, err := db.ExecContext(ctx, `
		INSERT INTO configurable_attributes (id, product_id, merchant_id, name, title, max_options)
		VALUES ($1, $2, $3, 'itest-toppings', 'ITest Toppings', 5)`,
		attrID, productP1, merchantID); err != nil {
		t.Fatalf("seed configurable_attribute: %v", err)
	}

	var optionBacon, optionSansSel int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO configurable_attribute_options (configurable_attribute_id, title, extra_price)
		VALUES ($1, 'ITest Extra Bacon', 400) RETURNING id`, attrID).Scan(&optionBacon); err != nil {
		t.Fatalf("seed option Extra Bacon: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO configurable_attribute_options (configurable_attribute_id, title, extra_price)
		VALUES ($1, 'ITest Sans Sel', 0) RETURNING id`, attrID).Scan(&optionSansSel); err != nil {
		t.Fatalf("seed option Sans Sel: %v", err)
	}

	var componentOlives int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO components (merchant_id, name, unit_of_measure)
		VALUES ($1, 'ITest Options Olives', 1) RETURNING component_id`, merchantID).Scan(&componentOlives); err != nil {
		t.Fatalf("seed component Olives: %v", err)
	}

	loc, err := time.LoadLocation(merchantTZ)
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	day := time.Date(2026, 1, 15, 12, 0, 0, 0, loc)

	seedItem := func(orderNum int, orderPrice int64, productID int64, quantity int, itemPrice int64) int64 {
		orderID := seedOrder(t, ctx, db, merchantID, orderNum, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", orderPrice, day)
		var itemID int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, price)
			VALUES ($1, $2, $3, $4, $5) RETURNING order_item_id`,
			orderID, productID, merchantID, quantity, itemPrice).Scan(&itemID); err != nil {
			t.Fatalf("seed orderitem for order %d: %v", orderNum, err)
		}
		return itemID
	}
	seedConfig := func(itemID, optionID int64, oicQuantity int, costPriceUnit *int, costPriceReason *string) {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO order_item_configuration (order_item_id, configuration_attribute_id, configuration_attribute_option_id, quantity, cost_price_unit, cost_price_reason)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			itemID, attrID, optionID, oicQuantity, costPriceUnit, costPriceReason); err != nil {
			t.Fatalf("seed order_item_configuration: %v", err)
		}
	}

	// ---- Extra Bacon (paid), 4 order units, one double-picked ----
	// L1: oic.qty=1, oi.qty=1, cost known (150) -> instances 1, adoption units 1.
	l1 := seedItem(101, 1400, productP1, 1, 1400)
	seedConfig(l1, optionBacon, 1, intPtr(150), nil)
	// L2: oic.qty=2 (double bacon on ONE product unit), cost known (200) ->
	// instances 2, adoption units 1 — this is what makes quantity_sold (5
	// total) diverge from the adoption numerator (4 total) for this option.
	l2 := seedItem(102, 1400, productP1, 1, 1400)
	seedConfig(l2, optionBacon, 2, intPtr(200), nil)
	// L3: NO_RECIPE -> instances 1, adoption units 1, no cost.
	l3 := seedItem(103, 1400, productP1, 1, 1400)
	seedConfig(l3, optionBacon, 1, nil, strPtr("NO_RECIPE"))
	// L4: INCOMPLETE_RECIPE -> instances 1, adoption units 1, no cost.
	l4 := seedItem(104, 1400, productP1, 1, 1400)
	seedConfig(l4, optionBacon, 1, nil, strPtr("INCOMPLETE_RECIPE"))

	// ---- Sans Sel (free), 3 order units, always NO_RECIPE ----
	for i, num := range []int{105, 106, 107} {
		itemID := seedItem(num, 1000, productP1, 1, 1000)
		seedConfig(itemID, optionSansSel, 1, nil, strPtr("NO_RECIPE"))
		_ = i
	}

	// ---- Plain P1 sales, no option at all: inflates the adoption
	// denominator without touching the numerator. ----
	seedItem(108, 1000, productP1, 1, 1000)
	seedItem(109, 1000, productP1, 1, 1000)

	// ---- Removed ingredient on P2 (Olives), one order/line, quantity 2 ----
	l10 := seedItem(110, 1000, productP2, 2, 1200)
	orderID10, err := lastOrderIDForItem(ctx, db, l10)
	if err != nil {
		t.Fatalf("lookup order_id for without seed: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO without (order_id, order_item_id, component_id, product_id, merchant_id)
		VALUES ($1, $2, $3, $4, $5)`,
		orderID10, l10, componentOlives, productP2, merchantID); err != nil {
		t.Fatalf("seed without row: %v", err)
	}

	repo := NewRepository(db)
	start, end := time.Date(2026, 1, 15, 0, 0, 0, 0, loc).UTC(), time.Date(2026, 1, 16, 0, 0, 0, 0, loc).UTC()
	allTypes := []string{OptionTypePaid, OptionTypeFree, OptionTypeRemoved}

	// ---- Scope totals, all types ----
	totals, err := repo.GetOptionsScopeTotals(ctx, []string{merchantID}, allTypes, start, end)
	if err != nil {
		t.Fatalf("GetOptionsScopeTotals: %v", err)
	}
	// quantity_sold: bacon 5 (1+2+1+1) + sansSel 3 + removed 2 = 10.
	if totals.QuantitySold != 10 {
		t.Fatalf("expected quantity_sold=10, got %d", totals.QuantitySold)
	}
	// revenue: bacon 5*400=2000, sansSel 0, removed 0 -> 2000.
	if totals.RevenueTTCCents != 2000 {
		t.Fatalf("expected revenue_ttc_cents=2000, got %d", totals.RevenueTTCCents)
	}
	// cost known: L1(150*1)+L2(200*1)=350, over cost-known adoption units 2,
	// cost-known revenue (1*400)+(2*400)=1200.
	if totals.CostPriceCents != 350 || totals.CostKnownRevenueTTCCents != 1200 {
		t.Fatalf("expected cost=350 revenue=1200, got cost=%d revenue=%d", totals.CostPriceCents, totals.CostKnownRevenueTTCCents)
	}
	// no_recipe: L3(1) + sansSel(3) = 4. incomplete: L4(1).
	if totals.NoRecipeQuantity != 4 || totals.IncompleteRecipeQuantity != 1 {
		t.Fatalf("expected no_recipe=4 incomplete=1, got %+v", totals)
	}

	// ---- optionTypes filter actually restricts the result ----
	paidOnly, err := repo.GetOptionsScopeTotals(ctx, []string{merchantID}, []string{OptionTypePaid}, start, end)
	if err != nil {
		t.Fatalf("GetOptionsScopeTotals (paid only): %v", err)
	}
	if paidOnly.QuantitySold != 5 || paidOnly.RevenueTTCCents != 2000 {
		t.Fatalf("expected paid-only quantity=5 revenue=2000, got %+v", paidOnly)
	}
	removedOnly, err := repo.GetOptionsScopeTotals(ctx, []string{merchantID}, []string{OptionTypeRemoved}, start, end)
	if err != nil {
		t.Fatalf("GetOptionsScopeTotals (removed only): %v", err)
	}
	if removedOnly.QuantitySold != 2 || removedOnly.RevenueTTCCents != 0 {
		t.Fatalf("expected removed-only quantity=2 revenue=0, got %+v", removedOnly)
	}

	// ---- Page: sort by quantity desc, one row per (entity, product) ----
	page, totalRows, err := repo.GetOptionsPage(ctx, []string{merchantID}, allTypes, OptionsSortQuantity, "desc", 1, 10, start, end)
	if err != nil {
		t.Fatalf("GetOptionsPage: %v", err)
	}
	if totalRows != 3 {
		t.Fatalf("expected 3 distinct rows (bacon, sans sel, removed olives), got %d", totalRows)
	}

	var baconRow, sansSelRow, removedRow *OptionAggRow
	for i := range page {
		switch page[i].EntityID {
		case itoa(optionBacon):
			baconRow = &page[i]
		case itoa(optionSansSel):
			sansSelRow = &page[i]
		case itoa(componentOlives):
			removedRow = &page[i]
		}
	}
	if baconRow == nil || sansSelRow == nil || removedRow == nil {
		t.Fatalf("expected all 3 rows present, got %+v", page)
	}

	// ---- The core divergence: quantity_sold (CA instances) != adoption
	// numerator (product units) for Extra Bacon, because of the double-pick line. ----
	if baconRow.QuantitySold != 5 {
		t.Fatalf("expected bacon quantity_sold=5, got %d", baconRow.QuantitySold)
	}
	if baconRow.AdoptionUnits != 4 {
		t.Fatalf("expected bacon adoption units=4 (4 distinct product units, one double-picked), got %d", baconRow.AdoptionUnits)
	}
	if baconRow.OptionType != OptionTypePaid {
		t.Fatalf("expected bacon option_type=paid, got %q", baconRow.OptionType)
	}
	if !baconRow.CostPriceCents.Valid || baconRow.CostPriceCents.Int64 != 350 {
		t.Fatalf("expected bacon cost_price_cents=350, got %+v", baconRow.CostPriceCents)
	}
	if baconRow.NoRecipeQuantity != 1 || baconRow.IncompleteRecipeQuantity != 1 {
		t.Fatalf("expected bacon no_recipe=1 incomplete=1, got %+v", baconRow)
	}

	// ---- Free option: real 0 revenue, never nil; cost NULL (never 0) ----
	if sansSelRow.OptionType != OptionTypeFree {
		t.Fatalf("expected sans-sel option_type=free, got %q", sansSelRow.OptionType)
	}
	if sansSelRow.RevenueTTCCents != 0 {
		t.Fatalf("expected sans-sel revenue=0 (real zero, free option), got %d", sansSelRow.RevenueTTCCents)
	}
	if sansSelRow.QuantitySold != 3 || sansSelRow.AdoptionUnits != 3 {
		t.Fatalf("expected sans-sel quantity=3 adoption=3, got %+v", sansSelRow)
	}
	if sansSelRow.CostPriceCents.Valid {
		t.Fatalf("expected sans-sel cost to be NULL (all NO_RECIPE), got %d", sansSelRow.CostPriceCents.Int64)
	}
	if sansSelRow.NoRecipeQuantity != 3 {
		t.Fatalf("expected sans-sel no_recipe_quantity=3, got %d", sansSelRow.NoRecipeQuantity)
	}

	// ---- Removed ingredient: real 0 revenue, cost structurally NULL, no
	// NO_RECIPE/INCOMPLETE_RECIPE bucketing (not applicable, not unresolved) ----
	if removedRow.OptionType != OptionTypeRemoved {
		t.Fatalf("expected removed option_type=removed, got %q", removedRow.OptionType)
	}
	if removedRow.RevenueTTCCents != 0 {
		t.Fatalf("expected removed revenue=0, got %d", removedRow.RevenueTTCCents)
	}
	if removedRow.QuantitySold != 2 || removedRow.AdoptionUnits != 2 {
		t.Fatalf("expected removed quantity=2 adoption=2 (orderitem quantity), got %+v", removedRow)
	}
	if removedRow.CostPriceCents.Valid {
		t.Fatalf("expected removed cost to be NULL, got %d", removedRow.CostPriceCents.Int64)
	}
	if removedRow.NoRecipeQuantity != 0 || removedRow.IncompleteRecipeQuantity != 0 {
		t.Fatalf("expected removed no_recipe=0 incomplete=0 (not applicable, not unresolved), got %+v", removedRow)
	}
	if removedRow.ProductID != itoa(productP2) {
		t.Fatalf("expected removed row's product to be P2, got %s", removedRow.ProductID)
	}

	// ---- Adoption denominator: P1's own total units sold in scope ----
	productTotals, err := repo.GetOptionsProductTotals(ctx, []string{merchantID}, []string{itoa(productP1), itoa(productP2)}, start, end)
	if err != nil {
		t.Fatalf("GetOptionsProductTotals: %v", err)
	}
	// P1: bacon lines (4 units) + sans sel lines (3 units) + 2 plain lines = 9.
	if productTotals[itoa(productP1)] != 9 {
		t.Fatalf("expected P1 total units=9, got %d", productTotals[itoa(productP1)])
	}
	// P2: only the removed-ingredient line, quantity 2.
	if productTotals[itoa(productP2)] != 2 {
		t.Fatalf("expected P2 total units=2, got %d", productTotals[itoa(productP2)])
	}
	// Adoption rates computed the same way service.go does: bacon 4/9,
	// sans sel 3/9 — neither is 100%, both below the (non-existent here)
	// ceiling, and the denominator is never 0 for a product that has any
	// option/removal activity at all.
	baconAdoption := float64(baconRow.AdoptionUnits) / float64(productTotals[itoa(productP1)])
	if baconAdoption <= 0 || baconAdoption >= 1 {
		t.Fatalf("expected bacon adoption strictly between 0 and 1, got %f", baconAdoption)
	}

	// ---- Sort by margin, NULLS LAST: bacon (margin known) first ----
	marginPage, _, err := repo.GetOptionsPage(ctx, []string{merchantID}, allTypes, OptionsSortMargin, "desc", 1, 10, start, end)
	if err != nil {
		t.Fatalf("GetOptionsPage (sort by margin): %v", err)
	}
	if marginPage[0].EntityID != itoa(optionBacon) {
		t.Fatalf("expected bacon (the only row with a known margin) first when sorting by margin desc, got %+v", marginPage[0])
	}

	// ---- Basket impact shares: bacon appears in 4 orders (1400 each),
	// removed-olives in 1 order (1000). ----
	baconShares, err := repo.GetOptionsBasketShares(ctx, []string{merchantID}, []string{itoa(optionBacon)}, start, end)
	if err != nil {
		t.Fatalf("GetOptionsBasketShares: %v", err)
	}
	if baconShares[itoa(optionBacon)].OrderCount != 4 || baconShares[itoa(optionBacon)].OrderPriceSum != 5600 {
		t.Fatalf("expected bacon share count=4 sum=5600, got %+v", baconShares[itoa(optionBacon)])
	}
	removedShares, err := repo.GetOptionsBasketSharesRemoved(ctx, []string{merchantID}, []string{itoa(componentOlives)}, start, end)
	if err != nil {
		t.Fatalf("GetOptionsBasketSharesRemoved: %v", err)
	}
	if removedShares[itoa(componentOlives)].OrderCount != 1 || removedShares[itoa(componentOlives)].OrderPriceSum != 1000 {
		t.Fatalf("expected removed share count=1 sum=1000, got %+v", removedShares[itoa(componentOlives)])
	}

	// ---- optionTypes filter on GetOptionsPage: removed-only excludes bacon/sans sel ----
	removedPage, removedTotal, err := repo.GetOptionsPage(ctx, []string{merchantID}, []string{OptionTypeRemoved}, OptionsSortQuantity, "desc", 1, 10, start, end)
	if err != nil {
		t.Fatalf("GetOptionsPage (removed only): %v", err)
	}
	if removedTotal != 1 || len(removedPage) != 1 || removedPage[0].EntityID != itoa(componentOlives) {
		t.Fatalf("expected exactly 1 removed-ingredient row, got total=%d rows=%+v", removedTotal, removedPage)
	}

	// ---- Zero-order period: must return zero, not an error ----
	emptyStart, emptyEnd := time.Date(2020, 1, 1, 0, 0, 0, 0, loc).UTC(), time.Date(2020, 2, 1, 0, 0, 0, 0, loc).UTC()
	emptyTotals, err := repo.GetOptionsScopeTotals(ctx, []string{merchantID}, allTypes, emptyStart, emptyEnd)
	if err != nil {
		t.Fatalf("GetOptionsScopeTotals (empty period): %v", err)
	}
	if emptyTotals.QuantitySold != 0 || emptyTotals.RevenueTTCCents != 0 {
		t.Fatalf("expected zero totals for a period with no orders, got %+v", emptyTotals)
	}
	emptyPage, emptyTotal, err := repo.GetOptionsPage(ctx, []string{merchantID}, allTypes, OptionsSortQuantity, "desc", 1, 10, emptyStart, emptyEnd)
	if err != nil {
		t.Fatalf("GetOptionsPage (empty period): %v", err)
	}
	if len(emptyPage) != 0 || emptyTotal != 0 {
		t.Fatalf("expected no rows/zero total for a period with no orders, got rows=%d total=%d", len(emptyPage), emptyTotal)
	}

	// ---- An unrecognized option type is rejected at the filter level, not
	// silently ignored (service.go's job, but the repository must never be
	// handed a raw client value either — belt and suspenders). ----
	if _, ok := optionTypesFilter([]string{"bogus"}); ok {
		t.Fatalf("expected optionTypesFilter to reject an unknown value")
	}
}

func lastOrderIDForItem(ctx context.Context, db *sql.DB, orderItemID int64) (int64, error) {
	var orderID int64
	err := db.QueryRowContext(ctx, `SELECT order_id FROM orderitems WHERE order_item_id = $1`, orderItemID).Scan(&orderID)
	return orderID, err
}
