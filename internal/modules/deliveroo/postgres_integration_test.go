//go:build postgres_integration

package deliveroo

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

func TestDeliverooRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "9394001"
	const brandOrderID = "itest-dlv-brand-order-1"
	var orderID int64

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM orderitems WHERE order_id = $1`, orderID)
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM integration_deliveroo WHERE merchant_id = $1`, merchantID)
	}
	t.Cleanup(func() { cleanup() })

	future := time.Now().UTC().Add(2 * time.Hour)
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand, brand_order_id, brand_status, price, tva, ht, created_by, estimated_ready)
		VALUES ($1, 1, 'DELIVEROO', $2, 'placed', 1200, 100, 1100, 'itest', $3)
		RETURNING order_id`, merchantID, brandOrderID, future).Scan(&orderID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, price)
		VALUES ($1, 9001, $2, 2, 600)`, orderID, merchantID); err != nil {
		t.Fatalf("seed orderitem: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO integration_deliveroo (merchant_id, location_id, brand_id)
		VALUES ($1, 'itest-loc-1', 'itest-brand-1')`, merchantID); err != nil {
		t.Fatalf("seed integration_deliveroo: %v", err)
	}

	repo := NewDeliverooRepo(db)

	gotBrandOrderID, err := repo.GetBrandOrderID(ctx, itoa(orderID))
	if err != nil {
		t.Fatalf("GetBrandOrderID failed against postgres: %v", err)
	}
	if gotBrandOrderID != brandOrderID {
		t.Fatalf("expected %q, got %q", brandOrderID, gotBrandOrderID)
	}

	gotBrandOrderID2, gotMerchantIntID, err := repo.GetBrandOrderIDAndMerchant(ctx, int(orderID))
	if err != nil {
		t.Fatalf("GetBrandOrderIDAndMerchant failed against postgres: %v", err)
	}
	if gotBrandOrderID2 != brandOrderID || itoa(int64(gotMerchantIntID)) != merchantID {
		t.Fatalf("unexpected result: brandOrderID=%q merchantID=%d (want %q)", gotBrandOrderID2, gotMerchantIntID, merchantID)
	}

	// UpdateAcceptedStatus: DATE_ADD(UTC_TIMESTAMP(), INTERVAL 30 MINUTE) fix.
	// estimated_ready is 2h out, well past the 30-minute deadline -> 'scheduled'.
	if err := repo.UpdateAcceptedStatus(ctx, brandOrderID); err != nil {
		t.Fatalf("UpdateAcceptedStatus failed against postgres: %v", err)
	}
	var brandStatus string
	if err := db.QueryRowContext(ctx, `SELECT brand_status FROM orders WHERE order_id = $1`, orderID).Scan(&brandStatus); err != nil {
		t.Fatalf("read back after UpdateAcceptedStatus: %v", err)
	}
	if brandStatus != "scheduled" {
		t.Fatalf("expected scheduled (estimated_ready > now+30min), got %q", brandStatus)
	}

	// MarkDeliverooDeliveryStarted: boolean literal fixes (isDistributed) +
	// dbx.UTCNow(). Pre-existing bug: orders.dateDeparture does not exist on
	// either dialect (same failure both sides, documented in Tier2 report) —
	// so this call is expected to fail, and the failure must be that specific
	// column, not a regression from the isDistributed/UTCNow fixes.
	if _, err := repo.MarkDeliverooDeliveryStarted(ctx, brandOrderID); err == nil {
		t.Fatal("expected MarkDeliverooDeliveryStarted to fail on the pre-existing dateDeparture bug")
	} else if !containsFold(err.Error(), "departure") {
		t.Fatalf("expected a dateDeparture error, got: %v", err)
	}

	// UpdateReadyForHandoffLocal / MarkOrderCanceledLocal: dbx.UTCNow() only,
	// no missing-column issue.
	if err := repo.UpdateReadyForHandoffLocal(ctx, itoa(orderID)); err != nil {
		t.Fatalf("UpdateReadyForHandoffLocal failed against postgres: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT brand_status FROM orders WHERE order_id = $1`, orderID).Scan(&brandStatus); err != nil {
		t.Fatalf("read back after UpdateReadyForHandoffLocal: %v", err)
	}
	if brandStatus != "READY_FOR_COLLECTION" {
		t.Fatalf("expected READY_FOR_COLLECTION, got %q", brandStatus)
	}

	if err := repo.MarkOrderCanceledLocal(ctx, itoa(orderID)); err != nil {
		t.Fatalf("MarkOrderCanceledLocal failed against postgres: %v", err)
	}
	var status int
	if err := db.QueryRowContext(ctx, `SELECT brand_status, status FROM orders WHERE order_id = $1`, orderID).Scan(&brandStatus, &status); err != nil {
		t.Fatalf("read back after MarkOrderCanceledLocal: %v", err)
	}
	if brandStatus != "CANCELED" || status != -1 {
		t.Fatalf("expected CANCELED/-1, got %q/%d", brandStatus, status)
	}

	// GetBrandIDByMerchant / UpdateMerchantBrandID / GetSiteIDByMerchant.
	brandID, err := repo.GetBrandIDByMerchant(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetBrandIDByMerchant failed against postgres: %v", err)
	}
	if brandID != "itest-brand-1" {
		t.Fatalf("expected itest-brand-1, got %q", brandID)
	}

	if err := repo.UpdateMerchantBrandID(ctx, merchantID, "itest-brand-2"); err != nil {
		t.Fatalf("UpdateMerchantBrandID failed against postgres: %v", err)
	}
	brandID, err = repo.GetBrandIDByMerchant(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetBrandIDByMerchant (after update) failed: %v", err)
	}
	if brandID != "itest-brand-2" {
		t.Fatalf("expected itest-brand-2, got %q", brandID)
	}

	siteID, err := repo.GetSiteIDByMerchant(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetSiteIDByMerchant failed against postgres: %v", err)
	}
	if siteID != "itest-loc-1" {
		t.Fatalf("expected itest-loc-1, got %q", siteID)
	}
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
