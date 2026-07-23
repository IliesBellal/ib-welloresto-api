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

	today := time.Now().UTC().Format("2006-01-02")
	dayStart := today + " 00:00:00"
	dayEnd := today + " 23:59:59"

	// --- accounting ---
	acctRepo := NewAccountingRepository(db)

	header, err := acctRepo.GetMerchantHeader(ctx, merchantID)
	if err != nil || header.MerchantName != "ITest Acct Merchant" || header.VATNumber == nil || *header.VATNumber != "FR123" {
		t.Fatalf("GetMerchantHeader = (%+v, %v)", header, err)
	}

	// mois passé sans commandes -> clôturé ; mois courant -> pas fini
	closed, err := acctRepo.IsMonthClosed(ctx, merchantID, "2025", "1")
	if err != nil || !closed {
		t.Fatalf("IsMonthClosed(2025-01) = (%v, %v), want true", closed, err)
	}
	nowMonth := time.Now().UTC()
	closed, err = acctRepo.IsMonthClosed(ctx, merchantID, strconv.Itoa(nowMonth.Year()), strconv.Itoa(int(nowMonth.Month())))
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
