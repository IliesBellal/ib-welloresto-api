//go:build postgres_integration

package accounting

import (
	"bytes"
	"compress/zlib"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/infrastructure/r2"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
	"welloresto-api/internal/modules/cash_registers"
	"welloresto-api/internal/modules/pos/reports"
)

// extractPDFText décompresse les content streams FlateDecode d'un PDF
// gofpdf (compression activée par défaut) et concatène leur contenu brut, de
// façon à pouvoir chercher un texte attendu dans le rendu réel plutôt que de
// se fier uniquement aux données en amont. Le texte est encodé cp1252 par
// `translate()` dans buildPDFReport : ne chercher que des sous-chaînes
// ASCII des libellés attendus (pas d'accents) pour rester indépendant de cet
// encodage.
func extractPDFText(t *testing.T, pdfBytes []byte) string {
	t.Helper()
	var out bytes.Buffer
	const startMarker = "stream"
	const endMarker = "endstream"
	pos := 0
	for {
		si := bytes.Index(pdfBytes[pos:], []byte(startMarker))
		if si < 0 {
			break
		}
		si += pos + len(startMarker)
		for si < len(pdfBytes) && (pdfBytes[si] == '\r' || pdfBytes[si] == '\n') {
			si++
		}
		ei := bytes.Index(pdfBytes[si:], []byte(endMarker))
		if ei < 0 {
			break
		}
		ei += si
		block := pdfBytes[si:ei]
		if r, err := zlib.NewReader(bytes.NewReader(block)); err == nil {
			decompressed, readErr := io.ReadAll(r)
			if readErr == nil {
				out.Write(decompressed)
			}
		} else {
			out.Write(block)
		}
		pos = ei + len(endMarker)
	}
	return out.String()
}

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

// mopPayment / customItemSeed : paramètres de seed pour openCloseRegister.
type mopPayment struct {
	mop    string
	amount int
}

type customItemSeed struct {
	label string
	value int
}

