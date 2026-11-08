//go:build postgres_integration

package fiscalarchive

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/fiscal"
)

// Chaîne commencée en cours de mois (mise en service le 9 octobre) : l'archive
// mensuelle d'octobre couvre les jours clos, et les jours précédents ont une
// archive à la demande marquée « antérieure à la première clôture ».
func TestFiscalArchive_FirstMonthPartial_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const siret = "siret-itest-archive-firstmonth"

	cleanup := func(mid string) {
		for _, q := range []string{
			`DELETE FROM fiscal_archives WHERE merchant_id = $1`,
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
	var mid int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, creation_date)
		VALUES ('ITest Archive Premier Mois', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', 'mt-archive-fm', 'Europe/Paris', '2025-01-01')
		RETURNING id`, siret).Scan(&mid); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(mid, 10)
	t.Cleanup(func() { cleanup(merchantID) })
	loc, _ := time.LoadLocation("Europe/Paris")
	at := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, loc) }

	// Une vente le 3 octobre (avant la chaîne) et une le 12 (après).
	for i, day := range []int{3, 12} {
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, order_source, price, TVA, HT, delivered_on, created_by)
			VALUES ($1, $2, 'WELLO_RESTO', 'CLOSED', 'CLOSED', 'IN', 'WELLO_RESTO_POS', 1100, 100, 1000, $3, 'itest') RETURNING order_id`,
			merchantID, i+1, at(day, 13)).Scan(&id); err != nil {
			t.Fatalf("seed order: %v", err)
		}
		for label, q := range map[string][]any{
			`INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, base_price, price, tva_rate)
				VALUES ($1, 1, $2, 1, 1100, 1100, 10)`: {id, merchantID},
			`INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, operation_type, enabled, payment_date)
				VALUES ($1, 'itest', $2, 1100, 'CB', 'SALE', TRUE, $3)`: {merchantID, id, at(day, 13)},
		} {
			if _, err := db.ExecContext(ctx, label, q...); err != nil {
				t.Fatalf("seed order rows: %v", err)
			}
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO receipts (receipt_id, merchant_id, order_id, receipt_number, total_ttc, total_ht, tax_details, items_snapshot, payments_snapshot, created_at, prev_hash, hash, signature)
			VALUES ($1, $2, $3, $4, 1100, 1000, '{"lines":[{"rate":10,"ttc":1100,"ht":1000,"tva":100}],"discount":0}',
			        '[{"name":"Plat","quantity":1,"price_ttc":1100,"tax_rate":1000,"tax_amount":100,"kind":"article","total_ttc":1100,"total_ht":1000,"total_tva":100}]',
			        '[]', $5, '', 'h', 's')`,
			fmt.Sprintf("itest-fm-%s-%d", merchantID, i), merchantID, id, fmt.Sprintf("F-2026-8%05d", i+1), at(day, 13)); err != nil {
			t.Fatalf("seed receipt: %v", err)
		}
	}

	// La chaîne commence le 9 octobre ; octobre est clos le 31, novembre commencé.
	from := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	if _, err := fiscal.CloseDueDays(ctx, db, merchantID, "Europe/Paris", &from, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 2, 12, 0, 0, 0, loc)); err != nil {
		t.Fatalf("CloseDueDays: %v", err)
	}

	store := &memStore{}
	manifestOf := func(a *Archive) Manifest {
		t.Helper()
		zipBytes := store.files[a.R2Key]
		zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range zr.File {
			if f.Name != "MANIFEST.json" {
				continue
			}
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			var m Manifest
			if err := json.Unmarshal(b, &m); err != nil {
				t.Fatal(err)
			}
			return m
		}
		t.Fatal("no MANIFEST.json")
		return Manifest{}
	}

	pending, err := PendingMonths(ctx, dbx.GetDB(ctx, db), merchantID, 10)
	if err != nil || len(pending) != 1 || pending[0].Start != time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("PendingMonths = (%+v, %v)", pending, err)
	}

	// Archive mensuelle d'octobre : du 9 au 31, sans anomalie.
	oct := Request{MerchantID: merchantID, Kind: KindMonth, Start: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		End: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), GeneratedBy: GeneratedBySystem}
	a, err := Generate(ctx, db, store, oct)
	if err != nil {
		t.Fatalf("Generate(October): %v", err)
	}
	if a.PeriodStart != "2026-10-09" || a.PeriodEnd != "2026-10-31" {
		t.Fatalf("October archive period = %s..%s, want 2026-10-09..2026-10-31", a.PeriodStart, a.PeriodEnd)
	}
	if a.Check == nil || !a.Check.OK() || a.Check.Tickets != 1 || a.Check.Days != 23 {
		t.Fatalf("October self-check: %+v", a.Check)
	}
	if m := manifestOf(a); m.Periode["debut"] != "2026-10-09" || m.Periode[ManifestFirstClosure] != "2026-10-09" ||
		m.Periode[ManifestBeforeFirstClosure] != nil {
		t.Fatalf("October manifest periode: %v", m.Periode)
	}

	// Idempotent malgré le début au 9 : même archive, rien de plus à archiver.
	again, err := Generate(ctx, db, store, oct)
	if err != nil || again.ID != a.ID || store.uploads != 1 {
		t.Fatalf("second Generate = (%+v, %v), uploads %d", again, err, store.uploads)
	}
	if pending, _ := PendingMonths(ctx, dbx.GetDB(ctx, db), merchantID, 10); len(pending) != 0 {
		t.Fatalf("after October, PendingMonths = %+v", pending)
	}

	// Jours précédant la première clôture : archive à la demande, marquée, sans
	// anomalie « sans clôture journalière ».
	pre, err := Generate(ctx, db, store, Request{MerchantID: merchantID, Kind: KindPeriod,
		Start: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC), GeneratedBy: "999999"})
	if err != nil {
		t.Fatalf("Generate(1-8 October): %v", err)
	}
	if pre.Check == nil || !pre.Check.OK() || pre.Check.Tickets != 1 || pre.Check.Days != 0 {
		t.Fatalf("pre-chain self-check: %+v", pre.Check)
	}
	if m := manifestOf(pre); m.Periode[ManifestBeforeFirstClosure] != true || m.Periode[ManifestFirstClosure] != "2026-10-09" {
		t.Fatalf("pre-chain manifest periode: %v", m.Periode)
	}

	// Une période à cheval sur la première clôture reste refusée : ses jours
	// d'avant n'ont pas de clôture.
	if _, err := Generate(ctx, db, store, Request{MerchantID: merchantID, Kind: KindPeriod,
		Start: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC), GeneratedBy: "999999"}); !errors.Is(err, ErrPeriodNotClosed) {
		t.Fatalf("Generate(straddling) = %v, want ErrPeriodNotClosed", err)
	}

	// Chaîne des archives sans fourche.
	all, err := List(ctx, dbx.GetDB(ctx, db), merchantID)
	if err != nil || len(all) != 2 {
		t.Fatalf("expected 2 archives, got %d (%v)", len(all), err)
	}
	if all[0].PreviousHash != a.Hash || a.PreviousHash != fiscal.GenesisHash {
		t.Fatalf("archive chain: %s -> %s, first parent %s", a.Hash, all[0].PreviousHash, a.PreviousHash)
	}
}
