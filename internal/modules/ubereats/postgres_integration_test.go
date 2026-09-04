//go:build postgres_integration

package ubereats

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

func TestUberEatsRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const storeID = "itest-ue-store-1"
	var merchantID string

	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM orderitems WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM integration_uber_eats WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, mid)
	}
	_, _ = db.ExecContext(ctx, `DELETE FROM external_tokens WHERE token_type = 'itest-uber'`)
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-ue' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	}
	t.Cleanup(func() {
		cleanupFor(merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM external_tokens WHERE token_type = 'itest-uber'`)
	})

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, lat, lng)
		VALUES ('ITest UE Merchant', 'a', '1', 's', '75001', 'Paris', 'siret-ue', 'https://x', '06', 'mtok-ue', 'Europe/Paris', 1, 2)
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO integration_uber_eats (merchant_id, store_id, pos_provisionning_refresh_token, pos_provisionning_token_expiration_date, delay_duration, auto_accept_orders, bearer_token)
		VALUES ($1, $2, 'refresh', now(), 0, true, 'bearer-tok')`, merchantID, storeID); err != nil {
		t.Fatalf("seed integration_uber_eats: %v", err)
	}

	repo := NewUberEatsRepository(db)

	// --- lookups (jointures merchant.id castées) ---
	gotMerchant, err := repo.GetMerchantIDFromStoreID(ctx, storeID)
	if err != nil || gotMerchant == nil || *gotMerchant != merchantID {
		t.Fatalf("GetMerchantIDFromStoreID = (%v, %v), want %s", gotMerchant, err, merchantID)
	}
	store, err := repo.GetStoreData(ctx, merchantID)
	if err != nil || store == nil || store.StoreID != storeID || store.Timezone != "Europe/Paris" || !store.AutoAcceptOrders {
		t.Fatalf("GetStoreData = (%+v, %v)", store, err)
	}
	if store.EstimatedPreparationTime != 30 {
		t.Fatalf("expected default prep time 30 (varchar scanné en int), got %d", store.EstimatedPreparationTime)
	}
	menuStore, err := repo.GetStoreInfoForMenu(ctx, merchantID, "")
	if err != nil || menuStore.BearerToken != "bearer-tok" {
		t.Fatalf("GetStoreInfoForMenu = (%+v, %v)", menuStore, err)
	}

	// --- tokens externes (upsert par dialecte + intervalle paramétré) ---
	if err := repo.SaveNewToken(ctx, "itest-uber", "tok-1", 3600); err != nil {
		t.Fatalf("SaveNewToken (insert) failed against postgres: %v", err)
	}
	if err := repo.SaveNewToken(ctx, "itest-uber", "tok-2", 7200); err != nil {
		t.Fatalf("SaveNewToken (update) failed against postgres: %v", err)
	}
	token, err := repo.GetCurrentToken(ctx, "itest-uber")
	if err != nil || token.AccessToken != "tok-2" {
		t.Fatalf("GetCurrentToken = (%+v, %v)", token, err)
	}

	// --- commandes ---
	var orderIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand, brand_order_id, brand_status, state, price, TVA, HT, created_by)
		VALUES ($1, 1, 'UBER_EATS', 'itest-ue-ord-1', 'PENDING', 'OPEN', 2500, 250, 2250, 'UBER_EATS')
		RETURNING order_id`, merchantID).Scan(&orderIntID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	orderID := strconv.FormatInt(orderIntID, 10)
	var productIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_Id, name, price, category, tva_in_id, tva_take_away_id, tva_delivery_id)
		VALUES ($1, 'itest-ue-prod', 0, 'itest', 0, 0, 0) RETURNING product_id`, merchantID).Scan(&productIntID); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM products WHERE merchant_Id = $1`, merchantID) })
	if _, err := db.ExecContext(ctx, `
		INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, price)
		VALUES ($1, $2, $3, 3, 800)`, orderIntID, productIntID, merchantID); err != nil {
		t.Fatalf("seed orderitems: %v", err)
	}

	meta, err := repo.GetOrderMetadata(ctx, orderID)
	if err != nil || meta.BrandOrderID != "itest-ue-ord-1" {
		t.Fatalf("GetOrderMetadata = (%+v, %v)", meta, err)
	}

	// CalculateAutoPrepTime : pas de données average_distribution_time → 0
	prep, err := repo.CalculateAutoPrepTime(ctx, merchantID, orderID)
	if err != nil || prep != 0 {
		t.Fatalf("CalculateAutoPrepTime = (%d, %v), want (0, nil)", prep, err)
	}

	// UpdateOrderAccepted (estimated_ready = now + interval paramétré)
	if err := repo.UpdateOrderAccepted(ctx, orderID, 20); err != nil {
		t.Fatalf("UpdateOrderAccepted failed against postgres: %v", err)
	}
	var brandStatus string
	var estimatedReady time.Time
	if err := db.QueryRowContext(ctx, `SELECT brand_status, estimated_ready FROM orders WHERE order_id = $1`, orderIntID).Scan(&brandStatus, &estimatedReady); err != nil {
		t.Fatalf("read back accepted order: %v", err)
	}
	if brandStatus != "ACCEPTED" {
		t.Fatalf("expected ACCEPTED, got %q", brandStatus)
	}
	if d := time.Until(estimatedReady); d < 18*time.Minute || d > 22*time.Minute {
		t.Fatalf("expected estimated_ready ≈ now+20min, got %v (delta %v)", estimatedReady, d)
	}

	// Statuts
	if err := repo.SetOrderStatusDenied(ctx, orderID); err != nil {
		t.Fatalf("SetOrderStatusDenied failed against postgres: %v", err)
	}
	if err := repo.SetOrderStatusCanceled(ctx, orderID); err != nil {
		t.Fatalf("SetOrderStatusCanceled failed against postgres: %v", err)
	}
	if err := repo.SetOrderStatusReady(ctx, orderID); err != nil {
		t.Fatalf("SetOrderStatusReady failed against postgres: %v", err)
	}
	var distributedQty int
	if err := db.QueryRowContext(ctx, `SELECT distributed_quantity FROM orderitems WHERE order_id = $1`, orderIntID).Scan(&distributedQty); err != nil {
		t.Fatalf("read back orderitems: %v", err)
	}
	if distributedQty != 3 {
		t.Fatalf("expected distributed_quantity = quantity (3), got %d", distributedQty)
	}

	// SyncOrderState CLOSED : la variante MySQL multi-table devient 2 requêtes PG
	if err := repo.SyncOrderState(ctx, "itest-ue-ord-1", "COMPLETED", "CLOSED", "ACCEPTED", sql.NullInt64{}); err != nil {
		t.Fatalf("SyncOrderState failed against postgres: %v", err)
	}
	var state string
	var deliveredOn *time.Time
	if err := db.QueryRowContext(ctx, `SELECT state, delivered_on FROM orders WHERE order_id = $1`, orderIntID).Scan(&state, &deliveredOn); err != nil {
		t.Fatalf("read back synced order: %v", err)
	}
	if state != "CLOSED" || deliveredOn == nil {
		t.Fatalf("expected CLOSED + delivered_on set, got %s / %v", state, deliveredOn)
	}

	// HandleOrderNotFound (CASE brand_status)
	if err := repo.HandleOrderNotFound(ctx, "itest-ue-ord-1"); err != nil {
		t.Fatalf("HandleOrderNotFound failed against postgres: %v", err)
	}

	// --- réglages boutique ---
	delayUntil := time.Now().UTC().Add(30 * time.Minute).Truncate(time.Second)
	if err := repo.UpdateBusyModeData(ctx, storeID, delayUntil, 30); err != nil {
		t.Fatalf("UpdateBusyModeData failed against postgres: %v", err)
	}
	if err := repo.UpdatePreparationTime(ctx, merchantID, "", 25, false); err != nil {
		t.Fatalf("UpdatePreparationTime (manual) failed against postgres: %v", err)
	}
	var prepStr, lastPrepStr string
	if err := db.QueryRowContext(ctx, `SELECT estimated_preparation_time, last_estimated_preparation_time FROM integration_uber_eats WHERE merchant_id = $1`, merchantID).Scan(&prepStr, &lastPrepStr); err != nil {
		t.Fatalf("read back prep time: %v", err)
	}
	if prepStr != "25" || lastPrepStr != "25" {
		t.Fatalf("expected prep 25/25, got %s/%s", prepStr, lastPrepStr)
	}
	// isAuto=true : estimated_preparation_time conservé, last_* mis à jour
	if err := repo.UpdatePreparationTime(ctx, merchantID, "", 40, true); err != nil {
		t.Fatalf("UpdatePreparationTime (auto) failed against postgres: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT estimated_preparation_time, last_estimated_preparation_time FROM integration_uber_eats WHERE merchant_id = $1`, merchantID).Scan(&prepStr, &lastPrepStr); err != nil {
		t.Fatalf("read back prep time (auto): %v", err)
	}
	if prepStr != "25" || lastPrepStr != "40" {
		t.Fatalf("expected prep 25/40, got %s/%s", prepStr, lastPrepStr)
	}

	closedUntil := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	if err := repo.UpdateStoreClosure(ctx, storeID, closedUntil); err != nil {
		t.Fatalf("UpdateStoreClosure failed against postgres: %v", err)
	}

	// --- EnableIntegration / DisableIntegration : bug préexistant (colonnes
	// access_token / is_active / updated_at inexistantes dans les deux
	// dialectes) — l'erreur SQL est attendue, à parité avec MySQL ---
	if err := repo.EnableIntegration(ctx, merchantID, storeID, "at", "rt"); err == nil {
		t.Fatal("expected EnableIntegration to fail (columns missing in both dialects)")
	}
	if err := repo.DisableIntegration(ctx, merchantID, ""); err == nil {
		t.Fatal("expected DisableIntegration to fail (columns missing in both dialects)")
	}
}

