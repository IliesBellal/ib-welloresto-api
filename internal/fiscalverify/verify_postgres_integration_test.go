//go:build postgres_integration

package fiscalverify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/fiscalarchive"
	"welloresto-api/internal/utils/dbutils"
)

type memFiles struct {
	files  map[string][]byte
	tamper bool
}

func (m *memFiles) UploadPrivateFile(_ context.Context, key string, file io.Reader, _ string) (string, error) {
	b, err := io.ReadAll(file)
	if m.files == nil {
		m.files = map[string][]byte{}
	}
	m.files[key] = b
	return key, err
}

func (m *memFiles) GetFile(_ context.Context, key string) ([]byte, error) {
	b, ok := m.files[key]
	if !ok {
		return nil, errors.New("absent")
	}
	if m.tamper {
		b = append(bytes.Clone(b), 0)
	}
	return b, nil
}

// Contrôle d'intégrité (lot E, phase 1) : données scellées par les fonctions
// réelles, vérifiées sans erreur, puis altérées une à une.
func TestVerify_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const siret = "siret-itest-verify"

	cleanup := func(mid string) {
		for _, q := range []string{
			`DELETE FROM fiscal_archives WHERE merchant_id = $1`,
			`DELETE FROM fiscal_closures WHERE merchant_id = $1`,
			`DELETE FROM receipts WHERE merchant_id = $1`,
			`DELETE FROM payments WHERE merchant_id = $1`,
			`DELETE FROM audit_logs WHERE merchant_id = $1`,
			`DELETE FROM orderitems WHERE merchant_id = $1`,
			`DELETE FROM orders WHERE merchant_id = $1`,
			`DELETE FROM cash_registers_items WHERE cash_register_id IN (SELECT cash_register_id FROM cash_registers WHERE merchant_id = $1)`,
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
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, creation_date)
		VALUES ('ITest Verify', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', 'mt-verify', 'Europe/Paris', '2025-01-01')
		RETURNING id`, siret).Scan(&mid); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(mid, 10)
	t.Cleanup(func() { cleanup(merchantID) })
	loc, _ := time.LoadLocation("Europe/Paris")
	at := func(d, h int) time.Time { return time.Date(2026, 9, d, h, 0, 0, 0, loc).UTC() }
	exec := func(label, q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}

	// Commandes : deux ventes, une annulée après son ticket (avoir).
	type sale struct {
		day, hour, price int
		status           string
	}
	sales := []sale{{1, 13, 1100, "CLOSED"}, {2, 12, 2000, "CLOSED"}, {2, 15, 500, "CANCELED"}}
	var orderIDs []int64
	for i, s := range sales {
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, order_source, price, TVA, HT, delivered_on, created_by)
			VALUES ($1, $2, 'WELLO_RESTO', $3, 'CLOSED', 'IN', 'WELLO_RESTO_POS', $4, 0, $4, $5, 'itest') RETURNING order_id`,
			merchantID, i+1, s.status, s.price, at(s.day, s.hour)).Scan(&id); err != nil {
			t.Fatalf("seed order: %v", err)
		}
		exec("line", `INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, base_price, price, tva_rate)
			VALUES ($1, 1, $2, 1, $3, $3, 10)`, id, merchantID, s.price)
		orderIDs = append(orderIDs, id)
	}

	// Tickets scellés et chaînés comme receipt.sealReceipt.
	prev := fiscal.GenesisHash
	receipt := func(n, orderIdx, ttc int, created time.Time) {
		t.Helper()
		ht := int(float64(ttc) / 1.1)
		tax := []byte(fmt.Sprintf(`{"lines":[{"rate":10,"ttc":%d,"ht":%d,"tva":%d}],"discount":0}`, ttc, ht, ttc-ht))
		items := []byte(`[]`)
		number := fmt.Sprintf("F-2026-%06d", n)
		oid := strconv.FormatInt(orderIDs[orderIdx], 10)
		p, err := fiscal.NewReceiptPayload(merchantID, number, oid, created, ttc, ht, tax, items, []byte(`[]`))
		if err != nil {
			t.Fatal(err)
		}
		h, sig, err := fiscal.Seal(fiscal.ChainReceipts, prev, p)
		if err != nil {
			t.Fatal(err)
		}
		exec("receipt", `INSERT INTO receipts (receipt_id, merchant_id, order_id, receipt_number, total_ttc, total_ht, tax_details, items_snapshot, payments_snapshot, created_at, prev_hash, hash, signature, hash_version)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, '[]', $9, $10, $11, $12, 2)`,
			"itest-verify-"+number, merchantID, orderIDs[orderIdx], number, ttc, ht, tax, items, created, prev, h, sig)
		prev = h
	}
	receipt(1, 0, 1100, at(1, 13))
	receipt(2, 1, 2000, at(2, 12))
	receipt(3, 2, 500, at(2, 14))
	receipt(4, 2, -500, at(2, 15))

	// Paiements scellés et chaînés comme addPaymentAndReturnID.
	prevPay := fiscal.GenesisHash
	var payIDs []int64
	payment := func(orderIdx, amount int, date time.Time, enabled bool) {
		t.Helper()
		oid := strconv.FormatInt(orderIDs[orderIdx], 10)
		p, err := fiscal.NewPaymentPayload(merchantID, oid, amount, "CB", "SALE", date, "itest", nil)
		if err != nil {
			t.Fatal(err)
		}
		h, sig, err := fiscal.Seal(fiscal.ChainPayments, prevPay, p)
		if err != nil {
			t.Fatal(err)
		}
		var id int64
		if err := db.QueryRowContext(ctx, `INSERT INTO payments (merchant_id, order_id, amount, net_amount, mop, payment_date, user_id, operation_type, enabled, previous_hash, hash, signature, hash_version)
			VALUES ($1, $2, $3, $3, 'CB', $4, 'itest', 'SALE', $5, $6, $7, $8, 2) RETURNING payment_id`,
			merchantID, orderIDs[orderIdx], amount, date, enabled, prevPay, h, sig).Scan(&id); err != nil {
			t.Fatalf("payment: %v", err)
		}
		payIDs = append(payIDs, id)
		prevPay = h
	}
	payment(0, 1100, at(1, 13), true)
	payment(1, 2000, at(2, 12), true)

	// Registre fermé et scellé comme CloseCashRegister.
	var reg int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO cash_registers (merchant_id, cash_desk_id, device_id, user_id, cash_fund, start_date, closure_comment, closed)
		VALUES ($1, 1, 'itest-verify', 'itest', 10000, $2, '', FALSE) RETURNING cash_register_id`, merchantID, at(2, 9)).Scan(&reg); err != nil {
		t.Fatalf("register: %v", err)
	}
	exec("z", `INSERT INTO cash_registers_items (cash_register_id, mop, amount) VALUES ($1, 'CB', 3100)`, reg)
	end := at(2, 23)
	cp, err := fiscal.LoadCashRegisterClosure(ctx, dbx.GetDB(ctx, db), strconv.FormatInt(reg, 10), end, 10000)
	if err != nil {
		t.Fatal(err)
	}
	rh, rsig, _ := fiscal.Seal(fiscal.ChainCashRegisters, fiscal.GenesisHash, cp)
	exec("close register", `UPDATE cash_registers SET closed = TRUE, end_date = $2, final_cash_fund = 10000, previous_hash = $3, hash = $4, signature = $5, hash_version = 2
		WHERE cash_register_id = $1`, reg, end, fiscal.GenesisHash, rh, rsig)

	// Journal, clôtures (1er au 3 septembre), archive à la demande.
	if err := dbutils.RunInTx(ctx, db, func(txCtx context.Context) error {
		return fiscal.AppendAuditLog(txCtx, dbx.GetDB(txCtx, db), fiscal.AuditEntry{
			ID: "itest-verify-audit-" + merchantID, MerchantID: merchantID, UserID: "itest", Action: "ORDER_CLOSE",
			ResourceType: "orders", ResourceID: strconv.FormatInt(orderIDs[0], 10), NewValues: []byte(`{"state":"CLOSED"}`),
		})
	}); err != nil {
		t.Fatalf("audit: %v", err)
	}
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if _, err := fiscal.CloseDueDays(ctx, db, merchantID, "Europe/Paris", &from, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 4, 12, 0, 0, 0, loc)); err != nil {
		t.Fatalf("CloseDueDays: %v", err)
	}
	files := &memFiles{}
	if _, err := fiscalarchive.Generate(ctx, db, files, fiscalarchive.Request{MerchantID: merchantID, Kind: fiscalarchive.KindPeriod,
		Start: from, End: time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), GeneratedBy: "itest"}); err != nil {
		t.Fatalf("archive: %v", err)
	}

	september := Options{MerchantID: merchantID, From: from, To: time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC),
		Now: time.Date(2026, 9, 4, 12, 0, 0, 0, loc), Archives: files}
	today := time.Now().In(loc)
	recent := Options{MerchantID: merchantID, From: today, To: today, Archives: files}

	run := func(label string, o Options) *Report {
		t.Helper()
		r, err := Run(ctx, db, o)
		if err != nil {
			t.Fatalf("%s: Run: %v", label, err)
		}
		return r
	}
	checked := func(r *Report, check string) int {
		for _, c := range r.Checks {
			if c.Check == check {
				return c.Checked
			}
		}
		return -1
	}
	expect := func(label string, r *Report, check string, sev Severity, substr string) {
		t.Helper()
		for _, f := range r.Findings {
			if f.Check == check && f.Severity == sev && strings.Contains(f.Message, substr) {
				return
			}
		}
		var b strings.Builder
		r.WriteText(&b)
		t.Fatalf("%s: no %s %s finding containing %q:\n%s", label, sev, check, substr, b.String())
	}

	// 1. Données intactes : aucune erreur, aucun avertissement.
	for _, o := range []Options{september, recent} {
		r := run("intact", o)
		if r.Errors != 0 || r.Warnings != 0 {
			var b strings.Builder
			r.WriteText(&b)
			t.Fatalf("intact data must verify cleanly:\n%s", b.String())
		}
	}
	r := run("intact", september)
	for check, want := range map[string]int{"paiements": 2, "tickets": 4, "registres": 1, "numerotation": 4,
		"clotures_jour": 3, "commandes_tickets": 3, "tickets_lignes": 4} {
		if got := checked(r, check); got != want {
			t.Fatalf("%s: %d checked, want %d", check, got, want)
		}
	}
	r = run("intact", recent)
	if checked(r, "journal") != 1 || checked(r, "clotures_chaine") != 3 || checked(r, "archives") != 1 {
		t.Fatalf("recent checks: %+v", r.Checks)
	}
	var text strings.Builder
	r.WriteText(&text)
	if !strings.Contains(text.String(), "RÉSULTAT : CONFORME") {
		t.Fatalf("text report:\n%s", text.String())
	}

	// 2. Paiement annulé après la clôture de son jour : avertissement seul.
	exec("cancel", `UPDATE payments SET enabled = FALSE WHERE payment_id = $1`, payIDs[1])
	r = run("cancel", september)
	if r.Errors != 0 {
		t.Fatalf("a later cancellation is not an error: %+v", r.Findings)
	}
	expect("cancel", r, "clotures_jour", SeverityWarning, "annulé après la clôture")
	exec("uncancel", `UPDATE payments SET enabled = TRUE WHERE payment_id = $1`, payIDs[1])

	// 3. Montant d'un paiement modifié : empreinte et clôture en écart.
	exec("tamper payment", `UPDATE payments SET amount = 2100 WHERE payment_id = $1`, payIDs[1])
	r = run("tamper payment", september)
	expect("tamper payment", r, "paiements", SeverityError, "empreinte recalculée différente")
	expect("tamper payment", r, "clotures_jour", SeverityError, "paiements du jour différents")
	exec("restore payment", `UPDATE payments SET amount = 2000 WHERE payment_id = $1`, payIDs[1])

	// 4. Ligne de commande modifiée après scellement.
	exec("tamper line", `UPDATE orderitems SET price = 1200 WHERE order_id = $1`, orderIDs[0])
	r = run("tamper line", september)
	expect("tamper line", r, "clotures_jour", SeverityError, "modifiée(s) après scellement : "+strconv.FormatInt(orderIDs[0], 10))
	exec("restore line", `UPDATE orderitems SET price = 1100 WHERE order_id = $1`, orderIDs[0])

	// 5. Fichier d'archive altéré dans le stockage.
	files.tamper = true
	r = run("tamper archive", recent)
	expect("tamper archive", r, "archives", SeverityError, "ne correspond pas à l'empreinte scellée")
	files.tamper = false

	// 6. Ticket supprimé : trou dans la chaîne et la numérotation, clôture et
	// commande en écart.
	exec("delete receipt", `DELETE FROM receipts WHERE merchant_id = $1 AND receipt_number = 'F-2026-000002'`, merchantID)
	r = run("delete receipt", september)
	expect("delete receipt", r, "tickets", SeverityError, "parent")
	expect("delete receipt", r, "numerotation", SeverityError, "numéros manquants de F-2026-000002 à F-2026-000002")
	expect("delete receipt", r, "clotures_jour", SeverityError, "tickets du jour différents")
	expect("delete receipt", r, "commandes_tickets", SeverityError, "vente sans ticket")
	if r.OK() {
		t.Fatal("report must not be OK")
	}
}
