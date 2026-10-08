//go:build postgres_integration

package fiscalverify

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/utils/dbutils"
)

// Mesure du contrôle d'intégrité sur un mois chargé (lot E) : 3 000 commandes
// et tickets scellés, 3 000 paiements scellés, 12 000 entrées de journal de
// ~7 Ko, clôtures des 29 jours. Lancée seulement avec FISCAL_PERF=1.
func TestVerifyPerf_Postgres(t *testing.T) {
	if os.Getenv("FISCAL_PERF") != "1" {
		t.Skip("FISCAL_PERF=1 not set — skipping verification measurement")
	}
	db := pgtest.Open(t)
	ctx := context.Background()
	const siret = "siret-itest-verify-perf"
	cleanup := func(mid string) {
		for _, q := range []string{
			`DELETE FROM fiscal_closures WHERE merchant_id = $1`,
			`DELETE FROM receipts WHERE merchant_id = $1`,
			`DELETE FROM payments WHERE merchant_id = $1`,
			`DELETE FROM audit_logs WHERE merchant_id = $1`,
			`DELETE FROM orderitems WHERE merchant_id = $1`,
			`DELETE FROM orders WHERE merchant_id = $1`,
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
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, creation_date)
		VALUES ('ITest Verify Perf', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', 'mt-verify-perf', 'Europe/Paris', '2025-01-01')
		RETURNING id`, siret).Scan(&mid); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(mid, 10)
	t.Cleanup(func() { cleanup(merchantID) })
	loc, _ := time.LoadLocation("Europe/Paris")

	seedStart := time.Now()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, order_source, price, TVA, HT, delivered_on, created_by)
		SELECT $1::text, g, 'WELLO_RESTO', 'CLOSED', 'CLOSED', 'IN', 'WELLO_RESTO_POS', 2500, 227, 2273,
		       timestamptz '2026-09-01 10:00:00+02' + ((g - 1) % 29) * interval '1 day' + (g / 29) * interval '1 second', 'itest'
		FROM generate_series(1, 3000) g`, merchantID); err != nil {
		t.Fatalf("seed orders: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, base_price, price, tva_rate)
		SELECT order_id, 1, $1::text, 1, 2500, 2500, 10 FROM orders WHERE merchant_id = $1::text`, merchantID); err != nil {
		t.Fatalf("seed lines: %v", err)
	}
	type ord struct {
		id string
		at time.Time
	}
	rows, err := db.QueryContext(ctx, `SELECT order_id::text, delivered_on FROM orders WHERE merchant_id = $1 ORDER BY delivered_on, order_id`, merchantID)
	if err != nil {
		t.Fatal(err)
	}
	var orders []ord
	for rows.Next() {
		var o ord
		_ = rows.Scan(&o.id, &o.at)
		orders = append(orders, o)
	}
	rows.Close()

	// Tickets et paiements scellés en lots de 500.
	tax := []byte(`{"lines":[{"rate":10,"ttc":2500,"ht":2273,"tva":227}],"discount":0}`)
	prevR, prevP := fiscal.GenesisHash, fiscal.GenesisHash
	for start := 0; start < len(orders); start += 500 {
		var rArgs, pArgs []any
		var rVals, pVals []string
		for i, o := range orders[start:min(start+500, len(orders))] {
			n := start + i + 1
			number := fmt.Sprintf("F-2026-%06d", n)
			rp, _ := fiscal.NewReceiptPayload(merchantID, number, o.id, o.at, 2500, 2273, tax, []byte(`[]`), []byte(`[]`))
			rh, rs, _ := fiscal.Seal(fiscal.ChainReceipts, prevR, rp)
			k := len(rArgs)
			rVals = append(rVals, fmt.Sprintf("($%d, $%d, $%d::int, $%d, 2500, 2273, $%d::jsonb, '[]', '[]', $%d, $%d, $%d, $%d, 2)", k+1, k+2, k+3, k+4, k+5, k+6, k+7, k+8, k+9))
			rArgs = append(rArgs, "itest-vperf-"+number, merchantID, o.id, number, tax, o.at, prevR, rh, rs)
			prevR = rh
			pp, _ := fiscal.NewPaymentPayload(merchantID, o.id, 2500, "CB", "SALE", o.at, "itest", nil)
			ph, ps, _ := fiscal.Seal(fiscal.ChainPayments, prevP, pp)
			k = len(pArgs)
			pVals = append(pVals, fmt.Sprintf("($%d, $%d::int, 2500, 2500, 'CB', $%d, 'itest', 'SALE', TRUE, $%d, $%d, $%d, 2)", k+1, k+2, k+3, k+4, k+5, k+6))
			pArgs = append(pArgs, merchantID, o.id, o.at, prevP, ph, ps)
			prevP = ph
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO receipts (receipt_id, merchant_id, order_id, receipt_number, total_ttc, total_ht, tax_details, items_snapshot, payments_snapshot, created_at, prev_hash, hash, signature, hash_version) VALUES `+strings.Join(rVals, ","), rArgs...); err != nil {
			t.Fatalf("seed receipts: %v", err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO payments (merchant_id, order_id, amount, net_amount, mop, payment_date, user_id, operation_type, enabled, previous_hash, hash, signature, hash_version) VALUES `+strings.Join(pVals, ","), pArgs...); err != nil {
			t.Fatalf("seed payments: %v", err)
		}
	}
	// Journal : 12 000 entrées de ~7 Ko, chaînées par AppendAuditLogs.
	big := strings.Repeat("x", 3500)
	for start := 0; start < 12000; start += 500 {
		entries := make([]fiscal.AuditEntry, 0, 500)
		for i := start; i < start+500; i++ {
			entries = append(entries, fiscal.AuditEntry{ID: fmt.Sprintf("itest-vperf-%s-%d", merchantID, i), MerchantID: merchantID,
				UserID: "itest", Action: "ORDER_UPDATE", ResourceType: "orders", ResourceID: orders[i%len(orders)].id,
				OldValues: []byte(`{"payload":"` + big + `"}`), NewValues: []byte(`{"payload":"` + big + `"}`)})
		}
		if err := dbutils.RunInTx(ctx, db, func(txCtx context.Context) error {
			return fiscal.AppendAuditLogs(txCtx, dbx.GetDB(txCtx, db), merchantID, entries)
		}); err != nil {
			t.Fatalf("seed audit: %v", err)
		}
	}
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if _, err := fiscal.CloseDueDays(ctx, db, merchantID, "Europe/Paris", &from, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 30, 12, 0, 0, 0, loc)); err != nil {
		t.Fatalf("CloseDueDays: %v", err)
	}
	t.Logf("données : %v", time.Since(seedStart).Round(time.Millisecond))

	// Septembre (tickets, paiements, clôtures), puis aujourd'hui (journal et
	// chaîne des clôtures, datés de l'écriture).
	today := time.Now().In(loc)
	for _, o := range []Options{
		{MerchantID: merchantID, From: from, To: time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC), Now: time.Date(2026, 9, 30, 12, 0, 0, 0, loc)},
		{MerchantID: merchantID, From: today, To: today},
	} {
		start := time.Now()
		r, err := Run(ctx, db, o)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		var b strings.Builder
		for _, c := range r.Checks {
			fmt.Fprintf(&b, " %s=%d", c.Check, c.Checked)
		}
		t.Logf("vérification %s → %s : %v, %d erreur(s), %d avertissement(s) ;%s", r.From, r.To,
			time.Since(start).Round(time.Millisecond), r.Errors, r.Warnings, b.String())
		if r.Errors != 0 {
			var txt strings.Builder
			r.WriteText(&txt)
			t.Fatalf("generated data must verify cleanly:\n%s", txt.String())
		}
	}
}
