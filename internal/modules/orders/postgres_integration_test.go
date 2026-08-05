//go:build postgres_integration

package orders

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

// Couvre le point le plus à risque du repo (14 sites de placeholders
// dynamiques, rapport 07) : chaque variante IN (...) est exécutée réellement
// contre Postgres, plus l'assemblage complet FetchAndBuildOrders (7 requêtes
// avec le même QueryFilter rebindé).
func TestOrdersRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const userID = "itest-ord-user"

	// merchant.id est une identity : l'id (et donc merchant_id partout) n'est
	// connu qu'après insertion. Le pré-nettoyage retrouve l'id d'un run
	// précédent via le siret distinctif.
	var merchantID string

	cleanupFor := func(mid string) {
		for _, q := range []string{
			`DELETE FROM order_item_configuration WHERE order_item_id IN (SELECT order_item_id FROM orderitems WHERE merchant_id = $1)`,
			`DELETE FROM extra WHERE merchant_id = $1`,
			`DELETE FROM without WHERE merchant_id = $1`,
			`DELETE FROM order_comments WHERE order_id IN (SELECT order_id FROM orders WHERE merchant_id = $1)`,
			`DELETE FROM order_location WHERE order_id IN (SELECT order_id FROM orders WHERE merchant_id = $1)`,
			`DELETE FROM payments WHERE merchant_id = $1`,
			`DELETE FROM delivery_session_order WHERE delivery_session_id IN (SELECT id FROM delivery_session WHERE merchant_id = $1)`,
			`DELETE FROM delivery_session WHERE merchant_id = $1`,
			`DELETE FROM orderitems WHERE merchant_id = $1`,
			`DELETE FROM orders WHERE merchant_id = $1`,
			`DELETE FROM cash_registers WHERE merchant_id = $1`,
			`DELETE FROM locations WHERE merchant_id = $1`,
			`DELETE FROM discounts_products_options WHERE discount_id IN ('itest-disc-1','itest-disc-2')`,
			`DELETE FROM discounts_products WHERE discount_id IN ('itest-disc-1','itest-disc-2')`,
			`DELETE FROM discounts_schedules WHERE discount_id IN ('itest-disc-1','itest-disc-2')`,
			`DELETE FROM discounts WHERE merchant_id = $1`,
			`DELETE FROM customer_rewards WHERE loyalty_program_id = 'itest-lp-1'`,
			`DELETE FROM customer_loyalty_program_reward_products WHERE loyalty_program_id = 'itest-lp-1'`,
			`DELETE FROM customer_loyalty_programs WHERE id = 'itest-lp-1'`,
			`DELETE FROM order_item_configuration WHERE configuration_attribute_option_id IN (SELECT id FROM configurable_attribute_options WHERE configurable_attribute_id = 'itest-attr-1')`,
			`DELETE FROM configurable_attribute_options WHERE configurable_attribute_id = 'itest-attr-1'`,
			`DELETE FROM product_configurable_attribute WHERE configurable_attribute_id = 'itest-attr-1'`,
			`DELETE FROM configurable_attributes WHERE merchant_id = $1`,
			`DELETE FROM requires WHERE recipe_id IN (SELECT recipe_id FROM recipes WHERE merchant_id = $1)`,
			`DELETE FROM recipes WHERE merchant_id = $1`,
			`DELETE FROM components WHERE merchant_id = $1`,
			`DELETE FROM products WHERE merchant_Id = $1`,
			`DELETE FROM unit_of_measure_desc WHERE id = 9901 AND lang = 'FR'`,
			`DELETE FROM tva_categories WHERE tva_id IN (9201, 9202, 9203)`,
			`DELETE FROM customer WHERE merchant_id = $1`,
			`DELETE FROM merchant_parameters WHERE merchant_id = $1`,
		} {
			if strings.Contains(q, "$1") {
				_, _ = db.ExecContext(ctx, q, mid)
			} else {
				_, _ = db.ExecContext(ctx, q)
			}
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id = $1`, userID)
		if mid != "" {
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, mid)
		}
	}
	// pré-nettoyage d'un run précédent éventuel
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-ord' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	}
	t.Cleanup(func() { cleanupFor(merchantID) })

	mustExec := func(desc, query string, args ...interface{}) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed %s: %v", desc, err)
		}
	}
	mustID := func(desc, query string, args ...interface{}) int64 {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx, query, args...).Scan(&id); err != nil {
			t.Fatalf("seed %s: %v", desc, err)
		}
		return id
	}

	// --- référentiels ---
	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, lat, lng)
		VALUES ('ITest Orders Merchant', 'a', '1', 's', '75001', 'Paris', 'siret-ord', 'https://x', '06', 'mtok-ord', 'Europe/Paris', 1, 2)
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)

	mustExec("users", `
		INSERT INTO users (user_id, name, first_name, last_name, password, email, token, merchant_id)
		VALUES ($1, 'ITest Ord', 'Ord', 'User', 'x', 'itest-ord@example.com', 'ord-tok', $2)`, userID, merchantID)
	mustExec("merchant_parameters", `
		INSERT INTO merchant_parameters (merchant_id, last_menu_update, currency, is_open, delivery_fees, delivery_fees_limit, minimum_cart_for_delivery_order)
		VALUES ($1, now(), 'EUR', true, 250, 2000, 1500)`, merchantID)

	mustExec("tva_categories", `
		INSERT INTO tva_categories (tva_id, delivery_type, tva_title, tva_desc, tva_rate, show_in_report)
		OVERRIDING SYSTEM VALUE VALUES
		(9201, 'IN', 'ITest TVA 10', 'itest', 10, TRUE),
		(9202, 'TAKE_AWAY', 'ITest TVA 5.5', 'itest', 5.5, TRUE),
		(9203, 'DELIVERY', 'ITest TVA 20', 'itest', 20, TRUE)`)
	mustExec("unit_of_measure_desc", `
		INSERT INTO unit_of_measure_desc (id, lang, uom_desc) VALUES (9901, 'FR', 'grammes')`)

	newProduct := func(name, status string) int64 {
		t.Helper()
		return mustID("product "+name, `
			INSERT INTO products (merchant_Id, name, price, price_take_away, price_delivery, category, status, tva_in_id, tva_delivery_id, tva_take_away_id)
			VALUES ($1, $2, 1000, 900, 1100, 'itest', $3, 9201, 9203, 9202)
			RETURNING product_id`, merchantID, name, status)
	}
	prodA := newProduct("itest-ord-plat", "1")
	prodB := newProduct("itest-ord-bloque", "0")
	prodC := newProduct("itest-ord-sans-compo", "1")
	prodAStr := strconv.FormatInt(prodA, 10)

	newComponent := func(name, status string) int64 {
		t.Helper()
		return mustID("component "+name, `
			INSERT INTO components (merchant_id, name, unit_of_measure, stock, enabled, status, available)
			VALUES ($1, $2, 1, 10, true, $3, true)
			RETURNING component_id`, merchantID, name, status)
	}
	compOK := newComponent("itest-comp-ok", "1")
	compBlocked := newComponent("itest-comp-hs", "out_of_stock")

	// recette de prodA → compOK (composants affichés), prodC → compBlocked (bloquant)
	recipeA := mustID("recipe A", `INSERT INTO recipes (merchant_id, product_id) VALUES ($1, $2) RETURNING recipe_id`, merchantID, prodA)
	recipeC := mustID("recipe C", `INSERT INTO recipes (merchant_id, product_id) VALUES ($1, $2) RETURNING recipe_id`, merchantID, prodC)
	mustExec("requires A", `
		INSERT INTO requires (recipe_id, component_id, quantity, unit_of_measure, enabled)
		VALUES ($1, $2, 50, 9901, true)`, recipeA, compOK)
	mustExec("requires C", `
		INSERT INTO requires (recipe_id, component_id, quantity, unit_of_measure, enabled)
		VALUES ($1, $2, 20, 9901, true)`, recipeC, compBlocked)

	customerID := mustID("customer", `
		INSERT INTO customer (customer_name, customer_first_name, customer_last_name, merchant_id, customer_lat, customer_lng)
		VALUES ('ITestClient', 'Jean', 'Dupont', $1, 48.85, 2.35) RETURNING customer_id`, merchantID)

	regID := mustID("cash_register", `
		INSERT INTO cash_registers (merchant_id, cash_desk_id, device_id, user_id, cash_fund, start_date, closure_comment)
		VALUES ($1, 1, 'itest-ord-device', $2, 1000, now(), '') RETURNING cash_register_id`, merchantID, userID)
	regIDStr := strconv.FormatInt(regID, 10)

	locationID := mustID("location", `
		INSERT INTO locations (merchant_id, location_name, location_desc, seats)
		VALUES ($1, 'T1', 'Terrasse', 4) RETURNING location_id`, merchantID)

	// --- commandes ---
	newOrder := func(orderType, state, brandStatus, brand, fulfillment string, custID interface{}, cashRegID interface{}) int64 {
		t.Helper()
		return mustID("order", `
			INSERT INTO orders (merchant_id, customer_id, cash_register_id, order_num, brand, brand_status, order_type, state, price, TVA, HT, created_by, fulfillment_type)
			VALUES ($1, $2, $3, 1, $4, $5, $6, $7, 2000, 200, 1800, $8, $9)
			RETURNING order_id`, merchantID, custID, cashRegID, brand, brandStatus, orderType, state, userID, fulfillment)
	}
	order1 := newOrder("EAT_IN", "OPEN", "ACCEPTED", "WELLO_RESTO", "DELIVERY_BY_RESTAURANT", customerID, regIDStr)
	order2 := newOrder("DELIVERY", "OPEN", "ACCEPTED", "WELLO_RESTO", "DELIVERY_BY_RESTAURANT", customerID, nil)
	order3 := newOrder("TAKE_AWAY", "CLOSED", "ACCEPTED", "WELLO_RESTO", "DELIVERY_BY_RESTAURANT", customerID, nil)
	orderUE := newOrder("DELIVERY", "CLOSED", "DELIVERED", "UBER_EATS", "DELIVERY_BY_PLATFORM", nil, nil)
	mustExec("brand_order_id", `UPDATE orders SET brand_order_id = 'itest-ue-1' WHERE order_id = $1`, orderUE)
	order1Str := strconv.FormatInt(order1, 10)
	order2Str := strconv.FormatInt(order2, 10)
	order3Str := strconv.FormatInt(order3, 10)

	item1 := mustID("orderitem", `
		INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, paid_quantity, price, isPaid, isDistributed)
		VALUES ($1, $2, $3, 2, 1, 1000, false, false) RETURNING order_item_id`, order1, prodA, merchantID)

	mustExec("extra", `
		INSERT INTO extra (order_item_id, order_id, component_id, product_id, price, merchant_id)
		VALUES ($1, $2, $3, $4, 150, $5)`, item1, order1, compOK, prodA, merchantID)
	mustExec("without", `
		INSERT INTO without (order_item_id, order_id, component_id, product_id, merchant_id)
		VALUES ($1, $2, $3, $4, $5)`, item1, order1, compOK, prodA, merchantID)

	mustExec("configurable_attributes", `
		INSERT INTO configurable_attributes (id, product_id, merchant_id, attribute_type, name, title, max_options)
		VALUES ('itest-attr-1', $1, $2, 'CHECK', 'sauce', 'Sauce', 2)`, prodA, merchantID)
	mustExec("product_configurable_attribute", `
		INSERT INTO product_configurable_attribute (product_id, configurable_attribute_id)
		VALUES ($1, 'itest-attr-1')`, prodAStr)
	optionID := mustID("configurable_attribute_options", `
		INSERT INTO configurable_attribute_options (configurable_attribute_id, title, extra_price, max_quantity)
		VALUES ('itest-attr-1', 'Ketchup', 50, 3) RETURNING id`)
	mustExec("order_item_configuration", `
		INSERT INTO order_item_configuration (order_item_id, configuration_attribute_id, configuration_attribute_option_id, quantity)
		VALUES ($1, 1, $2, 2)`, item1, optionID)

	mustExec("order_comments (order)", `
		INSERT INTO order_comments (order_id, user_id, content) VALUES ($1, $2, 'sans oignon svp')`, order1, userID)
	mustExec("order_comments (item)", `
		INSERT INTO order_comments (order_id, order_item_id, user_id, content) VALUES ($1, $2, $3, 'bien cuit')`, order1, item1, userID)

	mustExec("payment", `
		INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, cash_register_id, enabled)
		VALUES ($1, $2, $3, 1000, 'ES', $4, true)`, merchantID, userID, order1, regIDStr)

	mustExec("order_location", `
		INSERT INTO order_location (order_id, location_id) VALUES ($1, $2)`, order1, locationID)

	sessionID := mustID("delivery_session", `
		INSERT INTO delivery_session (user_id, merchant_id, start_date, status)
		VALUES ($1, $2, now(), 'active') RETURNING id`, userID, merchantID)
	mustExec("delivery_session_order", `
		INSERT INTO delivery_session_order (delivery_session_id, order_id, priority, status)
		VALUES ($1, $2, 3, 'en_route')`, sessionID, order2)

	// --- discounts ---
	mustExec("discount 1 (permanent)", `
		INSERT INTO discounts (discount_id, merchant_id, discount_name, discount_desc, discount_order_type, discount_code, discount_value, discount_unit, min_order_unit, discounted_quantity, is_cumulative, is_time_limited, available, valid_from)
		VALUES ('itest-disc-1', $1, 'Promo perm', 'itest', 'IN TAKE_AWAY DELIVERY', 'PERM10', 10, 'PERCENT', 'CURRENCY', 1, false, false, true, now() - interval '1 day')`, merchantID)
	mustExec("discount 2 (horaire)", `
		INSERT INTO discounts (discount_id, merchant_id, discount_name, discount_desc, discount_order_type, discount_code, discount_value, discount_unit, min_order_unit, discounted_quantity, is_cumulative, is_time_limited, available, valid_from)
		VALUES ('itest-disc-2', $1, 'Promo creneau', 'itest', 'IN TAKE_AWAY DELIVERY', 'CREN5', 5, 'PERCENT', 'CURRENCY', 1, false, true, true, now() - interval '1 day')`, merchantID)
	// créneau couvrant toute la journée courante (ISODOW du jour, UTC) —
	// rend le prédicat schedule déterministe quel que soit l'instant du test.
	mustExec("discount schedule", `
		INSERT INTO discounts_schedules (discount_id, day_of_week, available_from, available_to, enabled)
		VALUES ('itest-disc-2', EXTRACT(ISODOW FROM now() AT TIME ZONE 'UTC'), '00:00:00', '23:59:59', true)`)
	mustExec("discounts_products", `
		INSERT INTO discounts_products (discount_id, product_id, new_price) VALUES ('itest-disc-1', $1, 800)`, prodA)
	mustExec("discounts_products_options", `
		INSERT INTO discounts_products_options (discount_id, product_id, option_id, new_price, is_option_mandatory)
		VALUES ('itest-disc-1', $1, $2, 40, true)`, prodAStr, strconv.FormatInt(optionID, 10))

	// --- rewards ---
	mustExec("loyalty program", `
		INSERT INTO customer_loyalty_programs (id, merchant_id, name, description, type, target_value, reward_type, reward_value, min_order_value, max_rewards_per_order)
		VALUES ('itest-lp-1', $1, 'Fidelite', 'itest', 'AMOUNT', 5000, 'DISCOUNT', 500, 1000, 2)`, merchantID)
	rewardID := mustID("customer reward", `
		INSERT INTO customer_rewards (customer_id, loyalty_program_id, reward_type, reward_value, creation_date)
		VALUES ($1, 'itest-lp-1', 'DISCOUNT', 500, now()) RETURNING reward_id`, strconv.FormatInt(customerID, 10))
	mustExec("loyalty reward product", `
		INSERT INTO customer_loyalty_program_reward_products (id, product_id, loyalty_program_id)
		VALUES ('itest-lprp-1', $1, 'itest-lp-1')`, prodAStr)

	fetcher := NewOrdersFetcher(db)
	repo := NewOrdersRepository(db, fetcher)

	// ============ GetPendingOrderIDs (3 variantes de criteria) ============
	ids, err := repo.GetPendingOrderIDs(ctx, merchantID, "")
	if err != nil {
		t.Fatalf("GetPendingOrderIDs failed against postgres: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 pending orders, got %v", ids)
	}
	ids, err = repo.GetPendingOrderIDs(ctx, merchantID, "WR_DELIVERY")
	if err != nil || len(ids) != 1 || ids[0] != order2Str {
		t.Fatalf("WR_DELIVERY pending = (%v, %v), want [%s]", ids, err, order2Str)
	}
	ids, err = repo.GetPendingOrderIDs(ctx, merchantID, "WR_WAITER")
	if err != nil || len(ids) != 1 || ids[0] != order1Str {
		t.Fatalf("WR_WAITER pending = (%v, %v), want [%s]", ids, err, order1Str)
	}

	// ============ FetchAndBuildOrders via GetOrdersByIDs (IN à 2 valeurs) ============
	orders, err := repo.GetOrdersByIDs(ctx, merchantID, []string{order1Str, order2Str})
	if err != nil {
		t.Fatalf("GetOrdersByIDs failed against postgres: %v", err)
	}
	if len(orders) != 2 {
		t.Fatalf("expected 2 assembled orders, got %d", len(orders))
	}
	byID := map[string]models.Order{}
	for _, o := range orders {
		byID[o.OrderID] = o
	}
	o1 := byID[order1Str]
	if len(o1.Products) != 1 {
		t.Fatalf("order1: expected 1 product entry, got %+v", o1.Products)
	}
	p1 := o1.Products[0]
	if p1.Extra == nil || len(*p1.Extra) != 1 || p1.Without == nil || len(*p1.Without) != 1 {
		t.Fatalf("order1 product: expected 1 extra + 1 without, got extra=%v without=%v", p1.Extra, p1.Without)
	}
	if len(p1.Configuration.Attributes) != 1 || len(p1.Configuration.Attributes[0].Options) != 1 {
		t.Fatalf("order1 product: expected 1 config attribute with 1 option, got %+v", p1.Configuration.Attributes)
	}
	opt := p1.Configuration.Attributes[0].Options[0]
	if !opt.Selected || opt.Quantity != 2 || opt.ExtraPrice != 50 {
		t.Fatalf("unexpected config option: %+v", opt)
	}
	if len(p1.Components) != 1 || p1.Components[0].UnitOfMeasure != "grammes" {
		t.Fatalf("expected 1 recipe component with uom, got %+v", p1.Components)
	}
	if p1.Comment == nil {
		t.Fatal("expected the item-level comment to be attached")
	}
	if len(o1.Payments) != 1 || o1.Payments[0].MOP != "ES" {
		t.Fatalf("order1: expected 1 ES payment, got %+v", o1.Payments)
	}
	if o1.Comment == nil || *o1.Comment == "" {
		t.Fatalf("order1: expected a normalized order-level comment, got %+v", o1.Comment)
	}
	if len(o1.Location) != 1 || o1.Location[0].LocationName != "T1" {
		t.Fatalf("order1: expected table T1, got %+v", o1.Location)
	}
	if o1.Customer == nil || o1.Customer.CustomerFirstName == nil || *o1.Customer.CustomerFirstName != "Jean" {
		t.Fatalf("order1: expected customer Jean, got %+v", o1.Customer)
	}
	if o1.CashRegister == nil || o1.CashRegister.CashRegisterID != regIDStr {
		t.Fatalf("order1: expected cash register %s, got %+v", regIDStr, o1.CashRegister)
	}
	o2 := byID[order2Str]
	if o2.DeliverySessionID == nil || *o2.DeliverySessionID != strconv.FormatInt(sessionID, 10) || o2.DeliveryPriority == nil || *o2.DeliveryPriority != 3 {
		t.Fatalf("order2: expected active delivery session, got session=%v priority=%v", o2.DeliverySessionID, o2.DeliveryPriority)
	}

	// ============ GetOrder (placeholder unique) ============
	resp, err := repo.GetOrder(ctx, merchantID, order1Str)
	if err != nil || len(resp.Orders) != 1 {
		t.Fatalf("GetOrder = (%+v, %v)", resp, err)
	}

	// ============ GetOrders / GetOrdersBasic (filtre customer) ============
	custStr := strconv.FormatInt(customerID, 10)
	basicOrders, err := repo.GetOrders(ctx, merchantID, &models.OrderRequest{Customer: &models.CustomerRequest{CustomerID: &custStr}})
	if err != nil {
		t.Fatalf("GetOrders failed against postgres: %v", err)
	}
	if len(basicOrders) != 3 {
		t.Fatalf("expected 3 customer orders, got %d", len(basicOrders))
	}

	// ============ GetHistory (search + 3 IN dynamiques simultanés) ============
	search := "dupont"
	hist, total, _, page, limit, err := repo.GetHistory(ctx, merchantID, models.OrderHistoryRequest{
		Search:    &search,
		Channel:   []string{"wello_resto"},
		OrderType: []string{"take_away"},
		Status:    []string{"accepted"},
	})
	if err != nil {
		t.Fatalf("GetHistory failed against postgres: %v", err)
	}
	if total != 1 || len(hist) != 1 || hist[0].OrderID != order3Str || page != 1 || limit != 50 {
		t.Fatalf("unexpected history: total=%d page=%d limit=%d orders=%+v", total, page, limit, hist)
	}
	// recherche par order_id numérique (cast COALESCE integer → texte)
	hist, total, _, _, _, err = repo.GetHistory(ctx, merchantID, models.OrderHistoryRequest{Search: &order3Str})
	if err != nil || total != 1 {
		t.Fatalf("GetHistory (search by id) = total=%d err=%v, want 1", total, err)
	}

	// ============ GetPaymentsForOrder ============
	pays, err := repo.GetPaymentsForOrder(ctx, order1Str)
	if err != nil || len(pays) != 1 {
		t.Fatalf("GetPaymentsForOrder = (%+v, %v)", pays, err)
	}

	// ============ GetMerchantPricingInfo ============
	pricing, err := repo.GetMerchantPricingInfo(ctx, merchantID)
	if err != nil || pricing == nil || pricing.Currency == nil || *pricing.Currency != "EUR" || pricing.DeliveryFees != 250 {
		t.Fatalf("GetMerchantPricingInfo = (%+v, %v)", pricing, err)
	}

	// ============ ValidateProducts (IN + statuts varchar) ============
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	blocked, err := repo.ValidateProducts(ctx, tx, merchantIntID, []int64{prodA, prodB, prodC})
	_ = tx.Rollback()
	if err != nil {
		t.Fatalf("ValidateProducts failed against postgres: %v", err)
	}
	blockedSet := map[int64]bool{}
	for _, b := range blocked {
		blockedSet[b] = true
	}
	if len(blocked) != 2 || !blockedSet[prodB] || !blockedSet[prodC] {
		t.Fatalf("expected prodB+prodC blocked, got %v", blocked)
	}

	// ============ GetUnavailableProducts (IN + sous-requête ex-HAVING) ============
	preq := &models.PricingRequest{
		MerchantID: merchantID,
		Order: &models.OrderRequest{
			Products: []models.OrderProductPayload{
				{ProductID: prodAStr},
				{ProductID: strconv.FormatInt(prodB, 10)},
				{ProductID: strconv.FormatInt(prodC, 10)},
			},
		},
	}
	unavailable, err := repo.GetUnavailableProducts(ctx, preq)
	if err != nil {
		t.Fatalf("GetUnavailableProducts failed against postgres: %v", err)
	}
	if len(unavailable) != 2 {
		t.Fatalf("expected 2 unavailable products, got %+v", unavailable)
	}

	// ============ GetProductsForPricing (IN) ============
	dbProducts, err := repo.GetProductsForPricing(ctx, preq)
	if err != nil {
		t.Fatalf("GetProductsForPricing failed against postgres: %v", err)
	}
	if len(dbProducts) != 3 {
		t.Fatalf("expected 3 pricing products, got %d", len(dbProducts))
	}

	// ============ GetDiscounts (prédicat schedule par dialecte) ============
	discounts, err := repo.GetDiscounts(ctx, preq)
	if err != nil {
		t.Fatalf("GetDiscounts failed against postgres: %v", err)
	}
	if len(discounts) != 2 {
		t.Fatalf("expected both discounts (permanent + creneau du jour), got %d", len(discounts))
	}

	// ============ GetDiscountProducts ============
	dp, err := repo.GetDiscountProducts(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetDiscountProducts failed against postgres: %v", err)
	}
	if dp["itest-disc-1"] == nil || dp["itest-disc-1"][prodAStr] == nil || dp["itest-disc-1"][prodAStr].NewPrice != 800 {
		t.Fatalf("unexpected discount products: %+v", dp)
	}

	// ============ GetDiscountProductOptions (jointure castée + fenêtre horaire) ============
	// NOTE bug préexistant identique aux deux dialectes (documenté, non
	// corrigé) : l'ordre du Scan est décalé par rapport au SELECT — la map
	// externe est en réalité indexée par option_id (et OptionID contient le
	// discount_id). Le test fige ce comportement historique.
	dpo, err := repo.GetDiscountProductOptions(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetDiscountProductOptions failed against postgres: %v", err)
	}
	optIDStr := strconv.FormatInt(optionID, 10)
	if len(dpo[optIDStr][prodAStr]) != 1 || dpo[optIDStr][prodAStr][0].OptionID != "itest-disc-1" {
		t.Fatalf("unexpected discount product options: %+v", dpo)
	}

	// ============ GetRewards (2 IN dynamiques) ============
	rreq := &models.PricingRequest{
		MerchantID: merchantID,
		Order: &models.OrderRequest{
			Customer: &models.CustomerRequest{
				CustomerID:       &custStr,
				AvailableRewards: []models.DBReward{{RewardID: strconv.FormatInt(rewardID, 10)}},
			},
		},
	}
	rewards, err := repo.GetRewards(ctx, rreq)
	if err != nil {
		t.Fatalf("GetRewards failed against postgres: %v", err)
	}
	if len(rewards) != 1 || len(rewards[0].ProductIDs) != 1 || rewards[0].ProductIDs[0] != prodAStr {
		t.Fatalf("unexpected rewards: %+v", rewards)
	}

	// ============ GetEstimatedDistributionTime (aucune donnée adt → 0) ============
	sec, err := repo.GetEstimatedDistributionTime(ctx, preq, 3)
	if err != nil || sec != 0 {
		t.Fatalf("GetEstimatedDistributionTime = (%d, %v), want (0, nil)", sec, err)
	}

	// ============ GetConfigurationOptionPrices (IN) ============
	prices, err := repo.GetConfigurationOptionPrices(ctx, []string{strconv.FormatInt(optionID, 10)})
	if err != nil {
		t.Fatalf("GetConfigurationOptionPrices failed against postgres: %v", err)
	}
	if prices[strconv.FormatInt(optionID, 10)] != 50 {
		t.Fatalf("unexpected option prices: %+v", prices)
	}

	// ============ ExistsByBrandOrderID ============
	exists, err := repo.ExistsByBrandOrderID(ctx, "UBER_EATS", "itest-ue-1")
	if err != nil || !exists {
		t.Fatalf("ExistsByBrandOrderID = (%v, %v), want (true, nil)", exists, err)
	}
	exists, err = repo.ExistsByBrandOrderID(ctx, "UBER_EATS", "no-such-order")
	if err != nil || exists {
		t.Fatalf("ExistsByBrandOrderID (absent) = (%v, %v), want (false, nil)", exists, err)
	}
}
