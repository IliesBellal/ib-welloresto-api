//go:build postgres_integration

package fiscalarchive

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
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
	"welloresto-api/internal/utils/dbutils"
)

type memStore struct {
	files   map[string][]byte
	uploads int
}

func (m *memStore) UploadPrivateFile(_ context.Context, key string, file io.Reader, _ string) (string, error) {
	b, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}
	if m.files == nil {
		m.files = map[string][]byte{}
	}
	m.files[key] = b
	m.uploads++
	return key, nil
}

type failingStore struct{}

func (failingStore) UploadPrivateFile(context.Context, string, io.Reader, string) (string, error) {
	return "", errors.New("r2 indisponible")
}

// Archive fiscale (lot D conformité caisse, phase 2).
func TestFiscalArchive_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const siret = "siret-itest-archive"

	cleanup := func(mid string) {
		for _, q := range []string{
			`DELETE FROM fiscal_archives WHERE merchant_id = $1`,
			`DELETE FROM fiscal_closures WHERE merchant_id = $1`,
			`DELETE FROM receipts WHERE merchant_id = $1`,
			`DELETE FROM payments WHERE merchant_id = $1`,
			`DELETE FROM audit_logs WHERE merchant_id = $1`,
			`DELETE FROM extra WHERE merchant_id = $1`,
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
		VALUES ('ITest Archive', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', 'mt-archive', 'Europe/Paris', '2025-01-01')
		RETURNING id`, siret).Scan(&mid); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(mid, 10)
	t.Cleanup(func() { cleanup(merchantID) })
	loc, _ := time.LoadLocation("Europe/Paris")
	at := func(d, h int) time.Time { return time.Date(2026, 9, d, h, 0, 0, 0, loc) }
	exec := func(label, q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}

	// Septembre 2026 : deux ventes avec tickets et paiements, un avoir, un
	// registre fermé, une entrée de journal ; une vente le 1er octobre.
	var orderIDs []int64
	for i, day := range []int{3, 15} {
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, order_source, price, TVA, HT, delivered_on, created_by)
			VALUES ($1, $2, 'WELLO_RESTO', 'CLOSED', 'CLOSED', 'IN', 'WELLO_RESTO_POS', 1100, 100, 1000, $3, 'itest') RETURNING order_id`,
			merchantID, i+1, at(day, 13)).Scan(&id); err != nil {
			t.Fatalf("seed order: %v", err)
		}
		orderIDs = append(orderIDs, id)
		exec("line", `INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, base_price, price, tva_rate)
			VALUES ($1, 1, $2, 1, 1100, 1100, 10)`, id, merchantID)
		exec("payment", `INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, operation_type, enabled, payment_date)
			VALUES ($1, 'itest', $2, 1100, 'CB', 'SALE', TRUE, $3)`, merchantID, id, at(day, 13))
		exec("receipt", `INSERT INTO receipts (receipt_id, merchant_id, order_id, receipt_number, total_ttc, total_ht, tax_details, items_snapshot, payments_snapshot, created_at, prev_hash, hash, signature)
			VALUES ($1, $2, $3, $4, 1100, 1000, '{"lines":[{"rate":10,"ttc":1100,"ht":1000,"tva":100}],"discount":0}',
			        $5, '[]', $6, '', 'h', 's')`,
			fmt.Sprintf("itest-arch-%s-%d", merchantID, i), merchantID, id, fmt.Sprintf("F-2026-9%05d", i+1),
			[]string{
				`[{"name":"Plat ; « maison »","quantity":1,"price_ttc":1100,"tax_rate":1000,"tax_amount":100,"kind":"article","total_ttc":1100,"total_ht":1000,"total_tva":100}]`,
				`[{"name":"=SOMME(A1)","quantity":1,"price_ttc":1100,"tax_rate":1000,"tax_amount":100,"kind":"article","total_ttc":1100,"total_ht":1000,"total_tva":100}]`,
			}[i], at(day, 13))
	}
	exec("refund", `INSERT INTO receipts (receipt_id, merchant_id, order_id, receipt_number, total_ttc, total_ht, tax_details, items_snapshot, payments_snapshot, created_at, prev_hash, hash, signature)
		VALUES ($1, $2, $3, 'F-2026-900003', -300, -273, '{"lines":[{"rate":10,"ttc":-300,"ht":-273,"tva":-27}],"discount":0}', '[]', '[]', $4, '', 'h', 's')`,
		"itest-arch-"+merchantID+"-r", merchantID, orderIDs[1], at(20, 11))
	var reg int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO cash_registers (merchant_id, cash_desk_id, device_id, user_id, cash_fund, start_date, closure_comment, closed, end_date)
		VALUES ($1, 1, 'itest-archive', 'itest', 10000, $2, '', TRUE, $3) RETURNING cash_register_id`, merchantID, at(15, 9), at(15, 23)).Scan(&reg); err != nil {
		t.Fatalf("seed register: %v", err)
	}
	exec("z", `INSERT INTO cash_registers_items (cash_register_id, mop, amount) VALUES ($1, 'CB', 1100)`, reg)
	if err := dbutils.RunInTx(ctx, db, func(txCtx context.Context) error {
		return fiscal.AppendAuditLog(txCtx, dbx.GetDB(txCtx, db), fiscal.AuditEntry{
			ID: "itest-arch-audit-" + merchantID, MerchantID: merchantID, UserID: "itest", Action: "ORDER_CLOSE",
			ResourceType: "orders", ResourceID: strconv.FormatInt(orderIDs[0], 10),
			OldValues: []byte(`{"state":"OPEN"}`), NewValues: []byte(`{"state":"CLOSED"}`),
		})
	}); err != nil {
		t.Fatalf("seed audit: %v", err)
	}
	// La date d'écriture du journal est « maintenant » : on la ramène en
	// septembre (données de test, hors chaîne vérifiée ici).
	exec("audit date", `UPDATE audit_logs SET created_at = $2 WHERE merchant_id = $1`, merchantID, at(3, 14))

	// Clôtures d'août et de septembre (jours + mois) et du 1er octobre.
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if _, err := fiscal.CloseDueDays(ctx, db, merchantID, "Europe/Paris", &from, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 2, 12, 0, 0, 0, loc)); err != nil {
		t.Fatalf("CloseDueDays: %v", err)
	}

	store := &memStore{}

	// Tâche horaire (phase 3) : mois clos sans archive, du plus ancien au
	// plus récent ; octobre n'est pas clos.
	pending, err := PendingMonths(ctx, dbx.GetDB(ctx, db), merchantID, 10)
	if err != nil || len(pending) != 2 || pending[0].Start != time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC) ||
		pending[1].Start != time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("PendingMonths = (%+v, %v)", pending, err)
	}
	// Échéance atteinte : rien n'est tenté.
	if res, err := GenerateDue(ctx, db, store, merchantID, 10, time.Now(), DueHooks{}); err != nil || res != (DueResult{Remaining: 2}) {
		t.Fatalf("GenerateDue(past deadline) = (%+v, %v)", res, err)
	}
	// Envoi impossible : deux échecs signalés, rien d'écrit, retentés plus tard.
	var failed []MonthRef
	res, err := GenerateDue(ctx, db, failingStore{}, merchantID, 10, time.Now().Add(time.Minute),
		DueHooks{OnError: func(m MonthRef, _ error) { failed = append(failed, m) }})
	if err != nil || res != (DueResult{Failed: 2}) || len(failed) != 2 {
		t.Fatalf("GenerateDue(failing store) = (%+v, %v), failed %v", res, err, failed)
	}
	if again, _ := PendingMonths(ctx, dbx.GetDB(ctx, db), merchantID, 10); len(again) != 2 {
		t.Fatalf("a failed upload must leave both months pending, got %+v", again)
	}

	sept := Request{MerchantID: merchantID, Kind: KindMonth, Start: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		End: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), GeneratedBy: GeneratedBySystem}

	a, err := Generate(ctx, db, store, sept)
	if err != nil {
		t.Fatalf("Generate(September): %v", err)
	}
	zipBytes := store.files[a.R2Key]
	if a.Check == nil || !a.Check.OK() {
		t.Fatalf("generation self-check: %+v", a.Check)
	}
	if sum := fmt.Sprintf("%x", sha256.Sum256(zipBytes)); sum != a.SHA256 || int64(len(zipBytes)) != a.SizeBytes {
		t.Fatalf("stored file does not match the sealed sha256/size")
	}
	if a.Filename != "WelloResto_archive_siret-itest-archive_2026-09.zip" || a.PreviousHash != fiscal.GenesisHash {
		t.Fatalf("archive row: %+v", a)
	}

	// Contenu : 9 CSV, notice, manifeste ; chaque empreinte du manifeste se
	// recalcule.
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		files[f.Name] = b
	}
	if len(files) != 11 {
		t.Fatalf("expected 11 files, got %d", len(files))
	}
	var manifest Manifest
	if err := json.Unmarshal(files["MANIFEST.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(files["MANIFEST.json"])) != a.ManifestSHA256 || len(manifest.Fichiers) != 10 {
		t.Fatalf("manifest: %+v", manifest)
	}
	for _, mf := range manifest.Fichiers {
		if fmt.Sprintf("%x", sha256.Sum256(files[mf.Nom])) != mf.SHA256 {
			t.Fatalf("manifest sha256 mismatch for %s", mf.Nom)
		}
	}
	readCSV := func(name string) [][]string {
		t.Helper()
		content := bytes.TrimPrefix(files[name], []byte{0xEF, 0xBB, 0xBF})
		r := csv.NewReader(bytes.NewReader(content))
		r.Comma = ';'
		rows, err := r.ReadAll()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return rows
	}
	tickets := readCSV("tickets.csv")
	if len(tickets) != 4 || tickets[1][0] != "F-2026-900001" || tickets[3][1] != "AVOIR" || tickets[3][10] != "-3,00" {
		t.Fatalf("tickets.csv: %v", tickets)
	}
	if lines := readCSV("tickets_lignes.csv"); len(lines) != 3 || lines[1][3] != "Plat ; « maison »" || lines[1][8] != "1000" ||
		lines[2][3] != "'=SOMME(A1)" {
		t.Fatalf("tickets_lignes.csv: %v", lines)
	}
	if rows := readCSV("commandes.csv"); len(rows) != 3 {
		t.Fatalf("commandes.csv: %v", rows)
	}
	if rows := readCSV("paiements.csv"); len(rows) != 3 {
		t.Fatalf("paiements.csv: %v", rows)
	}
	if rows := readCSV("journal.csv"); len(rows) != 2 || rows[1][3] != "ORDER_CLOSE" {
		t.Fatalf("journal.csv: %v", rows)
	}
	// 30 jours + le mois ; pas le 1er octobre.
	if rows := readCSV("clotures.csv"); len(rows) != 32 || rows[len(rows)-1][0] != "MONTH" {
		t.Fatalf("clotures.csv: %d rows, last %v", len(rows), rows[len(rows)-1])
	}
	if rows := readCSV("registres.csv"); len(rows) != 2 || !strings.Contains(rows[1][11], `"amount" : 1100`) && !strings.Contains(rows[1][11], `"amount":1100`) {
		t.Fatalf("registres.csv: %v", rows)
	}
	if !strings.Contains(string(files["NOTICE.txt"]), "ARCHIVE FISCALE") {
		t.Fatal("missing notice")
	}

	// Contrôle croisé (phase 5) : manifeste, lignes / TVA de chaque ticket,
	// clôtures journalières recalculées depuis les tickets, mois = somme des
	// jours.
	report, err := Verify(zipBytes)
	if err != nil || !report.OK() || report.Files != 10 || report.Tickets != 3 || report.Days != 30 || report.Months != 1 {
		t.Fatalf("Verify(September) = (%+v, %v)", report, err)
	}
	// Archive retouchée : un montant de ticket modifié dans tickets.csv.
	tampered := rezip(t, zipBytes, "tickets.csv", func(b []byte) []byte {
		return bytes.Replace(b, []byte(";1100;1000;100;11,00;"), []byte(";1200;1000;200;12,00;"), 1)
	})
	if report, err := Verify(tampered); err != nil || report.OK() || !strings.Contains(strings.Join(report.Problems, "\n"), "tickets.csv : empreinte") {
		t.Fatalf("Verify(tampered) = (%+v, %v), want a manifest mismatch", report, err)
	}
	// TVA d'un ticket retouchée : repérée aussi par le recoupement avec ses
	// lignes et avec la clôture du jour.
	tampered = rezip(t, zipBytes, "tickets_tva.csv", func(b []byte) []byte {
		return bytes.Replace(b, []byte("F-2026-900001;10;1100;1000;100;0"), []byte("F-2026-900001;10;1200;1100;100;0"), 1)
	})
	if report, err := Verify(tampered); err != nil {
		t.Fatal(err)
	} else if all := strings.Join(report.Problems, "\n"); !strings.Contains(all, "ticket F-2026-900001 : lignes") ||
		!strings.Contains(all, "clôture du 2026-09-03") {
		t.Fatalf("Verify(tampered VAT) problems: %v", report.Problems)
	}

	// Ligne scellée : se re-scelle à l'identique depuis la base.
	stored, err := List(ctx, dbx.GetDB(ctx, db), merchantID)
	if err != nil || len(stored) != 1 {
		t.Fatalf("List = (%v, %v)", stored, err)
	}
	if h, s, _ := fiscal.Seal(fiscal.ChainFiscalArchives, stored[0].PreviousHash, PayloadOf(stored[0])); h != stored[0].Hash || s != stored[0].Signature {
		t.Fatal("archive row does not re-seal identically from the database")
	}

	// Idempotent pour un mois : même archive, aucun nouvel envoi.
	again, err := Generate(ctx, db, store, sept)
	if err != nil || again.ID != a.ID || store.uploads != 1 {
		t.Fatalf("second Generate = (%+v, %v), uploads %d", again, err, store.uploads)
	}

	if pending, _ := PendingMonths(ctx, dbx.GetDB(ctx, db), merchantID, 10); len(pending) != 1 || pending[0].Start.Month() != time.August {
		t.Fatalf("after September, PendingMonths = %+v", pending)
	}

	// Octobre n'est pas clos.
	oct := Request{MerchantID: merchantID, Kind: KindMonth, Start: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		End: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), GeneratedBy: GeneratedBySystem}
	if _, err := Generate(ctx, db, store, oct); !errors.Is(err, ErrPeriodNotClosed) {
		t.Fatalf("Generate(October) = %v, want ErrPeriodNotClosed", err)
	}

	// À la demande : période close quelconque, chaînée sur la précédente.
	p, err := Generate(ctx, db, store, Request{MerchantID: merchantID, Kind: KindPeriod,
		Start: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC), GeneratedBy: "999999"})
	if err != nil {
		t.Fatalf("Generate(PERIOD): %v", err)
	}
	if report, err := Verify(store.files[p.R2Key]); err != nil || !report.OK() || report.Days != 3 || report.Months != 0 {
		t.Fatalf("Verify(period) = (%+v, %v)", report, err)
	}
	if p.PreviousHash != a.Hash || p.GeneratedBy != "999999" || !strings.HasPrefix(p.Filename, "WelloResto_archive_siret-itest-archive_2026-09-14_2026-09-16_") {
		t.Fatalf("period archive: %+v", p)
	}

	// Deux instances génèrent août en même temps : une seule archive, la même
	// pour les deux, chaînée sans fourche.
	aug := Request{MerchantID: merchantID, Kind: KindMonth, Start: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		End: time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC), GeneratedBy: GeneratedBySystem}
	type result struct {
		a   *Archive
		err error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			a, err := Generate(ctx, db, &memStore{}, aug)
			results <- result{a, err}
		}()
	}
	r1, r2 := <-results, <-results
	if r1.err != nil || r2.err != nil || r1.a.ID != r2.a.ID {
		t.Fatalf("concurrent August = (%+v, %v) / (%+v, %v)", r1.a, r1.err, r2.a, r2.err)
	}
	all, err := List(ctx, dbx.GetDB(ctx, db), merchantID)
	if err != nil || len(all) != 3 {
		t.Fatalf("expected 3 archives (September, period, August), got %d (%v)", len(all), err)
	}
	parents := map[string]bool{}
	for _, x := range all {
		if parents[x.PreviousHash] {
			t.Fatalf("fork: two archives share the parent %s", x.PreviousHash)
		}
		parents[x.PreviousHash] = true
	}
	// Plus rien à archiver : un passage de la tâche ne fait rien.
	if res, err := GenerateDue(ctx, db, store, merchantID, 10, time.Now().Add(time.Minute), DueHooks{}); err != nil || res != (DueResult{}) {
		t.Fatalf("GenerateDue(nothing pending) = (%+v, %v)", res, err)
	}
}

// rezip recopie une archive en transformant un de ses fichiers.
func rezip(t *testing.T, src []byte, name string, edit func([]byte) []byte) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		if f.Name == name {
			edited := edit(b)
			if bytes.Equal(edited, b) {
				t.Fatalf("rezip: %s unchanged", name)
			}
			b = edited
		}
		w, _ := zw.Create(f.Name)
		_, _ = w.Write(b)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
