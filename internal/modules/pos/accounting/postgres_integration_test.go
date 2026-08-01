//go:build postgres_integration

package accounting

import (
	"context"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/modules/pos/reports"
)

// Vérification accounting + reports : agrégats TVA/paiements sur données
// réelles (DATE_FORMAT -> to_char, IFNULL -> COALESCE, ROUND -> numeric).
func TestPOSAccountingReports_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantID string
	cleanupFor := func(mid string) {
		_, _ = db.ExecContext(ctx, `DELETE FROM labels WHERE label LIKE 'itest-acct%'`)
		_, _ = db.ExecContext(ctx, `DELETE FROM tva_categories WHERE tva_title LIKE 'itest-acct%'`)
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM extra WHERE merchant_id = $1`,
			`DELETE FROM payments WHERE merchant_id = $1`,
			`DELETE FROM orderitems WHERE merchant_id = $1`,
			`DELETE FROM orders WHERE merchant_id = $1`,
			`DELETE FROM products WHERE merchant_Id = $1`,
			`DELETE FROM merchant_parameters WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-acct' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	} else {
		cleanupFor("")
	}
	t.Cleanup(func() { cleanupFor(merchantID) })

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, vat_number)
		VALUES ('ITest Acct Merchant', 'a', '1', 's', '75001', 'Paris', 'siret-acct', 'https://x', '06', 'mtok-acct', 'Europe/Paris', 'FR123')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)
	if _, err := db.ExecContext(ctx, `INSERT INTO merchant_parameters (merchant_id, last_menu_update, currency) VALUES ($1, now(), 'EUR')`, merchantID); err != nil {
		t.Fatalf("seed merchant_parameters: %v", err)
	}

	var tvaID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO tva_categories (delivery_type, tva_title, tva_desc, tva_rate)
		VALUES ('0', 'itest-acct-tva10', 'd', 10) RETURNING tva_id`).Scan(&tvaID); err != nil {
		t.Fatalf("seed tva_categories: %v", err)
	}

	var productID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_Id, name, price, category, tva_in_id, tva_take_away_id, tva_delivery_id)
		VALUES ($1, 'itest-acct-prod', 1000, 'itest', $2, $2, $2) RETURNING product_id`, merchantID, tvaID).Scan(&productID); err != nil {
		t.Fatalf("seed product: %v", err)
	}

	var orderID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand, brand_status, order_type, state, price, TVA, HT, created_by, delivery_fees)
		VALUES ($1, 1, 'WELLO_RESTO', 'CLOSED', 'IN', 'CLOSED', 2200, 200, 2000, 'itest-acct-cashier', 0)
		RETURNING order_id`, merchantID).Scan(&orderID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	var orderItemID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, price)
		VALUES ($1, $2, $3, 2, 1000) RETURNING order_item_id`, orderID, productID, merchantID).Scan(&orderItemID); err != nil {
		t.Fatalf("seed orderitem: %v", err)
	}
	// extra de 100 sur l'item : TTC = (1000 + 100) * 2 = 2200
	if _, err := db.ExecContext(ctx, `
		INSERT INTO extra (order_item_id, order_id, component_id, product_id, quantity, price, merchant_id)
		VALUES ($1, $2, 1, $3, 1, 100, $4)`, orderItemID, orderID, productID, merchantID); err != nil {
		t.Fatalf("seed extra: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, enabled)
		VALUES ($1, 'itest-acct-cashier', $2, 2200, 'ITESTMOP', true)`, merchantID, orderID); err != nil {
		t.Fatalf("seed payment: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO labels (label_value, label_type, lang, label)
		VALUES ('ITESTMOP', 'mop', 'FR', 'itest-acct-espece')`); err != nil {
		t.Fatalf("seed labels mop: %v", err)
	}

	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	today := time.Now().UTC().Format("2006-01-02")
	nowParis := time.Now().In(paris)
	// Journée comptable de l'établissement : 00:00:00 local -> lendemain
	// 00:00:00 local (borne haute exclusive, comme en production).
	dayStart := time.Date(nowParis.Year(), nowParis.Month(), nowParis.Day(), 0, 0, 0, 0, paris)
	dayEnd := dayStart.AddDate(0, 0, 1)

	// --- accounting ---
	acctRepo := NewAccountingRepository(db)

	header, err := acctRepo.GetMerchantHeader(ctx, merchantID)
	if err != nil || header.MerchantName != "ITest Acct Merchant" || header.VATNumber == nil || *header.VATNumber != "FR123" {
		t.Fatalf("GetMerchantHeader = (%+v, %v)", header, err)
	}

	// mois passé sans commandes -> clôturé ; mois courant -> pas fini
	closed, err := acctRepo.IsMonthClosed(ctx, merchantID, 2025, 1, paris)
	if err != nil || !closed {
		t.Fatalf("IsMonthClosed(2025-01) = (%v, %v), want true", closed, err)
	}
	closed, err = acctRepo.IsMonthClosed(ctx, merchantID, nowParis.Year(), int(nowParis.Month()), paris)
	if err != nil || closed {
		t.Fatalf("IsMonthClosed(mois courant) = (%v, %v), want false", closed, err)
	}

	tvaRows, err := acctRepo.GetTVAData(ctx, merchantID, dayStart, dayEnd)
	if err != nil || len(tvaRows) != 1 {
		t.Fatalf("GetTVAData = (%+v, %v), want 1 ligne", tvaRows, err)
	}
	if tvaRows[0].TVATitle != "itest-acct-tva10" || tvaRows[0].TTC != 2200 {
		t.Fatalf("GetTVAData row = %+v", tvaRows[0])
	}

	payRows, err := acctRepo.GetPaymentsData(ctx, merchantID, dayStart, dayEnd)
	if err != nil || len(payRows) != 1 || payRows[0].Amount != 2200 || payRows[0].Label != "itest-acct-espece" {
		t.Fatalf("GetPaymentsData = (%+v, %v)", payRows, err)
	}

	nowUTC := time.Now().UTC()
	vatRows, err := acctRepo.GetVATAggregationRows(ctx, merchantID, nowUTC, nowUTC, []string{"restaurant"}, []string{"in"})
	if err != nil || len(vatRows) != 1 {
		t.Fatalf("GetVATAggregationRows = (%+v, %v), want 1 ligne", vatRows, err)
	}
	r0 := vatRows[0]
	if r0.Channel != "restaurant" || r0.OrderType != "in" || r0.TTCCents != 2200 || r0.HTCents != 2000 || r0.VATCents != 200 {
		t.Fatalf("GetVATAggregationRows row = %+v", r0)
	}
	// filtre excluant -> aucune ligne
	vatRows, err = acctRepo.GetVATAggregationRows(ctx, merchantID, nowUTC, nowUTC, []string{"ubereats"}, nil)
	if err != nil || len(vatRows) != 0 {
		t.Fatalf("GetVATAggregationRows(filtre ubereats) = (%+v, %v), want 0", vatRows, err)
	}

	// --- bornes de journée en fuseau établissement ---
	// Le rapport comptable est ancré sur orders.creation_date exprimé dans le
	// fuseau de l'établissement. Vérifie la bascule de mois autour de minuit
	// heure locale, là où l'ancienne implémentation (bornes UTC + borne haute
	// non étendue à la fin de journée) perdait ou déplaçait des commandes.
	if got := time.Date(2025, 8, 1, 0, 0, 0, 0, paris).UTC().Format("2006-01-02 15:04:05"); got != "2025-07-31 22:00:00" {
		t.Fatalf("début août 2025 en UTC = %s, want 2025-07-31 22:00:00 (heure d'été)", got)
	}
	if got := time.Date(2025, 1, 1, 0, 0, 0, 0, paris).UTC().Format("2006-01-02 15:04:05"); got != "2024-12-31 23:00:00" {
		t.Fatalf("début janvier 2025 en UTC = %s, want 2024-12-31 23:00:00 (heure d'hiver)", got)
	}

	seedOrderAt := func(label string, orderNum int, creationUTC time.Time, price int) int64 {
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand, brand_status, order_type, state, price, TVA, HT, created_by, delivery_fees, creation_date)
			VALUES ($1, $2, 'WELLO_RESTO', 'CLOSED', 'IN', 'CLOSED', $3, 0, 0, 'itest-acct-cashier', 0, $4)
			RETURNING order_id`, merchantID, orderNum, price, creationUTC).Scan(&id); err != nil {
			t.Fatalf("seed order %s: %v", label, err)
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, price)
			VALUES ($1, $2, $3, 1, $4)`, id, productID, merchantID, price); err != nil {
			t.Fatalf("seed orderitem %s: %v", label, err)
		}
		return id
	}

	// A : créée le 31/08 à 23h30 heure de Paris -> appartient à août.
	// B : créée le 01/09 à 00h30 heure de Paris -> appartient à septembre.
	// Les deux tombent le 31/08 en UTC : c'est exactement le cas que des bornes
	// UTC confondaient.
	orderA := seedOrderAt("A (31/08 23h30 Paris)", 2, time.Date(2025, 8, 31, 21, 30, 0, 0, time.UTC), 1000)
	seedOrderAt("B (01/09 00h30 Paris)", 3, time.Date(2025, 8, 31, 22, 30, 0, 0, time.UTC), 1500)

	// A est encaissée le 01/09 à 00h30 heure de Paris : l'ancrage reste la date
	// de création de la commande, donc le paiement compte pour août.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, enabled, payment_date)
		VALUES ($1, 'itest-acct-cashier', $2, 1000, 'ITESTMOP', true, $3)`,
		merchantID, orderA, time.Date(2025, 8, 31, 22, 30, 0, 0, time.UTC)); err != nil {
		t.Fatalf("seed payment A: %v", err)
	}

	augStart := time.Date(2025, 8, 1, 0, 0, 0, 0, paris)
	augEnd := augStart.AddDate(0, 1, 0) // 01/09 00:00:00 Paris, borne exclusive
	sepEnd := augEnd.AddDate(0, 1, 0)

	sumTTC := func(rows []TVARow) float64 {
		var total float64
		for _, row := range rows {
			total += row.TTC
		}
		return total
	}

	augRows, err := acctRepo.GetTVAData(ctx, merchantID, augStart, augEnd)
	if err != nil {
		t.Fatalf("GetTVAData(août 2025) = %v", err)
	}
	if got := sumTTC(augRows); got != 1000 {
		t.Fatalf("TTC août 2025 = %v, want 1000 (commande A seule)", got)
	}

	sepRows, err := acctRepo.GetTVAData(ctx, merchantID, augEnd, sepEnd)
	if err != nil {
		t.Fatalf("GetTVAData(septembre 2025) = %v", err)
	}
	if got := sumTTC(sepRows); got != 1500 {
		t.Fatalf("TTC septembre 2025 = %v, want 1500 (commande B seule)", got)
	}

	augPay, err := acctRepo.GetPaymentsData(ctx, merchantID, augStart, augEnd)
	if err != nil || len(augPay) != 1 || augPay[0].Amount != 1000 {
		t.Fatalf("GetPaymentsData(août 2025) = (%+v, %v), want 1 ligne à 1000 — commande créée en août, encaissée en septembre", augPay, err)
	}

	sepPay, err := acctRepo.GetPaymentsData(ctx, merchantID, augEnd, sepEnd)
	if err != nil || len(sepPay) != 0 {
		t.Fatalf("GetPaymentsData(septembre 2025) = (%+v, %v), want 0 ligne", sepPay, err)
	}

	// --- reports ---
	repRepo := reports.NewReportsRepository(db)

	dayReports, err := repRepo.GetTVAReportData(ctx, merchantID, today, today)
	if err != nil || len(dayReports) != 1 {
		t.Fatalf("GetTVAReportData = (%+v, %v), want 1 jour", dayReports, err)
	}
	if len(dayReports[0].VATData) != 1 || dayReports[0].TTCSum != 2200 {
		t.Fatalf("GetTVAReportData jour = %+v", dayReports[0])
	}

	payReports, err := repRepo.GetPaymentsReportData(ctx, merchantID, today, today)
	if err != nil || len(payReports) != 1 {
		t.Fatalf("GetPaymentsReportData = (%+v, %v), want 1 jour", payReports, err)
	}
	if len(payReports[0].Payments) != 1 || payReports[0].Payments[0].Amount != 2200 {
		t.Fatalf("GetPaymentsReportData jour = %+v", payReports[0])
	}
}
