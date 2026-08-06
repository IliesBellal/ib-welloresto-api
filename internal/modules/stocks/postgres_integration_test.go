//go:build postgres_integration

package stocks

import (
	"context"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

func TestStocksRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "itest-stk-m1"
	const userID = "itest-stk-user-1"
	const barcode = "itest-barcode-1"
	var uomIntID int64
	var componentIntID int64
	var componentIntID2 int64
	var componentIntID3 int64
	var productIntID int64
	var recipeIntID int64
	var orderIntID int64
	const attrID = "itest-stk-attr-1"

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM stock_movements WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM expiration_dates WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM purchased_components WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM barcodes WHERE merchant_id = $1`, merchantID)
		if orderIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM order_item_configuration WHERE order_item_id IN (SELECT order_item_id FROM orderitems WHERE order_id = $1)`, orderIntID)
			_, _ = db.ExecContext(ctx, `DELETE FROM orderitems WHERE order_id = $1`, orderIntID)
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE order_id = $1`, orderIntID)
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM configurable_attribute_options WHERE configurable_attribute_id = $1`, attrID)
		_, _ = db.ExecContext(ctx, `DELETE FROM configurable_attributes WHERE id = $1`, attrID)
		if recipeIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM requires WHERE recipe_id = $1`, recipeIntID)
			_, _ = db.ExecContext(ctx, `DELETE FROM recipes WHERE recipe_id = $1`, recipeIntID)
		}
		if productIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE product_id = $1`, productIntID)
		}
		if componentIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM components WHERE component_id = $1`, componentIntID)
		}
		if componentIntID2 != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM components WHERE component_id = $1`, componentIntID2)
		}
		if componentIntID3 != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM components WHERE component_id = $1`, componentIntID3)
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM productcateg WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM component_category WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id = $1`, userID)
		if uomIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM unit_of_measure_convert WHERE id_from = $1`, uomIntID)
			_, _ = db.ExecContext(ctx, `DELETE FROM unit_of_measure_desc WHERE id = $1`, uomIntID)
			_, _ = db.ExecContext(ctx, `DELETE FROM unit_of_measure WHERE id = $1`, uomIntID)
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant_parameters WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO merchant_parameters (merchant_id, last_menu_update, disable_components_under_safety_stock)
		VALUES ($1, $2, true)`, merchantID, time.Now().UTC()); err != nil {
		t.Fatalf("seed merchant_parameters: %v", err)
	}

	if err := db.QueryRowContext(ctx, `INSERT INTO unit_of_measure (uom) VALUES ('g') RETURNING id`).Scan(&uomIntID); err != nil {
		t.Fatalf("seed unit_of_measure: %v", err)
	}
	uom := strconv.FormatInt(uomIntID, 10)
	if _, err := db.ExecContext(ctx, `INSERT INTO unit_of_measure_desc (id, lang, uom_desc, uom_short_desc) VALUES ($1, 'FR', 'Grammes', 'g')`, uomIntID); err != nil {
		t.Fatalf("seed unit_of_measure_desc: %v", err)
	}
	// barcodes.uom defaults to 0 until AddStockBarcode sets a real unit — seed
	// a fallback so GetBarcodeInfo (called right after CreateBarcode, before
	// any scan) resolves a UOM description like it would in production.
	if _, err := db.ExecContext(ctx, `INSERT INTO unit_of_measure_desc (id, lang, uom_desc, uom_short_desc) VALUES (0, 'FR', 'Non défini', '-') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatalf("seed unit_of_measure_desc (default 0): %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM unit_of_measure_desc WHERE id = 0 AND lang = 'FR'`) })
	// Self-conversion (ratio 1) so a barcode/movement scanned in the same unit
	// as the component's native unit resolves via the unit_of_measure_convert
	// join (now a scalar subquery after the Tier2 rewrite).
	if _, err := db.ExecContext(ctx, `INSERT INTO unit_of_measure_convert (id_from, id_to, ratio) VALUES ($1, $1, 1)`, uomIntID); err != nil {
		t.Fatalf("seed unit_of_measure_convert: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO component_category (merchant_id, merchant_categ_id, name, categ_order, available)
		VALUES ($1, 'cat-1', 'ITest Category', 1, true)`, merchantID); err != nil {
		t.Fatalf("seed component_category: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO components (merchant_id, name, category_id, unit_of_measure, stock, safety_stock, safety_triggered, auto_update_purchase_info)
		VALUES ($1, 'ITest Component', 'cat-1', $2, 200, 100, false, true)
		RETURNING component_id`, merchantID, uomIntID).Scan(&componentIntID); err != nil {
		t.Fatalf("seed components: %v", err)
	}
	componentID := strconv.FormatInt(componentIntID, 10)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO users (user_id, name, first_name, last_name, password, email, token, enabled, merchant_id)
		VALUES ($1, 'ITest Stock User', 'ITest', 'User', 'hash', 'itest-stk@example.com', 'stk-tok', true, $2)`, userID, merchantID); err != nil {
		t.Fatalf("seed users: %v", err)
	}

	repo := NewStockRepository(db)

	// --- Barcodes ---
	if err := repo.CreateBarcode(ctx, merchantID, barcode, componentID); err != nil {
		t.Fatalf("CreateBarcode failed against postgres: %v", err)
	}
	info, availableUOM, err := repo.GetBarcodeInfo(ctx, merchantID, barcode)
	if err != nil {
		t.Fatalf("GetBarcodeInfo failed against postgres: %v", err)
	}
	if info == nil || info.ComponentID != componentID {
		t.Fatalf("unexpected barcode info: %+v", info)
	}
	if len(availableUOM) != 1 || availableUOM[0].UOMID != uom {
		t.Fatalf("unexpected available UOM: %+v", availableUOM)
	}

	// AddStockBarcode: dbx.UTCNow() fix, 2x UPDATE...JOIN->scalar-subquery
	// rewrite, purchased_components/expiration_dates InsertReturningID fixes.
	dlc := time.Now().UTC().Add(48 * time.Hour).Format("2006-01-02")
	if err := repo.AddStockBarcode(ctx, merchantID, userID, barcode, models.BarcodeSpecs{
		ComponentID: componentID, BCPrice: 2.5, BCQuantity: 10, BCUOM: uom, CQuantity: 3, DLC: &dlc,
	}); err != nil {
		t.Fatalf("AddStockBarcode failed against postgres: %v", err)
	}
	var stockAfterAdd float64
	var statusAfterAdd string
	if err := db.QueryRowContext(ctx, `SELECT stock, status FROM components WHERE component_id = $1`, componentIntID).
		Scan(&stockAfterAdd, &statusAfterAdd); err != nil {
		t.Fatalf("read back component after AddStockBarcode: %v", err)
	}
	// stock = 200 + (3 * 10 * 1) = 230
	if stockAfterAdd != 230 {
		t.Fatalf("expected stock 230 after AddStockBarcode, got %v", stockAfterAdd)
	}
	if statusAfterAdd != "1" {
		t.Fatalf("expected status '1' (varchar literal fix) after AddStockBarcode, got %q", statusAfterAdd)
	}
	var expirationCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM expiration_dates WHERE component_id = $1`, componentIntID).Scan(&expirationCount); err != nil {
		t.Fatalf("count expiration_dates: %v", err)
	}
	if expirationCount != 1 {
		t.Fatalf("expected 1 expiration_dates row (identity-id fix), got %d", expirationCount)
	}
	var purchasedCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM purchased_components WHERE component_id = $1`, componentIntID).Scan(&purchasedCount); err != nil {
		t.Fatalf("count purchased_components: %v", err)
	}
	if purchasedCount != 1 {
		t.Fatalf("expected 1 purchased_components row, got %d", purchasedCount)
	}

	if err := repo.DeleteBarcode(ctx, merchantID, barcode); err != nil {
		t.Fatalf("DeleteBarcode failed against postgres: %v", err)
	}
	info, _, err = repo.GetBarcodeInfo(ctx, merchantID, barcode)
	if err != nil {
		t.Fatalf("GetBarcodeInfo (after delete) failed: %v", err)
	}
	if info != nil {
		t.Fatalf("expected nil after DeleteBarcode, got %+v", info)
	}

	// --- SetStockLoss (COMPONENT): 2-way UPDATE...JOIN -> scalar subqueries,
	// safety_triggered boolean fix, status varchar literal fix.
	if err := repo.SetStockLoss(ctx, merchantID, userID, models.StockLossRequest{
		Type: "COMPONENT", ObjectID: componentID, Qty: 150, UOM: uom, Comment: "itest loss",
	}); err != nil {
		t.Fatalf("SetStockLoss (COMPONENT) failed against postgres: %v", err)
	}
	var stockAfterLoss float64
	var statusAfterLoss string
	var safetyTriggered bool
	if err := db.QueryRowContext(ctx, `SELECT stock, status, safety_triggered FROM components WHERE component_id = $1`, componentIntID).
		Scan(&stockAfterLoss, &statusAfterLoss, &safetyTriggered); err != nil {
		t.Fatalf("read back component after SetStockLoss: %v", err)
	}
	// stock = 230 - (150 * 1) = 80, which is < safety_stock (100) -> safety branch fires.
	if stockAfterLoss != 80 {
		t.Fatalf("expected stock 80 after SetStockLoss, got %v", stockAfterLoss)
	}
	if statusAfterLoss != "0" || !safetyTriggered {
		t.Fatalf("expected status '0' and safety_triggered=true (below safety stock), got status=%q triggered=%v", statusAfterLoss, safetyTriggered)
	}
	var lossMovementCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM stock_movements WHERE component_id = $1 AND movement = 'loss'`, componentID).Scan(&lossMovementCount); err != nil {
		t.Fatalf("count loss movements: %v", err)
	}
	if lossMovementCount != 1 {
		t.Fatalf("expected 1 loss movement, got %d", lossMovementCount)
	}

	// --- SetStockLoss (PRODUCT): product -> recipe -> requires -> component chain.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO productcateg (merchant_id, merchant_categ_id, categ_name, categ_order, available)
		VALUES ($1, 'pcat-1', 'ITest Product Category', 1, true)`, merchantID); err != nil {
		t.Fatalf("seed productcateg: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, category, price, available)
		VALUES ($1, 'ITest Product', 'pcat-1', 500, true)
		RETURNING product_id`, merchantID).Scan(&productIntID); err != nil {
		t.Fatalf("seed products: %v", err)
	}
	productID := strconv.FormatInt(productIntID, 10)
	if err := db.QueryRowContext(ctx, `
		INSERT INTO recipes (merchant_id, product_id) VALUES ($1, $2) RETURNING recipe_id`, merchantID, productIntID).Scan(&recipeIntID); err != nil {
		t.Fatalf("seed recipes: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO components (merchant_id, name, category_id, unit_of_measure, stock, safety_stock, auto_update_purchase_info)
		VALUES ($1, 'ITest Component 2', 'cat-1', $2, 100, 10, true)
		RETURNING component_id`, merchantID, uomIntID).Scan(&componentIntID2); err != nil {
		t.Fatalf("seed components 2: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO requires (recipe_id, component_id, quantity, unit_of_measure, enabled)
		VALUES ($1, $2, 2, $3, true)`, recipeIntID, componentIntID2, uomIntID); err != nil {
		t.Fatalf("seed requires: %v", err)
	}

	if err := repo.SetStockLoss(ctx, merchantID, userID, models.StockLossRequest{
		Type: "PRODUCT", ObjectID: productID, Qty: 3, Comment: "itest product loss",
	}); err != nil {
		t.Fatalf("SetStockLoss (PRODUCT) failed against postgres: %v", err)
	}
	var stockComponent2 float64
	if err := db.QueryRowContext(ctx, `SELECT stock FROM components WHERE component_id = $1`, componentIntID2).Scan(&stockComponent2); err != nil {
		t.Fatalf("read back component 2: %v", err)
	}
	// deduct = ROUND(rq.quantity(2) * ratio(1) * req.Qty(3), 4) = 6 -> stock = 100 - 6 = 94.
	if stockComponent2 != 94 {
		t.Fatalf("expected component 2 stock 94 after PRODUCT loss, got %v", stockComponent2)
	}

	// --- GetStockProducts: boolean available literal fix, both PRODUCT and COMPONENT.
	prodCats, err := repo.GetStockProducts(ctx, merchantID, "PRODUCT")
	if err != nil {
		t.Fatalf("GetStockProducts (PRODUCT) failed against postgres: %v", err)
	}
	if len(prodCats) != 1 || len(prodCats[0].Objects) != 1 {
		t.Fatalf("unexpected PRODUCT stock categories: %+v", prodCats)
	}
	compCats, err := repo.GetStockProducts(ctx, merchantID, "COMPONENT")
	if err != nil {
		t.Fatalf("GetStockProducts (COMPONENT) failed against postgres: %v", err)
	}
	if len(compCats) != 1 || len(compCats[0].Objects) != 2 {
		t.Fatalf("unexpected COMPONENT stock categories: %+v", compCats)
	}

	// --- GetComponentsList: enabled boolean literal fix.
	list, err := repo.GetComponentsList(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetComponentsList failed against postgres: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 components, got %d: %+v", len(list), list)
	}

	// --- RecordComponentMovement: UPDATE...JOIN -> plain UPDATE using the
	// already-fetched ratio.
	if err := repo.RecordComponentMovement(ctx, merchantID, userID, StockComponentMovementRequest{
		ComponentID: componentID, Unit: uom, Quantity: 20, Type: "add",
	}); err != nil {
		t.Fatalf("RecordComponentMovement (add) failed against postgres: %v", err)
	}
	var stockAfterMovement float64
	if err := db.QueryRowContext(ctx, `SELECT stock FROM components WHERE component_id = $1`, componentIntID).Scan(&stockAfterMovement); err != nil {
		t.Fatalf("read back component after RecordComponentMovement: %v", err)
	}
	if stockAfterMovement != 100 { // 80 + 20
		t.Fatalf("expected stock 100 after RecordComponentMovement add, got %v", stockAfterMovement)
	}
	if err := repo.RecordComponentMovement(ctx, merchantID, userID, StockComponentMovementRequest{
		ComponentID: componentID, Unit: "no-such-unit", Quantity: 1, Type: "add",
	}); err != ErrUnitNotFound {
		t.Fatalf("expected ErrUnitNotFound for an unconvertible unit, got %v", err)
	}

	// --- ConsumeOrderStock: consumes componentIntID2 via the order's items.
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand_status, price, tva, ht, created_by, state)
		VALUES ($1, 1, 'ACCEPTED', 500, 0, 500, $2, 'CLOSED')
		RETURNING order_id`, merchantID, userID).Scan(&orderIntID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	var orderItemIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, price)
		VALUES ($1, $2, $3, 2, 500)
		RETURNING order_item_id`, orderIntID, productIntID, merchantID).Scan(&orderItemIntID); err != nil {
		t.Fatalf("seed orderitem: %v", err)
	}
	if err := repo.ConsumeOrderStock(ctx, merchantID, userID, strconv.FormatInt(orderIntID, 10)); err != nil {
		t.Fatalf("ConsumeOrderStock failed against postgres: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT stock FROM components WHERE component_id = $1`, componentIntID2).Scan(&stockComponent2); err != nil {
		t.Fatalf("read back component 2 after ConsumeOrderStock: %v", err)
	}
	// deduct = ROUND(rq.quantity(2) * ratio(1) * oi.quantity(2), 4) = 4 -> 94 - 4 = 90.
	if stockComponent2 != 90 {
		t.Fatalf("expected component 2 stock 90 after ConsumeOrderStock, got %v", stockComponent2)
	}

	// --- ConsumeOrderOptionsStock: consumes componentIntID3 via a selected
	// attribute option ("Extra itest"), independently of the recipe above.
	if err := db.QueryRowContext(ctx, `
		INSERT INTO components (merchant_id, name, category_id, unit_of_measure, stock, safety_stock, auto_update_purchase_info)
		VALUES ($1, 'ITest Component 3 (option)', 'cat-1', $2, 50, 5, true)
		RETURNING component_id`, merchantID, uomIntID).Scan(&componentIntID3); err != nil {
		t.Fatalf("seed components 3: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO configurable_attributes (id, product_id, merchant_id, attribute_type, name, title, min_options, max_options, enabled)
		VALUES ($1, 0, $2, 'CHECK', 'itest-attr', 'itest-attr', 0, 1, true)`, attrID, merchantID); err != nil {
		t.Fatalf("seed configurable_attributes: %v", err)
	}
	var optionIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO configurable_attribute_options (configurable_attribute_id, title, max_quantity, extra_price, enabled, component_id, quantity, unit_of_measure)
		VALUES ($1, 'Extra itest', 5, 100, 1, $2, 2, $3)
		RETURNING id`, attrID, componentIntID3, uomIntID).Scan(&optionIntID); err != nil {
		t.Fatalf("seed configurable_attribute_options: %v", err)
	}
	// configuration_attribute_id est resté "integer" sur cette table alors que
	// configurable_attributes.id est varchar(64) (IDs préfixés applicatifs) —
	// mismatch de schéma préexistant, sans lien avec ConsumeOrderOptionsStock
	// (qui ne lit pas cette colonne). Un entier factice suffit ici.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO order_item_configuration (order_item_id, configuration_attribute_id, configuration_attribute_option_id, quantity)
		VALUES ($1, 0, $2, 3)`, orderItemIntID, optionIntID); err != nil {
		t.Fatalf("seed order_item_configuration: %v", err)
	}
	if err := repo.ConsumeOrderOptionsStock(ctx, merchantID, userID, strconv.FormatInt(orderIntID, 10)); err != nil {
		t.Fatalf("ConsumeOrderOptionsStock failed against postgres: %v", err)
	}
	var stockComponent3 float64
	if err := db.QueryRowContext(ctx, `SELECT stock FROM components WHERE component_id = $1`, componentIntID3).Scan(&stockComponent3); err != nil {
		t.Fatalf("read back component 3 after ConsumeOrderOptionsStock: %v", err)
	}
	// deduct = ROUND(cao.quantity(2) * ratio(1) * oic.quantity(3), 4) = 6 -> 50 - 6 = 44.
	if stockComponent3 != 44 {
		t.Fatalf("expected component 3 stock 44 after ConsumeOrderOptionsStock, got %v", stockComponent3)
	}
	var optionConsumeMovementCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM stock_movements WHERE component_id = $1 AND movement = 'consume'`, strconv.FormatInt(componentIntID3, 10)).Scan(&optionConsumeMovementCount); err != nil {
		t.Fatalf("count option consume movements: %v", err)
	}
	if optionConsumeMovementCount != 1 {
		t.Fatalf("expected 1 consume movement for component 3, got %d", optionConsumeMovementCount)
	}
	// componentIntID2 (recette) ne doit pas être affecté par la déduction des options.
	if err := db.QueryRowContext(ctx, `SELECT stock FROM components WHERE component_id = $1`, componentIntID2).Scan(&stockComponent2); err != nil {
		t.Fatalf("read back component 2 after ConsumeOrderOptionsStock: %v", err)
	}
	if stockComponent2 != 90 {
		t.Fatalf("expected component 2 stock unchanged at 90 after ConsumeOrderOptionsStock, got %v", stockComponent2)
	}

	// --- GetMovements: DATE_FORMAT/DATE() fix + cross-type (varchar/integer) joins.
	today := time.Now().UTC().Format("2006-01-02")
	movements, err := repo.GetMovements(ctx, merchantID, today, today)
	if err != nil {
		t.Fatalf("GetMovements failed against postgres: %v", err)
	}
	if len(movements) == 0 {
		t.Fatal("expected at least one stock movement")
	}
	foundConsume := false
	for _, m := range movements {
		if m.ComponentName == "" || m.CreatedAt == "" {
			t.Fatalf("expected component_name/created_at to resolve via the CAST-fixed joins, got %+v", m)
		}
		if m.Type == "consume" {
			foundConsume = true
			if m.ProductName == nil || *m.ProductName != "ITest Product" {
				t.Fatalf("expected product_name to resolve via the CAST-fixed join, got %+v", m)
			}
		}
	}
	if !foundConsume {
		t.Fatalf("expected a 'consume' movement from ConsumeOrderStock, got %+v", movements)
	}
}
