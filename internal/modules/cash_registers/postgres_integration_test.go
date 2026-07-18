//go:build postgres_integration

package cash_registers

import (
	"context"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

// Vérifie la traduction SQL des ex-procédures GET_CASH_REGISTER_REPORT et
// GET_CASH_REGISTER_REPORT_MOP contre le Postgres Docker de dev.
//
// Données seedées :
//   - TVA 10% (IN, id 9101), TVA 5.5% (TAKE_AWAY, id 9102), TVA Livraison
//     (id -1, 20%, show_in_report=false — ne sort que par la branche fees) ;
//   - commande 1 (sur place, CLOSED) : 2 × 1000 → TTC 2000, HT 1818, TVA 182 ;
//   - commande 2 (à emporter, CLOSED, delivery_fees 300) : 3 × 500 →
//     TTC 1500, HT 1422, TVA 78 ; fees → HT 240, TTC 300, TVA 60 ;
//   - commande 3 (OPEN) et commande 4 (CANCELED) : exclues ;
//   - paiements : ES 2000 + CB (1000+500) rattachés au registre ; un paiement
//     désactivé, un paiement NULL et un paiement d'une commande CANCELED : exclus.
func TestGetCashRegisterReport_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const (
		merchantID = "999922"
		userID     = "itest-cashreg-user"
	)

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM payments WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM orderitems WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE merchant_Id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM cash_registers WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM tva_categories WHERE tva_id IN (9101, 9102, -1)`)
		_, _ = db.ExecContext(ctx, `DELETE FROM labels WHERE label_type = 'delivery_type' AND lang = 'FR' AND label_value IN ('IN','TAKE_AWAY','DELIVERY')`)
	}
	cleanup()
	t.Cleanup(cleanup)

	mustExec := func(desc, query string, args ...interface{}) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed %s: %v", desc, err)
		}
	}

	mustExec("tva_categories",
		`INSERT INTO tva_categories (tva_id, delivery_type, tva_title, tva_desc, tva_rate, show_in_report)
		 OVERRIDING SYSTEM VALUE VALUES
		 (9101, 'IN', 'TVA 10%', 'itest', 10, TRUE),
		 (9102, 'TAKE_AWAY', 'TVA 5.5%', 'itest', 5.5, TRUE),
		 (-1, 'DELIVERY', 'TVA Livraison', 'itest', 20, FALSE)`)

	mustExec("labels",
		`INSERT INTO labels (label_value, label_type, lang, label) VALUES
		 ('IN', 'delivery_type', 'FR', 'Sur place'),
		 ('TAKE_AWAY', 'delivery_type', 'FR', 'A emporter'),
		 ('DELIVERY', 'delivery_type', 'FR', 'Livraison')`)

	var regID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO cash_registers (merchant_id, cash_desk_id, device_id, user_id, cash_fund, start_date, end_date, closure_comment)
		VALUES ($1, 1, 'itest-cashreg-device', $2, 10000, now(), now(), '')
		RETURNING cash_register_id`, merchantID, userID).Scan(&regID); err != nil {
		t.Fatalf("seed cash_registers: %v", err)
	}
	regIDStr := strconv.FormatInt(regID, 10)

	newProduct := func(name string, tvaIn, tvaTakeAway int) int64 {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO products (merchant_Id, name, price, category, tva_in_id, tva_take_away_id, tva_delivery_id)
			VALUES ($1, $2, 0, 'itest', $3, $4, 0)
			RETURNING product_id`, merchantID, name, tvaIn, tvaTakeAway).Scan(&id); err != nil {
			t.Fatalf("seed product %s: %v", name, err)
		}
		return id
	}
	productA := newProduct("itest-plat", 9101, 0)
	productB := newProduct("itest-dessert", 0, 9102)

	newOrder := func(orderType, state, brandStatus string, deliveryFees int) int64 {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, cash_register_id, order_num, brand_status, order_type, state, price, TVA, HT, delivery_fees, created_by)
			VALUES ($1, $2, 1, $3, $4, $5, 0, 0, 0, $6, $7)
			RETURNING order_id`, merchantID, regIDStr, brandStatus, orderType, state, deliveryFees, userID).Scan(&id); err != nil {
			t.Fatalf("seed order: %v", err)
		}
		return id
	}
	order1 := newOrder("EAT_IN", "CLOSED", "ACCEPTED", 0)
	order2 := newOrder("TAKE_AWAY", "CLOSED", "ACCEPTED", 300)
	order3 := newOrder("EAT_IN", "OPEN", "ACCEPTED", 100)   // exclue : state OPEN
	order4 := newOrder("EAT_IN", "CLOSED", "CANCELED", 100) // exclue : brand_status CANCELED

	addItem := func(orderID, productID int64, qty, price int) {
		t.Helper()
		mustExec("orderitem",
			`INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, price)
			 VALUES ($1, $2, $3, $4, $5)`, orderID, productID, merchantID, qty, price)
	}
	addItem(order1, productA, 2, 1000)
	addItem(order2, productB, 3, 500)
	addItem(order3, productA, 1, 700) // ne doit pas compter
	addItem(order4, productA, 1, 900) // ne doit pas compter

	addPayment := func(orderID int64, mop string, amount int, cashRegisterID interface{}, enabled bool) {
		t.Helper()
		mustExec("payment",
			`INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, cash_register_id, enabled)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`, merchantID, userID, orderID, amount, mop, cashRegisterID, enabled)
	}
	addPayment(order1, "ES", 2000, regIDStr, true)
	addPayment(order2, "CB", 1000, regIDStr, true)
	addPayment(order2, "CB", 500, regIDStr, true)
	addPayment(order1, "ES", 999, regIDStr, false) // exclue : enabled=false
	addPayment(order1, "ES", 888, nil, true)       // exclue : pas de registre
	addPayment(order4, "CB", 777, regIDStr, true)  // exclue : commande CANCELED

	repo := NewCashRegisterRepository(db)

	report, err := repo.GetCashRegisterReport(ctx, regIDStr)
	if err != nil {
		t.Fatalf("GetCashRegisterReport failed against postgres: %v", err)
	}

	// -------- Totaux --------
	if report.HT != 3480 || report.TTC != 3800 || report.TVA != 320 {
		t.Errorf("totaux: got HT=%d TTC=%d TVA=%d, want HT=3480 TTC=3800 TVA=320", report.HT, report.TTC, report.TVA)
	}
	if report.CashFund != 10000 {
		t.Errorf("cash_fund: got %v, want 10000", report.CashFund)
	}

	// -------- Ventilation TVA --------
	type key struct{ deliveryType, tvaTitle string }
	type vals struct {
		label        string
		ht, ttc, tva int
	}
	got := map[key]vals{}
	for _, group := range report.CashReport {
		for _, cat := range group.TVACategories {
			got[key{group.DeliveryTypeID, cat.TVATitle}] = vals{group.DeliveryTypeLabel, cat.HT, cat.TTC, cat.TVA}
		}
	}
	want := map[key]vals{
		{"IN", "TVA 10%"}:             {"Sur place", 1818, 2000, 182},
		{"TAKE_AWAY", "TVA 5.5%"}:     {"A emporter", 1422, 1500, 78},
		{"DELIVERY", "TVA Livraison"}: {"Livraison", 240, 300, 60},
	}
	if len(got) != len(want) {
		t.Errorf("ventilation TVA: got %d lignes (%v), want %d", len(got), got, len(want))
	}
	for k, w := range want {
		g, ok := got[k]
		if !ok {
			t.Errorf("ventilation TVA: ligne manquante %v", k)
			continue
		}
		if g != w {
			t.Errorf("ventilation TVA %v: got %+v, want %+v", k, g, w)
		}
	}

	// -------- Ventilation MOP --------
	gotMOP := map[string]int{}
	for _, line := range report.MOP {
		gotMOP[line.MOP] = line.Amount
	}
	wantMOP := map[string]int{"ES": 2000, "CB": 1500}
	if len(gotMOP) != len(wantMOP) {
		t.Errorf("MOP: got %v, want %v", gotMOP, wantMOP)
	}
	for mop, amount := range wantMOP {
		if gotMOP[mop] != amount {
			t.Errorf("MOP %s: got %d, want %d", mop, gotMOP[mop], amount)
		}
	}

	// -------- GetCashRegisterTVADetails (mêmes requêtes, header scopé merchant) --------
	details, err := repo.GetCashRegisterTVADetails(ctx, merchantID, regIDStr)
	if err != nil {
		t.Fatalf("GetCashRegisterTVADetails failed against postgres: %v", err)
	}
	if details == nil {
		t.Fatal("GetCashRegisterTVADetails: nil pour un registre existant")
	}
	if details.HT != 3480 || details.TTC != 3800 || details.TVA != 320 {
		t.Errorf("details totaux: got HT=%d TTC=%d TVA=%d, want HT=3480 TTC=3800 TVA=320", details.HT, details.TTC, details.TVA)
	}

	// Registre inexistant pour ce merchant → nil, nil
	other, err := repo.GetCashRegisterTVADetails(ctx, "999923", regIDStr)
	if err != nil {
		t.Fatalf("GetCashRegisterTVADetails (mauvais merchant): %v", err)
	}
	if other != nil {
		t.Fatalf("GetCashRegisterTVADetails (mauvais merchant): attendu nil, got %+v", other)
	}
}
