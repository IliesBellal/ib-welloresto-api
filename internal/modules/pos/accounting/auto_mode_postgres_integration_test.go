//go:build postgres_integration

package accounting

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/modules/auth"
	"welloresto-api/internal/modules/cash_registers"
)

// Export comptable en clôture automatique (docs/EXPORT_COMPTABLE_MODES_CLOTURE.md) :
// TVA ventilée à partir des paiements (total TTC = encaissements), remise de
// caisse déduite, filtre par canal sur orders.order_source, commande refusée
// exclue ; mode de la période et refus d'une période à cheval sur un
// changement de mode.
func TestAccountingAutoMode_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantID string
	cleanupFor := func(mid string) {
		_, _ = db.ExecContext(ctx, `DELETE FROM tva_categories WHERE tva_title LIKE 'itest-auto-%'`)
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM payments WHERE merchant_id = $1`,
			`DELETE FROM orderitems WHERE merchant_id = $1`,
			`DELETE FROM orders WHERE merchant_id = $1`,
			`DELETE FROM products WHERE merchant_Id = $1`,
			`DELETE FROM merchant_closing_modes WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE CAST(id AS TEXT) = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-acct-auto' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	} else {
		cleanupFor("")
	}
	t.Cleanup(func() { cleanupFor(merchantID) })

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Acct Auto', 'a', '1', 's', '75001', 'Paris', 'siret-acct-auto', 'https://x', '06', 'mtok-acct-auto', 'Europe/Paris')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)

	newCategory := func(title string, rate float64) int64 {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO tva_categories (delivery_type, tva_title, tva_desc, tva_rate)
			VALUES ('0', $1, 'd', $2) RETURNING tva_id`, title, rate).Scan(&id); err != nil {
			t.Fatalf("seed tva_categories %s: %v", title, err)
		}
		return id
	}
	cat10 := newCategory("itest-auto-10", 10)
	cat20 := newCategory("itest-auto-20", 20)
	newProduct := func(name string, cat int64) int64 {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO products (merchant_Id, name, price, category, tva_in_id, tva_take_away_id, tva_delivery_id)
			VALUES ($1, $2, 0, 'itest', $3, $3, $3) RETURNING product_id`, merchantID, name, cat).Scan(&id); err != nil {
			t.Fatalf("seed product %s: %v", name, err)
		}
		return id
	}
	plat := newProduct("itest-auto-plat", cat10)
	boisson := newProduct("itest-auto-boisson", cat20)

	paris, _ := time.LoadLocation("Europe/Paris")
	day := time.Date(2025, 7, 10, 12, 0, 0, 0, paris)
	newOrder := func(num int, source, brandStatus string) int64 {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand, brand_status, order_type, state, price, TVA, HT, created_by, delivery_fees, creation_date, order_source)
			VALUES ($1, $2, 'WELLO_RESTO', $3, 'IN', 'CLOSED', 0, 0, 0, 'itest-auto', 0, $4, $5)
			RETURNING order_id`, merchantID, num, brandStatus, day.UTC(), source).Scan(&id); err != nil {
			t.Fatalf("seed order %d: %v", num, err)
		}
		return id
	}
	addLine := func(orderID, productID int64, price int) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
			INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, price)
			VALUES ($1, $2, $3, 1, $4)`, orderID, productID, merchantID, price); err != nil {
			t.Fatalf("seed orderitem: %v", err)
		}
	}
	addPayment := func(orderID int64, mop string, amount int, enabled bool) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
			INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, enabled)
			VALUES ($1, 'itest-auto', $2, $3, $4, $5)`, merchantID, orderID, amount, mop, enabled); err != nil {
			t.Fatalf("seed payment: %v", err)
		}
	}

	// Caisse : 20 € à 10 % + 10 € à 20 %, 27 € en CB + 3 € de remise.
	pos := newOrder(1, "WELLO_RESTO_POS", "CLOSED")
	addLine(pos, plat, 2000)
	addLine(pos, boisson, 1000)
	addPayment(pos, "CB", 2700, true)
	addPayment(pos, "CURRENCY", 300, true)
	// Borne : 5 € à 10 %, payés 5 €.
	kiosk := newOrder(2, "KIOSK", "CLOSED")
	addLine(kiosk, plat, 500)
	addPayment(kiosk, "KIOSK", 500, true)
	// ScanNOrder refusée : jamais vendue, paiement désactivé au refus.
	denied := newOrder(3, "SCANNORDER", "DENIED")
	addLine(denied, plat, 800)
	addPayment(denied, "STRIPE", 800, false)

	acctRepo := NewAccountingRepository(db)
	crRepo := cash_registers.NewCashRegisterRepository(db)
	svc := NewAccountingService(acctRepo, crRepo)

	from := time.Date(2025, 7, 1, 0, 0, 0, 0, paris)
	toExclusive := from.AddDate(0, 1, 0)
	lastDay := toExclusive.AddDate(0, 0, -1)

	// Établissement sans historique : clôture automatique.
	mode, err := svc.periodClosingMode(ctx, merchantID, from, lastDay)
	if err != nil || mode != cash_registers.ClosingModeAuto {
		t.Fatalf("periodClosingMode = (%s, %v), want AUTO", mode, err)
	}
	authedCtx := middleware.WithUser(ctx, &auth.UserLoginRow{UserID: "itest-auto", MerchantID: merchantID})
	options, err := svc.ExportOptions(authedCtx, "2025-07-01")
	if err != nil || options.ClosingMode != cash_registers.ClosingModeAuto || len(options.Channels) != 5 {
		t.Fatalf("ExportOptions (AUTO) = (%+v, %v), want AUTO et 5 canaux", options, err)
	}

	sum := func(rows []TVARow, payments []PaymentRow) (int64, int64, map[float64]int64) {
		byRate := map[float64]int64{}
		var tva, pay int64
		for _, r := range rows {
			tva += int64(r.TTC)
			byRate[r.Rate] += int64(r.TTC)
		}
		for _, p := range payments {
			pay += p.Amount
		}
		return tva, pay, byRate
	}

	// Tous les canaux.
	rows, payments, discounts, err := svc.buildAutoSections(ctx, merchantID, from, toExclusive, orderScope{})
	if err != nil {
		t.Fatalf("buildAutoSections (tous canaux): %v", err)
	}
	tvaTTC, payTotal, byRate := sum(rows, payments)
	if tvaTTC != 3200 || payTotal != 3200 {
		t.Fatalf("tous canaux : TTC TVA = %d, encaissements = %d, want 3200 = 3200", tvaTTC, payTotal)
	}
	if byRate[10] != 1800+500 || byRate[20] != 900 {
		t.Fatalf("tous canaux : TTC par taux = %v, want 10 %% = 2300, 20 %% = 900 (remise de 3 € répartie 2 € / 1 €)", byRate)
	}
	if discounts != (DiscountSummary{TTCBeforeDiscounts: 3500, Discounts: 300, TTC: 3200}) {
		t.Fatalf("tous canaux : remises = %+v", discounts)
	}

	// Borne seulement.
	rows, payments, _, err = svc.buildAutoSections(ctx, merchantID, from, toExclusive, orderScope{sources: []string{"KIOSK"}})
	if err != nil {
		t.Fatalf("buildAutoSections (borne): %v", err)
	}
	if tvaTTC, payTotal, _ = sum(rows, payments); tvaTTC != 500 || payTotal != 500 {
		t.Fatalf("borne seule : TTC TVA = %d, encaissements = %d, want 500 = 500", tvaTTC, payTotal)
	}

	// Déclaration de TVA de juillet (CalculateVAT), alignée sur l'export : en
	// AUTO, ventilation des encaissements. Caisse : 18 € à 10 % (HT 16,36 /
	// TVA 1,64) et 9 € à 20 % (HT 7,50 / TVA 1,50) ; borne : 5 € à 10 %
	// (HT 4,55 / TVA 0,45). Total TVA 3,59 €.
	declaration, err := svc.CalculateVAT(authedCtx, VATCalculateRequest{StartDate: "2025-07-01", EndDate: "2025-07-31"})
	if err != nil {
		t.Fatalf("CalculateVAT (AUTO) : %v", err)
	}
	if declaration.TotalVAT != 359 || len(declaration.MonthlyBreakdown) != 1 ||
		declaration.MonthlyBreakdown[0].Month != "2025-07" || declaration.MonthlyBreakdown[0].ClosingMode != "AUTO" ||
		declaration.MonthlyBreakdown[0].RevenueTTC != 3200 {
		t.Fatalf("CalculateVAT (AUTO) = %+v, want TVA 359, juillet AUTO, TTC 3200", declaration)
	}
	if declaration.ByChannel["WELLO_RESTO_POS"].VAT != 314 || declaration.ByChannel["KIOSK"].VAT != 45 || len(declaration.ByChannel) != 5 {
		t.Fatalf("CalculateVAT (AUTO) par canal = %+v, want caisse 314, borne 45, 5 canaux", declaration.ByChannel)
	}
	// Anciennes valeurs de canal acceptées : « restaurant » = caisse + borne.
	if legacy, err := svc.CalculateVAT(authedCtx, VATCalculateRequest{StartDate: "2025-07-01", EndDate: "2025-07-31", Channels: []string{"restaurant"}}); err != nil || legacy.TotalVAT != 359 {
		t.Fatalf("CalculateVAT (restaurant) = (%+v, %v), want TVA 359", legacy, err)
	}
	if uber, err := svc.CalculateVAT(authedCtx, VATCalculateRequest{StartDate: "2025-07-01", EndDate: "2025-07-31", Channels: []string{"UBER_EATS"}}); err != nil || uber.TotalVAT != 0 {
		t.Fatalf("CalculateVAT (Uber Eats) = (%+v, %v), want TVA 0", uber, err)
	}
	if _, err := svc.CalculateVAT(authedCtx, VATCalculateRequest{StartDate: "2025-07-01", EndDate: "2025-07-31", Channels: []string{"telepathie"}}); err == nil {
		t.Fatal("CalculateVAT (canal inconnu) : erreur attendue")
	}

	// Mode manuel sur la même période : TVA sur les lignes, remise déduite.
	manualRows, _, manualDiscounts, err := svc.buildManualSections(ctx, merchantID, from, toExclusive)
	if err != nil {
		t.Fatalf("buildManualSections: %v", err)
	}
	if tvaTTC, _, _ = sum(manualRows, nil); tvaTTC != 3200 || manualDiscounts.Discounts != 300 {
		t.Fatalf("manuel : TTC TVA = %d, remises = %+v, want 3200 et 300 de remise", tvaTTC, manualDiscounts)
	}

	// Période à cheval sur un changement de mode : refusée.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO merchant_closing_modes (merchant_id, mode, effective_from, created_by) VALUES
		($1, 'MANUAL', DATE '1970-01-01', 'itest'),
		($1, 'AUTO', DATE '2025-07-15', 'itest')`, merchantID); err != nil {
		t.Fatalf("seed merchant_closing_modes: %v", err)
	}
	_, err = svc.periodClosingMode(ctx, merchantID, from, lastDay)
	var straddle *closingModeStraddleError
	if !errors.As(err, &straddle) || straddle.effectiveFrom.Format("2006-01-02") != "2025-07-15" {
		t.Fatalf("période à cheval : err = %v, want changement au 2025-07-15", err)
	}
	if mode, err := svc.periodClosingMode(ctx, merchantID, from, time.Date(2025, 7, 14, 0, 0, 0, 0, paris)); err != nil || mode != cash_registers.ClosingModeManual {
		t.Fatalf("période avant le changement = (%s, %v), want MANUAL", mode, err)
	}
	if mode, err := svc.periodClosingMode(ctx, merchantID, time.Date(2025, 7, 15, 0, 0, 0, 0, paris), lastDay); err != nil || mode != cash_registers.ClosingModeAuto {
		t.Fatalf("période à partir du changement = (%s, %v), want AUTO", mode, err)
	}

	// Déclaration de juillet à cheval sur le changement : chaque tranche
	// suit son mode (commandes du 10/07 → tranche MANUAL, lignes − remise) ;
	// même total ici, toutes les commandes étant payées.
	if split, err := svc.CalculateVAT(authedCtx, VATCalculateRequest{StartDate: "2025-07-01", EndDate: "2025-07-31"}); err != nil ||
		split.TotalVAT != 359 || len(split.MonthlyBreakdown) != 1 || split.MonthlyBreakdown[0].ClosingMode != "MANUAL" {
		t.Fatalf("CalculateVAT (juillet, changement au 15) = (%+v, %v), want TVA 359, juillet MANUAL", split, err)
	}

	// Options d'export selon la date : manuel avant le changement (aucun canal
	// filtrable), automatique après.
	if options, err := svc.ExportOptions(authedCtx, "2025-07-01"); err != nil || options.ClosingMode != cash_registers.ClosingModeManual || len(options.Channels) != 0 {
		t.Fatalf("ExportOptions (avant changement) = (%+v, %v), want MANUAL sans canal", options, err)
	}
	if options, err := svc.ExportOptions(authedCtx, "2025-07-20"); err != nil || options.ClosingMode != cash_registers.ClosingModeAuto || len(options.Channels) != 5 {
		t.Fatalf("ExportOptions (après changement) = (%+v, %v), want AUTO et 5 canaux", options, err)
	}
	if _, err := svc.ExportOptions(authedCtx, "20/07/2025"); err == nil {
		t.Fatal("ExportOptions (date invalide) : erreur attendue")
	}
}
