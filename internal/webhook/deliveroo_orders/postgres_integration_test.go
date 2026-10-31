//go:build postgres_integration

package deliveroo_orders

import (
	"context"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

func TestRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	var merchantID string
	const locationID = "itest-location-1"

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM integration_deliveroo WHERE location_id = $1`, locationID)
		if merchantIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
	}
	t.Cleanup(func() { cleanup() })

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token)
		VALUES ('ITest Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-val', 'https://example.com', '0600000000', 'tok')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO integration_deliveroo (merchant_id, location_id, brand_id, auto_accept_orders, enabled)
		VALUES ($1, $2, 'brand-1', true, true)`, merchantID, locationID); err != nil {
		t.Fatalf("seed integration_deliveroo: %v", err)
	}

	repo := NewRepository(db)

	// GetMerchantByLocationID: merchant.id (integer) joined against
	// integration_deliveroo.merchant_id (varchar) via the dialect-branched CAST,
	// plus the auto_accept_orders boolean scan fix.
	merchantData, err := repo.GetMerchantByLocationID(ctx, locationID)
	if err != nil {
		t.Fatalf("GetMerchantByLocationID failed against postgres: %v", err)
	}
	if merchantData.MerchantID != merchantID || !merchantData.AutoAcceptOrders {
		t.Fatalf("unexpected merchant data: %+v (want merchant_id=%s)", merchantData, merchantID)
	}

	if _, err := repo.GetMerchantByLocationID(ctx, ""); err == nil {
		t.Fatal("expected error for empty location_id")
	}
	if _, err := repo.GetMerchantByLocationID(ctx, "no-such-location"); err == nil {
		t.Fatal("expected error for unknown location_id")
	}

	// GetNextOrderNum: no existing orders yet.
	next, err := repo.GetNextOrderNum(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetNextOrderNum (empty) failed against postgres: %v", err)
	}
	if next != "1" {
		t.Fatalf("expected \"1\" for a merchant with no orders, got %q", next)
	}
}

func TestRepository_OrderLifecycle_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "itest-do-order-m1"
	const brandOrderID = "itest-do-brand-order-1"
	var orderID int64

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM payments WHERE order_id = $1`, orderID)
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, merchantID)
	}
	t.Cleanup(func() { cleanup() })

	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand, brand_order_id, brand_status, price, tva, ht, created_by)
		VALUES ($1, 5, 'DELIVEROO', $2, 'SCHEDULED', 2000, 150, 1850, 'itest')
		RETURNING order_id`, merchantID, brandOrderID).Scan(&orderID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, enabled)
		VALUES ($1, 'itest-user', $2, 2000, 'CARD', true)`, merchantID, orderID); err != nil {
		t.Fatalf("seed payment: %v", err)
	}

	repo := NewRepository(db)

	orderIDStr, err := repo.GetOrderIDByBrandID(ctx, brandOrderID)
	if err != nil {
		t.Fatalf("GetOrderIDByBrandID failed against postgres: %v", err)
	}
	if orderIDStr == "" {
		t.Fatal("expected a non-empty order id")
	}
	orderIDStr2, err := repo.GetOrderIDByBrandIDTx(ctx, brandOrderID)
	if err != nil || orderIDStr2 != orderIDStr {
		t.Fatalf("GetOrderIDByBrandIDTx mismatch: %q vs %q (err=%v)", orderIDStr2, orderIDStr, err)
	}

	// UpdateOrderAccepted: SCHEDULED/ACCEPTED CASE WHEN toggle branch. Seed and
	// expectations uppercase (B3, PROMPT 07 lot 1 — brand_status always
	// written/compared uppercase; this fixture predated that fix and was
	// stale, found while validating PROMPT 11).
	if err := repo.UpdateOrderAccepted(ctx, brandOrderID, true); err != nil {
		t.Fatalf("UpdateOrderAccepted (toggle) failed against postgres: %v", err)
	}
	var brandStatus, approval string
	if err := db.QueryRowContext(ctx, `SELECT brand_status, merchant_approval FROM orders WHERE order_id = $1`, orderID).
		Scan(&brandStatus, &approval); err != nil {
		t.Fatalf("read back after UpdateOrderAccepted: %v", err)
	}
	if brandStatus != "ACCEPTED" || approval != "ACCEPTED" {
		t.Fatalf("unexpected state after UpdateOrderAccepted: status=%q approval=%q", brandStatus, approval)
	}

	if err := repo.UpdateOrderConfirmed(ctx, brandOrderID); err != nil {
		t.Fatalf("UpdateOrderConfirmed failed against postgres: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT brand_status FROM orders WHERE order_id = $1`, orderID).Scan(&brandStatus); err != nil {
		t.Fatalf("read back after UpdateOrderConfirmed: %v", err)
	}
	if brandStatus != "CONFIRMED" {
		t.Fatalf("expected CONFIRMED, got %q", brandStatus)
	}

	// GetNextOrderNum: existing order_num 5 -> "6".
	next, err := repo.GetNextOrderNum(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetNextOrderNum failed against postgres: %v", err)
	}
	if next != "6" {
		t.Fatalf("expected \"6\", got %q", next)
	}
}