// TestGetRealPaymentsData_Postgres vérifie le "réel" du rapport comptable
// (GetTrustedEnclosedRegisterIDs + GetRealPaymentsData) : aucun registre
// enclosed -> vide (comportement identique à l'ancien GetPaymentsData non
// touché, cf. TestPOSAccountingReports_Postgres ci-dessus qui continue de le
// couvrir séparément) ; registre enclosed sans dérive -> réel utilisé,
// exclusivement depuis cash_registers_custom_items (cash_registers_items,
// l'instantané MOP automatique à la clôture, est ignoré — le restaurateur ne
// peut enclose sa caisse qu'après avoir ressaisi lui-même le détail réel en
// custom items avec un écart proche de 0, ce qui rend cash_registers_items
// redondant pour ce rapport) ; custom item non libellé affiché sous son code
// brut ; custom item à libellé libre -> ligne à part ; canaux hors périmètre
// (UBER_EATS/STRIPE/DELIVEROO) exclus ; registre en dérive (paiement corrigé
// après enclose) -> écarté entièrement, pas de repli partiel ; isolation
// stricte entre marchands. Les mopPayment ci-dessous restent seedés pour
// exercer GetTrustedEnclosedRegisterIDs (comparaison frozen/live) ; seuls les
// customItemSeed correspondants alimentent désormais GetRealPaymentsData.
func TestGetRealPaymentsData_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	acctRepo := NewAccountingRepository(db)
	crRepo := cash_registers.NewCashRegisterRepository(db)

	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	periodStart := time.Date(2025, 6, 1, 0, 0, 0, 0, paris)
	periodEnd := periodStart.AddDate(0, 1, 0)
	insidePeriod := periodStart.Add(2 * time.Hour)

	type fixture struct {
		merchantID string
		userID     string
		cashDeskID int64
	}

	setupMerchant := func(siret, userID string) fixture {
		t.Helper()
		var merchantIntID int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, vat_number)
			VALUES ('ITest Real Merchant', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', $1, 'Europe/Paris', 'FR999')
			RETURNING id`, siret).Scan(&merchantIntID); err != nil {
			t.Fatalf("seed merchant %s: %v", siret, err)
		}
		merchantID := strconv.FormatInt(merchantIntID, 10)
		if _, err := db.ExecContext(ctx, `INSERT INTO merchant_parameters (merchant_id, last_menu_update, currency) VALUES ($1, now(), 'EUR')`, merchantID); err != nil {
			t.Fatalf("seed merchant_parameters %s: %v", siret, err)
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO users (user_id, name, first_name, last_name, password, email, token)
			VALUES ($1, $2, 'Real', 'Testeur', 'x', $3, $1)`, userID, userID, userID+"@example.com"); err != nil {
			t.Fatalf("seed users %s: %v", userID, err)
		}
		var cashDeskID int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO cash_desks (merchant_id, name) VALUES ($1, 'ITest Real Desk')
			RETURNING cash_desk_id`, merchantID).Scan(&cashDeskID); err != nil {
			t.Fatalf("seed cash_desks %s: %v", siret, err)
		}
		return fixture{merchantID: merchantID, userID: userID, cashDeskID: cashDeskID}
	}

	cleanup := func(f fixture) {
		_, _ = db.ExecContext(ctx, `DELETE FROM payments WHERE merchant_id = $1`, f.merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, f.merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM cash_registers_custom_items WHERE merchant_id = $1`, f.merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM cash_registers_items WHERE cash_register_id IN (SELECT cash_register_id FROM cash_registers WHERE merchant_id = $1)`, f.merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM cash_registers WHERE merchant_id = $1`, f.merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM cash_desks WHERE merchant_id = $1`, f.merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant_parameters WHERE merchant_id = $1`, f.merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, f.merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id = $1`, f.userID)
	}

	// openCloseRegister ouvre un registre pour f, y attache les paiements
	// donnés directement (déjà rattachés au bon cash_register_id — pas besoin
	// de passer par la requalification de CloseCashRegister, qui ne concerne
	// que les paiements orphelins), force start_date dans la période testée,
	// le clôture, ajoute d'éventuels custom items, puis l'enclose. Retourne
	// l'ID du registre (int64, colonne native) et l'ID de la commande seedée.
	openCloseRegister := func(f fixture, startDate time.Time, payments []mopPayment, customItems []customItemSeed) (regID int64, orderID int64) {
		t.Helper()
		openReq := &models.OpenCashRegisterRequest{
			DeviceID: "itest-real-device-" + f.merchantID + "-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		}
		openReq.CashRegister.CashDeskID = strconv.FormatInt(f.cashDeskID, 10)
		openReq.CashRegister.UserID = f.userID
		openReq.CashRegister.CashFund = 0
		openResp, err := crRepo.OpenCashRegister(ctx, openReq, f.merchantID)
		if err != nil || openResp.CashRegister == nil {
			t.Fatalf("OpenCashRegister: (%+v, %v)", openResp, err)
		}
		regIDStr := openResp.CashRegister.CashRegisterId
		regID, err = strconv.ParseInt(regIDStr, 10, 64)
		if err != nil {
			t.Fatalf("parse regID %q: %v", regIDStr, err)
		}

		if _, err := db.ExecContext(ctx, `UPDATE cash_registers SET start_date = $1 WHERE cash_register_id = $2`, startDate, regID); err != nil {
			t.Fatalf("force start_date: %v", err)
		}

		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, cash_register_id, order_num, brand, brand_status, state, price, TVA, HT, created_by)
			VALUES ($1, $2, 1, 'WELLO_RESTO', 'ACCEPTED', 'CLOSED', 0, 0, 0, $3)
			RETURNING order_id`, f.merchantID, regIDStr, f.userID).Scan(&orderID); err != nil {
			t.Fatalf("seed order: %v", err)
		}
		for _, p := range payments {
			if _, err := db.ExecContext(ctx, `
				INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, cash_register_id, enabled)
				VALUES ($1, $2, $3, $4, $5, $6, true)`, f.merchantID, f.userID, orderID, p.amount, p.mop, regIDStr); err != nil {
				t.Fatalf("seed payment %s: %v", p.mop, err)
			}
		}

		if _, err := crRepo.CloseCashRegister(ctx, regIDStr, f.merchantID, &models.CloseCashRegisterRequest{}); err != nil {
			t.Fatalf("CloseCashRegister: %v", err)
		}

		user := &auth.UserLoginRow{UserID: f.userID, MerchantID: f.merchantID}
		for _, ci := range customItems {
			if _, err := crRepo.AddCustomItem(ctx, regIDStr, &models.AddCustomItemRequest{Label: ci.label, Value: ci.value}, user); err != nil {
				t.Fatalf("AddCustomItem %s: %v", ci.label, err)
			}
		}

		if err := crRepo.EncloseCashRegister(ctx, f.userID, regIDStr, "itest close"); err != nil {
			t.Fatalf("EncloseCashRegister: %v", err)
		}

		return regID, orderID
	}

	amountFor := func(rows []PaymentRow, label string) (int64, bool) {
		for _, row := range rows {
			if row.Label == label {
				return row.Amount, true
			}
		}
		return 0, false
	}
	containsID := func(ids []int64, want int64) bool {
		for _, id := range ids {
			if id == want {
				return true
			}
		}
		return false
	}

	// --- Merchant A ---
	fA := setupMerchant("siret-real-a", "itest-real-user-a")
	t.Cleanup(func() { cleanup(fA) })

	// (a) Aucun registre enclosed encore créé -> vide des deux côtés.
	trustedNone, err := acctRepo.GetTrustedEnclosedRegisterIDs(ctx, fA.merchantID, periodStart, periodEnd)
	if err != nil || len(trustedNone) != 0 {
		t.Fatalf("GetTrustedEnclosedRegisterIDs (aucun registre) = (%+v, %v), want vide", trustedNone, err)
	}
	realNone, err := acctRepo.GetRealPaymentsData(ctx, trustedNone)
	if err != nil || len(realNone) != 0 {
		t.Fatalf("GetRealPaymentsData (aucun registre) = (%+v, %v), want vide", realNone, err)
	}

	// (a) suite : le PDF doit afficher le message placeholder plutôt qu'un
	// tableau vide silencieux — assertion sur le rendu réel (stream PDF
	// décompressé), pas seulement sur les données en amont.
	svc := NewAccountingService(acctRepo)
	header := &MerchantHeader{MerchantName: "ITest Real Merchant", SIRET: "000", Currency: "EUR", Timezone: "Europe/Paris"}
	emptyPDF, err := svc.buildPDFReport(2025, 6, header, nil, realNone, periodStart, periodEnd.Add(-time.Second), "Europe/Paris")
	if err != nil {
		t.Fatalf("buildPDFReport (aucun paiement réel): %v", err)
	}
	emptyText := extractPDFText(t, emptyPDF)
	if !bytes.Contains([]byte(emptyText), []byte("Aucune")) || !bytes.Contains([]byte(emptyText), []byte("caisse valid")) {
		t.Fatalf("buildPDFReport (aucun paiement réel) : message placeholder absent du rendu (%d octets de texte extrait)", len(emptyText))
	}

	// Fix A (audit) : un code MOP avec une ligne `labels` existante mais au
	// libellé vide ('') doit retomber sur le code brut, exactement comme un
	// code sans aucune ligne `labels` — pas une ligne à blanc (NULLIF sur
	// l.label).
	if _, err := db.ExecContext(ctx, `
		INSERT INTO labels (label_value, label_type, lang, label)
		VALUES ('ITESTEMPTY', 'mop', 'FR', '')`); err != nil {
		t.Fatalf("seed labels (empty label): %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM labels WHERE label_value = 'ITESTEMPTY' AND label_type = 'mop'`)
	})

	// (b)+(e) Un registre enclosed, MOP connus + un canal hors périmètre.
	// 'ITESTES'/'ITESTCB' sont des codes propres à ce test, volontairement
	// absents de `labels` (table de référence globale partagée, où de vrais
	// codes comme 'ES'/'CB' ont déjà un libellé réel dans cette base de dev) :
	// ça isole le test des données de référence existantes et vérifie du même
	// coup le repli sur le code brut (LEFT JOIN délibéré, cf. GetRealPaymentsData).
	// Les customItemSeed reproduisent ici la ressaisie que le restaurateur
	// doit faire pour pouvoir enclose (écart proche de 0) : ce sont eux, pas
	// les mopPayment/cash_registers_items, qui alimentent GetRealPaymentsData.
	regB, _ := openCloseRegister(fA, insidePeriod, []mopPayment{
		{mop: "ITESTES", amount: 500},
		{mop: "ITESTCB", amount: 300},
		{mop: "UBER_EATS", amount: 999},
		{mop: "ITESTEMPTY", amount: 123},
	}, []customItemSeed{
		{label: "ITESTES", value: 500},
		{label: "ITESTCB", value: 300},
		{label: "UBER_EATS", value: 999},
		{label: "ITESTEMPTY", value: 123},
	})

	// (c) Un second registre avec un custom item à libellé libre.
	regC, _ := openCloseRegister(fA, insidePeriod, []mopPayment{
		{mop: "ITESTES", amount: 200},
	}, []customItemSeed{
		{label: "ITESTES", value: 200},
		{label: "Pourboire", value: 150},
	})

	// (d) Un troisième registre, enclosed correctement, puis corrigé après
	// coup (paiement ajouté directement en base sur le même cash_register_id,
	// exactement le scénario qui a motivé ce chantier) -> doit être écarté
	// en bloc de GetTrustedEnclosedRegisterIDs.
	regD, orderD := openCloseRegister(fA, insidePeriod, []mopPayment{
		{mop: "ITESTCB", amount: 400},
	}, nil)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, cash_register_id, enabled)
		VALUES ($1, $2, $3, $4, 'ITESTCB', $5, true)`,
		fA.merchantID, fA.userID, orderD, 999, strconv.FormatInt(regD, 10)); err != nil {
		t.Fatalf("seed drift payment: %v", err)
	}

	trustedA, err := acctRepo.GetTrustedEnclosedRegisterIDs(ctx, fA.merchantID, periodStart, periodEnd)
	if err != nil {
		t.Fatalf("GetTrustedEnclosedRegisterIDs (merchant A): %v", err)
	}
	if !containsID(trustedA, regB) || !containsID(trustedA, regC) {
		t.Fatalf("GetTrustedEnclosedRegisterIDs (merchant A) = %v, want regB=%d et regC=%d présents", trustedA, regB, regC)
	}
	if containsID(trustedA, regD) {
		t.Fatalf("GetTrustedEnclosedRegisterIDs (merchant A) = %v, want regD=%d absent (dérive post-enclose)", trustedA, regD)
	}

	realA, err := acctRepo.GetRealPaymentsData(ctx, trustedA)
	if err != nil {
		t.Fatalf("GetRealPaymentsData (merchant A): %v", err)
	}
	if amount, ok := amountFor(realA, "ITESTES"); !ok || amount != 700 {
		t.Fatalf("GetRealPaymentsData (merchant A) ITESTES = (%d, %v), want 700 (500 regB + 200 regC)", amount, ok)
	}
	if amount, ok := amountFor(realA, "ITESTCB"); !ok || amount != 300 {
		t.Fatalf("GetRealPaymentsData (merchant A) ITESTCB = (%d, %v), want 300 (regB seul — regD écarté en entier malgré son ITESTCB=400)", amount, ok)
	}
	if amount, ok := amountFor(realA, "Pourboire"); !ok || amount != 150 {
		t.Fatalf("GetRealPaymentsData (merchant A) Pourboire = (%d, %v), want 150", amount, ok)
	}
	if _, ok := amountFor(realA, "UBER_EATS"); ok {
		t.Fatalf("GetRealPaymentsData (merchant A) : UBER_EATS ne doit pas apparaître (canal hors périmètre)")
	}
	// Fix A (audit) : label='' en base -> repli sur le code brut, pas de ligne à blanc.
	if amount, ok := amountFor(realA, "ITESTEMPTY"); !ok || amount != 123 {
		t.Fatalf("GetRealPaymentsData (merchant A) ITESTEMPTY = (%d, %v), want 123 sous le code brut (labels.label='' doit retomber comme si absent)", amount, ok)
	}
	if _, ok := amountFor(realA, ""); ok {
		t.Fatalf("GetRealPaymentsData (merchant A) : une ligne à libellé vide ('') ne doit jamais apparaître")
	}

	// (a) contrepreuve : avec du réel présent, le placeholder ne doit pas
	// apparaître et les montants doivent être lisibles dans le rendu.
	filledPDF, err := svc.buildPDFReport(2025, 6, header, nil, realA, periodStart, periodEnd.Add(-time.Second), "Europe/Paris")
	if err != nil {
		t.Fatalf("buildPDFReport (avec réel): %v", err)
	}
	filledText := extractPDFText(t, filledPDF)
	if bytes.Contains([]byte(filledText), []byte("Aucune")) {
		t.Fatalf("buildPDFReport (avec réel) : le message placeholder ne devrait pas apparaître quand il y a du réel")
	}
	if !bytes.Contains([]byte(filledText), []byte("ITESTES")) || !bytes.Contains([]byte(filledText), []byte("7.00")) {
		t.Fatalf("buildPDFReport (avec réel) : libellé/montant ITESTES=700 absents du rendu (%d octets de texte extrait)", len(filledText))
	}

	// --- (h) Merchant B : isolation stricte, même MOP, même période ---
	fB := setupMerchant("siret-real-b", "itest-real-user-b")
	t.Cleanup(func() { cleanup(fB) })

	regE, _ := openCloseRegister(fB, insidePeriod, []mopPayment{
		{mop: "ITESTCB", amount: 777},
	}, []customItemSeed{
		{label: "ITESTCB", value: 777},
	})

	trustedB, err := acctRepo.GetTrustedEnclosedRegisterIDs(ctx, fB.merchantID, periodStart, periodEnd)
	if err != nil || !containsID(trustedB, regE) {
		t.Fatalf("GetTrustedEnclosedRegisterIDs (merchant B) = (%+v, %v), want regE=%d présent", trustedB, err, regE)
	}
	if containsID(trustedB, regB) || containsID(trustedB, regC) {
		t.Fatalf("GetTrustedEnclosedRegisterIDs (merchant B) = %v, want aucun registre du merchant A", trustedB)
	}

	realB, err := acctRepo.GetRealPaymentsData(ctx, trustedB)
	if err != nil {
		t.Fatalf("GetRealPaymentsData (merchant B): %v", err)
	}
	if amount, ok := amountFor(realB, "ITESTCB"); !ok || amount != 777 {
		t.Fatalf("GetRealPaymentsData (merchant B) ITESTCB = (%d, %v), want 777 (pas mélangé avec les 300 du merchant A)", amount, ok)
	}

	// Re-vérification merchant A : l'activité de B ne doit rien avoir changé.
	realAAfterB, err := acctRepo.GetRealPaymentsData(ctx, trustedA)
	if err != nil {
		t.Fatalf("GetRealPaymentsData (merchant A, après B): %v", err)
	}
	if amount, ok := amountFor(realAAfterB, "ITESTCB"); !ok || amount != 300 {
		t.Fatalf("GetRealPaymentsData (merchant A, après B) ITESTCB = (%d, %v), want toujours 300", amount, ok)
	}
}

// TestExportAccountingReport_Postgres est le seul test qui appelle réellement
// AccountingService.ExportAccountingReport de bout en bout (pas seulement les
// méthodes du repository) : c'est la garantie que service.go appelle bien
// GetTrustedEnclosedRegisterIDs+GetRealPaymentsData et non plus l'ancien
// GetPaymentsData si quelqu'un revert le branchement par erreur. L'upload R2
// est intercepté par un faux serveur S3 (httptest) qui capture le PDF
// uploadé, décompressé ensuite avec extractPDFText pour vérifier que le
// total Encaissements du PDF généré correspond bien au réel attendu.
func TestExportAccountingReport_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	acctRepo := NewAccountingRepository(db)
	crRepo := cash_registers.NewCashRegisterRepository(db)
	svc := NewAccountingService(acctRepo)

	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	const siret = "siret-real-e2e"
	const userID = "itest-real-e2e-user"

	cleanup := func(merchantID string) {
		_, _ = db.ExecContext(ctx, `DELETE FROM payments WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM cash_registers_custom_items WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM cash_registers_items WHERE cash_register_id IN (SELECT cash_register_id FROM cash_registers WHERE merchant_id = $1)`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM cash_registers WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM cash_desks WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant_parameters WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id = $1`, userID)
	}

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, vat_number)
		VALUES ('ITest E2E Merchant', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', $1, 'Europe/Paris', 'FR999')
		RETURNING id`, siret).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(merchantIntID, 10)
	t.Cleanup(func() { cleanup(merchantID) })

	if _, err := db.ExecContext(ctx, `INSERT INTO merchant_parameters (merchant_id, last_menu_update, currency) VALUES ($1, now(), 'EUR')`, merchantID); err != nil {
		t.Fatalf("seed merchant_parameters: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users (user_id, name, first_name, last_name, password, email, token)
		VALUES ($1, $1, 'Real', 'E2E', 'x', $2, $1)`, userID, userID+"@example.com"); err != nil {
		t.Fatalf("seed users: %v", err)
	}
	var cashDeskID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO cash_desks (merchant_id, name) VALUES ($1, 'ITest E2E Desk')
		RETURNING cash_desk_id`, merchantID).Scan(&cashDeskID); err != nil {
		t.Fatalf("seed cash_desks: %v", err)
	}

	// Période définitivement passée (juin 2025, "aujourd'hui" étant après)
	// pour qu'IsMonthClosed passe : mois terminé + toutes les commandes du
	// merchant sur ce mois sont CLOSED (la seule seedée ici l'est).
	juneStart := time.Date(2025, 6, 15, 10, 0, 0, 0, paris)

	openReq := &models.OpenCashRegisterRequest{DeviceID: "itest-e2e-device"}
	openReq.CashRegister.CashDeskID = strconv.FormatInt(cashDeskID, 10)
	openReq.CashRegister.UserID = userID
	openReq.CashRegister.CashFund = 0
	openResp, err := crRepo.OpenCashRegister(ctx, openReq, merchantID)
	if err != nil || openResp.CashRegister == nil {
		t.Fatalf("OpenCashRegister: (%+v, %v)", openResp, err)
	}
	regIDStr := openResp.CashRegister.CashRegisterId
	regID, err := strconv.ParseInt(regIDStr, 10, 64)
	if err != nil {
		t.Fatalf("parse regID: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE cash_registers SET start_date = $1 WHERE cash_register_id = $2`, juneStart, regID); err != nil {
		t.Fatalf("force start_date: %v", err)
	}

	var orderID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, cash_register_id, order_num, brand, brand_status, state, price, TVA, HT, created_by, creation_date)
		VALUES ($1, $2, 1, 'WELLO_RESTO', 'ACCEPTED', 'CLOSED', 0, 0, 0, $3, $4)
		RETURNING order_id`, merchantID, regIDStr, userID, juneStart).Scan(&orderID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, cash_register_id, enabled)
		VALUES ($1, $2, $3, 650, 'ITESTE2E', $4, true)`, merchantID, userID, orderID, regIDStr); err != nil {
		t.Fatalf("seed payment: %v", err)
	}

	if _, err := crRepo.CloseCashRegister(ctx, regIDStr, merchantID, &models.CloseCashRegisterRequest{}); err != nil {
		t.Fatalf("CloseCashRegister: %v", err)
	}
	// GetRealPaymentsData ne lit plus que cash_registers_custom_items : il faut
	// la ressaisie manuelle (comme le restaurateur doit le faire pour pouvoir
	// enclose) pour que le montant apparaisse dans le rapport.
	authedUser := &auth.UserLoginRow{UserID: userID, MerchantID: merchantID}
	if _, err := crRepo.AddCustomItem(ctx, regIDStr, &models.AddCustomItemRequest{Label: "ITESTE2E", Value: 650}, authedUser); err != nil {
		t.Fatalf("AddCustomItem: %v", err)
	}
	if err := crRepo.EncloseCashRegister(ctx, userID, regIDStr, "itest e2e close"); err != nil {
		t.Fatalf("EncloseCashRegister: %v", err)
	}

	// Faux endpoint S3/R2 : capture le corps uploadé (le PDF), répond 200 OK.
	var uploadedPDF []byte
	var uploadedPath string
	fakeR2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			body, _ := io.ReadAll(r.Body)
			uploadedPDF = body
			uploadedPath = r.URL.Path
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer fakeR2.Close()

	r2Client, err := r2.NewClient(r2.UploadConfig{
		AccessKeyID:     "itest-key",
		SecretAccessKey: "itest-secret",
		Endpoint:        fakeR2.URL,
		Bucket:          "itest-bucket",
		PublicBaseURL:   fakeR2.URL,
	})
	if err != nil {
		t.Fatalf("r2.NewClient: %v", err)
	}

	authedCtx := middleware.WithUser(ctx, &auth.UserLoginRow{UserID: userID, MerchantID: merchantID})

	resp, err := svc.ExportAccountingReport(authedCtx, "itest-token", "2025-06-01", "2025-06-30", r2Client)
	if err != nil {
		t.Fatalf("ExportAccountingReport error: %v", err)
	}
	if resp.Status != "1" {
		t.Fatalf("ExportAccountingReport = %+v, want Status=1 (erreur: %s)", resp, resp.Error)
	}
	if uploadedPDF == nil {
		t.Fatalf("ExportAccountingReport : aucun PDF n'a été uploadé vers le faux R2 (path=%q)", uploadedPath)
	}

	text := extractPDFText(t, uploadedPDF)
	if bytes.Contains([]byte(text), []byte("Aucune")) {
		t.Fatalf("PDF généré par ExportAccountingReport : le placeholder ne devrait pas apparaître, il y a un registre de confiance avec du réel")
	}
	if !bytes.Contains([]byte(text), []byte("ITESTE2E")) || !bytes.Contains([]byte(text), []byte("6.50")) {
		t.Fatalf("PDF généré par ExportAccountingReport : libellé/montant ITESTE2E=650 absents du rendu (%d octets de texte extrait) — le call site service.go semble ne pas utiliser GetTrustedEnclosedRegisterIDs/GetRealPaymentsData", len(text))
	}
}
