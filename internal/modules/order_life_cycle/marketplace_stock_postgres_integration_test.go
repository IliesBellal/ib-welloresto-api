//go:build postgres_integration

package order_life_cycle

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/customers"
)

// Une commande Uber Eats / Deliveroo contenant un produit en rupture est créée
// (le client l'a payée), avec la rupture signalée en tête du commentaire ; la même
// commande saisie à la caisse reste refusée (unavailable_products).
func TestCreateOrder_MarketplaceOutOfStock_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantID string
	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM order_comments WHERE order_id IN (SELECT order_id FROM orders WHERE merchant_id = $1)`,
			`DELETE FROM order_item_configuration WHERE order_item_id IN (SELECT order_item_id FROM orderitems WHERE merchant_id = $1)`,
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
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-olc-mkt' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	}
	t.Cleanup(func() { cleanupFor(merchantID) })

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest OLC Marketplace', 'a', '1', 's', '75001', 'Paris', 'siret-olc-mkt', 'https://x', '06', 'mtok-olc-mkt', 'UTC')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)
	if _, err := db.ExecContext(ctx, `INSERT INTO merchant_parameters (merchant_id, last_menu_update) VALUES ($1, now())`, merchantID); err != nil {
		t.Fatalf("seed params: %v", err)
	}

	var prodOK, prodOut int64
	if err := db.QueryRowContext(ctx, `INSERT INTO products (merchant_Id, name, price, category, status) VALUES ($1, 'itest-mkt-ok', 1000, 'c', '1') RETURNING product_id`, merchantID).Scan(&prodOK); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO products (merchant_Id, name, price, category, status) VALUES ($1, 'itest-mkt-out', 500, 'c', 'out_of_stock') RETURNING product_id`, merchantID).Scan(&prodOut); err != nil {
		t.Fatalf("seed blocked product: %v", err)
	}
	okID, outID := strconv.FormatInt(prodOK, 10), strconv.FormatInt(prodOut, 10)

	repo := NewOrdersLifeCycleRepository(db, customers.NewCustomerRepository(db))

	newReq := func(brand, orderID string, products ...models.OrderProductPayload) *models.RequestObject {
		createdBy := "WEBHOOK_" + brand
		register := brand
		comment := "Sans oignons"
		num := "MK" + strconv.Itoa(len(orderID)) + brand[:1] // brand_order_num : varchar(10)
		return &models.RequestObject{
			MerchantID: merchantID,
			Order: models.OrderRequest{
				Brand: brand, BrandOrderID: &orderID, BrandOrderNum: &num, CashRegisterId: &register,
				TTC: 1500, OrderType: models.OrderTypeDelivery, CreatedBy: &createdBy, Comment: &comment,
				MerchantApproval: models.MerchantApprovalPendingApproval, Products: products,
			},
		}
	}
	lines := []models.OrderProductPayload{
		{ProductID: okID, ProductName: "Pizza OK", Quantity: 1, Price: 1000},
		{ProductID: outID, ProductName: "Pizza en rupture", Quantity: 1, Price: 500},
	}
	orderComment := func(orderID string) string {
		var content string
		if err := db.QueryRowContext(ctx, `SELECT content FROM order_comments WHERE order_id = $1 AND order_item_id IS NULL`, orderID).Scan(&content); err != nil {
			t.Fatalf("lecture du commentaire de la commande %s: %v", orderID, err)
		}
		return content
	}

	for _, brand := range []string{models.BrandUberEats, models.BrandDeliveroo} {
		res, err := repo.CreateOrder(ctx, newReq(brand, "itest-mkt-"+brand+"-1", lines...))
		if err != nil || res.Status != "success" || res.OrderID == "" {
			t.Fatalf("%s : CreateOrder(rupture) = (%+v, %v), want une commande créée", brand, res, err)
		}
		if len(res.UnavailableProducts) != 1 || res.UnavailableProducts[0] != outID {
			t.Fatalf("%s : UnavailableProducts = %v, want [%s]", brand, res.UnavailableProducts, outID)
		}
		var nItems int
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM orderitems WHERE order_id = $1`, res.OrderID).Scan(&nItems)
		if nItems != 2 {
			t.Fatalf("%s : %d lignes créées, want 2 (le produit en rupture reste dans la commande)", brand, nItems)
		}
		got := orderComment(res.OrderID)
		if !strings.HasPrefix(got, "Rupture de stock : Pizza en rupture") || !strings.HasSuffix(got, "\nSans oignons") {
			t.Fatalf("%s : commentaire = %q, want la rupture en tête puis « Sans oignons »", brand, got)
		}
	}

	// sans rupture : rien ne change
	res, err := repo.CreateOrder(ctx, newReq(models.BrandUberEats, "itest-mkt-ok-1", lines[0]))
	if err != nil || res.Status != "success" || len(res.UnavailableProducts) != 0 {
		t.Fatalf("CreateOrder(sans rupture) = (%+v, %v)", res, err)
	}
	if got := orderComment(res.OrderID); got != "Sans oignons" {
		t.Fatalf("commentaire sans rupture = %q, want inchangé", got)
	}
}
