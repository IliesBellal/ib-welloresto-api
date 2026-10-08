//go:build postgres_integration

package fiscal

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

// Annulation de paiement (lot C conformité caisse, R4 et R5 :
// docs/attestation-conformite-03-lot-C-brief.md).
func TestCancelPayments_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const siret = "siret-itest-paycxl"

	cleanup := func(mid string) {
		for _, q := range []string{
			`DELETE FROM audit_logs WHERE merchant_id = $1`,
			`DELETE FROM payments WHERE merchant_id = $1`,
			`DELETE FROM orders WHERE merchant_id = $1`,
			`DELETE FROM cash_registers WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id::text = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var old int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = $1 LIMIT 1`, siret).Scan(&old); err == nil {
		cleanup(strconv.FormatInt(old, 10))
	}
	var mid int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Payment Cancel', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', $2, 'Europe/Paris')
		RETURNING id`, siret, "mt-paycxl").Scan(&mid); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(mid, 10)
	t.Cleanup(func() { cleanup(merchantID) })

	register := func(closed bool) string {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO cash_registers (merchant_id, cash_desk_id, device_id, user_id, cash_fund, start_date, closure_comment, closed, end_date)
			VALUES ($1, 1, 'itest-cancel', 'itest', 0, now(), '', $2, CASE WHEN $2 THEN now() END) RETURNING cash_register_id`,
			merchantID, closed).Scan(&id); err != nil {
			t.Fatalf("seed register: %v", err)
		}
		return strconv.FormatInt(id, 10)
	}
	orderNum := 0
	order := func(state string) string {
		t.Helper()
		orderNum++
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, price, TVA, HT, created_by)
			VALUES ($1, $2, 'WELLO_RESTO', 'PENDING', $3, 3000, 0, 3000, 'itest') RETURNING order_id`,
			merchantID, orderNum, state).Scan(&id); err != nil {
			t.Fatalf("seed order: %v", err)
		}
		return strconv.FormatInt(id, 10)
	}
	payDate := time.Date(2026, 10, 7, 12, 30, 0, 123456000, time.UTC)
	payment := func(orderID, mop string, amount int, registerID *string) string {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, operation_type, enabled, payment_date, cash_register_id)
			VALUES ($1, 'itest', $2, $3, $4, 'SALE', TRUE, $5, $6) RETURNING payment_id`,
			merchantID, orderID, amount, mop, payDate, registerID).Scan(&id); err != nil {
			t.Fatalf("seed payment: %v", err)
		}
		return strconv.FormatInt(id, 10)
	}
	type paymentRow struct {
		amount   int64
		mop, op  string
		date     time.Time
		register sql.NullString
		enabled  bool
	}
	readPayment := func(id string) paymentRow {
		t.Helper()
		var p paymentRow
		if err := db.QueryRowContext(ctx, `
			SELECT amount, mop, operation_type, payment_date, cash_register_id, enabled FROM payments WHERE payment_id = $1`, id).
			Scan(&p.amount, &p.mop, &p.op, &p.date, &p.register, &p.enabled); err != nil {
			t.Fatalf("read payment %s: %v", id, err)
		}
		return p
	}
	auditCount := func() int {
		t.Helper()
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_logs WHERE merchant_id = $1`, merchantID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	cancel := func(orderID, paymentID string) ([]CancelledPayment, error) {
		p, err := CancelPayment(ctx, db, PaymentCancellation{
			MerchantID: merchantID, OrderID: orderID, PaymentID: paymentID,
			Source: CancelSourceStaff, UserID: "itest-user", Reason: "carte refusée",
		})
		if p == nil {
			return nil, err
		}
		return []CancelledPayment{*p}, err
	}
	cancelOrder := func(c PaymentCancellation) ([]CancelledPayment, error) {
		return CancelOrderPayments(ctx, db, c, nil)
	}

	openReg, closedReg := register(false), register(true)

	t.Run("commande et registre ouverts : annulé, trace scellée, paiement d'origine intact", func(t *testing.T) {
		o := order("OPEN")
		p := payment(o, "CB", 1200, &openReg)
		before := readPayment(p)
		got, err := cancel(o, p)
		if err != nil || len(got) != 1 || strconv.FormatInt(got[0].PaymentID, 10) != p || got[0].Amount != 1200 {
			t.Fatalf("CancelPayments = (%+v, %v)", got, err)
		}
		after := readPayment(p)
		if after.enabled {
			t.Fatal("payment still enabled")
		}
		after.enabled = true
		if after != before {
			t.Fatalf("payment changed beyond enabled: before %+v, after %+v", before, after)
		}

		var id, user, action, rtype, rid, prev, hash, sig string
		var oldV, newV []byte
		var at time.Time
		if err := db.QueryRowContext(ctx, `
			SELECT id, user_id, action, resource_type, resource_id, created_at, old_values::text, new_values::text, previous_hash, hash, signature
			FROM audit_logs WHERE merchant_id = $1 AND resource_id = $2`, merchantID, p).
			Scan(&id, &user, &action, &rtype, &rid, &at, &oldV, &newV, &prev, &hash, &sig); err != nil {
			t.Fatalf("audit entry: %v", err)
		}
		if action != models.ActionPaymentCancelled || rtype != models.ResourcePayment || user != "itest-user" {
			t.Fatalf("audit entry = %s %s %s", action, rtype, user)
		}
		payload, err := NewAuditLogPayload(id, merchantID, user, action, rtype, rid, at, oldV, newV)
		if err != nil {
			t.Fatal(err)
		}
		if h, s, _ := Seal(ChainAuditLogs, prev, payload); h != hash || s != sig {
			t.Fatal("audit entry does not re-seal identically from the database")
		}
		var oldState map[string]any
		var change map[string]any
		_ = json.Unmarshal(oldV, &oldState)
		_ = json.Unmarshal(newV, &change)
		if oldState["amount"] != float64(1200) || oldState["mop"] != "CB" || oldState["cash_register_id"] != openReg ||
			oldState["order_id"] != o || oldState["enabled"] != true || oldState["payment_date"] != FormatTime(payDate) {
			t.Fatalf("old_values = %s", oldV)
		}
		if change["enabled"] != false || change["source"] != CancelSourceStaff || change["reason"] != "carte refusée" {
			t.Fatalf("new_values = %s", newV)
		}

		// Rejoué (double appui, webhook rejoué) : no-op, aucune entrée de plus.
		n := auditCount()
		if got, err := cancel(o, p); err != nil || len(got) != 0 {
			t.Fatalf("replay = (%+v, %v), want no-op", got, err)
		}
		if auditCount() != n {
			t.Fatal("replay wrote an audit entry")
		}
	})

	t.Run("paiement sans registre (borne, plateforme) : annulable", func(t *testing.T) {
		o := order("OPEN")
		marker := "SCANNORDER"
		p1, p2 := payment(o, "KIOSK", 500, nil), payment(o, "STRIPE", 700, &marker)
		if got, err := cancel(o, p1); err != nil || len(got) != 1 {
			t.Fatalf("no register = (%+v, %v)", got, err)
		}
		if got, err := cancel(o, p2); err != nil || len(got) != 1 {
			t.Fatalf("marker register = (%+v, %v)", got, err)
		}
	})

	t.Run("refus : commande close", func(t *testing.T) {
		o := order("CLOSED")
		p := payment(o, "ES", 1000, &openReg)
		n := auditCount()
		if _, err := cancel(o, p); !errors.Is(err, models.ErrPaymentOrderClosed) {
			t.Fatalf("closed order = %v, want ErrPaymentOrderClosed", err)
		}
		if _, err := cancelOrder(PaymentCancellation{MerchantID: merchantID, OrderID: o, Source: CancelSourceStripe}); !errors.Is(err, models.ErrOrderClosed) {
			t.Fatalf("closed order (all payments) = %v, want ErrOrderClosed", err)
		}
		if !readPayment(p).enabled || auditCount() != n {
			t.Fatal("refusal changed something")
		}
	})

	t.Run("refus : registre fermé, y compris avec un autre registre encore ouvert", func(t *testing.T) {
		o := order("OPEN")
		pOpen := payment(o, "CB", 1000, &openReg)
		pClosed := payment(o, "ES", 2000, &closedReg)
		n := auditCount()
		if _, err := cancel(o, pClosed); !errors.Is(err, models.ErrPaymentRegisterClosed) {
			t.Fatalf("closed register = %v, want ErrPaymentRegisterClosed", err)
		}
		if _, err := cancelOrder(PaymentCancellation{MerchantID: merchantID, OrderID: o, Source: CancelSourceStaff}); !errors.Is(err, models.ErrOrderPaymentRegisterClosed) {
			t.Fatalf("order with a closed-register payment = %v, want ErrOrderPaymentRegisterClosed", err)
		}
		if !readPayment(pOpen).enabled || !readPayment(pClosed).enabled || auditCount() != n {
			t.Fatal("refusal changed something")
		}
		// Le paiement du registre ouvert reste annulable seul.
		if got, err := cancel(o, pOpen); err != nil || len(got) != 1 {
			t.Fatalf("open-register payment = (%+v, %v)", got, err)
		}
	})

	t.Run("fermeture du registre en cours : l'annulation attend puis est refusée", func(t *testing.T) {
		reg := register(false)
		o := order("OPEN")
		p := payment(o, "CB", 1000, &reg)
		// La fermeture verrouille le registre en tête de transaction
		// (isCashRegisterClosedForMerchant, FOR UPDATE).
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctx, `SELECT closed FROM cash_registers WHERE cash_register_id = $1 FOR UPDATE`, reg); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() {
			_, err := cancel(o, p)
			done <- err
		}()
		select {
		case err := <-done:
			t.Fatalf("cancel did not wait for the register lock (err %v)", err)
		case <-time.After(1500 * time.Millisecond):
		}
		if _, err := tx.ExecContext(ctx, `UPDATE cash_registers SET closed = TRUE, end_date = now() WHERE cash_register_id = $1`, reg); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if err := <-done; !errors.Is(err, models.ErrPaymentRegisterClosed) {
			t.Fatalf("cancel after concurrent close = %v, want ErrPaymentRegisterClosed", err)
		}
		if !readPayment(p).enabled {
			t.Fatal("payment cancelled although its register closed first")
		}
	})

	t.Run("annulation de toute la commande", func(t *testing.T) {
		o := order("OPEN")
		p1, p2 := payment(o, "CB", 1000, &openReg), payment(o, "ES", 2000, &openReg)
		n := auditCount()
		got, err := cancelOrder(PaymentCancellation{MerchantID: merchantID, OrderID: o, Source: CancelSourceStaff, UserID: "itest-user", Reason: "Annulation de la commande"})
		if err != nil || len(got) != 2 {
			t.Fatalf("CancelPayments(order) = (%+v, %v)", got, err)
		}
		if readPayment(p1).enabled || readPayment(p2).enabled || auditCount() != n+2 {
			t.Fatal("expected both payments cancelled with one audit entry each")
		}
		// Sans paiement : contrôle de la commande seulement.
		if got, err := cancelOrder(PaymentCancellation{MerchantID: merchantID, OrderID: order("OPEN"), Source: CancelSourceSystem}); err != nil || len(got) != 0 {
			t.Fatalf("order without payment = (%+v, %v)", got, err)
		}
	})

	t.Run("refus : autre établissement ou paiement d'une autre commande", func(t *testing.T) {
		o, other := order("OPEN"), order("OPEN")
		p := payment(o, "CB", 1000, &openReg)
		if _, err := CancelPayment(ctx, db, PaymentCancellation{MerchantID: "999999999", OrderID: o, PaymentID: p}); !errors.Is(err, models.ErrNotFound) {
			t.Fatalf("other merchant = %v, want ErrNotFound", err)
		}
		if _, err := cancel(other, p); !errors.Is(err, models.ErrNotFound) {
			t.Fatalf("payment of another order = %v, want ErrNotFound", err)
		}
		if !readPayment(p).enabled {
			t.Fatal("refusal changed the payment")
		}
	})

	// Toute la chaîne d'audit de l'établissement (entrées seules et par lot) :
	// chaque entrée se re-scelle à l'identique et a pour parent la précédente.
	t.Run("chaîne d'audit continue", func(t *testing.T) {
		rows, err := db.QueryContext(ctx, `
			SELECT id, user_id, action, resource_type, resource_id, created_at, old_values::text, new_values::text, previous_hash, hash, signature
			FROM audit_logs WHERE merchant_id = $1 ORDER BY created_at, id`, merchantID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		prev, n := GenesisHash, 0
		for rows.Next() {
			var id, user, action, rtype, rid, prevHash, hash, sig string
			var oldV, newV []byte
			var at time.Time
			if err := rows.Scan(&id, &user, &action, &rtype, &rid, &at, &oldV, &newV, &prevHash, &hash, &sig); err != nil {
				t.Fatal(err)
			}
			if prevHash != prev {
				t.Fatalf("entry %s: parent %s, want %s (fork or reorder)", id, prevHash, prev)
			}
			payload, err := NewAuditLogPayload(id, merchantID, user, action, rtype, rid, at, oldV, newV)
			if err != nil {
				t.Fatal(err)
			}
			if h, sg, _ := Seal(ChainAuditLogs, prevHash, payload); h != hash || sg != sig {
				t.Fatalf("entry %s does not re-seal identically", id)
			}
			prev, n = hash, n+1
		}
		if n < 5 {
			t.Fatalf("only %d audit entries", n)
		}
	})
}
