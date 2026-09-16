//go:build postgres_integration

package stripeclient

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

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
			_, _ = db.ExecContext(ctx, `DELETE FROM stripe_accounts WHERE merchant_id = $1`, mid)
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

	// --- CountPaymentIntentAttemptsForOrder : sert de numéro de tentative pour
	// la clé d'idempotence Stripe de CreateTerminalPaymentIntent, voir
	// docs/KIOSK_DECISIONS.md. Une seule ligne créée jusqu'ici (piID, ci-dessus,
	// désormais CANCELED) -> 1 tentative déjà comptée. ---
	if n, err := store.CountPaymentIntentAttemptsForOrder(ctx, merchantID, orderID); err != nil || n != 1 {
		t.Fatalf("CountPaymentIntentAttemptsForOrder = (%d, %v), want (1, nil)", n, err)
	}
	const secondPI = "itest-pi-attempt-2"
	if err := store.CreateMapping(ctx, orderID, secondPI); err != nil {
		t.Fatalf("CreateMapping (second attempt): %v", err)
	}
	if n, err := store.CountPaymentIntentAttemptsForOrder(ctx, merchantID, orderID); err != nil || n != 2 {
		t.Fatalf("CountPaymentIntentAttemptsForOrder (after second attempt) = (%d, %v), want (2, nil)", n, err)
	}
	// Un autre merchant/commande ne doit jamais compter dans ce total (jointure orders).
	if n, err := store.CountPaymentIntentAttemptsForOrder(ctx, otherMerchantID, orderID); err != nil || n != 0 {
		t.Fatalf("CountPaymentIntentAttemptsForOrder(wrong merchant) = (%d, %v), want (0, nil)", n, err)
	}

	// --- Correctif retry (docs/KIOSK_DECISIONS.md) : un statut local FAILED
	// n'exclut plus GetActivePaymentIntentForOrder — seuls CANCELED/CAPTURED/
	// TO_REFUND excluent désormais. secondPI est encore REQUIRES_CONFIRMATION ;
	// on le marque FAILED puis on vérifie qu'il reste "actif". ---
	if err := store.MarkPaymentIntentStatus(ctx, secondPI, "FAILED"); err != nil {
		t.Fatalf("MarkPaymentIntentStatus(FAILED): %v", err)
	}
	if gotPI, found, err := store.GetActivePaymentIntentForOrder(ctx, merchantID, orderID); err != nil || !found || gotPI != secondPI {
		t.Fatalf("GetActivePaymentIntentForOrder after FAILED = (%q, %v, %v), want (%q, true, nil) -- FAILED must stay active for retry-reuse", gotPI, found, err, secondPI)
	}
	// CAPTURED/TO_REFUND, eux, restent bien exclus.
	if err := store.MarkPaymentIntentStatus(ctx, secondPI, "CAPTURED"); err != nil {
		t.Fatalf("MarkPaymentIntentStatus(CAPTURED): %v", err)
	}
	if _, found, err := store.GetActivePaymentIntentForOrder(ctx, merchantID, orderID); err != nil || found {
		t.Fatalf("GetActivePaymentIntentForOrder after CAPTURED = (found=%v, err=%v), want (false, nil)", found, err)
	}

	// --- GetLatestPaymentIntentRecordForOrder : le plus récent quel que soit
	// son statut (secondPI, CAPTURED), avec kiosk_id + détails carte nuls tant
	// que rien n'a été posé. ---
	record, found, err := store.GetLatestPaymentIntentRecordForOrder(ctx, merchantID, orderID)
	if err != nil || !found || record.PaymentIntentID != secondPI || record.LocalStatus != "CAPTURED" {
		t.Fatalf("GetLatestPaymentIntentRecordForOrder = (%+v, %v, %v), want PaymentIntentID=%q LocalStatus=CAPTURED", record, found, err, secondPI)
	}
	if record.KioskID != nil || record.CardBrand != nil {
		t.Fatalf("GetLatestPaymentIntentRecordForOrder: expected nil KioskID/CardBrand before SetKioskIDForPaymentIntent/SetTerminalCardDetails, got %+v", record)
	}

	// --- SetKioskIDForPaymentIntent : posé au dispatch, lu par GetLatestPaymentIntentRecordForOrder ---
	if err := store.SetKioskIDForPaymentIntent(ctx, secondPI, "kiosk_itest_1"); err != nil {
		t.Fatalf("SetKioskIDForPaymentIntent: %v", err)
	}
	record, found, err = store.GetLatestPaymentIntentRecordForOrder(ctx, merchantID, orderID)
	if err != nil || !found || record.KioskID == nil || *record.KioskID != "kiosk_itest_1" {
		t.Fatalf("GetLatestPaymentIntentRecordForOrder after SetKioskIDForPaymentIntent = (%+v, %v, %v), want KioskID=kiosk_itest_1", record, found, err)
	}

	// --- GetLatestPaymentIntentRecordForOrder : aucune ligne pour une commande inconnue ---
	if _, found, err := store.GetLatestPaymentIntentRecordForOrder(ctx, merchantID, "999999999"); err != nil || found {
		t.Fatalf("GetLatestPaymentIntentRecordForOrder(commande inconnue) = (found=%v, err=%v), want (false, nil)", found, err)
	}

	// --- GetTerminalLocationID (TerminalAccountStore) ---
	accountStore := NewTerminalAccountStore(db)
	if _, err := db.ExecContext(ctx, `INSERT INTO stripe_accounts (account_id, merchant_id, verification_status) VALUES ($1, $2, 'verified')`, "acct_itest_terminal", merchantID); err != nil {
		t.Fatalf("seed stripe_accounts: %v", err)
	}
	if loc, err := accountStore.GetTerminalLocationID(ctx, merchantID); err != nil || loc != nil {
		t.Fatalf("GetTerminalLocationID (no location set) = (%v, %v), want (nil, nil)", loc, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE stripe_accounts SET terminal_location_id = $1 WHERE merchant_id = $2`, "tml_itest_1", merchantID); err != nil {
		t.Fatalf("set terminal_location_id: %v", err)
	}
	if loc, err := accountStore.GetTerminalLocationID(ctx, merchantID); err != nil || loc == nil || *loc != "tml_itest_1" {
		t.Fatalf("GetTerminalLocationID = (%v, %v), want tml_itest_1", loc, err)
	}
	if loc, err := accountStore.GetTerminalLocationID(ctx, otherMerchantID); err != nil || loc != nil {
		t.Fatalf("GetTerminalLocationID(no stripe_accounts row) = (%v, %v), want (nil, nil)", loc, err)
	}
}

// TestWithOrderLock_SerializesConcurrentCalls_Postgres valide le primitif de
// verrouillage lui-même (pg_advisory_xact_lock) dont dépend
// resolveOrCreatePaymentIntent/ProcessPaymentIntentOnReader pour fermer le
// gap de concurrence "deux dispatches concurrents pour la même commande" —
// voir docs/KIOSK_DECISIONS.md. Deux goroutines appellent WithOrderLock sur
// le MÊME orderID ; leurs fenêtres d'exécution ne doivent jamais se
// chevaucher.
func TestWithOrderLock_SerializesConcurrentCalls_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	store := NewTerminalPaymentStore(db)

	const orderID = "itest-lock-order-1"
	var mu sync.Mutex
	var windows []([2]time.Time)

	run := func() {
		_ = store.WithOrderLock(ctx, orderID, func(txCtx context.Context) error {
			start := time.Now()
			time.Sleep(150 * time.Millisecond)
			end := time.Now()
			mu.Lock()
			windows = append(windows, [2]time.Time{start, end})
			mu.Unlock()
			return nil
		})
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); run() }()
	go func() { defer wg.Done(); run() }()
	wg.Wait()

	if len(windows) != 2 {
		t.Fatalf("expected 2 recorded windows, got %d", len(windows))
	}
	a, b := windows[0], windows[1]
	overlap := a[0].Before(b[1]) && b[0].Before(a[1])
	if overlap {
		t.Fatalf("WithOrderLock did not serialize: windows overlapped (a=%v-%v, b=%v-%v)", a[0], a[1], b[0], b[1])
	}
}

// TestWithOrderLock_CommitsWritesEvenWhenFnBodyWouldFailAfter_Postgres valide
// le MÉCANISME dont ProcessPaymentIntentOnReader dépend pour sa règle de
// commit (docs/TERMINAL_SERVER_DRIVEN_CONTRACT.md, section Dispatch) : un
// échec survenu APRÈS des écritures réussies, si fn retourne nil malgré cet
// échec (capturé ailleurs par l'appelant, comme le fait
// ProcessPaymentIntentOnReader avec dispatchErr), laisse ces écritures
// commitées. Ce test n'invoque pas ProcessPaymentIntentOnReader lui-même
// (appellerait un vrai reader Stripe) — il couvre le patron générique
// garanti par WithOrderLock.
func TestWithOrderLock_CommitsWritesEvenWhenFnBodyWouldFailAfter_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Lock Commit', 'a', '1', 's', '75001', 'Paris', 'siret-itest-lockc', 'https://x', '06', 'mtok-itest-lockc', 'UTC')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(merchantIntID, 10)
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = db.ExecContext(bg, `DELETE FROM stripe_payments WHERE order_id IN (SELECT order_id FROM orders WHERE merchant_id = $1)`, merchantID)
		_, _ = db.ExecContext(bg, `DELETE FROM orders WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(bg, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
	})

	var orderIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand_status, price, TVA, HT, created_by)
		VALUES ($1, 1, 'PENDING_CARD_PAYMENT', 1000, 100, 900, 'itest')
		RETURNING order_id`, merchantID).Scan(&orderIntID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	orderID := strconv.FormatInt(orderIntID, 10)

	store := NewTerminalPaymentStore(db)
	const piID = "pi_itest_lock_commit"
	const kioskID = "kiosk_itest_lock_commit"

	var dispatchErr error
	lockErr := store.WithOrderLock(ctx, orderID, func(txCtx context.Context) error {
		if err := store.CreateMapping(txCtx, orderID, piID); err != nil {
			return err
		}
		if err := store.SetKioskIDForPaymentIntent(txCtx, piID, kioskID); err != nil {
			return err
		}
		// Simule un échec de dispatch (reader offline/busy) : capturé dans
		// dispatchErr, mais fn retourne nil -- même patron que
		// ProcessPaymentIntentOnReader.
		dispatchErr = context.DeadlineExceeded
		return nil
	})
	if lockErr != nil {
		t.Fatalf("WithOrderLock returned an error, want nil (the simulated dispatch failure must not roll back): %v", lockErr)
	}
	if dispatchErr == nil {
		t.Fatal("expected dispatchErr to be captured (sanity check on the test itself)")
	}

	record, found, err := store.GetLatestPaymentIntentRecordForOrder(ctx, merchantID, orderID)
	if err != nil || !found || record.PaymentIntentID != piID {
		t.Fatalf("GetLatestPaymentIntentRecordForOrder = (%+v, %v, %v), want a committed row for pi=%q", record, found, err, piID)
	}
	if record.KioskID == nil || *record.KioskID != kioskID {
		t.Fatalf("expected kiosk_id=%q to be committed despite the simulated dispatch failure, got %+v", kioskID, record)
	}
}
