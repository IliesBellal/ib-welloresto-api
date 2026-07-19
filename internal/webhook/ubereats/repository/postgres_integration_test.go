//go:build postgres_integration

package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

func TestOrdersRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "itest-ue-m1"
	const brandOrderID = "itest-ue-brand-order-1"

	var orderID int64
	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM payments WHERE order_id = $1`, orderID)
		_, _ = db.ExecContext(ctx, `DELETE FROM orderitems WHERE order_id = $1`, orderID)
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, merchantID)
	}
	t.Cleanup(func() { cleanup() })

	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand, brand_order_id, brand_status, price, tva, ht, created_by)
		VALUES ($1, 1, 'UBER_EATS', $2, 'ACCEPTED', 1500, 100, 1400, 'itest')
		RETURNING order_id`, merchantID, brandOrderID).Scan(&orderID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, enabled)
		VALUES ($1, 'itest-user', $2, 1500, 'CARD', true)`, merchantID, orderID); err != nil {
		t.Fatalf("seed payment: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, price)
		VALUES ($1, 9001, $2, 1, 1500)`, orderID, merchantID); err != nil {
		t.Fatalf("seed orderitem: %v", err)
	}

	repo := NewOrdersRepository(db)

	gotMerchantID, gotOrderIDStr, err := repo.GetOrderIDsByBrandOrderID(ctx, brandOrderID)
	if err != nil {
		t.Fatalf("GetOrderIDsByBrandOrderID failed against postgres: %v", err)
	}
	if gotMerchantID != merchantID || gotOrderIDStr == "" {
		t.Fatalf("unexpected order lookup: merchant=%q order=%q", gotMerchantID, gotOrderIDStr)
	}

	// MarkEnRouteToDropoff: FOR UPDATE lock + dbx.UTCNow() + boolean literal.
	if err := repo.MarkEnRouteToDropoff(ctx, brandOrderID); err != nil {
		t.Fatalf("MarkEnRouteToDropoff failed against postgres: %v", err)
	}
	var brandStatus string
	var deliveryStart *time.Time
	var isDistributed bool
	if err := db.QueryRowContext(ctx, `SELECT brand_status, delivery_start, isdistributed FROM orders WHERE order_id = $1`, orderID).
		Scan(&brandStatus, &deliveryStart, &isDistributed); err != nil {
		t.Fatalf("read back order after MarkEnRouteToDropoff: %v", err)
	}
	if brandStatus != "EN_ROUTE_TO_DROPOFF" || deliveryStart == nil || !isDistributed {
		t.Fatalf("unexpected order state after MarkEnRouteToDropoff: status=%q delivery_start=%v distributed=%v", brandStatus, deliveryStart, isDistributed)
	}
	var itemDistributed bool
	var distributedOn *time.Time
	if err := db.QueryRowContext(ctx, `SELECT isdistributed, distributed_on FROM orderitems WHERE order_id = $1`, orderID).
		Scan(&itemDistributed, &distributedOn); err != nil {
		t.Fatalf("read back orderitem: %v", err)
	}
	if !itemDistributed || distributedOn == nil {
		t.Fatalf("expected orderitem distributed, got distributed=%v distributed_on=%v", itemDistributed, distributedOn)
	}

	// CancelOrder: deletion_reason_id varchar literal + UPDATE...FROM (Postgres) vs UPDATE...JOIN (MySQL).
	if err := repo.CancelOrder(ctx, brandOrderID); err != nil {
		t.Fatalf("CancelOrder failed against postgres: %v", err)
	}
	var deletionReasonID string
	if err := db.QueryRowContext(ctx, `SELECT brand_status, deletion_reason_id FROM orders WHERE order_id = $1`, orderID).
		Scan(&brandStatus, &deletionReasonID); err != nil {
		t.Fatalf("read back order after CancelOrder: %v", err)
	}
	if brandStatus != "CANCELED" || deletionReasonID != "39" {
		t.Fatalf("unexpected order state after CancelOrder: status=%q reason=%q", brandStatus, deletionReasonID)
	}
	var paymentEnabled bool
	if err := db.QueryRowContext(ctx, `SELECT enabled FROM payments WHERE order_id = $1`, orderID).Scan(&paymentEnabled); err != nil {
		t.Fatalf("read back payment after CancelOrder: %v", err)
	}
	if paymentEnabled {
		t.Fatal("expected payment disabled after CancelOrder (UPDATE...FROM branch)")
	}

	// MarkFailed on a second order.
	const brandOrderID2 = "itest-ue-brand-order-2"
	var orderID2 int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand, brand_order_id, brand_status, price, tva, ht, created_by)
		VALUES ($1, 2, 'UBER_EATS', $2, 'ACCEPTED', 1000, 80, 920, 'itest')
		RETURNING order_id`, merchantID, brandOrderID2).Scan(&orderID2); err != nil {
		t.Fatalf("seed order 2: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE order_id = $1`, orderID2) })

	if err := repo.MarkFailed(ctx, brandOrderID2); err != nil {
		t.Fatalf("MarkFailed failed against postgres: %v", err)
	}
	var state string
	if err := db.QueryRowContext(ctx, `SELECT state, brand_status FROM orders WHERE order_id = $1`, orderID2).Scan(&state, &brandStatus); err != nil {
		t.Fatalf("read back order after MarkFailed: %v", err)
	}
	if state != "CLOSED" || brandStatus != "FAILED" {
		t.Fatalf("unexpected order state after MarkFailed: state=%q status=%q", state, brandStatus)
	}
}

func TestAttributeMappingRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "itest-ue-attr-m1"
	const modifierGroupID = "itest-modifier-group-1"
	const uberItemID = "itest-uber-item-1"

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM integration_uber_eats_attributes_mapping WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM integration_uber_eats_options_mapping WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM configurable_attribute_options WHERE title = 'ITest Option'`)
		_, _ = db.ExecContext(ctx, `DELETE FROM configurable_attributes WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewAttributeMappingRepository(db)

	// CreateAttributeFromUberGroup: id is now generated client-side (varchar PK,
	// no LastInsertId — fixed per Tier2 report). The insert still fails on
	// postgres because configurable_attributes.product_id is NOT NULL with no
	// default and this method never sets it — a second, independent
	// pre-existing bug (confirmed present in the MySQL source DDL too, so this
	// insert has apparently never actually succeeded in production). Left
	// unfixed per the Tier2 report; this assertion pins the *specific* failure
	// (product_id, not id) so a regression in the id fix would be caught here.
	_, err := repo.CreateAttributeFromUberGroup(ctx, merchantID, "Sauces & Extras")
	if err == nil {
		t.Fatal("expected CreateAttributeFromUberGroup to fail on the pre-existing product_id NOT NULL bug")
	}
	if !strings.Contains(err.Error(), "product_id") {
		t.Fatalf("expected a product_id NOT NULL violation (id-generation fix regressed?), got: %v", err)
	}

	// Exercise the rest of the repository directly against a fabricated
	// attribute id — configurable_attribute_options.configurable_attribute_id
	// has no FK constraint to configurable_attributes.id in the target schema,
	// so this is valid test setup independent of the bug above.
	const attrID = "itest-fake-attr-1"

	// CreateOptionFromUber: configurable_attribute_options.id is an identity
	// column — exercises dbx.InsertReturningID.
	optID, err := repo.CreateOptionFromUber(ctx, attrID, "ITest Option", 150)
	if err != nil {
		t.Fatalf("CreateOptionFromUber failed against postgres: %v", err)
	}
	if optID == "" || optID == "0" {
		t.Fatalf("expected a generated option id, got %q", optID)
	}

	// integration_uber_eats_attributes_mapping.configurable_attribute_id is a
	// plain integer column (pre-existing type mismatch vs. configurable_attributes.id
	// varchar — documented in the Tier2 report, not fixed here). Use a numeric
	// id to exercise this repository's own SQL/dbx plumbing independently of
	// that upstream bug.
	const numericAttrID = "424242"
	if err := repo.CreateAttributeMapping(ctx, merchantID, numericAttrID, modifierGroupID); err != nil {
		t.Fatalf("CreateAttributeMapping failed against postgres: %v", err)
	}
	gotAttrID, err := repo.GetAttributeIDByModifierGroupID(ctx, merchantID, modifierGroupID)
	if err != nil {
		t.Fatalf("GetAttributeIDByModifierGroupID failed against postgres: %v", err)
	}
	if gotAttrID == nil || *gotAttrID != numericAttrID {
		t.Fatalf("expected %q, got %v", numericAttrID, gotAttrID)
	}

	if err := repo.CreateOptionMapping(ctx, merchantID, optID, uberItemID); err != nil {
		t.Fatalf("CreateOptionMapping failed against postgres: %v", err)
	}
	gotOptID, err := repo.GetOptionIDByUberItemID(ctx, attrID, uberItemID)
	if err != nil {
		t.Fatalf("GetOptionIDByUberItemID failed against postgres: %v", err)
	}
	if gotOptID == nil || *gotOptID != optID {
		t.Fatalf("expected %q, got %v", optID, gotOptID)
	}

	// sql.ErrNoRows branch.
	notFound, err := repo.GetAttributeIDByModifierGroupID(ctx, merchantID, "no-such-group")
	if err != nil {
		t.Fatalf("GetAttributeIDByModifierGroupID (not found) failed: %v", err)
	}
	if notFound != nil {
		t.Fatalf("expected nil for unmapped group, got %v", notFound)
	}
}

func TestProductMappingRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "itest-ue-prod-m1"
	const uberItemID = "itest-uber-product-item-1"
	const productID = "9042"

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM integration_uber_eats_products_mapping WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewProductMappingRepository(db)

	notFound, err := repo.FindProductIDByUberItemID(ctx, merchantID, uberItemID)
	if err != nil {
		t.Fatalf("FindProductIDByUberItemID (empty) failed against postgres: %v", err)
	}
	if notFound != nil {
		t.Fatalf("expected nil before mapping exists, got %v", notFound)
	}

	if err := repo.CreateProductMapping(ctx, merchantID, productID, uberItemID); err != nil {
		t.Fatalf("CreateProductMapping failed against postgres: %v", err)
	}

	found, err := repo.FindProductIDByUberItemID(ctx, merchantID, uberItemID)
	if err != nil {
		t.Fatalf("FindProductIDByUberItemID failed against postgres: %v", err)
	}
	if found == nil || *found != productID {
		t.Fatalf("expected %q, got %v", productID, found)
	}
}
