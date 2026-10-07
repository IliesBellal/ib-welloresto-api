//go:build postgres_integration

package fiscal

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/database/dbx/pgtest"
)

type closureEnv struct {
	t          *testing.T
	db         *sql.DB
	ctx        context.Context
	merchantID string
	loc        *time.Location
	orderNum   int
	receiptNum int
}

func newClosureEnv(t *testing.T, db *sql.DB, siret string) *closureEnv {
	t.Helper()
	ctx := context.Background()
	cleanup := func(mid string) {
		for _, q := range []string{
			`DELETE FROM fiscal_closures WHERE merchant_id = $1`,
			`DELETE FROM receipts WHERE merchant_id = $1`,
			`DELETE FROM payments WHERE merchant_id = $1`,
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
	var id int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, creation_date)
		VALUES ('ITest Closures', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', $2, 'Europe/Paris', '2025-01-01')
		RETURNING id`, siret, "mt-"+siret).Scan(&id); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	loc, _ := time.LoadLocation("Europe/Paris")
	e := &closureEnv{t: t, db: db, ctx: ctx, merchantID: strconv.FormatInt(id, 10), loc: loc, orderNum: 100}
	t.Cleanup(func() { cleanup(e.merchantID) })
	return e
}

func (e *closureEnv) at(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, e.loc) }

// order crée une commande ; deliveredOn nil = jamais clôturée.
func (e *closureEnv) order(state, brandStatus string, deliveredOn time.Time, lines [][3]float64) int64 {
	e.t.Helper()
	e.orderNum++
	var id int64
	if err := e.db.QueryRowContext(e.ctx, `
		INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, order_source, price, TVA, HT, delivered_on, created_by)
		VALUES ($1, $2, 'WELLO_RESTO', $3, $4, 'IN', 'WELLO_RESTO_POS', 2800, 0, 2800, $5, 'itest') RETURNING order_id`,
		e.merchantID, e.orderNum, brandStatus, state, deliveredOn).Scan(&id); err != nil {
		e.t.Fatalf("seed order: %v", err)
	}
	for _, l := range lines {
		if _, err := e.db.ExecContext(e.ctx, `
			INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, base_price, price, tva_rate)
			VALUES ($1, 1, $2, $3, $4, $4, $5)`, id, e.merchantID, int(l[0]), int(l[1]), l[2]); err != nil {
			e.t.Fatalf("seed line: %v", err)
		}
	}
	return id
}

func (e *closureEnv) receipt(orderID int64, at time.Time, ttc, ht int64, tax *TaxDetails) string {
	e.t.Helper()
	e.receiptNum++
	number := fmt.Sprintf("F-%d-%06d", at.Year(), e.receiptNum)
	taxJSON := []byte(`{}`)
	if tax != nil {
		taxJSON, _ = json.Marshal(tax)
	}
	if _, err := e.db.ExecContext(e.ctx, `
		INSERT INTO receipts (receipt_id, merchant_id, order_id, receipt_number, total_ttc, total_ht, tax_details, items_snapshot, payments_snapshot, created_at, prev_hash, hash, signature)
		VALUES ($1, $2, $3, $4, $5, $6, $7, '[]', '[]', $8, '', 'itest', 'itest')`,
		fmt.Sprintf("itest-%s-%d", e.merchantID, e.receiptNum), e.merchantID, orderID, number, ttc, ht, taxJSON, at); err != nil {
		e.t.Fatalf("seed receipt: %v", err)
	}
	return number
}

func (e *closureEnv) payment(orderID int64, at time.Time, mop, opType string, amount int, enabled bool) {
	e.t.Helper()
	if _, err := e.db.ExecContext(e.ctx, `
		INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, operation_type, enabled, payment_date)
		VALUES ($1, 'itest', $2, $3, $4, $5, $6, $7)`, e.merchantID, orderID, amount, mop, opType, enabled, at); err != nil {
		e.t.Fatalf("seed payment: %v", err)
	}
}

func (e *closureEnv) closeDue(start, now time.Time) int {
	e.t.Helper()
	n, err := CloseDueDays(e.ctx, e.db, e.merchantID, "Europe/Paris", &start, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), now)
	if err != nil {
		e.t.Fatalf("CloseDueDays: %v", err)
	}
	return n
}

func (e *closureEnv) closure(periodType, start string) StoredClosure {
	e.t.Helper()
	var id int64
	if err := e.db.QueryRowContext(e.ctx, `
		SELECT id FROM fiscal_closures WHERE merchant_id = $1 AND period_type = $2 AND period_start = $3::date`,
		e.merchantID, periodType, start).Scan(&id); err != nil {
		e.t.Fatalf("closure %s %s: %v", periodType, start, err)
	}
	c, err := LoadClosure(e.ctx, dbx.GetDB(e.ctx, e.db), id)
	if err != nil {
		e.t.Fatal(err)
	}
	return c
}

// checkChain : chaque clôture se re-scelle à l'identique depuis la base, et la
// chaîne de l'établissement est linéaire.
func (e *closureEnv) checkChain() int {
	e.t.Helper()
	rows, err := e.db.QueryContext(e.ctx, `SELECT id FROM fiscal_closures WHERE merchant_id = $1 ORDER BY closed_at, id`, e.merchantID)
	if err != nil {
		e.t.Fatal(err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	prev := GenesisHash
	for _, id := range ids {
		c, err := LoadClosure(e.ctx, dbx.GetDB(e.ctx, e.db), id)
		if err != nil {
			e.t.Fatal(err)
		}
		h, sig, err := Seal(ChainFiscalClosures, c.Prev, c.Payload)
		if err != nil || h != c.Hash || sig != c.Signature || c.Version != HashVersion {
			e.t.Fatalf("closure %d (%s %s): seal not reproducible", id, c.Payload.PeriodType, c.Payload.PeriodStart)
		}
		if c.Prev != prev {
			e.t.Fatalf("closure %d: chain broken", id)
		}
		prev = c.Hash
	}
	return len(ids)
}

func TestFiscalClosures_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	e := newClosureEnv(t, db, "siret-fc-a")
	ctx := e.ctx
	d := dbx.GetDB(ctx, db)

	// Tickets antérieurs (valeur d'ouverture) : un l'an dernier, un cette année.
	legacy := e.order("CLOSED", "CLOSED", e.at(2025, 12, 20, 12), nil)
	e.receipt(legacy, e.at(2025, 12, 20, 12), 500, 455, nil)
	e.receipt(legacy, e.at(2026, 2, 10, 12), 1000, 909, nil)

	// 27 février : une vente à deux taux, remise de caisse, un paiement
	// désactivé ; une commande rouverte (OPEN) ne compte pas.
	saleOn := e.at(2026, 2, 27, 20)
	sale := e.order("CLOSED", "CLOSED", saleOn, [][3]float64{{2, 600, 10}, {1, 800, 10}, {1, 500, 20}})
	saleTax := BuildTaxDetails([]TaxLine{{10, 2000}, {20, 500}, {20, 300}}, 280)
	saleNumber := e.receipt(sale, saleOn, 2800, 2800, &saleTax)
	e.payment(sale, saleOn, "CB", "SALE", 2520, true)
	e.payment(sale, saleOn, "CURRENCY", "SALE", 280, true)
	e.payment(sale, saleOn, "ES", "SALE", 999, false)
	reopened := e.order("OPEN", "CLOSED", e.at(2026, 2, 27, 21), [][3]float64{{1, 100, 10}})

	// 28 février : avoir de 10 € et une annulation.
	refundTax := ProrateTaxDetails(saleTax, -1000)
	e.receipt(sale, e.at(2026, 2, 28, 13), -1000, refundTax.TotalHT(), &refundTax)
	e.payment(sale, e.at(2026, 2, 28, 13), "CB", "REFUND", -1000, true)
	canceled := e.order("CLOSED", "CANCELED", e.at(2026, 2, 28, 12), [][3]float64{{1, 300, 10}})

	start := time.Date(2026, 2, 27, 0, 0, 0, 0, time.UTC)

	// 2 mars, 2 h : le 1er mars n'est pas encore échu (avant 3 h).
	if n := e.closeDue(start, e.at(2026, 3, 2, 2)); n != 2 {
		t.Fatalf("expected 27 and 28 February closed, got %d", n)
	}
	d1 := e.closure(PeriodDay, "2026-02-27")
	p1 := d1.Payload
	if p1.Opening == nil || p1.Opening.ReceiptsCount != 2 || p1.Opening.NetTTC != 1500 || p1.Opening.YearNetTTC != 1000 {
		t.Fatalf("opening: %+v", p1.Opening)
	}
	if p1.SalesTTC != 2520 || p1.NetTTC != 2520 || p1.RefundsTTC != 0 || p1.PerpetualTotal != 1500+2520 || p1.GrandTotalPeriod != 1000+2520 {
		t.Fatalf("27/02 totals: %+v", p1)
	}
	if len(p1.VATByRate) != 2 || p1.VATByRate[0] != (VATTotal{10, 1800, 1636, 164}) || p1.VATByRate[1].TTC != 720 {
		t.Fatalf("27/02 vat: %+v", p1.VATByRate)
	}
	if len(p1.PaymentsByMOP) != 3 || len(p1.ByChannel) != 1 || p1.ByChannel[0] != (ChannelTotal{"WELLO_RESTO_POS", 2520}) {
		t.Fatalf("27/02 payments/channels: %+v / %+v", p1.PaymentsByMOP, p1.ByChannel)
	}
	if p1.ReceiptsCount != 1 || *p1.FirstReceiptNumber != saleNumber || *p1.LastReceiptNumber != saleNumber {
		t.Fatalf("27/02 receipts: %+v", p1)
	}
	// Empreinte de la commande : celle du chargeur unitaire (même payload que
	// le chargement groupé) ; la commande rouverte n'y est pas.
	single, err := LoadOrderClosure(ctx, d, strconv.FormatInt(sale, 10), saleOn)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := Fingerprint("order_closure", single)
	if len(p1.Orders) != 1 || p1.Orders[0].OrderID != sale || p1.Orders[0].Status != "CLOSED" || p1.Orders[0].Hash != want {
		t.Fatalf("27/02 orders: %+v (want %d/%s)", p1.Orders, sale, want)
	}

	p2 := e.closure(PeriodDay, "2026-02-28").Payload
	if p2.Opening != nil || p2.RefundsTTC != -1000 || p2.NetTTC != -1000 || p2.PerpetualTotal != 3020 || p2.GrandTotalPeriod != 2520 {
		t.Fatalf("28/02 totals: %+v", p2)
	}
	if len(p2.Orders) != 1 || p2.Orders[0].OrderID != canceled || p2.Orders[0].Status != "CANCELED" {
		t.Fatalf("28/02 orders: %+v", p2.Orders)
	}

	month := e.closure(PeriodMonth, "2026-02-01").Payload
	if month.PeriodEnd != "2026-02-28" || month.SalesTTC != 2520 || month.RefundsTTC != -1000 || month.NetTTC != 1520 ||
		month.OrdersCount != 2 || month.Orders != nil || month.PerpetualTotal != 3020 || month.GrandTotalPeriod != 2520 {
		t.Fatalf("February: %+v", month)
	}
	var monthVAT int64
	for _, v := range month.VATByRate {
		monthVAT += v.TTC
	}
	if monthVAT != 1520 {
		t.Fatalf("February vat total %d, want 1520", monthVAT)
	}

	// 2 mars, 4 h : le 1er mars (sans activité) est clos à zéro.
	if n := e.closeDue(start, e.at(2026, 3, 2, 4)); n != 1 {
		t.Fatalf("expected 1 March closed, got %d", n)
	}
	if p := e.closure(PeriodDay, "2026-03-01").Payload; p.NetTTC != 0 || p.PerpetualTotal != 3020 || p.GrandTotalPeriod != 2520 || len(p.Orders) != 0 {
		t.Fatalf("01/03: %+v", p)
	}

	// Deux instances en même temps : le 2 mars n'est clos qu'une fois.
	var wg sync.WaitGroup
	results := make([]int, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			n, err := CloseDueDays(ctx, db, e.merchantID, "Europe/Paris", &start, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), e.at(2026, 3, 3, 4))
			if err != nil {
				t.Errorf("concurrent CloseDueDays: %v", err)
			}
			results[i] = n
		}(i)
	}
	wg.Wait()
	if results[0]+results[1] != 1 {
		t.Fatalf("2 March closed %d times", results[0]+results[1])
	}

	// Chaîne : 4 jours + février, toutes re-scellables.
	if n := e.checkChain(); n != 5 {
		t.Fatalf("expected 5 closures, got %d", n)
	}

	// Scellement des commandes : une ligne modifiée après la clôture est
	// détectée en recalculant la journée.
	if _, err := db.ExecContext(ctx, `UPDATE orderitems SET price = price + 1 WHERE order_id = $1`, sale); err != nil {
		t.Fatal(err)
	}
	again, err := ComputeDayClosure(ctx, d, e.merchantID, "Europe/Paris", e.loc, time.Date(2026, 2, 27, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Orders) != 1 || again.Orders[0].Hash == p1.Orders[0].Hash {
		t.Fatal("modified sealed order not detected")
	}

	// Commande scellée : la vente oui ; la commande rouverte et une commande
	// d'un jour non clos, non.
	later := e.order("CLOSED", "CLOSED", e.at(2026, 3, 5, 12), nil)
	for id, wantSealed := range map[int64]bool{sale: true, reopened: false, later: false} {
		got, err := IsOrderSealed(ctx, d, strconv.FormatInt(id, 10))
		if err != nil || got != wantSealed {
			t.Fatalf("IsOrderSealed(%d) = %v, %v; want %v", id, got, err, wantSealed)
		}
	}
}

// Passage d'année : le grand total repart de zéro le 1er janvier, le total
// perpétuel continue ; l'année close est la somme de ses journées.
func TestFiscalClosures_YearBoundary_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	e := newClosureEnv(t, db, "siret-fc-b")

	o := e.order("CLOSED", "CLOSED", e.at(2025, 12, 31, 12), nil)
	tax1 := BuildTaxDetails([]TaxLine{{10, 1000}}, 0)
	e.receipt(o, e.at(2025, 12, 31, 12), 1000, tax1.TotalHT(), &tax1)
	tax2 := BuildTaxDetails([]TaxLine{{10, 500}}, 0)
	e.receipt(o, e.at(2026, 1, 1, 12), 500, tax2.TotalHT(), &tax2)

	if n := e.closeDue(time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC), e.at(2026, 1, 2, 5)); n != 2 {
		t.Fatalf("expected 31/12 and 01/01 closed, got %d", n)
	}
	dec31 := e.closure(PeriodDay, "2025-12-31").Payload
	jan1 := e.closure(PeriodDay, "2026-01-01").Payload
	year := e.closure(PeriodYear, "2025-01-01").Payload
	if dec31.GrandTotalPeriod != 1000 || dec31.PerpetualTotal != 1000 {
		t.Fatalf("31/12: %+v", dec31)
	}
	if jan1.GrandTotalPeriod != 500 || jan1.PerpetualTotal != 1500 {
		t.Fatalf("01/01: grand total must reset, perpetual must not: %+v", jan1)
	}
	if year.PeriodEnd != "2025-12-31" || year.NetTTC != 1000 || year.GrandTotalPeriod != 1000 {
		t.Fatalf("2025: %+v", year)
	}
	if n := e.checkChain(); n != 4 { // 31/12, décembre, 2025, 01/01
		t.Fatalf("expected 4 closures, got %d", n)
	}
}

// Sans rattrapage (tâche horaire) : un établissement sans clôture commence au
// dernier jour échu ; les tickets antérieurs sont comptés dans la valeur
// d'ouverture de cette première clôture.
func TestFiscalClosures_AutoStart_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	e := newClosureEnv(t, db, "siret-fc-c")

	o := e.order("CLOSED", "CLOSED", e.at(2026, 5, 10, 12), nil)
	for _, day := range []int{10, 11, 12} {
		tax := BuildTaxDetails([]TaxLine{{10, 100}}, 0)
		e.receipt(o, e.at(2026, 5, day, 12), 100, tax.TotalHT(), &tax)
	}

	n, err := CloseDueDays(e.ctx, e.db, e.merchantID, "Europe/Paris", nil, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), e.at(2026, 5, 13, 5))
	if err != nil || n != 1 {
		t.Fatalf("auto start: closed %d, %v; want only 12 May", n, err)
	}
	p := e.closure(PeriodDay, "2026-05-12").Payload
	if p.NetTTC != 100 || p.Opening == nil || p.Opening.ReceiptsCount != 2 || p.Opening.NetTTC != 200 || p.PerpetualTotal != 300 {
		t.Fatalf("12 May: %+v opening %+v", p, p.Opening)
	}
	// Le lendemain : continue au jour suivant, sans revenir en arrière.
	if n, err := CloseDueDays(e.ctx, e.db, e.merchantID, "Europe/Paris", nil, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), e.at(2026, 5, 14, 5)); err != nil || n != 1 {
		t.Fatalf("next day: closed %d, %v", n, err)
	}
	// Un rattrapage demandé après coup ne remonte pas avant la première clôture.
	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	if first, _, ok, err := DueRange(e.ctx, e.db, e.merchantID, "Europe/Paris", &from, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), e.at(2026, 5, 14, 5)); err != nil || ok || first.Format("2006-01-02") != "2026-05-14" {
		t.Fatalf("backfill after first closure: first=%s ok=%v err=%v", first.Format("2006-01-02"), ok, err)
	}
}

