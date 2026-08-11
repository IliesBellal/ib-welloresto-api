//go:build postgres_integration

package cash_registers

import (
	"context"
	"database/sql"
	"math"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
)

// Vérifie la traduction SQL des ex-procédures GET_CASH_REGISTER_REPORT et
// GET_CASH_REGISTER_REPORT_MOP contre le Postgres Docker de dev.
//
// tva_categories et labels(delivery_type) sont des tables de référence
// globales, non scopées par marchand ni par caisse — cashRegisterReportSQL
// part de `FROM tva_categories ... WHERE show_in_report IS TRUE` sans filtre
// merchant/caisse, donc une base déjà peuplée (chargement réel complet) y
// ajoute des catégories qui n'appartiennent pas à ce test. tva_id = -1 en
// particulier est un identifiant sentinelle réel (utilisé tel quel par la
// branche "frais de livraison" de la requête), pas un id que ce test peut
// s'approprier sans vérification. Ce test :
//   - insère ses propres catégories (id 9101/9102, hors de toute plage
//     d'auto-incrément réaliste) et ne les tient pour siennes que via elles ;
//   - pour tva_id = -1 et les labels IN/TAKE_AWAY/DELIVERY, vérifie d'abord
//     s'ils existent déjà (données réelles ou résidu d'un run précédent
//     identifié par tva_desc = 'itest') ; ne les insère/nettoie que s'ils
//     sont absents, et sinon les utilise tels quels pour calculer ses valeurs
//     attendues ;
//   - scope ses assertions de ventilation TVA à ses propres lignes (par
//     clé delivery_type/tva_title) plutôt que de compter le nombre total de
//     lignes du rapport, qui dépend du contenu global de tva_categories.
//
// Données seedées :
//   - TVA 10% (IN, id 9101), TVA 5.5% (TAKE_AWAY, id 9102) ;
//   - commande 1 (sur place, CLOSED) : 2 × 1000 → TTC 2000, HT 1818, TVA 182 ;
//   - commande 2 (à emporter, CLOSED, delivery_fees 300) : 3 × 500 →
//     TTC 1500, HT 1422, TVA 78 ; fees → TTC 300, HT/TVA dérivés du taux
//     effectif de la catégorie tva_id=-1 (insérée à 20% si absente, sinon
//     celui déjà en base) ;
//   - commande 3 (OPEN) et commande 4 (CANCELED) : exclues ;
//   - paiements : ES 2000 + CB (1000+500) rattachés au registre ; un paiement
//     désactivé, un paiement NULL et un paiement d'une commande CANCELED : exclus.
func TestGetCashRegisterReport_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const (
		merchantID  = "999922"
		userID      = "itest-cashreg-user"
		ownTVAMark  = "itest"
		deliveryTVA = -1 // identifiant sentinelle réel, cf. commentaire de fonction
	)

	// tva_id = -1 est-il déjà présent, et n'appartient-il pas à ce test
	// (pas de résidu marqué tva_desc = 'itest' d'un run précédent) ?
	isForeignDeliveryTVA := func() bool {
		var desc sql.NullString
		err := db.QueryRowContext(ctx, `SELECT tva_desc FROM tva_categories WHERE tva_id = $1`, deliveryTVA).Scan(&desc)
		if err == sql.ErrNoRows {
			return false
		}
		if err != nil {
			t.Fatalf("check tva_categories %d preexistence: %v", deliveryTVA, err)
		}
		return desc.String != ownTVAMark
	}
	labelExists := func(value string) bool {
		var exists bool
		if err := db.QueryRowContext(ctx, `
			SELECT EXISTS(SELECT 1 FROM labels WHERE label_type = 'delivery_type' AND lang = 'FR' AND label_value = $1)
		`, value).Scan(&exists); err != nil {
			t.Fatalf("check labels %s preexistence: %v", value, err)
		}
		return exists
	}

	foreignDeliveryTVA := isForeignDeliveryTVA()
	foreignLabel := map[string]bool{}
	for _, v := range []string{"IN", "TAKE_AWAY", "DELIVERY"} {
		foreignLabel[v] = labelExists(v)
	}

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM payments WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM orderitems WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE merchant_Id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM cash_registers WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM tva_categories WHERE tva_id IN (9101, 9102)`)
		if !foreignDeliveryTVA {
			_, _ = db.ExecContext(ctx, `DELETE FROM tva_categories WHERE tva_id = $1 AND tva_desc = $2`, deliveryTVA, ownTVAMark)
		}
		for value, wasForeign := range foreignLabel {
			if !wasForeign {
				_, _ = db.ExecContext(ctx, `DELETE FROM labels WHERE label_type = 'delivery_type' AND lang = 'FR' AND label_value = $1`, value)
			}
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	mustExec := func(desc, query string, args ...interface{}) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed %s: %v", desc, err)
		}
	}

	mustExec("tva_categories (IN/TAKE_AWAY)",
		`INSERT INTO tva_categories (tva_id, delivery_type, tva_title, tva_desc, tva_rate, show_in_report)
		 OVERRIDING SYSTEM VALUE VALUES
		 (9101, 'IN', 'itest TVA 10%', 'itest', 10, TRUE),
		 (9102, 'TAKE_AWAY', 'itest TVA 5.5%', 'itest', 5.5, TRUE)`)

	if !foreignDeliveryTVA {
		mustExec("tva_categories (delivery sentinel)",
			`INSERT INTO tva_categories (tva_id, delivery_type, tva_title, tva_desc, tva_rate, show_in_report)
			 OVERRIDING SYSTEM VALUE VALUES ($1, 'DELIVERY', 'itest TVA Livraison', $2, 20, FALSE)`,
			deliveryTVA, ownTVAMark)
	}

	// Taux effectif de la catégorie "frais de livraison" — celui qu'on vient
	// d'insérer (20%), ou celui déjà en base si la ligne préexistait.
	var deliveryType, deliveryTVATitle string
	var deliveryRate float64
	if err := db.QueryRowContext(ctx, `SELECT delivery_type, tva_title, tva_rate FROM tva_categories WHERE tva_id = $1`, deliveryTVA).
		Scan(&deliveryType, &deliveryTVATitle, &deliveryRate); err != nil {
		t.Fatalf("read tva_categories %d: %v", deliveryTVA, err)
	}

	labelText := map[string]string{"IN": "itest Sur place", "TAKE_AWAY": "itest A emporter", "DELIVERY": "itest Livraison"}
	for _, value := range []string{"IN", "TAKE_AWAY", "DELIVERY"} {
		if foreignLabel[value] {
			continue
		}
		mustExec("labels "+value,
			`INSERT INTO labels (label_value, label_type, lang, label) VALUES ($1, 'delivery_type', 'FR', $2)`,
			value, labelText[value])
	}

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

	// Frais de livraison (commande 2, 300) : HT et TVA arrondis
	// indépendamment côté SQL (pas de HT = TTC - TVA), on reproduit donc le
	// même arrondi ici plutôt que de le dériver, pour matcher exactement
	// cashRegisterReportSQL quel que soit le taux effectif de tva_id=-1.
	const deliveryFeesTTC = 300
	deliveryHT := int(math.Round(deliveryFeesTTC * (100 - deliveryRate) / 100))
	deliveryTVAAmount := int(math.Round(deliveryFeesTTC * deliveryRate / 100))

	// -------- Totaux --------
	// TTC est indépendant du taux de TVA appliqué (somme des montants bruts) ;
	// HT/TVA en revanche dépendent du taux effectif de tva_id=-1 (le nôtre,
	// ou celui déjà en base s'il préexistait).
	wantTTC := 2000 + 1500 + deliveryFeesTTC
	wantTVA := 182 + 78 + deliveryTVAAmount
	wantHT := 1818 + 1422 + deliveryHT
	if report.HT != wantHT || report.TTC != wantTTC || report.TVA != wantTVA {
		t.Errorf("totaux: got HT=%d TTC=%d TVA=%d, want HT=%d TTC=%d TVA=%d", report.HT, report.TTC, report.TVA, wantHT, wantTTC, wantTVA)
	}
	if report.CashFund != 10000 {
		t.Errorf("cash_fund: got %v, want 10000", report.CashFund)
	}

	// -------- Ventilation TVA --------
	// tva_categories est une table de référence globale : le rapport peut
	// légitimement contenir des lignes à 0 pour des catégories qui n'ont
	// aucun lien avec ce test (autres marchands/commandes). On scope donc
	// les assertions à ses propres clés (delivery_type, tva_title) plutôt
	// que de comparer le nombre total de lignes du rapport.
	type key struct{ deliveryType, tvaTitle string }
	type vals struct{ ht, ttc, tva int }
	got := map[key]vals{}
	for _, group := range report.CashReport {
		for _, cat := range group.TVACategories {
			got[key{group.DeliveryTypeID, cat.TVATitle}] = vals{cat.HT, cat.TTC, cat.TVA}
		}
	}
	want := map[key]vals{
		{"IN", "itest TVA 10%"}:          {1818, 2000, 182},
		{"TAKE_AWAY", "itest TVA 5.5%"}:  {1422, 1500, 78},
		{deliveryType, deliveryTVATitle}: {deliveryHT, deliveryFeesTTC, deliveryTVAAmount},
	}
	for k, w := range want {
		g, ok := got[k]
		if !ok {
			t.Errorf("ventilation TVA: ligne manquante %v (lignes obtenues : %v)", k, got)
			continue
		}
		if g != w {
			t.Errorf("ventilation TVA %v: got %+v, want %+v", k, g, w)
		}
	}
	// Toute autre ligne du rapport (catégories globales étrangères à ce
	// test) ne doit porter aucun montant lié à ce registre.
	for k, g := range got {
		if _, isOwn := want[k]; isOwn {
			continue
		}
		if g.ht != 0 || g.ttc != 0 || g.tva != 0 {
			t.Errorf("ventilation TVA: ligne étrangère %v porte des montants non nuls: %+v", k, g)
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
	if details.HT != wantHT || details.TTC != wantTTC || details.TVA != wantTVA {
		t.Errorf("details totaux: got HT=%d TTC=%d TVA=%d, want HT=%d TTC=%d TVA=%d", details.HT, details.TTC, details.TVA, wantHT, wantTTC, wantTVA)
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

// Cycle de vie complet du registre converti à dbx : ouverture (InsertReturningID),
// clôture (requalification des paiements UPDATE...FROM, items, hash), résumé,
// items personnalisés, en-clôture, historique (IN dynamique) et device_link
// (upsert ON CONFLICT).
func TestCashRegisterLifecycle_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const (
		merchantID = "999925"
		userID     = "itest-cr-lc-user"
		deviceID   = "itest-cashreg-lc-device"
	)

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM device_link WHERE device_id = $1`, deviceID)
		_, _ = db.ExecContext(ctx, `DELETE FROM payments WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM cash_registers_custom_items WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM cash_registers_items WHERE cash_register_id IN (SELECT cash_register_id FROM cash_registers WHERE merchant_id = $1)`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM cash_registers WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM cash_desks WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant_parameters WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id = $1`, userID)
	}
	cleanup()
	t.Cleanup(cleanup)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO users (user_id, name, first_name, last_name, password, email, token)
		VALUES ($1, 'ITest CR', 'Caisse', 'Testeur', 'x', 'itest-cr-lc@example.com', 'cr-tok')`, userID); err != nil {
		t.Fatalf("seed users: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO merchant_parameters (merchant_id, last_menu_update, currency, is_open)
		VALUES ($1, now(), 'EUR', true)`, merchantID); err != nil {
		t.Fatalf("seed merchant_parameters: %v", err)
	}
	var cashDeskID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO cash_desks (merchant_id, name) VALUES ($1, 'ITest Desk')
		RETURNING cash_desk_id`, merchantID).Scan(&cashDeskID); err != nil {
		t.Fatalf("seed cash_desks: %v", err)
	}

	repo := NewCashRegisterRepository(db)

	// --- OpenCashRegister ---
	openReq := &models.OpenCashRegisterRequest{DeviceID: deviceID}
	openReq.CashRegister.CashDeskID = strconv.FormatInt(cashDeskID, 10)
	openReq.CashRegister.UserID = userID
	openReq.CashRegister.CashFund = 5000
	openResp, err := repo.OpenCashRegister(ctx, openReq, merchantID)
	if err != nil {
		t.Fatalf("OpenCashRegister failed against postgres: %v", err)
	}
	if openResp.Status != "cash_register_created" || openResp.CashRegister == nil {
		t.Fatalf("unexpected open response: %+v", openResp)
	}
	regID := openResp.CashRegister.CashRegisterId

	// Réouverture même device → refusée.
	dupResp, err := repo.OpenCashRegister(ctx, openReq, merchantID)
	if err != nil {
		t.Fatalf("OpenCashRegister (dup) failed: %v", err)
	}
	if dupResp.Status != "device_already_opened_cash_register" {
		t.Fatalf("expected duplicate status, got %+v", dupResp)
	}

	// --- commandes + paiements à requalifier ---
	var closedOrder int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, cash_register_id, order_num, brand_status, state, price, TVA, HT, created_by)
		VALUES ($1, $2, 1, 'ACCEPTED', 'CLOSED', 0, 0, 0, $3)
		RETURNING order_id`, merchantID, regID, userID).Scan(&closedOrder); err != nil {
		t.Fatalf("seed order: %v", err)
	}

	addPayment := func(mop string, amount int, cashRegisterID interface{}) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
			INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, cash_register_id, enabled)
			VALUES ($1, $2, $3, $4, $5, $6, true)`, merchantID, userID, closedOrder, amount, mop, cashRegisterID); err != nil {
			t.Fatalf("seed payment: %v", err)
		}
	}
	addPayment("ES", 500, regID)             // déjà rattaché
	addPayment("STRIPE", 600, "SCANNORDER")  // étape 2
	addPayment("UBER_EATS", 400, nil)        // étape 3
	addPayment("CB", 800, "KIOSK")           // étape 3bis (Kiosk)
	addPayment("CB", 200, nil)               // étape 3bis (NULL)

	// --- CloseCashRegister ---
	already, err := repo.CloseCashRegister(ctx, regID, merchantID, &models.CloseCashRegisterRequest{})
	if err != nil {
		t.Fatalf("CloseCashRegister failed against postgres: %v", err)
	}
	if already {
		t.Fatal("expected first close to report not-already-closed")
	}

	var requalified int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE merchant_id = $1 AND cash_register_id = $2`, merchantID, regID).Scan(&requalified); err != nil {
		t.Fatalf("count requalified payments: %v", err)
	}
	if requalified != 5 {
		t.Fatalf("expected all 5 payments attached to the register after close, got %d", requalified)
	}

	var closed bool
	var finalCashFund int
	var hash *string
	if err := db.QueryRowContext(ctx, `SELECT closed, final_cash_fund, hash FROM cash_registers WHERE cash_register_id = $1`, regID).Scan(&closed, &finalCashFund, &hash); err != nil {
		t.Fatalf("read back register: %v", err)
	}
	if !closed || hash == nil || *hash == "" {
		t.Fatalf("expected closed register with fiscal hash, got closed=%v hash=%v", closed, hash)
	}
	if finalCashFund != 5000+500 {
		t.Fatalf("expected final cash fund 5500 (fund + ES), got %d", finalCashFund)
	}

	// Seconde clôture → idempotente (déjà fermée).
	already, err = repo.CloseCashRegister(ctx, regID, merchantID, &models.CloseCashRegisterRequest{})
	if err != nil {
		t.Fatalf("CloseCashRegister (repeat) failed: %v", err)
	}
	if !already {
		t.Fatal("expected second close to report already-closed")
	}

	// --- custom items ---
	user := &auth.UserLoginRow{UserID: userID, MerchantID: merchantID}
	itemID, err := repo.AddCustomItem(ctx, regID, &models.AddCustomItemRequest{Label: "Pourboire", Value: 150}, user)
	if err != nil {
		t.Fatalf("AddCustomItem failed against postgres: %v", err)
	}
	if itemID == "" || itemID == "0" {
		t.Fatalf("expected generated custom item id, got %q", itemID)
	}

	// --- EncloseCashRegister (SET non qualifié) ---
	if err := repo.EncloseCashRegister(ctx, userID, regID, "clôture ok"); err != nil {
		t.Fatalf("EncloseCashRegister failed against postgres: %v", err)
	}

	// --- GetCashRegisterSummary ---
	summary, err := repo.GetCashRegisterSummary(ctx, regID, merchantID)
	if err != nil {
		t.Fatalf("GetCashRegisterSummary failed against postgres: %v", err)
	}
	cr := summary.CashRegister
	if cr.CashRegisterID != regID || !cr.Closed || !cr.Enclosed || cr.Currency != "EUR" {
		t.Fatalf("unexpected summary: %+v", cr)
	}
	if len(cr.Payments) != 5 || len(cr.Orders) != 1 {
		t.Fatalf("expected 5 payments / 1 order in summary, got %d / %d", len(cr.Payments), len(cr.Orders))
	}
	if len(cr.Items) == 0 || len(cr.CustomItems) != 1 {
		t.Fatalf("expected MOP items + 1 custom item, got %d / %d", len(cr.Items), len(cr.CustomItems))
	}

	// --- AddCustomItem / DeleteCustomItem rejetés une fois enclosed : le
	// registre doit rester immuable (cf. accounting.GetRealPaymentsData, qui
	// suppose que cash_registers_items/cash_registers_custom_items ne bougent
	// plus après enclose). ---
	if _, err := repo.AddCustomItem(ctx, regID, &models.AddCustomItemRequest{Label: "Trop tard", Value: 50}, user); err != models.ErrCashRegisterStillOpen {
		t.Fatalf("AddCustomItem after enclose: expected ErrCashRegisterStillOpen, got %v", err)
	}
	if err := repo.DeleteCustomItem(ctx, regID, itemID, user); err != models.ErrCashRegisterStillOpen {
		t.Fatalf("DeleteCustomItem after enclose: expected ErrCashRegisterStillOpen, got %v", err)
	}
	summary, err = repo.GetCashRegisterSummary(ctx, regID, merchantID)
	if err != nil {
		t.Fatalf("GetCashRegisterSummary (after rejected mutations) failed: %v", err)
	}
	if len(summary.CashRegister.CustomItems) != 1 {
		t.Fatalf("expected the custom item to remain untouched after enclose, got %+v", summary.CashRegister.CustomItems)
	}

	// --- GetCashRegisterHistory (IN dynamique + agrégats paiements) ---
	history, err := repo.GetCashRegisterHistory(ctx, merchantID, userID, CashRegisterHistoryRequest{})
	if err != nil {
		t.Fatalf("GetCashRegisterHistory failed against postgres: %v", err)
	}
	if history.Metadata.TotalItems != 1 || len(history.CashRegisters) != 1 {
		t.Fatalf("expected 1 history item, got %+v", history.Metadata)
	}
	item := history.CashRegisters[0]
	if item.CashRegisterID != regID || !item.Closed || !item.Enclosed {
		t.Fatalf("unexpected history item: %+v", item)
	}
	if item.TransactionCount != 5 || item.TotalRevenu != 2500 {
		t.Fatalf("expected 5 transactions / 2500 revenu, got %d / %d", item.TransactionCount, item.TotalRevenu)
	}
	if len(item.PaymentMethods) == 0 {
		t.Fatal("expected payment methods breakdown in history")
	}

	// --- device_link : upsert + circularité + delete ---
	if err := repo.UpsertDeviceLink(ctx, deviceID, userID, deviceID+"-2"); err != nil {
		t.Fatalf("UpsertDeviceLink (insert) failed against postgres: %v", err)
	}
	if err := repo.UpsertDeviceLink(ctx, deviceID, userID, deviceID+"-3"); err != nil {
		t.Fatalf("UpsertDeviceLink (update) failed against postgres: %v", err)
	}
	var onBehalf string
	if err := db.QueryRowContext(ctx, `SELECT on_behalf_of FROM device_link WHERE device_id = $1`, deviceID).Scan(&onBehalf); err != nil {
		t.Fatalf("read back device_link: %v", err)
	}
	if onBehalf != deviceID+"-3" {
		t.Fatalf("expected upsert to update on_behalf_of, got %q", onBehalf)
	}
	circular, err := repo.IsCircularDeviceLink(ctx, deviceID+"-3", deviceID)
	if err != nil {
		t.Fatalf("IsCircularDeviceLink failed against postgres: %v", err)
	}
	if !circular {
		t.Fatal("expected circular link to be detected")
	}
	deleted, err := repo.DeleteDeviceLink(ctx, deviceID)
	if err != nil || deleted != 1 {
		t.Fatalf("DeleteDeviceLink = (%d, %v), want (1, nil)", deleted, err)
	}
}
