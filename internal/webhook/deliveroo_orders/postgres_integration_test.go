//go:build postgres_integration

package deliveroo_orders

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
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

	// DisablePayments: UPDATE...FROM (Postgres) vs UPDATE...JOIN (MySQL).
	if err := repo.DisablePayments(ctx, brandOrderID); err != nil {
		t.Fatalf("DisablePayments failed against postgres: %v", err)
	}
	var paymentEnabled bool
	if err := db.QueryRowContext(ctx, `SELECT enabled FROM payments WHERE order_id = $1`, orderID).Scan(&paymentEnabled); err != nil {
		t.Fatalf("read back payment: %v", err)
	}
	if paymentEnabled {
		t.Fatal("expected payment disabled after DisablePayments")
	}

	if err := repo.UpdateOrderRejected(ctx, brandOrderID, "REJECTED"); err != nil {
		t.Fatalf("UpdateOrderRejected failed against postgres: %v", err)
	}
	var state, cancelledByType string
	if err := db.QueryRowContext(ctx, `SELECT brand_status, state, merchant_approval, cancelled_by_type FROM orders WHERE order_id = $1`, orderID).
		Scan(&brandStatus, &state, &approval, &cancelledByType); err != nil {
		t.Fatalf("read back after UpdateOrderRejected: %v", err)
	}
	if brandStatus != "REJECTED" || state != "CLOSED" || approval != "DENIED" {
		t.Fatalf("unexpected state after UpdateOrderRejected: status=%q state=%q approval=%q", brandStatus, state, approval)
	}
	// PROMPT 11, §2: no cancelled_by_type was set before this call, so the
	// webhook confirming the rejection is treated as platform-driven.
	if cancelledByType != "PLATFORM" {
		t.Fatalf("expected cancelled_by_type=PLATFORM after UpdateOrderRejected (previously unset), got %q", cancelledByType)
	}

	// GetNextOrderNum: existing order_num 5 -> "6".
	next, err := repo.GetNextOrderNum(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetNextOrderNum failed against postgres: %v", err)
	}
	if next != "6" {
		t.Fatalf("expected \"6\", got %q", next)
	}

	// A staff-initiated Deliveroo deny (order_life_cycle.SetOrderDenied calls
	// DenyOrderLocal synchronously, STAFF, before async-triggering
	// deliverooSvc.DenyOrder — whose webhook confirmation lands here) must not
	// be clobbered by this webhook's own write. Seeded after GetNextOrderNum
	// above so its order_num doesn't shift that assertion.
	const brandOrderIDStaffDenied = "itest-deliveroo-brand-order-staffdenied"
	var orderIDStaffDenied int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand, brand_order_id, brand_status, price, TVA, HT, created_by, cancelled_by_type)
		VALUES ($1, 6, 'DELIVEROO', $2, 'ACCEPTED', 1000, 0, 1000, '226', 'STAFF')
		RETURNING order_id`, merchantID, brandOrderIDStaffDenied).Scan(&orderIDStaffDenied); err != nil {
		t.Fatalf("seed staff-denied order: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE order_id = $1`, orderIDStaffDenied) })

	if err := repo.UpdateOrderRejected(ctx, brandOrderIDStaffDenied, "REJECTED"); err != nil {
		t.Fatalf("UpdateOrderRejected (staff-denied) failed against postgres: %v", err)
	}
	var cancelledByTypeAfter string
	if err := db.QueryRowContext(ctx, `SELECT cancelled_by_type FROM orders WHERE order_id = $1`, orderIDStaffDenied).Scan(&cancelledByTypeAfter); err != nil {
		t.Fatalf("read back cancelled_by_type after UpdateOrderRejected (staff-denied): %v", err)
	}
	if cancelledByTypeAfter != "STAFF" {
		t.Fatalf("UpdateOrderRejected must not overwrite an already-set cancelled_by_type — got %q, want STAFF (preserved)", cancelledByTypeAfter)
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
		_, _ = db.ExecContext(ctx, `DELETE FROM configurable_attribute_options WHERE title = 'ITest Deliveroo Option'`)
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

	// SyncProduct's create path has its own pre-existing, independent bug:
	// products.category is NOT NULL with no default (confirmed in the MySQL
	// source DDL too) and this INSERT never sets it — same bug class as
	// webhook/ubereats.CreateAttributeFromUberGroup and this module's
	// getOrCreateDefaultGroupTx. Documented in the Tier2 report, left unfixed.
	// Confirm the failure is that specific bug, not a regression in the
	// dbx/InsertReturningID conversion done here.
	if _, err := repo.SyncProduct(ctx, merchantID, item); err == nil {
		t.Fatal("expected SyncProduct create-path to fail on the pre-existing category NOT NULL bug")
	} else if !strings.Contains(err.Error(), "category") {
		t.Fatalf("expected a category NOT NULL violation, got: %v", err)
	}

	// Seed the product/mapping directly (working around the bug above) to
	// exercise the parts of SyncProduct/GetProductMapping that are this
	// module's actual Tier2 conversion surface: the CAST-based
	// products.product_id (integer) <-> *.product_id (varchar) join.
	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, product_desc, price, category)
		VALUES ($1, $2, $3, $4, 'itest-category') RETURNING product_id`,
		merchantID, item.Name, item.OperationalName, item.UnitPrice.Fractional).Scan(&productIntID); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	productID := strconv.FormatInt(productIntID, 10)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO integration_deliveroo_products_mapping(merchant_id, product_id, item_id, item_name)
		VALUES ($1, $2, $3, $4)`, merchantID, productID, posItemID, item.OperationalName); err != nil {
		t.Fatalf("seed integration_deliveroo_products_mapping: %v", err)
	}

	// SyncProduct's "already mapped" read path: exercises the CAST-based join.
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

	// getOrCreateDefaultGroupTx has a pre-existing, independent bug: it never
	// sets configurable_attributes.product_id (NOT NULL, no default — same
	// class of issue as webhook/ubereats.CreateAttributeFromUberGroup,
	// confirmed present in the MySQL source DDL too). SyncOption therefore
	// cannot complete the "create" path against a real schema on either
	// dialect; documented in the Tier2 report and left unfixed. Confirm the
	// failure is that specific pre-existing bug, not a regression in the
	// dbx/cast/InsertReturningID work done in this module.
	mod := DeliverooModifier{
		PosItemID: "itest-do-mod-1",
		Name:      "ITest Deliveroo Option",
		UnitPrice: DeliverooMoney{Fractional: 150, CurrencyCode: "EUR"},
	}
	if _, _, err := repo.SyncOption(ctx, merchantID, productID, mod); err == nil {
		t.Fatal("expected SyncOption create-path to fail on the pre-existing product_id NOT NULL bug")
	} else if !strings.Contains(err.Error(), "product_id") {
		t.Fatalf("expected a product_id NOT NULL violation, got: %v", err)
	}

	// Exercise the "already mapped" branch of SyncOption directly, and the
	// option-creation SQL (dbx.InsertReturningID) independent of the
	// configurable_attributes bug above — configurable_attribute_options has
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