func TestRepository_ProductAndOptionSync_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "itest-do-sync-m1"
	const posItemID = "itest-do-item-1"

	var productIntID int64
	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM integration_deliveroo_products_mapping WHERE merchant_id = $1`, merchantID)
		if productIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE product_id = $1`, productIntID)
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM integration_deliveroo_options_mapping WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM configurable_attribute_options WHERE title LIKE 'ITest Deliveroo Option%'`)
		if productIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM product_configurable_attribute WHERE product_id = $1`, strconv.FormatInt(productIntID, 10))
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM product_configurable_attribute WHERE configurable_attribute_id LIKE 'itest-%'`)
		_, _ = db.ExecContext(ctx, `DELETE FROM configurable_attributes WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewRepository(db)

	item := DeliverooItem{
		PosItemID:       posItemID,
		Name:            "ITest Product",
		OperationalName: "itest-op-name",
		UnitPrice:       DeliverooMoney{Fractional: 990, CurrencyCode: "EUR"},
	}

	// SyncProduct's create path: an unknown Deliveroo item becomes a product
	// in the DELIVEROO ghost category (products.category is NOT NULL with no
	// default — this INSERT used to omit it and failed) plus its mapping.
	productID, err := repo.SyncProduct(ctx, merchantID, item)
	if err != nil {
		t.Fatalf("SyncProduct create-path failed against postgres: %v", err)
	}
	productIntID, err = strconv.ParseInt(productID, 10, 64)
	if err != nil {
		t.Fatalf("SyncProduct returned a non-numeric product id %q: %v", productID, err)
	}
	var category string
	if err := db.QueryRowContext(ctx, `SELECT category FROM products WHERE product_id = $1`, productIntID).Scan(&category); err != nil {
		t.Fatalf("read created product: %v", err)
	}
	if category != models.MarketplaceCategoryDeliveroo {
		t.Fatalf("created product category = %q, want %q", category, models.MarketplaceCategoryDeliveroo)
	}
	var tva models.MarketplaceTVA
	if err := db.QueryRowContext(ctx, `SELECT tva_in_id, tva_take_away_id, tva_delivery_id FROM products WHERE product_id = $1`, productIntID).
		Scan(&tva.In, &tva.TakeAway, &tva.Delivery); err != nil {
		t.Fatalf("read created product TVA: %v", err)
	}
	if tva != models.MarketplaceTVAStandard {
		t.Fatalf("created product TVA = %+v, want %+v (10 %%)", tva, models.MarketplaceTVAStandard)
	}

	// SyncProduct's "already mapped" read path: exercises the CAST-based
	// products.product_id (integer) <-> *.product_id (varchar) join.
	productID2, err := repo.SyncProduct(ctx, merchantID, item)
	if err != nil {
		t.Fatalf("SyncProduct (existing mapping via CAST join) failed against postgres: %v", err)
	}
	if productID2 != productID {
		t.Fatalf("expected the seeded product id via the CAST join, got %q vs %q", productID2, productID)
	}

	mapping, err := repo.GetProductMapping(ctx, merchantID, posItemID)
	if err != nil {
		t.Fatalf("GetProductMapping failed against postgres: %v", err)
	}
	if mapping == nil || mapping.ItemID != posItemID {
		t.Fatalf("unexpected product mapping: %+v", mapping)
	}

	// SyncOption's create path : un modifier inconnu crée le groupe d'options
	// Deliveroo du produit (configurable_attributes.product_id est NOT NULL — sa
	// valeur manquante faisait échouer l'import de toute commande contenant une
	// option jamais vue), l'option, son mapping et le lien produit/groupe.
	newMod := DeliverooModifier{
		PosItemID: "itest-do-mod-new",
		Name:      "ITest Deliveroo Option New",
		UnitPrice: DeliverooMoney{Fractional: 100, CurrencyCode: "EUR"},
	}
	newAttrID, newOptID, err := repo.SyncOption(ctx, merchantID, productID, newMod)
	if err != nil {
		t.Fatalf("SyncOption create-path failed against postgres: %v", err)
	}
	var groupProductID int64
	if err := db.QueryRowContext(ctx, `SELECT product_id FROM configurable_attributes WHERE id = $1`, newAttrID).Scan(&groupProductID); err != nil {
		t.Fatalf("read created group: %v", err)
	}
	if groupProductID != productIntID {
		t.Fatalf("group product_id = %d, want %d", groupProductID, productIntID)
	}
	// deuxième lecture : option déjà mappée, mêmes identifiants
	againAttrID, againOptID, err := repo.SyncOption(ctx, merchantID, productID, newMod)
	if err != nil || againAttrID != newAttrID || againOptID != newOptID {
		t.Fatalf("SyncOption (already mapped) = (%q, %q, %v), want (%q, %q, nil)", againAttrID, againOptID, err, newAttrID, newOptID)
	}
	// une autre option du même produit réutilise le groupe
	otherMod := DeliverooModifier{
		PosItemID: "itest-do-mod-other",
		Name:      "ITest Deliveroo Option Other",
		UnitPrice: DeliverooMoney{Fractional: 0, CurrencyCode: "EUR"},
	}
	otherAttrID, _, err := repo.SyncOption(ctx, merchantID, productID, otherMod)
	if err != nil || otherAttrID != newAttrID {
		t.Fatalf("SyncOption (second option) = (%q, %v), want the same group %q", otherAttrID, err, newAttrID)
	}

	mod := DeliverooModifier{
		PosItemID: "itest-do-mod-1",
		Name:      "ITest Deliveroo Option",
		UnitPrice: DeliverooMoney{Fractional: 150, CurrencyCode: "EUR"},
	}

	// Exercise the "already mapped" branch of SyncOption directly with a mapping
	// seeded by hand — configurable_attribute_options has
	// no FK enforcing configurable_attribute_id against configurable_attributes.id.
	const fakeAttrID = "itest-fake-attr-do-1"
	var optionID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO configurable_attribute_options (configurable_attribute_id, title, extra_price)
		VALUES ($1, 'ITest Deliveroo Option', 150) RETURNING id`, fakeAttrID).Scan(&optionID); err != nil {
		t.Fatalf("seed configurable_attribute_options: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO integration_deliveroo_options_mapping (merchant_id, configurable_attribute_option_id, item_id)
		VALUES ($1, $2, $3)`, merchantID, optionID, mod.PosItemID); err != nil {
		t.Fatalf("seed integration_deliveroo_options_mapping: %v", err)
	}

	gotAttrID, gotOptionID, err := repo.SyncOption(ctx, merchantID, productID, mod)
	if err != nil {
		t.Fatalf("SyncOption (already mapped) failed against postgres: %v", err)
	}
	if gotAttrID != fakeAttrID || gotOptionID != strconv.FormatInt(optionID, 10) {
		t.Fatalf("unexpected SyncOption result: attr=%q option=%q", gotAttrID, gotOptionID)
	}

	var linkCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM product_configurable_attribute WHERE product_id = $1 AND configurable_attribute_id = $2`,
		productID, fakeAttrID).Scan(&linkCount); err != nil {
		t.Fatalf("read back product_configurable_attribute: %v", err)
	}
	if linkCount != 1 {
		t.Fatalf("expected ensureProductAttributeLink to create exactly 1 link, got %d", linkCount)
	}
}
