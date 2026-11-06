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

	if err := repo.UpdateMerchantBrandID(ctx, merchantID, "itest-loc-1", "itest-brand-2"); err != nil {
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

// TestDeliverooOnboardingRepository_Postgres : écritures de la connexion d'un
// site depuis le back-office (docs/marketplace-onboarding.md).
func TestDeliverooOnboardingRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "9394002"
	const otherMerchant = "9394003"
	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM integration_deliveroo WHERE merchant_id IN ($1, $2)`, merchantID, otherMerchant)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewDeliverooRepo(db)

	// Création : valeurs par défaut (acceptation automatique off, 60 min).
	if err := repo.ActivateSite(ctx, merchantID, merchantID, "brand-a"); err != nil {
		t.Fatalf("ActivateSite (insert): %v", err)
	}
	var enabled, autoAccept bool
	var prep int
	var brand string
	if err := db.QueryRowContext(ctx, `SELECT enabled, auto_accept_orders, preparation_time_minutes, brand_id FROM integration_deliveroo WHERE merchant_id = $1 AND location_id = $1`, merchantID).Scan(&enabled, &autoAccept, &prep, &brand); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !enabled || autoAccept || prep != 60 || brand != "brand-a" {
		t.Fatalf("new site row: enabled=%v auto=%v prep=%d brand=%s", enabled, autoAccept, prep, brand)
	}

	// Réactivation : réglages conservés, brand_id rafraîchi.
	if _, err := db.ExecContext(ctx, `UPDATE integration_deliveroo SET enabled = false, auto_accept_orders = true WHERE merchant_id = $1`, merchantID); err != nil {
		t.Fatal(err)
	}
	if owner, _ := repo.ActiveLocationOwner(ctx, merchantID); owner != "" {
		t.Fatalf("ActiveLocationOwner on disabled row = %q", owner)
	}
	if err := repo.ActivateSite(ctx, merchantID, merchantID, "brand-b"); err != nil {
		t.Fatalf("ActivateSite (reactivate): %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT enabled, auto_accept_orders, brand_id FROM integration_deliveroo WHERE merchant_id = $1 AND location_id = $1`, merchantID).Scan(&enabled, &autoAccept, &brand); err != nil {
		t.Fatal(err)
	}
	if !enabled || !autoAccept || brand != "brand-b" {
		t.Fatalf("reactivated row: enabled=%v auto=%v brand=%s", enabled, autoAccept, brand)
	}
	if owner, err := repo.ActiveLocationOwner(ctx, merchantID); err != nil || owner != merchantID {
		t.Fatalf("ActiveLocationOwner = (%q, %v)", owner, err)
	}

	// Compte principal = actif d'abord : un site inactif « plus petit » ne
	// passe pas devant.
	if _, err := db.ExecContext(ctx, `INSERT INTO integration_deliveroo (merchant_id, location_id, brand_id, enabled) VALUES ($1, '0-old', 'brand-old', false)`, merchantID); err != nil {
		t.Fatal(err)
	}
	if site, err := repo.GetSiteIDByMerchant(ctx, merchantID); err != nil || site != merchantID {
		t.Fatalf("GetSiteIDByMerchant = (%q, %v), want the active site", site, err)
	}
	if b, err := repo.GetBrandIDByMerchant(ctx, merchantID); err != nil || b != "brand-b" {
		t.Fatalf("GetBrandIDByMerchant = (%q, %v), want brand-b", b, err)
	}
	sites, err := repo.ListSites(ctx, merchantID)
	if err != nil || len(sites) != 2 || sites[0].LocationID != merchantID || !sites[0].Enabled || sites[1].Enabled {
		t.Fatalf("ListSites = (%+v, %v)", sites, err)
	}
}
