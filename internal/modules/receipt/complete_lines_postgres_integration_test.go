//go:build postgres_integration

package receipt

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

// Lot D conformité caisse, ticket complet (docs/attestation-conformite-05-lot-D-brief.md,
// constat 2) : les lignes figées du ticket comptent options payantes,
// suppléments, frais de livraison et remises de caisse, et leur somme vaut la
// TVA ventilée. Nécessite la migration 173 (order_item_configuration.extra_price).
func TestReceiptCompleteLines_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const siret = "siret-rcpt-complete"
	var merchantID string
	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM receipts WHERE merchant_id = $1`,
			`DELETE FROM payments WHERE merchant_id = $1`,
			`DELETE FROM order_item_configuration WHERE order_item_id IN (SELECT order_item_id FROM orderitems WHERE merchant_id = $1)`,
			`DELETE FROM extra WHERE merchant_id = $1`,
			`DELETE FROM orderitems WHERE merchant_id = $1`,
			`DELETE FROM orders WHERE merchant_id = $1`,
			`DELETE FROM configurable_attribute_options WHERE configurable_attribute_id = 'itest-attr-' || $1`,
			`DELETE FROM components WHERE merchant_id = $1`,
			`DELETE FROM products WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id::text = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = $1 LIMIT 1`, siret).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	}
	var mid int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Receipt Complete', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', 'mtok-rcpt-complete', 'UTC')
		RETURNING id`, siret).Scan(&mid); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(mid, 10)
	t.Cleanup(func() { cleanupFor(merchantID) })

	exec := func(label, q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}
	scanID := func(label, q string, args ...any) int64 {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx, q, args...).Scan(&id); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		return id
	}

	var tvaID int64
	if err := db.QueryRowContext(ctx, `SELECT tva_id FROM tva_categories WHERE tva_id <> -1 ORDER BY tva_id LIMIT 1`).Scan(&tvaID); err != nil {
		t.Fatalf("pick tva category: %v", err)
	}
	pizza := scanID("product", `
		INSERT INTO products (merchant_id, name, price, category, tva_in_id, tva_take_away_id, tva_delivery_id)
		VALUES ($1, 'Pizza', 0, 'itest', $2, $2, $2) RETURNING product_id`, merchantID, tvaID)
	beer := scanID("product", `
		INSERT INTO products (merchant_id, name, price, category, tva_in_id, tva_take_away_id, tva_delivery_id)
		VALUES ($1, 'Bière', 0, 'itest', $2, $2, $2) RETURNING product_id`, merchantID, tvaID)
	attr := "itest-attr-" + merchantID
	sizeOpt := scanID("option", `
		INSERT INTO configurable_attribute_options (configurable_attribute_id, title, extra_price)
		VALUES ($1, 'Grande taille', 999) RETURNING id`, attr)
	legacyOpt := scanID("option", `
		INSERT INTO configurable_attribute_options (configurable_attribute_id, title, extra_price)
		VALUES ($1, 'Pâte fine', 50) RETURNING id`, attr)
	freeOpt := scanID("option", `
		INSERT INTO configurable_attribute_options (configurable_attribute_id, title, extra_price)
		VALUES ($1, 'Sans oignon', 0) RETURNING id`, attr)
	bacon := scanID("component", `
		INSERT INTO components (merchant_id, name, unit_of_measure) VALUES ($1, 'Bacon', 1) RETURNING component_id`, merchantID)

	// Livraison : 2 pizzas à 10 % (+ grande taille figée à 150 au lieu des 999
	// du catalogue, + pâte fine sans prix figé -> 50 du catalogue, + option
	// gratuite, + bacon 100), 1 bière à 20 %, livraison 300 à 20 %, remise de
	// caisse 280.
	order := scanID("order", `
		INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, price, TVA, HT, delivery_fees, delivery_fees_tva_rate, created_by)
		VALUES ($1, 1, 'WELLO_RESTO', 'CLOSED', 'CLOSED', 'DELIVERY', 2420, 0, 2420, 300, 20, 'itest') RETURNING order_id`, merchantID)
	pizzaLine := scanID("line", `
		INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, base_price, price, tva_id, tva_rate)
		VALUES ($1, $2, $3, 2, 600, 600, $4, 10) RETURNING order_item_id`, order, pizza, merchantID, tvaID)
	exec("line", `
		INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, base_price, price, tva_id, tva_rate)
		VALUES ($1, $2, $3, 1, 500, 500, $4, 20)`, order, beer, merchantID, tvaID)
	exec("config", `
		INSERT INTO order_item_configuration (order_item_id, configuration_attribute_id, configuration_attribute_option_id, quantity, extra_price)
		VALUES ($1, $2, $3, 1, 150), ($1, $2, $4, 1, NULL), ($1, $2, $5, 1, 0)`, pizzaLine, attr, sizeOpt, legacyOpt, freeOpt)
	exec("extra", `
		INSERT INTO extra (order_item_id, order_id, component_id, product_id, merchant_id, price)
		VALUES ($1, $2, $3, $4, $5, 100)`, pizzaLine, order, bacon, pizza, merchantID)
	exec("discount", `
		INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, enabled) VALUES ($1, 'itest', $2, 280, 'CURRENCY', TRUE)`, merchantID, order)

	svc := NewReceiptService(NewReceiptRepository(db))
	orderID := strconv.FormatInt(order, 10)
	ht := int64(2420)
	if err := dbutils.RunInTx(ctx, db, func(txCtx context.Context) error {
		return svc.GenerateFiscalReceipt(txCtx, &models.Order{OrderID: orderID, MerchantID: &merchantID, TTC: 2420, HT: &ht}, nil)
	}); err != nil {
		t.Fatalf("GenerateFiscalReceipt: %v", err)
	}
	sale, err := svc.GetSaleReceiptByOrderID(ctx, orderID)
	if err != nil {
		t.Fatal(err)
	}
	var items []models.SnapshotItem
	if err := json.Unmarshal(sale.ItemsSnapshot, &items); err != nil {
		t.Fatal(err)
	}
	tax, ok := fiscal.ParseTaxDetails(sale.TaxDetails)
	if !ok {
		t.Fatalf("no tax details: %s", sale.TaxDetails)
	}

	// 2 × (600 + 150 + 50 + 100) = 1 800 à 10 % ; 500 + 300 = 800 à 20 % ;
	// 2 600 brut, remise de caisse 280 : 2 320 net.
	if tax.TotalTTC() != 2320 || tax.Discount != 280 {
		t.Fatalf("tax details %+v", tax)
	}
	var sumTTC, sumHT int64
	kinds := map[string]int{}
	for _, it := range items {
		sumTTC += it.TotalTTC
		sumHT += it.TotalHT
		kinds[it.Kind]++
	}
	if sumTTC != tax.TotalTTC() || sumHT != tax.TotalHT() {
		t.Fatalf("lines TTC %d / HT %d, tax details TTC %d / HT %d", sumTTC, sumHT, tax.TotalTTC(), tax.TotalHT())
	}
	// Articles (2), options payantes (2 : la gratuite n'apparaît pas),
	// supplément (1), livraison (1), remises (une par taux : 2).
	if kinds[models.SnapshotKindArticle] != 2 || kinds[models.SnapshotKindOption] != 2 || kinds[models.SnapshotKindSupplement] != 1 ||
		kinds[models.SnapshotKindDelivery] != 1 || kinds[models.SnapshotKindDiscount] != 2 {
		t.Fatalf("line kinds %v: %+v", kinds, items)
	}
	// Ordre et valeurs : pizza, ses options (figée 150, catalogue 50), son
	// supplément, puis la bière.
	if items[0].Name != "Pizza" || items[1].Name != "Grande taille" || items[1].PriceTTC != 150 || items[1].Quantity != 2 ||
		items[2].Name != "Pâte fine" || items[2].PriceTTC != 50 || items[3].Name != "Bacon" || items[3].TotalTTC != 200 ||
		items[4].Name != "Bière" || items[5].Kind != models.SnapshotKindDelivery || items[5].TotalTTC != 300 {
		t.Fatalf("lines: %+v", items)
	}
	for _, i := range []int{1, 2, 3} {
		if items[i].Parent == nil || *items[i].Parent != 0 {
			t.Fatalf("line %d must point to the pizza: %+v", i, items[i])
		}
	}
	// Le TTC transmis (2 420) diffère des lignes (2 320) : journalisé, ticket
	// émis quand même avec le TTC de la commande.
	if sale.TotalTTC != 2420 {
		t.Fatalf("ticket TTC %d, want the order TTC 2420", sale.TotalTTC)
	}

	// Commande Uber Eats avec une option payante : pas de ligne d'option tant
	// qu'on ne sait pas si le prix unitaire Uber inclut ses modificateurs.
	uber := scanID("order", `
		INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, price, TVA, HT, created_by)
		VALUES ($1, 2, 'UBER_EATS', 'COMPLETED', 'CLOSED', 'DELIVERY', 1200, 0, 1200, 'WEBHOOK_UBER_EATS') RETURNING order_id`, merchantID)
	uberLine := scanID("line", `
		INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, base_price, price, tva_id, tva_rate)
		VALUES ($1, $2, $3, 1, 1200, 1200, $4, 10) RETURNING order_item_id`, uber, pizza, merchantID, tvaID)
	exec("config", `
		INSERT INTO order_item_configuration (order_item_id, configuration_attribute_id, configuration_attribute_option_id, quantity, extra_price)
		VALUES ($1, $2, $3, 1, 200)`, uberLine, attr, sizeOpt)
	uberID := strconv.FormatInt(uber, 10)
	uberHT := int64(1200)
	if err := dbutils.RunInTx(ctx, db, func(txCtx context.Context) error {
		return svc.GenerateFiscalReceipt(txCtx, &models.Order{OrderID: uberID, MerchantID: &merchantID, TTC: 1200, HT: &uberHT}, nil)
	}); err != nil {
		t.Fatalf("GenerateFiscalReceipt (Uber): %v", err)
	}
	uberSale, err := svc.GetSaleReceiptByOrderID(ctx, uberID)
	if err != nil {
		t.Fatal(err)
	}
	var uberItems []models.SnapshotItem
	_ = json.Unmarshal(uberSale.ItemsSnapshot, &uberItems)
	if len(uberItems) != 1 || uberItems[0].TotalTTC != 1200 {
		t.Fatalf("Uber ticket must not get option lines: %+v", uberItems)
	}
}
