//go:build postgres_integration

package stripeclient

import (
	"context"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

// Vérification réelle de terminalPaymentStore (mapping order_id <->
// payment_intent_id dans stripe_payments, remplace l'ancien mapping Redis —
// voir docs/KIOSK_DECISIONS.md, "Retrait de Redis du mapping
// order_id/payment_intent_id").
func TestTerminalPaymentStore_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	cleanup := func() {
		if merchantIntID != 0 {
			mid := strconv.FormatInt(merchantIntID, 10)
			_, _ = db.ExecContext(ctx, `DELETE FROM stripe_payments WHERE order_id IN (SELECT order_id FROM orders WHERE merchant_id = $1)`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, mid)
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-itest-terminal' LIMIT 1`).Scan(&oldID); err == nil {
		merchantIntID = oldID
		cleanup()
	}
	t.Cleanup(func() { cleanup() })

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Terminal', 'a', '1', 's', '75001', 'Paris', 'siret-itest-terminal', 'https://x', '06', 'mtok-itest-terminal', 'UTC')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(merchantIntID, 10)
	otherMerchantID := strconv.FormatInt(merchantIntID+1, 10)

	var orderIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand_status, price, TVA, HT, created_by)
		VALUES ($1, 1, 'PENDING_CARD_PAYMENT', 1000, 100, 900, 'itest')
		RETURNING order_id`, merchantID).Scan(&orderIntID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	orderID := strconv.FormatInt(orderIntID, 10)

	store := NewTerminalPaymentStore(db)
	const piID = "pi_itest_terminal_store"

	// --- CreateMapping : ligne pré-créée, payment_id NULL, statut par défaut ---
	if err := store.CreateMapping(ctx, orderID, piID); err != nil {
		t.Fatalf("CreateMapping: %v", err)
	}
	var status string
	var paymentIDNull bool
	if err := db.QueryRowContext(ctx, `SELECT payment_intent_status, payment_id IS NULL FROM stripe_payments WHERE payment_intent_id = $1`, piID).Scan(&status, &paymentIDNull); err != nil {
		t.Fatalf("read back created mapping: %v", err)
	}
	if status != "REQUIRES_CONFIRMATION" || !paymentIDNull {
		t.Fatalf("CreateMapping row = (status=%q, payment_id NULL=%v), want (REQUIRES_CONFIRMATION, true)", status, paymentIDNull)
	}

	// --- GetActivePaymentIntentForOrder : trouvé pour le bon merchant ---
	gotPI, found, err := store.GetActivePaymentIntentForOrder(ctx, merchantID, orderID)
	if err != nil || !found || gotPI != piID {
		t.Fatalf("GetActivePaymentIntentForOrder = (%q, %v, %v), want (%q, true, nil)", gotPI, found, err, piID)
	}

	// --- GetActivePaymentIntentForOrder : pas trouvé pour un autre merchant
	// (vérification d'appartenance via jointure orders) ---
	if _, found, err := store.GetActivePaymentIntentForOrder(ctx, otherMerchantID, orderID); err != nil || found {
		t.Fatalf("GetActivePaymentIntentForOrder(wrong merchant) = (found=%v, err=%v), want (false, nil)", found, err)
	}

	// --- GetMerchantIDForPaymentIntent ---
	gotMerchant, found, err := store.GetMerchantIDForPaymentIntent(ctx, piID)
	if err != nil || !found || gotMerchant != merchantID {
		t.Fatalf("GetMerchantIDForPaymentIntent = (%q, %v, %v), want (%q, true, nil)", gotMerchant, found, err, merchantID)
	}
	if _, found, err := store.GetMerchantIDForPaymentIntent(ctx, "pi_unknown"); err != nil || found {
		t.Fatalf("GetMerchantIDForPaymentIntent(unknown) = (found=%v, err=%v), want (false, nil)", found, err)
	}

	// --- MarkPaymentIntentStatus : sort de l'ensemble "actif" ---
	if err := store.MarkPaymentIntentStatus(ctx, piID, "CANCELED"); err != nil {
		t.Fatalf("MarkPaymentIntentStatus: %v", err)
	}
	if _, found, err := store.GetActivePaymentIntentForOrder(ctx, merchantID, orderID); err != nil || found {
		t.Fatalf("GetActivePaymentIntentForOrder after CANCELED = (found=%v, err=%v), want (false, nil)", found, err)
	}

	// --- GetActivePaymentIntentForOrder : aucune ligne pour une commande inconnue ---
	if _, found, err := store.GetActivePaymentIntentForOrder(ctx, merchantID, "999999999"); err != nil || found {
		t.Fatalf("GetActivePaymentIntentForOrder(commande inconnue) = (found=%v, err=%v), want (false, nil)", found, err)
	}
}
