//go:build postgres_integration

package fiscalarchive

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// Mesure de la construction d'une archive sur un gros mois généré (lot D,
// risque mémoire) : 3 000 commandes et tickets, 6 000 paiements, 12 000
// entrées de journal de ~7 Ko (états avant / après complets, comme
// ExecuteOrderMutation). Lancée seulement avec FISCAL_PERF=1 ; journalise la
// durée, la taille et le pic de mémoire du tas.
func TestFiscalArchiveBuildPerf_Postgres(t *testing.T) {
	if os.Getenv("FISCAL_PERF") != "1" {
		t.Skip("FISCAL_PERF=1 not set — skipping archive build measurement")
	}
	db := pgtest.Open(t)
	ctx := context.Background()
	const siret = "siret-itest-arch-perf"

	cleanup := func(mid string) {
		for _, q := range []string{
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
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Archive Perf', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', 'mt-arch-perf', 'Europe/Paris')
		RETURNING id`, siret).Scan(&mid); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(mid, 10)
	t.Cleanup(func() { cleanup(merchantID) })

	seed := func(label, q string) {
		t.Helper()
		start := time.Now()
		if _, err := db.ExecContext(ctx, q, merchantID); err != nil {
			t.Fatalf("seed %s: %v", label, err)
		}
		t.Logf("données %s : %v", label, time.Since(start).Round(time.Millisecond))
	}
	// Généré côté serveur (generate_series) : septembre 2026.
	seed("commandes", `
		INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, order_source, price, TVA, HT, delivered_on, created_by)
		SELECT $1::text, g, 'WELLO_RESTO', 'CLOSED', 'CLOSED', 'IN', 'WELLO_RESTO_POS', 2500, 227, 2273,
		       timestamptz '2026-09-01 10:00:00+02' + (g % 29) * interval '1 day', 'itest'
		FROM generate_series(1, 3000) g`)
	seed("lignes", `
		INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, base_price, price, tva_rate)
		SELECT order_id, 1, $1::text, 1, 2500, 2500, 10 FROM orders WHERE merchant_id = $1::text`)
	seed("tickets", `
		INSERT INTO receipts (receipt_id, merchant_id, order_id, receipt_number, total_ttc, total_ht, tax_details, items_snapshot, payments_snapshot, created_at, prev_hash, hash, signature)
		SELECT 'itest-perf-' || order_id, $1::text, order_id, 'F-2026-' || lpad(order_num::text, 6, '0'), 2500, 2273,
		       '{"lines":[{"rate":10,"ttc":2500,"ht":2273,"tva":227}],"discount":0}',
		       '[{"name":"Plat","quantity":1,"price_ttc":2500,"tax_rate":1000,"tax_amount":227,"kind":"article","total_ttc":2500,"total_ht":2273,"total_tva":227}]',
		       '[]', delivered_on, repeat('a', 64), repeat('b', 64), repeat('c', 64)
		FROM orders WHERE merchant_id = $1::text`)
	seed("paiements", `
		INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, operation_type, enabled, payment_date, previous_hash, hash, signature)
		SELECT $1::text, '42', order_id, 1250, CASE WHEN k = 1 THEN 'CB' ELSE 'ES' END, 'SALE', TRUE, delivered_on, repeat('a', 64), repeat('b', 64), repeat('c', 64)
		FROM orders, generate_series(1, 2) k WHERE merchant_id = $1::text`)
	seed("journal", `
		INSERT INTO audit_logs (id, user_id, merchant_id, action, resource_type, resource_id, old_values, new_values, previous_hash, hash, signature, hash_version, created_at)
		SELECT 'itest-perf-' || order_id || '-' || k, '42', $1::text, 'ORDER_UPDATE', 'orders', order_id::text,
		       jsonb_build_object('state', 'OPEN', 'payload', repeat('x', 3500)),
		       jsonb_build_object('state', 'CLOSED', 'payload', repeat('y', 3500)),
		       repeat('a', 64), repeat('b', 64), repeat('c', 64), 2, delivered_on + k * interval '1 minute'
		FROM orders, generate_series(1, 4) k WHERE merchant_id = $1::text`)

	// Pic du tas pendant la construction, échantillonné toutes les 5 ms.
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	var peak atomic.Uint64
	stop := make(chan struct{})
	go func() {
		var m runtime.MemStats
		for {
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
				runtime.ReadMemStats(&m)
				if m.HeapAlloc > peak.Load() {
					peak.Store(m.HeapAlloc)
				}
			}
		}
	}()
	start := time.Now()
	built, err := Build(ctx, db, merchantID, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), KindMonth, time.Now().UTC())
	elapsed := time.Since(start)
	close(stop)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var journalBytes int
	for _, f := range built.Manifest.Fichiers {
		t.Logf("  %s : %d lignes, %d octets", f.Nom, f.Lignes, f.Octets)
		if f.Nom == "journal.csv" {
			journalBytes = f.Octets
		}
	}
	t.Logf("construction : %v ; ZIP : %.1f Mo (journal en clair : %.1f Mo) ; tas avant : %.1f Mo, pic : %.1f Mo",
		elapsed.Round(time.Millisecond), float64(len(built.Zip))/1e6, float64(journalBytes)/1e6,
		float64(before.HeapAlloc)/1e6, float64(peak.Load())/1e6)

	// Contrôle croisé (phase 5) : durée de Verify sur le même ZIP. Le jeu n'a
	// pas de clôtures : les anomalies « sans clôture » sont attendues.
	start = time.Now()
	report, err := Verify(built.Zip)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	t.Logf("vérification : %v ; %d fichiers, %d tickets, %d anomalies", time.Since(start).Round(time.Millisecond),
		report.Files, report.Tickets, report.Anomalies)
}