// TestUberEatsRepository_SyncOrderState_CancelledByType_Postgres verifies
// PROMPT 11 §2's fix to the two reconciliation write paths
// (SyncOrderState/HandleOrderNotFound, both called from RecoverOrderState
// after a failed outbound Uber Eats API call — see service.go): they must set
// cancelled_by_type=PLATFORM when nothing has classified the cancellation
// yet, but never overwrite a STAFF classification already written
// synchronously by order_life_cycle.DenyOrderLocal/DeleteOrderLocal before
// the outbound API call that triggered this reconciliation.
func TestUberEatsRepository_SyncOrderState_CancelledByType_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "itest-ue-cbt-m1"
	cleanup := func() { _, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, merchantID) }
	cleanup()
	t.Cleanup(cleanup)

	repo := NewUberEatsRepository(db)

	seedOrder := func(t *testing.T, brandOrderID string, orderNum int, initialCancelledByType *string) int64 {
		t.Helper()
		var orderID int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand, brand_order_id, brand_status, state, price, TVA, HT, created_by, cancelled_by_type)
			VALUES ($1, $2, 'UBER_EATS', $3, 'PENDING', 'OPEN', 1000, 0, 1000, 'UBER_EATS', $4)
			RETURNING order_id`, merchantID, orderNum, brandOrderID, initialCancelledByType).Scan(&orderID); err != nil {
			t.Fatalf("seed order: %v", err)
		}
		return orderID
	}
	readCancelledByType := func(t *testing.T, orderID int64) string {
		t.Helper()
		var v string
		if err := db.QueryRowContext(ctx, `SELECT cancelled_by_type FROM orders WHERE order_id = $1`, orderID).Scan(&v); err != nil {
			t.Fatalf("read back cancelled_by_type: %v", err)
		}
		return v
	}

	t.Run("SyncOrderState DENIED, previously unset -> PLATFORM", func(t *testing.T) {
		orderID := seedOrder(t, "itest-ue-cbt-denied", 1, nil)
		if err := repo.SyncOrderState(ctx, "itest-ue-cbt-denied", "DENIED", "CLOSED", "DENIED", sql.NullInt64{Int64: 40, Valid: true}); err != nil {
			t.Fatalf("SyncOrderState: %v", err)
		}
		if got := readCancelledByType(t, orderID); got != "PLATFORM" {
			t.Fatalf("expected cancelled_by_type=PLATFORM, got %q", got)
		}
	})

	t.Run("SyncOrderState CANCELED, already STAFF -> preserved", func(t *testing.T) {
		staff := "STAFF"
		orderID := seedOrder(t, "itest-ue-cbt-canceled-staff", 2, &staff)
		if err := repo.SyncOrderState(ctx, "itest-ue-cbt-canceled-staff", "CANCELED", "CLOSED", "ACCEPTED", sql.NullInt64{Int64: 39, Valid: true}); err != nil {
			t.Fatalf("SyncOrderState: %v", err)
		}
		if got := readCancelledByType(t, orderID); got != "STAFF" {
			t.Fatalf("SyncOrderState must not overwrite an already-set cancelled_by_type — got %q, want STAFF (preserved)", got)
		}
	})

	t.Run("SyncOrderState COMPLETED does not classify a cancellation", func(t *testing.T) {
		orderID := seedOrder(t, "itest-ue-cbt-completed", 3, nil)
		if err := repo.SyncOrderState(ctx, "itest-ue-cbt-completed", "COMPLETED", "CLOSED", "ACCEPTED", sql.NullInt64{}); err != nil {
			t.Fatalf("SyncOrderState: %v", err)
		}
		var got sql.NullString
		if err := db.QueryRowContext(ctx, `SELECT cancelled_by_type FROM orders WHERE order_id = $1`, orderID).Scan(&got); err != nil {
			t.Fatalf("read back: %v", err)
		}
		if got.Valid {
			t.Fatalf("a COMPLETED order is not a cancellation, expected cancelled_by_type still NULL, got %q", got.String)
		}
	})

	t.Run("HandleOrderNotFound, previously unset -> PLATFORM", func(t *testing.T) {
		orderID := seedOrder(t, "itest-ue-cbt-notfound", 4, nil)
		if err := repo.HandleOrderNotFound(ctx, "itest-ue-cbt-notfound"); err != nil {
			t.Fatalf("HandleOrderNotFound: %v", err)
		}
		if got := readCancelledByType(t, orderID); got != "PLATFORM" {
			t.Fatalf("expected cancelled_by_type=PLATFORM, got %q", got)
		}
	})

	t.Run("HandleOrderNotFound, already STAFF -> preserved", func(t *testing.T) {
		staff := "STAFF"
		orderID := seedOrder(t, "itest-ue-cbt-notfound-staff", 5, &staff)
		if err := repo.HandleOrderNotFound(ctx, "itest-ue-cbt-notfound-staff"); err != nil {
			t.Fatalf("HandleOrderNotFound: %v", err)
		}
		if got := readCancelledByType(t, orderID); got != "STAFF" {
			t.Fatalf("HandleOrderNotFound must not overwrite an already-set cancelled_by_type — got %q, want STAFF (preserved)", got)
		}
	})
}
