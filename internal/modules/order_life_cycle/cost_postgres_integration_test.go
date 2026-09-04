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

// Vérification réelle du gel du coût de revient (B2, PROMPT 07 lot 1) contre
// Postgres : le coût snapshoté sur une ligne déjà écrite ne bouge plus quand
// components.purchase_price change ensuite, une recette absente donne NULL/
// NO_RECIPE (jamais 0), une recette existante mais mal paramétrée donne NULL/
// INCOMPLETE_RECIPE, et le coût d'une option sélectionnée
// (configurable_attribute_options.component_id) est bien inclus.
func TestOrderLifeCycleRepository_CostFreeze_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantID string
	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM orderitems WHERE merchant_id = $1`,
			`DELETE FROM orders WHERE merchant_id = $1`,
			`DELETE FROM configurable_attribute_options WHERE configurable_attribute_id IN (SELECT id FROM configurable_attributes WHERE merchant_id = $1)`,
			`DELETE FROM configurable_attributes WHERE merchant_id = $1`,
			`DELETE FROM requires WHERE recipe_id IN (SELECT recipe_id FROM recipes WHERE merchant_id = $1)`,
			`DELETE FROM recipes WHERE merchant_id = $1`,
			`DELETE FROM components WHERE merchant_id = $1`,
			`DELETE FROM products WHERE merchant_Id = $1`,
			`DELETE FROM merchant_parameters WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-olc-cost' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	}
	t.Cleanup(func() { cleanupFor(merchantID) })

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest OLC Cost', 'a', '1', 's', '75001', 'Paris', 'siret-olc-cost', 'https://x', '06', 'mtok-olc-cost', 'UTC')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)

	custoRepo := customers.NewCustomerRepository(db)
	repo := NewOrdersLifeCycleRepository(db, custoRepo)

	// --- Cas 1 : recette simple, une seule unité (Pce), coût connu -----------
	var componentID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO components (merchant_id, name, unit_of_measure, purchase_price, purchase_price_quantity)
		VALUES ($1, 'itest-cost-comp', 1, 500, 10) RETURNING component_id`, merchantID).Scan(&componentID); err != nil {
		t.Fatalf("seed component: %v", err)
	}
	// 500 cents / 10 Pce = 50 cents/Pce.

	var productID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_Id, name, price, category, status)
		VALUES ($1, 'itest-cost-prod', 1000, 'c', '1') RETURNING product_id`, merchantID).Scan(&productID); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	productIDStr := strconv.FormatInt(productID, 10)

	var recipeID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO recipes (merchant_id, product_id) VALUES ($1, $2) RETURNING recipe_id`, merchantID, productID).Scan(&recipeID); err != nil {
		t.Fatalf("seed recipe: %v", err)
	}
	// 3 Pce du composant -> 3 * 50 = 150 cents.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO requires (recipe_id, component_id, quantity, unit_of_measure) VALUES ($1, $2, 3, 1)`,
		recipeID, componentID); err != nil {
		t.Fatalf("seed requires: %v", err)
	}

	costCents, reason := repo.resolveOrderItemCost(ctx, merchantID, productIDStr, nil)
	if reason != nil {
		t.Fatalf("expected a resolvable cost, got reason=%v", *reason)
	}
	if costCents == nil || *costCents != 150 {
		t.Fatalf("expected cost=150, got %v", costCents)
	}

	// Écrit une première ligne au prix actuel.
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
	item.CostPriceUnit, item.CostPriceReason = repo.resolveOrderItemCost(ctx, merchantID, productIDStr, nil)
	firstOrderItemID, err := repo.InsertOrderItem(ctx, item)
	if err != nil {
		t.Fatalf("InsertOrderItem: %v", err)
	}

	var storedCost sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT cost_price_unit FROM orderitems WHERE order_item_id = $1`, firstOrderItemID).Scan(&storedCost); err != nil {
		t.Fatalf("read back cost_price_unit: %v", err)
	}
	if !storedCost.Valid || storedCost.Int64 != 150 {
		t.Fatalf("expected stored cost_price_unit=150, got %v", storedCost)
	}

	// --- Le coeur du test B2 : on change le prix d'achat après coup ----------
	if _, err := db.ExecContext(ctx, `UPDATE components SET purchase_price = 9999 WHERE component_id = $1`, componentID); err != nil {
		t.Fatalf("update purchase_price: %v", err)
	}

	var storedCostAfter sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT cost_price_unit FROM orderitems WHERE order_item_id = $1`, firstOrderItemID).Scan(&storedCostAfter); err != nil {
		t.Fatalf("re-read cost_price_unit: %v", err)
	}
	if storedCostAfter.Int64 != 150 {
		t.Fatalf("B2 violation: cost_price_unit changed after a later purchase_price update — got %v, want 150 (frozen)", storedCostAfter)
	}

	// Une ligne écrite APRES le changement de prix doit, elle, refléter le
	// nouveau prix — la résolution reste vivante, ce n'est que l'écriture
	// existante qui est figée.
	newCost, newReason := repo.resolveOrderItemCost(ctx, merchantID, productIDStr, nil)
	if newReason != nil || newCost == nil {
		t.Fatalf("expected resolvable cost after price change, got cost=%v reason=%v", newCost, newReason)
	}
	// 3 Pce * (9999/10) = 3 * 999.9 = 2999.7 -> round -> 3000.
	if *newCost != 3000 {
		t.Fatalf("expected new resolution=3000 after price change, got %d", *newCost)
	}

	// --- Cas 2 : produit sans recette -> NO_RECIPE, jamais 0 -----------------
	var productNoRecipeID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_Id, name, price, category, status)
		VALUES ($1, 'itest-cost-norecipe', 500, 'c', '1') RETURNING product_id`, merchantID).Scan(&productNoRecipeID); err != nil {
		t.Fatalf("seed product no recipe: %v", err)
	}
	cost, reason := repo.resolveOrderItemCost(ctx, merchantID, strconv.FormatInt(productNoRecipeID, 10), nil)
	if cost != nil {
		t.Fatalf("expected nil cost for a product with no recipe, got %d", *cost)
	}
	if reason == nil || *reason != "NO_RECIPE" {
		t.Fatalf("expected reason=NO_RECIPE, got %v", reason)
	}

	// --- Cas 3 : recette existante mais composant sans prix d'achat ----------
	// -> INCOMPLETE_RECIPE, distinct de NO_RECIPE.
	var componentUnpriced int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO components (merchant_id, name, unit_of_measure) VALUES ($1, 'itest-cost-unpriced', 1) RETURNING component_id`,
		merchantID).Scan(&componentUnpriced); err != nil {
		t.Fatalf("seed unpriced component: %v", err)
	}
	var productIncompleteID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_Id, name, price, category, status)
		VALUES ($1, 'itest-cost-incomplete', 500, 'c', '1') RETURNING product_id`, merchantID).Scan(&productIncompleteID); err != nil {
		t.Fatalf("seed product incomplete: %v", err)
	}
	var recipeIncompleteID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO recipes (merchant_id, product_id) VALUES ($1, $2) RETURNING recipe_id`,
		merchantID, productIncompleteID).Scan(&recipeIncompleteID); err != nil {
		t.Fatalf("seed recipe incomplete: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO requires (recipe_id, component_id, quantity, unit_of_measure) VALUES ($1, $2, 1, 1)`,
		recipeIncompleteID, componentUnpriced); err != nil {
		t.Fatalf("seed requires incomplete: %v", err)
	}
	cost, reason = repo.resolveOrderItemCost(ctx, merchantID, strconv.FormatInt(productIncompleteID, 10), nil)
	if cost != nil {
		t.Fatalf("expected nil cost for an incomplete recipe (unpriced component), got %d", *cost)
	}
	if reason == nil || *reason != "INCOMPLETE_RECIPE" {
		t.Fatalf("expected reason=INCOMPLETE_RECIPE, got %v", reason)
	}

	// --- Cas 4 : coût d'une option sélectionnée (configurable_attribute_options) ---
	var attrID = "itest-cost-attr"
	if _, err := db.ExecContext(ctx, `
		INSERT INTO configurable_attributes (id, product_id, merchant_id, name, title, max_options)
		VALUES ($1, $2, $3, 'taille', 'Taille', 1)`, attrID, productID, merchantID); err != nil {
		t.Fatalf("seed configurable_attribute: %v", err)
	}
	var optionID int64
	// Option liée à 2 Pce du même composant (prix courant 9999/10 = 999.9/Pce) -> 2*999.9=1999.8 -> round -> 2000.
	if err := db.QueryRowContext(ctx, `
		INSERT INTO configurable_attribute_options (configurable_attribute_id, title, component_id, quantity, unit_of_measure)
		VALUES ($1, 'Grande', $2, 2, 1) RETURNING id`, attrID, componentID).Scan(&optionID); err != nil {
		t.Fatalf("seed configurable_attribute_option: %v", err)
	}
	costWithOption, reasonWithOption := repo.resolveOrderItemCost(ctx, merchantID, productIDStr, []costOptionSelection{
		{OptionID: strconv.FormatInt(optionID, 10), Quantity: 1},
	})
	if reasonWithOption != nil {
		t.Fatalf("expected resolvable cost with option, got reason=%v", *reasonWithOption)
	}
	// Base recette (3000, cf. plus haut) + option (2000) = 5000.
	if costWithOption == nil || *costWithOption != 5000 {
		t.Fatalf("expected cost with option=5000, got %v", costWithOption)
	}

	// --- Cas 5 : la résolution batchée (chemin CreateOrder) donne exactement
	// les mêmes résultats que la résolution ligne par ligne ci-dessus, pour
	// un panier mélangeant les 4 cas (OK, NO_RECIPE, INCOMPLETE_RECIPE, avec
	// option) — et deux lignes du même produit dans le même panier.
	products := []models.OrderProductPayload{
		{ProductID: productIDStr, Quantity: 1},                     // OK, 3000
		{ProductID: productIDStr, Quantity: 2},                     // même produit, deuxième ligne
		{ProductID: strconv.FormatInt(productNoRecipeID, 10), Quantity: 1},   // NO_RECIPE
		{ProductID: strconv.FormatInt(productIncompleteID, 10), Quantity: 1}, // INCOMPLETE_RECIPE
		{
			ProductID: productIDStr, Quantity: 1,
			Config: &models.ProductConfiguration{Attributes: []models.ConfigurationAttribute{
				{ID: attrID, Options: []models.ConfigurationOption{{ID: strconv.FormatInt(optionID, 10), Quantity: 1}}},
			}},
		}, // OK + option, 5000
	}
	batched := repo.resolveOrderItemCostsForOrder(ctx, merchantID, products)
	if len(batched) != len(products) {
		t.Fatalf("expected %d batched results, got %d", len(products), len(batched))
	}
	checkOK := func(i int, wantCost int) {
		if batched[i].costPriceReason != nil {
			t.Fatalf("line %d: expected resolvable, got reason=%v", i, *batched[i].costPriceReason)
		}
		if batched[i].costPriceUnit == nil || *batched[i].costPriceUnit != wantCost {
			t.Fatalf("line %d: expected cost=%d, got %v", i, wantCost, batched[i].costPriceUnit)
		}
	}
	checkReason := func(i int, wantReason string) {
		if batched[i].costPriceUnit != nil {
			t.Fatalf("line %d: expected nil cost, got %d", i, *batched[i].costPriceUnit)
		}
		if batched[i].costPriceReason == nil || *batched[i].costPriceReason != wantReason {
			t.Fatalf("line %d: expected reason=%s, got %v", i, wantReason, batched[i].costPriceReason)
		}
	}
	checkOK(0, 3000)
	checkOK(1, 3000) // même produit, même coût unitaire indépendamment de la quantité de la ligne
	checkReason(2, "NO_RECIPE")
	checkReason(3, "INCOMPLETE_RECIPE")
	checkOK(4, 5000)
}
