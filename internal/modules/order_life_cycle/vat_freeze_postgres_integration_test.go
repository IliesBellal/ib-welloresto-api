//go:build postgres_integration

package order_life_cycle

import (
	"context"
	"database/sql"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/customers"
	"welloresto-api/internal/modules/orders"
)

// Vérification réelle de la TVA figée sur les lignes (migration 164,
// docs/TVA_FIGEE_LIGNES_COMMANDE.md) : freezeOrderVAT fige catégorie et taux
// selon le type de commande, les recalcule quand le type change tant que la
// commande est ouverte, ne touche plus une commande close sauf pour remplir
// une ligne encore vide, et fige le taux des frais de livraison.
func TestOrderLifeCycleRepository_FreezeOrderVAT_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantID string
	cleanupFor := func(mid string) {
		_, _ = db.ExecContext(ctx, `DELETE FROM tva_categories WHERE tva_title LIKE 'itest-olc-vat%'`)
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM orderitems WHERE merchant_id = $1`,
			`DELETE FROM orders WHERE merchant_id = $1`,
			`DELETE FROM products WHERE merchant_Id = $1`,
			`DELETE FROM merchant WHERE id = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-olc-vat' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	} else {
		cleanupFor("")
	}
	t.Cleanup(func() { cleanupFor(merchantID) })

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest OLC VAT', 'a', '1', 's', '75001', 'Paris', 'siret-olc-vat', 'https://x', '06', 'mtok-olc-vat', 'UTC')
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
	catIn := newCategory("itest-olc-vat-10", 10)
	catTakeAway := newCategory("itest-olc-vat-5.5", 5.5)

	var productID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_Id, name, price, category, status, tva_in_id, tva_take_away_id, tva_delivery_id)
		VALUES ($1, 'itest-olc-vat-prod', 1000, 'c', '1', $2, $3, $2) RETURNING product_id`,
		merchantID, catIn, catTakeAway).Scan(&productID); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	productIDStr := strconv.FormatInt(productID, 10)

	var orderID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, price, TVA, HT, created_by, delivery_fees)
		VALUES ($1, 1, 'WELLO_RESTO', 'PENDING', 'OPEN', 'IN', 1300, 0, 0, 'itest', 300)
		RETURNING order_id`, merchantID).Scan(&orderID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	orderIDStr := strconv.FormatInt(orderID, 10)

	custoRepo := customers.NewCustomerRepository(db)
	repo := NewOrdersLifeCycleRepository(db, custoRepo)

	insertLine := func() int64 {
		t.Helper()
		id, err := repo.InsertOrderItem(ctx, &models.OrderItemInsert{
			OrderID: orderIDStr, ProductID: productIDStr, MerchantID: merchantID,
			Quantity: 1, Price: 1000, BasePrice: 1000, CreatedBy: "itest",
		})
		if err != nil {
			t.Fatalf("InsertOrderItem: %v", err)
		}
		return id
	}
	type frozen struct {
		tvaID         sql.NullInt64
		rate          sql.NullFloat64
		reconstructed bool
	}
	readLine := func(orderItemID int64) frozen {
		t.Helper()
		var f frozen
		if err := db.QueryRowContext(ctx, `SELECT tva_id, tva_rate, tva_reconstructed FROM orderitems WHERE order_item_id = $1`,
			orderItemID).Scan(&f.tvaID, &f.rate, &f.reconstructed); err != nil {
			t.Fatalf("read orderitem %d: %v", orderItemID, err)
		}
		return f
	}
	freeze := func() {
		t.Helper()
		if err := repo.freezeOrderVAT(ctx, orderIDStr); err != nil {
			t.Fatalf("freezeOrderVAT: %v", err)
		}
	}
	mustExec := func(label, q string, args ...interface{}) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}

	line1 := insertLine()
	if f := readLine(line1); f.tvaID.Valid {
		t.Fatalf("ligne insérée sans figeage : tva_id = %v, want NULL", f.tvaID)
	}

	// 1. Commande ouverte sur place : catégorie « sur place » du produit.
	freeze()
	if f := readLine(line1); f.tvaID.Int64 != catIn || f.rate.Float64 != 10 || f.reconstructed {
		t.Fatalf("après figeage (IN) = %+v, want tva_id=%d rate=10 reconstructed=false", f, catIn)
	}

	// 2. Changement de type pendant que la commande est ouverte : refigée.
	mustExec("order_type -> TAKE_AWAY", `UPDATE orders SET order_type = 'TAKE_AWAY' WHERE order_id = $1`, orderID)
	freeze()
	if f := readLine(line1); f.tvaID.Int64 != catTakeAway || f.rate.Float64 != 5.5 {
		t.Fatalf("après changement de type (TAKE_AWAY) = %+v, want tva_id=%d rate=5.5", f, catTakeAway)
	}

	// Lecture des commandes (caisse, facture) : taux renvoyé pour le type de la
	// commande sur la ligne line1.
	fetcher := orders.NewOrdersFetcher(db)
	takeAwayRateOfLine1 := func() float64 {
		t.Helper()
		fetched, err := fetcher.FetchAndBuildOrders(ctx, merchantID, orders.NewFilter(" AND o.order_id = ? ", orderIDStr), "", "")
		if err != nil || len(fetched) != 1 {
			t.Fatalf("FetchAndBuildOrders = (%d commandes, %v), want 1", len(fetched), err)
		}
		for _, p := range fetched[0].Products {
			if p.OrderItemID == strconv.FormatInt(line1, 10) {
				if p.TVATakeAway == nil {
					t.Fatalf("ligne %d : tva_rate_take_away absent", line1)
				}
				return *p.TVATakeAway
			}
		}
		t.Fatalf("ligne %d absente de FetchAndBuildOrders", line1)
		return 0
	}

	// Commande encore ouverte, catalogue modifié sans réenregistrement : la
	// caisse reçoit le taux du produit (elle recalcule ses totaux elle-même).
	mustExec("change product category", `UPDATE products SET tva_take_away_id = $1 WHERE product_id = $2`, catIn, productID)
	if got := takeAwayRateOfLine1(); got != 10 {
		t.Fatalf("commande ouverte : tva_rate_take_away = %v, want 10 (taux du produit)", got)
	}

	// 3. Commande close puis catalogue modifié : la ligne figée ne bouge plus,
	// même si freezeOrderVAT est rappelée, et la lecture renvoie le taux figé.
	mustExec("close order", `UPDATE orders SET state = 'CLOSED' WHERE order_id = $1`, orderID)
	mustExec("change category rate", `UPDATE tva_categories SET tva_rate = 7 WHERE tva_id = $1`, catTakeAway)
	freeze()
	if f := readLine(line1); f.tvaID.Int64 != catTakeAway || f.rate.Float64 != 5.5 {
		t.Fatalf("commande close, catalogue modifié = %+v, want toujours tva_id=%d rate=5.5", f, catTakeAway)
	}
	if got := takeAwayRateOfLine1(); got != 5.5 {
		t.Fatalf("commande close : tva_rate_take_away = %v, want 5.5 (taux figé, pas celui du produit)", got)
	}

	// 4. Une ligne encore vide d'une commande close est remplie (configuration
	// du jour) ; la ligne déjà figée reste intacte.
	line2 := insertLine()
	freeze()
	if f := readLine(line2); f.tvaID.Int64 != catIn || f.rate.Float64 != 10 {
		t.Fatalf("ligne vide sur commande close = %+v, want tva_id=%d rate=10 (catégorie du jour)", f, catIn)
	}
	if f := readLine(line1); f.tvaID.Int64 != catTakeAway || f.rate.Float64 != 5.5 {
		t.Fatalf("ligne figée touchée par le remplissage d'une autre : %+v", f)
	}

	// 5. Frais de livraison : taux de la catégorie -1 figé sur la commande, et
	// relu par GetDeliveryFeesVATRate (facture).
	var feesCategoryRate sql.NullFloat64
	if err := db.QueryRowContext(ctx, `SELECT tva_rate FROM tva_categories WHERE tva_id = -1`).Scan(&feesCategoryRate); err != nil && err != sql.ErrNoRows {
		t.Fatalf("read category -1: %v", err)
	}
	var feesRate sql.NullFloat64
	if err := db.QueryRowContext(ctx, `SELECT delivery_fees_tva_rate FROM orders WHERE order_id = $1`, orderID).Scan(&feesRate); err != nil {
		t.Fatalf("read delivery_fees_tva_rate: %v", err)
	}
	if feesRate != feesCategoryRate {
		t.Fatalf("delivery_fees_tva_rate = %v, want taux de la catégorie -1 (%v)", feesRate, feesCategoryRate)
	}
	if feesCategoryRate.Valid {
		// Un taux figé différent de la catégorie doit primer pour la facture.
		mustExec("force frozen fees rate", `UPDATE orders SET delivery_fees_tva_rate = 2.1 WHERE order_id = $1`, orderID)
		rate, found, err := repo.GetDeliveryFeesVATRate(ctx, orderIDStr)
		if err != nil || !found || rate != float64(float32(2.1)) {
			t.Fatalf("GetDeliveryFeesVATRate = (%v, %v, %v), want le taux figé 2.1", rate, found, err)
		}
	}
}
