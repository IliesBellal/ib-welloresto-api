//go:build postgres_integration

package fiscal

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/utils/dbutils"
)

// Clôture d'une commande plateforme qu'Uber ne paie pas (annulée, refusée,
// livraison échouée) : ses paiements sont annulés avec la clôture ; un refus
// (registre fermé) ou un échec n'empêche jamais la clôture, qui se fait alors
// comme avant, paiements intacts (docs/marketplace-fiabilite-import.md, lot 6).
func TestCloseVoidPlatformOrder_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const siret = "siret-itest-voidpf"

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
		VALUES ('ITest Void Platform', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', 'mt-voidpf', 'Europe/Paris')
		RETURNING id`, siret).Scan(&mid); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(mid, 10)
	t.Cleanup(func() { cleanup(merchantID) })

	var closedReg int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO cash_registers (merchant_id, cash_desk_id, device_id, user_id, cash_fund, start_date, closure_comment, closed, end_date)
		VALUES ($1, 1, 'itest-voidpf', 'itest', 0, now(), '', TRUE, now()) RETURNING cash_register_id`, merchantID).Scan(&closedReg); err != nil {
		t.Fatalf("seed register: %v", err)
	}
	num := 0
	seed := func(t *testing.T, register *string) (orderID, paymentID string) {
		t.Helper()
		num++
		var oid, pid int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, price, TVA, HT, created_by)
			VALUES ($1, $2, 'UBER_EATS', 'ACCEPTED', 'OPEN', 5158, 469, 4689, 'UBER_EATS') RETURNING order_id`,
			merchantID, num).Scan(&oid); err != nil {
			t.Fatalf("seed order: %v", err)
		}
		if err := db.QueryRowContext(ctx, `
			INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, operation_type, enabled, cash_register_id)
			VALUES ($1, 'WEBHOOK_UBER_EATS', $2, 5158, 'UBER_EATS', 'SALE', TRUE, $3) RETURNING payment_id`,
			merchantID, oid, register).Scan(&pid); err != nil {
			t.Fatalf("seed payment: %v", err)
		}
		return strconv.FormatInt(oid, 10), strconv.FormatInt(pid, 10)
	}
	read := func(t *testing.T, orderID, paymentID string) (state, status string, enabled bool, audits int) {
		t.Helper()
		if err := db.QueryRowContext(ctx, `
			SELECT o.state, COALESCE(o.brand_status, ''), p.enabled,
			       (SELECT count(*) FROM audit_logs a WHERE a.merchant_id = o.merchant_id AND a.action = 'PAYMENT_CANCELLED'
			          AND a.old_values->>'order_id' = o.order_id::text)
			FROM orders o JOIN payments p ON p.order_id = o.order_id
			WHERE o.order_id = $1 AND p.payment_id = $2`, orderID, paymentID).Scan(&state, &status, &enabled, &audits); err != nil {
			t.Fatalf("read order %s: %v", orderID, err)
		}
		return
	}
	cancellation := func(orderID string) PaymentCancellation {
		return PaymentCancellation{
			MerchantID: merchantID, OrderID: orderID,
			Source: CancelSourceUberEats, UserID: "WEBHOOK_UBER_EATS", Reason: "Livraison Uber Eats échouée",
		}
	}
	// closer écrit la clôture ; failFirst : le premier appel écrit, puis échoue.
	type call struct{ hasSale bool }
	closer := func(orderID string, calls *[]call, failFirst bool) func(context.Context, bool) error {
		return func(txCtx context.Context, hasSale bool) error {
			*calls = append(*calls, call{hasSale})
			status := "DELIVERY_FAILED"
			if failFirst && len(*calls) == 1 {
				status = "FIRST_ATTEMPT"
			}
			if _, err := dbx.GetDB(txCtx, db).ExecContext(txCtx,
				`UPDATE orders SET state = 'CLOSED', brand_status = ? WHERE order_id = ?`, status, orderID); err != nil {
				return err
			}
			if failFirst && len(*calls) == 1 {
				return errors.New("itest: first close attempt fails")
			}
			return nil
		}
	}
	run := func(c PaymentCancellation, closeOrder func(context.Context, bool) error) error {
		return dbutils.RunInTx(ctx, db, func(txCtx context.Context) error {
			return CloseVoidPlatformOrder(txCtx, db, c, closeOrder)
		})
	}

	t.Run("hors transaction refusé", func(t *testing.T) {
		if err := CloseVoidPlatformOrder(ctx, db, cancellation("0"), nil); !errors.Is(err, ErrNoTransaction) {
			t.Fatalf("err = %v, want ErrNoTransaction", err)
		}
	})

	t.Run("paiements annulés avec la clôture", func(t *testing.T) {
		o, p := seed(t, nil)
		var calls []call
		if err := run(cancellation(o), closer(o, &calls, false)); err != nil {
			t.Fatalf("CloseVoidPlatformOrder: %v", err)
		}
		state, status, enabled, audits := read(t, o, p)
		if state != "CLOSED" || status != "DELIVERY_FAILED" || enabled || audits != 1 || len(calls) != 1 || calls[0].hasSale {
			t.Fatalf("state=%s status=%s enabled=%v audits=%d calls=%v, want CLOSED DELIVERY_FAILED, payment cancelled, 1 audit, 1 call", state, status, enabled, audits, calls)
		}
		var source, reason string
		if err := db.QueryRowContext(ctx, `
			SELECT new_values->>'source', new_values->>'reason' FROM audit_logs
			WHERE merchant_id = $1 AND action = 'PAYMENT_CANCELLED' AND old_values->>'order_id' = $2`, merchantID, o).
			Scan(&source, &reason); err != nil || source != CancelSourceUberEats || reason != "Livraison Uber Eats échouée" {
			t.Fatalf("audit source=%q reason=%q err=%v", source, reason, err)
		}
	})

	t.Run("registre fermé : clôture sans annuler", func(t *testing.T) {
		reg := strconv.FormatInt(closedReg, 10)
		o, p := seed(t, &reg)
		var calls []call
		if err := run(cancellation(o), closer(o, &calls, false)); err != nil {
			t.Fatalf("CloseVoidPlatformOrder: %v", err)
		}
		state, status, enabled, audits := read(t, o, p)
		if state != "CLOSED" || status != "DELIVERY_FAILED" || !enabled || audits != 0 || len(calls) != 1 {
			t.Fatalf("state=%s status=%s enabled=%v audits=%d calls=%v, want CLOSED, payment kept, no audit, 1 call", state, status, enabled, audits, calls)
		}
	})

	t.Run("échec de la clôture : défaite puis refaite sans annuler", func(t *testing.T) {
		o, p := seed(t, nil)
		var calls []call
		if err := run(cancellation(o), closer(o, &calls, true)); err != nil {
			t.Fatalf("CloseVoidPlatformOrder: %v", err)
		}
		// Le premier essai (annulation + FIRST_ATTEMPT) est défait par le point
		// de sauvegarde ; la clôture de repli garde le paiement.
		state, status, enabled, audits := read(t, o, p)
		if state != "CLOSED" || status != "DELIVERY_FAILED" || !enabled || audits != 0 || len(calls) != 2 || calls[1].hasSale {
			t.Fatalf("state=%s status=%s enabled=%v audits=%d calls=%v, want CLOSED DELIVERY_FAILED, payment kept, no audit, 2 calls", state, status, enabled, audits, calls)
		}
	})

	t.Run("échec de la clôture de repli : tout est défait", func(t *testing.T) {
		o, p := seed(t, nil)
		failing := func(context.Context, bool) error { return errors.New("itest: close fails") }
		if err := run(cancellation(o), failing); err == nil {
			t.Fatal("expected the fallback close error")
		}
		state, _, enabled, audits := read(t, o, p)
		if state != "OPEN" || !enabled || audits != 0 {
			t.Fatalf("state=%s enabled=%v audits=%d, want rolled back (OPEN, payment kept, no audit)", state, enabled, audits)
		}
	})
}
