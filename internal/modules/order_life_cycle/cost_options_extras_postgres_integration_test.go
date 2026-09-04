//go:build postgres_integration

package order_life_cycle

import (
	"context"
	"database/sql"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/customers"
)

// Vérification réelle du gel du coût de revient d'une option de configuration
// (order_item_configuration) et d'un supplément (extra) — PROMPT 11, §3.
// Même règle que B2 (lot 1) pour orderitems.cost_price_unit : le coût
// snapshoté à l'écriture ne bouge plus quand components.purchase_price change
// ensuite ; NULL + une raison distincte, jamais 0, quand le coût n'est pas
// calculable.
func TestOrderLifeCycleRepository_OptionsExtrasCostFreeze_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantID string
	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM extra WHERE merchant_id = $1`,
			`DELETE FROM order_item_configuration WHERE order_item_id IN (SELECT order_item_id FROM orderitems WHERE merchant_id = $1)`,
			`DELETE FROM orderitems WHERE merchant_id = $1`,
			`DELETE FROM orders WHERE merchant_id = $1`,
			`DELETE FROM configurable_attribute_options WHERE configurable_attribute_id IN (SELECT id FROM configurable_attributes WHERE merchant_id = $1)`,
			`DELETE FROM configurable_attributes WHERE merchant_id = $1`,
			`DELETE FROM components WHERE merchant_id = $1`,
			`DELETE FROM products WHERE merchant_Id = $1`,
			`DELETE FROM merchant_parameters WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-olc-optext' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	}
	t.Cleanup(func() { cleanupFor(merchantID) })

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest OLC OptExt', 'a', '1', 's', '75001', 'Paris', 'siret-olc-optext', 'https://x', '06', 'mtok-olc-optext', 'UTC')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)

	custoRepo := customers.NewCustomerRepository(db)
	repo := NewOrdersLifeCycleRepository(db, custoRepo)

	// --- Composant priced (500 cents / 10 unités = 50 cents/unité) -----------
	var pricedComponentID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO components (merchant_id, name, unit_of_measure, purchase_price, purchase_price_quantity)
		VALUES ($1, 'itest-optext-priced', 1, 500, 10) RETURNING component_id`, merchantID).Scan(&pricedComponentID); err != nil {
		t.Fatalf("seed priced component: %v", err)
	}
	// --- Composant sans prix d'achat ------------------------------------------
	var unpricedComponentID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO components (merchant_id, name, unit_of_measure) VALUES ($1, 'itest-optext-unpriced', 1) RETURNING component_id`,
		merchantID).Scan(&unpricedComponentID); err != nil {
		t.Fatalf("seed unpriced component: %v", err)
	}

	var productID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_Id, name, price, category, status)
		VALUES ($1, 'itest-optext-prod', 1000, 'c', '1') RETURNING product_id`, merchantID).Scan(&productID); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	productIDStr := strconv.FormatInt(productID, 10)

	attrID := "itest-optext-attr"
	if _, err := db.ExecContext(ctx, `
		INSERT INTO configurable_attributes (id, product_id, merchant_id, name, title, max_options)
		VALUES ($1, $2, $3, 'taille', 'Taille', 1)`, attrID, productID, merchantID); err != nil {
		t.Fatalf("seed configurable_attribute: %v", err)
	}

	// Option A : liée au composant priced, 2 unités par sélection -> 2*50=100/sélection.
	var linkedOptionID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO configurable_attribute_options (configurable_attribute_id, title, component_id, quantity, unit_of_measure)
		VALUES ($1, 'Grande', $2, 2, 1) RETURNING id`, attrID, pricedComponentID).Scan(&linkedOptionID); err != nil {
		t.Fatalf("seed linked option: %v", err)
	}
	// Option B : liée au composant SANS prix -> INCOMPLETE_RECIPE.
	var unpricedOptionID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO configurable_attribute_options (configurable_attribute_id, title, component_id, quantity, unit_of_measure)
		VALUES ($1, 'Sans-prix', $2, 1, 1) RETURNING id`, attrID, unpricedComponentID).Scan(&unpricedOptionID); err != nil {
		t.Fatalf("seed unpriced option: %v", err)
	}
	// Option C : aucun composant lié (ex. "sans glaçons") -> NO_RECIPE, jamais 0.
	var unlinkedOptionID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO configurable_attribute_options (configurable_attribute_id, title, quantity, unit_of_measure)
		VALUES ($1, 'Sans glaçons', 0, 1) RETURNING id`, attrID).Scan(&unlinkedOptionID); err != nil {
		t.Fatalf("seed unlinked option: %v", err)
	}

	var orderID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand, brand_status, price, TVA, HT, created_by)
		VALUES ($1, 1, 'WELLO_RESTO', 'PENDING', 1000, 0, 1000, 'itest')
		RETURNING order_id`, merchantID).Scan(&orderID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	orderIDStr := strconv.FormatInt(orderID, 10)

	item := &models.OrderItemInsert{
		OrderID: orderIDStr, ProductID: productIDStr, MerchantID: merchantID,
		Quantity: 1, Price: 1000, BasePrice: 1000, CreatedBy: "itest",
	}
	orderItemID, err := repo.InsertOrderItem(ctx, item)
	if err != nil {
		t.Fatalf("InsertOrderItem: %v", err)
	}
	usedItems := []models.UsedItem{{OrderItemID: strconv.FormatInt(orderItemID, 10), Quantity: 1}}

	req := &models.RequestObject{
		MerchantID: merchantID,
		Order: models.OrderRequest{
			OrderID: &orderIDStr,
			Products: []models.OrderProductPayload{
				{
					ProductID: productIDStr,
					Quantity:  1,
					Extra: []*models.OrderExtraPayload{
						{ComponentID: strconv.FormatInt(pricedComponentID, 10), Price: 150},
						{ComponentID: strconv.FormatInt(unpricedComponentID, 10), Price: 150},
					},
					Config: &models.ProductConfiguration{Attributes: []models.ConfigurationAttribute{
						{ID: attrID, Options: []models.ConfigurationOption{
							{ID: strconv.FormatInt(linkedOptionID, 10), Quantity: 3},   // 3 * 100 = 300
							{ID: strconv.FormatInt(unpricedOptionID, 10), Quantity: 1}, // INCOMPLETE_RECIPE
							{ID: strconv.FormatInt(unlinkedOptionID, 10), Quantity: 1}, // NO_RECIPE
						}},
					}},
				},
			},
		},
	}
	// Mirrors the real CreateOrder flow: insertOrderItems computes optionCosts
	// once and hands it to insertExtrasWithoutsConfigs (see repository.go) —
	// reproduced here since this test calls InsertOrderItem directly instead
	// of going through the full insertOrderItems.
	_, optionCosts := repo.resolveOrderItemCostsForOrder(ctx, merchantID, req.Order.Products)
	if err := repo.insertExtrasWithoutsConfigs(ctx, req, usedItems, optionCosts); err != nil {
		t.Fatalf("insertExtrasWithoutsConfigs: %v", err)
	}

	// --- extra : composant priced -> coût réel figé (100 cents = 1 * 50 cents/unité, quantity=1 par défaut) ---
	var extraCostPriced sql.NullInt64
	var extraReasonPriced sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT cost_price_unit, cost_price_reason FROM extra WHERE order_item_id = $1 AND component_id = $2`,
		orderItemID, pricedComponentID).Scan(&extraCostPriced, &extraReasonPriced); err != nil {
		t.Fatalf("read back extra (priced): %v", err)
	}
	if !extraCostPriced.Valid || extraCostPriced.Int64 != 50 {
		t.Fatalf("expected extra cost_price_unit=50 (priced component), got %v", extraCostPriced)
	}
	if extraReasonPriced.Valid {
		t.Fatalf("expected extra cost_price_reason=NULL for a resolvable cost, got %q", extraReasonPriced.String)
	}

	// --- extra : composant sans prix -> NULL + INCOMPLETE_RECIPE, jamais 0 ---
	var extraCostUnpriced sql.NullInt64
	var extraReasonUnpriced sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT cost_price_unit, cost_price_reason FROM extra WHERE order_item_id = $1 AND component_id = $2`,
		orderItemID, unpricedComponentID).Scan(&extraCostUnpriced, &extraReasonUnpriced); err != nil {
		t.Fatalf("read back extra (unpriced): %v", err)
	}
	if extraCostUnpriced.Valid {
		t.Fatalf("expected extra cost_price_unit=NULL for an unpriced component, got %d (never 0)", extraCostUnpriced.Int64)
	}
	if !extraReasonUnpriced.Valid || extraReasonUnpriced.String != "INCOMPLETE_RECIPE" {
		t.Fatalf("expected extra cost_price_reason=INCOMPLETE_RECIPE, got %v", extraReasonUnpriced)
	}

	// --- order_item_configuration : option liée -> coût réel figé (300 = 3*100) ---
	var configCostLinked sql.NullInt64
	var configReasonLinked sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT cost_price_unit, cost_price_reason FROM order_item_configuration WHERE order_item_id = $1 AND configuration_attribute_option_id = $2`,
		orderItemID, linkedOptionID).Scan(&configCostLinked, &configReasonLinked); err != nil {
		t.Fatalf("read back order_item_configuration (linked): %v", err)
	}
	if !configCostLinked.Valid || configCostLinked.Int64 != 300 {
		t.Fatalf("expected order_item_configuration cost_price_unit=300, got %v", configCostLinked)
	}
	if configReasonLinked.Valid {
		t.Fatalf("expected order_item_configuration cost_price_reason=NULL for a resolvable cost, got %q", configReasonLinked.String)
	}

	// --- order_item_configuration : option liée à un composant sans prix -> INCOMPLETE_RECIPE ---
	var configCostUnpriced sql.NullInt64
	var configReasonUnpriced sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT cost_price_unit, cost_price_reason FROM order_item_configuration WHERE order_item_id = $1 AND configuration_attribute_option_id = $2`,
		orderItemID, unpricedOptionID).Scan(&configCostUnpriced, &configReasonUnpriced); err != nil {
		t.Fatalf("read back order_item_configuration (unpriced): %v", err)
	}
	if configCostUnpriced.Valid {
		t.Fatalf("expected order_item_configuration cost_price_unit=NULL for an unpriced-linked option, got %d (never 0)", configCostUnpriced.Int64)
	}
	if !configReasonUnpriced.Valid || configReasonUnpriced.String != "INCOMPLETE_RECIPE" {
		t.Fatalf("expected order_item_configuration cost_price_reason=INCOMPLETE_RECIPE, got %v", configReasonUnpriced)
	}

	// --- order_item_configuration : option sans composant lié -> NO_RECIPE, jamais 0 ---
	var configCostUnlinked sql.NullInt64
	var configReasonUnlinked sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT cost_price_unit, cost_price_reason FROM order_item_configuration WHERE order_item_id = $1 AND configuration_attribute_option_id = $2`,
		orderItemID, unlinkedOptionID).Scan(&configCostUnlinked, &configReasonUnlinked); err != nil {
		t.Fatalf("read back order_item_configuration (unlinked): %v", err)
	}
	if configCostUnlinked.Valid {
		t.Fatalf("expected order_item_configuration cost_price_unit=NULL for an option with no linked component, got %d (never 0)", configCostUnlinked.Int64)
	}
	if !configReasonUnlinked.Valid || configReasonUnlinked.String != "NO_RECIPE" {
		t.Fatalf("expected order_item_configuration cost_price_reason=NO_RECIPE, got %v", configReasonUnlinked)
	}

	// --- Le cœur du test : on change purchase_price après coup, rien ne bouge ---
	if _, err := db.ExecContext(ctx, `UPDATE components SET purchase_price = 999999 WHERE component_id = $1`, pricedComponentID); err != nil {
		t.Fatalf("update purchase_price: %v", err)
	}

	var extraCostAfter, configCostAfter sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT cost_price_unit FROM extra WHERE order_item_id = $1 AND component_id = $2`,
		orderItemID, pricedComponentID).Scan(&extraCostAfter); err != nil {
		t.Fatalf("re-read extra cost_price_unit: %v", err)
	}
	if extraCostAfter.Int64 != 50 {
		t.Fatalf("extra cost_price_unit changed after a later purchase_price update — got %v, want 50 (frozen)", extraCostAfter)
	}
	if err := db.QueryRowContext(ctx, `SELECT cost_price_unit FROM order_item_configuration WHERE order_item_id = $1 AND configuration_attribute_option_id = $2`,
		orderItemID, linkedOptionID).Scan(&configCostAfter); err != nil {
		t.Fatalf("re-read order_item_configuration cost_price_unit: %v", err)
	}
	if configCostAfter.Int64 != 300 {
		t.Fatalf("order_item_configuration cost_price_unit changed after a later purchase_price update — got %v, want 300 (frozen)", configCostAfter)
	}
}
