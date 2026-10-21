//go:build postgres_integration

package fiscal

import (
	"context"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

// Un paiement v2 scellé avec son saisisseur d'origine, puis réécrit au livreur
// comme le faisait FinalizeDeliveredStop avant la migration 188 : sa signature
// ne vérifie plus. FindReassignedPayments retrouve l'ancien user_id par
// l'empreinte, ApplyCollectorRepairs le remet et inscrit le livreur dans
// collected_by (paiement en main propre seulement).
//
// Prérequis : migration 188 (payments.collected_by).
func TestCollectorRepair_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var hasColumn bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'payments' AND column_name = 'collected_by')`).Scan(&hasColumn); err != nil {
		t.Fatalf("check migration 188: %v", err)
	}
	if !hasColumn {
		t.Skip("migration 188 (payments.collected_by) non appliquée sur cette base")
	}

	const (
		cashier = "itest-repair-cashier"
		driver  = "itest-repair-driver"
	)
	cleanup := func(mid string) {
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM payments WHERE merchant_id = $1`,
			`DELETE FROM delivery_session_order WHERE delivery_session_id IN (SELECT id FROM delivery_session WHERE merchant_id = $1)`,
			`DELETE FROM delivery_session WHERE merchant_id = $1`,
			`DELETE FROM users_rights WHERE merchant_id = $1`,
			`DELETE FROM orders WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-collrepair' LIMIT 1`).Scan(&oldID); err == nil {
		cleanup(strconv.FormatInt(oldID, 10))
	}
	var merchantInt int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest CollectorRepair', 'a', '1', 's', '75001', 'Paris', 'siret-collrepair', 'https://x', '06', 'tok-collrepair', 'UTC')
		RETURNING id`).Scan(&merchantInt); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(merchantInt, 10)
	t.Cleanup(func() { cleanup(merchantID) })

	mustExec := func(desc, q string, args ...interface{}) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("seed %s: %v", desc, err)
		}
	}
	mustExec("users_rights cashier", `INSERT INTO users_rights (user_id, merchant_id, token, enabled) VALUES ($1, $2, 'itest-repair-rt', TRUE)`, cashier, merchantID)

	var orderInt int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand_status, tva, ht, price, created_by)
		VALUES ($1, 1, 'ACCEPTED', 0, 800, 800, $2) RETURNING order_id`, merchantID, driver).Scan(&orderInt); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	orderID := strconv.FormatInt(orderInt, 10)

	var sessionID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO delivery_session (user_id, merchant_id, start_date, status) VALUES ($1, $2, now(), 'active') RETURNING id`, driver, merchantID).Scan(&sessionID); err != nil {
		t.Fatalf("seed delivery_session: %v", err)
	}
	mustExec("delivery_session_order", `INSERT INTO delivery_session_order (delivery_session_id, order_id, priority, status) VALUES ($1, $2, 1, 'delivered')`, sessionID, orderID)

	// Deux paiements scellés par le caissier : l'un en main propre (ES), l'autre en ligne (STRIPE).
	insertSealed := func(mop string, amount int) string {
		t.Helper()
		at := Now()
		payload, err := NewPaymentPayload(merchantID, orderID, amount, mop, "SALE", at, cashier, nil)
		if err != nil {
			t.Fatalf("payload: %v", err)
		}
		hash, sig, err := Seal(ChainPayments, GenesisHash, payload)
		if err != nil {
			t.Fatalf("seal: %v", err)
		}
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, operation_type, payment_date, previous_hash, hash, signature, hash_version)
			VALUES ($1, $2, $3, $4, $5, 'SALE', $6, $7, $8, $9, $10) RETURNING payment_id`,
			merchantID, cashier, orderID, amount, mop, at, GenesisHash, hash, sig, HashVersion).Scan(&id); err != nil {
			t.Fatalf("insert payment: %v", err)
		}
		return strconv.FormatInt(id, 10)
	}
	cash, online := insertSealed("ES", 500), insertSealed("STRIPE", 300)
	// Un paiement dont le livreur est bien le saisisseur : rien à réparer.
	own := func() string {
		t.Helper()
		at := Now()
		payload, _ := NewPaymentPayload(merchantID, orderID, 100, "CB", "SALE", at, driver, nil)
		hash, sig, _ := Seal(ChainPayments, GenesisHash, payload)
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, operation_type, payment_date, previous_hash, hash, signature, hash_version)
			VALUES ($1, $2, $3, 100, 'CB', 'SALE', $4, $5, $6, $7, $8) RETURNING payment_id`,
			merchantID, driver, orderID, at, GenesisHash, hash, sig, HashVersion).Scan(&id); err != nil {
			t.Fatalf("insert own payment: %v", err)
		}
		return strconv.FormatInt(id, 10)
	}()

	// L'ancien code réécrivait user_id au livreur sur tous les paiements de la commande.
	mustExec("simulate old reassignment", `UPDATE payments SET user_id = $1 WHERE payment_id IN ($2, $3)`, driver, cash, online)

	repairs, err := FindReassignedPayments(ctx, db)
	if err != nil {
		t.Fatalf("FindReassignedPayments: %v", err)
	}
	byID := map[string]CollectorRepair{}
	for _, r := range repairs {
		if r.MerchantID == merchantID {
			byID[r.PaymentID] = r
		}
	}
	if len(byID) != 2 {
		t.Fatalf("expected the 2 rewritten payments only, got %+v", byID)
	}
	if _, found := byID[own]; found {
		t.Fatal("a payment legitimately recorded by the delivery person must not be listed")
	}
	if r := byID[cash]; r.OriginalUserID != cashier || r.CollectedBy != driver {
		t.Fatalf("cash payment: original=%q collected_by=%q, want %q / %q", r.OriginalUserID, r.CollectedBy, cashier, driver)
	}
	if r := byID[online]; r.OriginalUserID != cashier || r.CollectedBy != "" {
		t.Fatalf("online payment: original=%q collected_by=%q, want %q / empty", r.OriginalUserID, r.CollectedBy, cashier)
	}

	fixed, err := ApplyCollectorRepairs(ctx, db, repairs)
	if err != nil {
		t.Fatalf("ApplyCollectorRepairs: %v", err)
	}
	if fixed < 2 {
		t.Fatalf("expected at least 2 repaired payments, got %d", fixed)
	}

	read := func(id string) (user string, collected *string) {
		t.Helper()
		if err := db.QueryRowContext(ctx, `SELECT user_id, collected_by FROM payments WHERE payment_id = $1`, id).Scan(&user, &collected); err != nil {
			t.Fatalf("read payment %s: %v", id, err)
		}
		return user, collected
	}
	if u, c := read(cash); u != cashier || c == nil || *c != driver {
		t.Fatalf("cash payment after repair: user_id=%q collected_by=%v", u, c)
	}
	if u, c := read(online); u != cashier || c != nil {
		t.Fatalf("online payment after repair: user_id=%q collected_by=%v", u, c)
	}

	// Rejouable : la réparation ne trouve plus rien pour cet établissement, et
	// l'empreinte de chaque paiement se recalcule de nouveau.
	again, err := FindReassignedPayments(ctx, db)
	if err != nil {
		t.Fatalf("second FindReassignedPayments: %v", err)
	}
	for _, r := range again {
		if r.MerchantID == merchantID {
			t.Fatalf("nothing left to repair, got %+v", r)
		}
	}
}
