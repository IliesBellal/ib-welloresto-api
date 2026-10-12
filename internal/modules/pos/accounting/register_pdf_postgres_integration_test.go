//go:build postgres_integration

package accounting

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
	"welloresto-api/internal/modules/cash_registers"
)

// PDF des registres de caisse (docs/EXPORT_REGISTRES_PDF.md) : contenu selon le
// mode de clôture du registre, refus (non validé, inconnu, autre établissement),
// ZIP par identifiants et par période avec ignores.txt, registre en dérive.
func TestRegisterPDFExport_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	type fixture struct {
		merchantID string
		userID     string
		cashDeskID int64
	}

	cleanup := func(f fixture) {
		for _, q := range []string{
			`DELETE FROM payments WHERE merchant_id = $1`,
			`DELETE FROM orderitems WHERE merchant_id = $1`,
			`DELETE FROM orders WHERE merchant_id = $1`,
			`DELETE FROM products WHERE merchant_id = $1`,
			`DELETE FROM cash_registers_custom_items WHERE merchant_id = $1`,
			`DELETE FROM cash_registers_items WHERE cash_register_id IN (SELECT cash_register_id FROM cash_registers WHERE merchant_id = $1)`,
			`DELETE FROM cash_registers WHERE merchant_id = $1`,
			`DELETE FROM cash_desks WHERE merchant_id = $1`,
			`DELETE FROM merchant_parameters WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE CAST(id AS TEXT) = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, f.merchantID)
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id = $1`, f.userID)
	}

	setupMerchant := func(siret, userID string) fixture {
		t.Helper()
		var old int64
		if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = $1 LIMIT 1`, siret).Scan(&old); err == nil {
			cleanup(fixture{merchantID: strconv.FormatInt(old, 10), userID: userID})
		}
		var mid int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, vat_number)
			VALUES ('ITest Register PDF', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', $1, 'Europe/Paris', 'FR999')
			RETURNING id`, siret).Scan(&mid); err != nil {
			t.Fatalf("seed merchant %s: %v", siret, err)
		}
		merchantID := strconv.FormatInt(mid, 10)
		if _, err := db.ExecContext(ctx, `INSERT INTO merchant_parameters (merchant_id, last_menu_update, currency) VALUES ($1, now(), 'EUR')`, merchantID); err != nil {
			t.Fatalf("seed merchant_parameters: %v", err)
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO users (user_id, name, first_name, last_name, password, email, token)
			VALUES ($1, $1, 'Reg', 'Testeur', 'x', $2, $1)
			ON CONFLICT (user_id) DO NOTHING`, userID, userID+"@example.com"); err != nil {
			t.Fatalf("seed users: %v", err)
		}
		var deskID int64
		if err := db.QueryRowContext(ctx, `INSERT INTO cash_desks (merchant_id, name) VALUES ($1, 'ITest Reg Desk') RETURNING cash_desk_id`, merchantID).Scan(&deskID); err != nil {
			t.Fatalf("seed cash_desks: %v", err)
		}
		f := fixture{merchantID: merchantID, userID: userID, cashDeskID: deskID}
		t.Cleanup(func() { cleanup(f) })
		return f
	}

	fA := setupMerchant("siret-regpdf-a", "itest-regpdf-user-a")
	fB := setupMerchant("siret-regpdf-b", "itest-regpdf-user-b")

	_, _ = db.ExecContext(ctx, `DELETE FROM tva_categories WHERE tva_title LIKE 'itest-regpdf-%'`)
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM tva_categories WHERE tva_title LIKE 'itest-regpdf-%'`) })
	var cat10 int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO tva_categories (delivery_type, tva_title, tva_desc, tva_rate)
		VALUES ('0', 'itest-regpdf-10', 'd', 10) RETURNING tva_id`).Scan(&cat10); err != nil {
		t.Fatalf("seed tva_categories: %v", err)
	}
	var product int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, price, category, tva_in_id, tva_take_away_id, tva_delivery_id)
		VALUES ($1, 'itest-regpdf-plat', 0, 'itest', $2, $2, $2) RETURNING product_id`, fA.merchantID, cat10).Scan(&product); err != nil {
		t.Fatalf("seed products: %v", err)
	}

	crRepo := cash_registers.NewCashRegisterRepository(db)
	svc := NewAccountingService(NewAccountingRepository(db), crRepo)
	paris, _ := time.LoadLocation("Europe/Paris")
	registerStart := time.Date(2026, 9, 1, 12, 0, 0, 0, paris)
	orderNum := 0

	// newRegister ouvre un registre de fA, y rattache une commande de 20 € à
	// 10 % payée en CB, puis le ferme. mode force closing_mode ; enclose valide
	// le registre ; manualReal ressaisit le réel (relevé) en CB pour 20 €.
	newRegister := func(mode string, enclose, manualReal bool) (regID string, orderID int64) {
		t.Helper()
		req := &models.OpenCashRegisterRequest{DeviceID: "itest-regpdf-" + strconv.FormatInt(time.Now().UnixNano(), 10)}
		req.CashRegister.CashDeskID = strconv.FormatInt(fA.cashDeskID, 10)
		req.CashRegister.UserID = fA.userID
		opened, err := crRepo.OpenCashRegister(ctx, req, fA.merchantID)
		if err != nil || opened.CashRegister == nil {
			t.Fatalf("OpenCashRegister: (%+v, %v)", opened, err)
		}
		regID = opened.CashRegister.CashRegisterId
		if _, err := db.ExecContext(ctx, `UPDATE cash_registers SET start_date = $1, closing_mode = $2 WHERE cash_register_id = $3`, registerStart.UTC(), mode, regID); err != nil {
			t.Fatalf("force start_date / closing_mode: %v", err)
		}

		orderNum++
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, cash_register_id, order_num, brand, brand_status, state, price, TVA, HT, created_by, order_type, delivery_fees, creation_date, order_source)
			VALUES ($1, $2, $3, 'WELLO_RESTO', 'CLOSED', 'CLOSED', 0, 0, 0, $4, 'IN', 0, $5, 'WELLO_RESTO_POS')
			RETURNING order_id`, fA.merchantID, regID, orderNum, fA.userID, registerStart.UTC()).Scan(&orderID); err != nil {
			t.Fatalf("seed order: %v", err)
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, price) VALUES ($1, $2, $3, 1, 2000)`,
			orderID, product, fA.merchantID); err != nil {
			t.Fatalf("seed orderitem: %v", err)
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, cash_register_id, enabled)
			VALUES ($1, $2, $3, 2000, 'CB', $4, true)`, fA.merchantID, fA.userID, orderID, regID); err != nil {
			t.Fatalf("seed payment: %v", err)
		}

		if _, err := crRepo.CloseCashRegister(ctx, regID, fA.merchantID, &models.CloseCashRegisterRequest{}); err != nil {
			t.Fatalf("CloseCashRegister: %v", err)
		}
		if manualReal {
			user := &auth.UserLoginRow{UserID: fA.userID, MerchantID: fA.merchantID}
			if _, err := crRepo.AddCustomItem(ctx, regID, &models.AddCustomItemRequest{Label: "CB", Value: 2000}, user); err != nil {
				t.Fatalf("AddCustomItem: %v", err)
			}
		}
		if enclose {
			if err := crRepo.EncloseCashRegister(ctx, fA.userID, regID, "itest"); err != nil {
				t.Fatalf("EncloseCashRegister: %v", err)
			}
		}
		return regID, orderID
	}

	autoReg, _ := newRegister(cash_registers.ClosingModeAuto, true, false)
	manualReg, manualOrder := newRegister(cash_registers.ClosingModeManual, true, true)
	openReg, _ := newRegister(cash_registers.ClosingModeManual, false, false) // fermé mais non validé

	ctxA := middleware.WithUser(ctx, &auth.UserLoginRow{UserID: fA.userID, MerchantID: fA.merchantID})
	ctxB := middleware.WithUser(ctx, &auth.UserLoginRow{UserID: fB.userID, MerchantID: fB.merchantID})

	pdfText := func(regID string) string {
		t.Helper()
		pdf, filename, err := svc.ExportRegisterPDF(ctxA, regID)
		if err != nil {
			t.Fatalf("ExportRegisterPDF(%s): %v", regID, err)
		}
		if filename != "WR_registre_"+regID+".pdf" || !bytes.HasPrefix(pdf, []byte("%PDF")) {
			t.Fatalf("ExportRegisterPDF(%s) = (%q, %d octets), want un PDF WR_registre_%s.pdf", regID, filename, len(pdf), regID)
		}
		return extractPDFText(t, pdf)
	}
	mustContain := func(label, text string, wants ...string) {
		t.Helper()
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s : %q absent du PDF", label, want)
			}
		}
	}

	// AUTO : TVA ventilée sur les paiements (20 € à 10 % : HT 18,18 / TVA 1,82),
	// encaissements = paiements enregistrés.
	autoText := pdfText(autoReg)
	mustContain("AUTO", autoText, "Registre de caisse", "Automatique", "TVA", "itest-regpdf-10", "18.18 EUR", "1.82 EUR", "Encaissements", "20.00 EUR")
	if strings.Contains(autoText, "Attention") {
		t.Errorf("AUTO : pas de mention de dérive attendue")
	}

	// MANUAL : TVA sur les lignes ; encaissements = réel du relevé de caisse.
	manualText := pdfText(manualReg)
	mustContain("MANUAL", manualText, "Registre de caisse", "Manuel", "itest-regpdf-10", "Encaissements", "20.00 EUR")
	if strings.Contains(manualText, "Attention") {
		t.Errorf("MANUAL sain : pas de mention de dérive attendue")
	}

	// Refus : non validé, inconnu, identifiant invalide, autre établissement.
	if _, _, err := svc.ExportRegisterPDF(ctxA, openReg); !errors.Is(err, models.ErrRegisterNotEnclosed) {
		t.Errorf("registre non validé : err = %v, want ErrRegisterNotEnclosed", err)
	}
	for _, id := range []string{"999999999", "abc"} {
		if _, _, err := svc.ExportRegisterPDF(ctxA, id); !errors.Is(err, models.ErrNotFound) {
			t.Errorf("registre %q : err = %v, want ErrNotFound", id, err)
		}
	}
	if _, _, err := svc.ExportRegisterPDF(ctxB, autoReg); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("registre d'un autre établissement : err = %v, want ErrNotFound (jamais son contenu)", err)
	}

	// zipEntries lit un ZIP d'export : nom -> contenu.
	zipEntries := func(export *RegistersExport) map[string]string {
		t.Helper()
		zr, err := zip.NewReader(bytes.NewReader(export.Data), int64(len(export.Data)))
		if err != nil {
			t.Fatalf("ZIP invalide: %v", err)
		}
		out := map[string]string{}
		for _, f := range zr.File {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("ouverture %s: %v", f.Name, err)
			}
			b, _ := io.ReadAll(rc)
			rc.Close()
			out[f.Name] = string(b)
		}
		return out
	}
	wantAuto := "WR_registre_" + autoReg + "_2026-09-01.pdf"
	wantManual := "WR_registre_" + manualReg + "_2026-09-01.pdf"

	// Par identifiants : les registres non exportables sont listés, pas fatals.
	byIDs, err := svc.ExportRegistersPDF(ctxA, RegisterExportRequest{RegisterIDs: []string{autoReg, manualReg, openReg, "999999999", autoReg}})
	if err != nil {
		t.Fatalf("ExportRegistersPDF (ids): %v", err)
	}
	entries := zipEntries(byIDs)
	if byIDs.Exported != 2 || byIDs.Ignored != 2 || len(entries) != 3 || entries[wantAuto] == "" || entries[wantManual] == "" {
		t.Fatalf("ExportRegistersPDF (ids) = exportés %d, ignorés %d, fichiers %v", byIDs.Exported, byIDs.Ignored, keysOf(entries))
	}
	if ignores := entries["ignores.txt"]; !strings.Contains(ignores, "registre "+openReg+" : registre non clôturé") || !strings.Contains(ignores, "registre 999999999 : registre introuvable") {
		t.Errorf("ignores.txt = %q", ignores)
	}
	if !strings.HasPrefix(byIDs.Filename, "WR_registres_4_") {
		t.Errorf("nom du ZIP = %q (4 identifiants distincts)", byIDs.Filename)
	}

	// Par période : tous les registres du jour, les non validés écartés.
	byPeriod, err := svc.ExportRegistersPDF(ctxA, RegisterExportRequest{DateFrom: "2026-09-01", DateTo: "2026-09-01"})
	if err != nil {
		t.Fatalf("ExportRegistersPDF (période): %v", err)
	}
	entries = zipEntries(byPeriod)
	if byPeriod.Exported != 2 || byPeriod.Ignored != 1 || entries[wantAuto] == "" || entries[wantManual] == "" ||
		!strings.Contains(entries["ignores.txt"], "registre "+openReg) {
		t.Fatalf("ExportRegistersPDF (période) = exportés %d, ignorés %d, fichiers %v", byPeriod.Exported, byPeriod.Ignored, keysOf(entries))
	}
	if byPeriod.Filename != "WR_registres_2026-09-01_2026-09-01.zip" {
		t.Errorf("nom du ZIP = %q", byPeriod.Filename)
	}

	// Rien d'exportable : période sans registre, autre établissement.
	if _, err := svc.ExportRegistersPDF(ctxA, RegisterExportRequest{DateFrom: "2026-10-01", DateTo: "2026-10-31"}); !errors.Is(err, models.ErrRegisterExportNoneEligible) {
		t.Errorf("période vide : err = %v, want ErrRegisterExportNoneEligible", err)
	}
	if _, err := svc.ExportRegistersPDF(ctxB, RegisterExportRequest{DateFrom: "2026-09-01", DateTo: "2026-09-01"}); !errors.Is(err, models.ErrRegisterExportNoneEligible) {
		t.Errorf("autre établissement (période) : err = %v, want ErrRegisterExportNoneEligible", err)
	}
	if _, err := svc.ExportRegistersPDF(ctxB, RegisterExportRequest{RegisterIDs: []string{autoReg, manualReg}}); !errors.Is(err, models.ErrRegisterExportNoneEligible) {
		t.Errorf("autre établissement (ids) : err = %v, want ErrRegisterExportNoneEligible", err)
	}

	// Registre en dérive : un paiement ajouté après la clôture (cas hérité,
	// aujourd'hui interdit par les verrous du lot C) rend l'instantané figé
	// différent du recalcul. Le PDF est produit, avec la mention.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, cash_register_id, enabled)
		VALUES ($1, $2, $3, 500, 'CB', $4, true)`, fA.merchantID, fA.userID, manualOrder, manualReg); err != nil {
		t.Fatalf("seed paiement a posteriori: %v", err)
	}
	if trusted, err := svc.repo.IsRegisterTrusted(ctx, fA.merchantID, mustParseInt(t, manualReg)); err != nil || trusted {
		t.Fatalf("IsRegisterTrusted (dérive) = (%v, %v), want faux", trusted, err)
	}
	mustContain("dérive", pdfText(manualReg), "Attention", "modifi", "rapport comptable mensuel")
	if strings.Contains(pdfText(autoReg), "Attention") {
		t.Errorf("le registre AUTO sain ne doit pas porter la mention de dérive")
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func mustParseInt(t *testing.T, s string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return n
}
