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

// Lot B conformité caisse, C9 : le ticket porte sa TVA ventilée par taux,
// nette des remises de caisse (mêmes règles que l'export comptable), et
// l'avoir est ventilé au prorata du ticket d'origine — ou, pour un ticket
// antérieur sans ventilation, des taux de la commande.
func TestReceiptTaxDetails_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const siret = "siret-rcpt-tax"
	var merchantID string
	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM receipts WHERE merchant_id = $1`,
			`DELETE FROM payments WHERE merchant_id = $1`,
			`DELETE FROM orderitems WHERE merchant_id = $1`,
			`DELETE FROM orders WHERE merchant_id = $1`,
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
		VALUES ('ITest Receipt Tax', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', 'mtok-rcpt-tax', 'UTC')
		RETURNING id`, siret).Scan(&mid); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(mid, 10)
	t.Cleanup(func() { cleanupFor(merchantID) })

	// Catégorie de TVA existante quelconque : les lignes portent leur taux
	// figé (tva_rate), qui prime sur celui de la catégorie.
	var tvaID int64
	if err := db.QueryRowContext(ctx, `SELECT tva_id FROM tva_categories WHERE tva_id <> -1 ORDER BY tva_id LIMIT 1`).Scan(&tvaID); err != nil {
		t.Fatalf("pick tva category: %v", err)
	}
	var productID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, price, category, tva_in_id, tva_take_away_id, tva_delivery_id)
		VALUES ($1, 'itest-rcpt-tax', 0, 'itest', $2, $2, $2) RETURNING product_id`, merchantID, tvaID).Scan(&productID); err != nil {
		t.Fatalf("seed product: %v", err)
	}

	// Commande : 2 × 6 € + 8 € à 10 %, 5 € à 20 %, livraison 3 € à 20 % ;
	// remise de caisse de 2,80 €. Total 28 €, net 25,20 €.
	var orderIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, price, TVA, HT, delivery_fees, delivery_fees_tva_rate, created_by)
		VALUES ($1, 1, 'WELLO_RESTO', 'CLOSED', 'CLOSED', 'DELIVERY', 2800, 0, 2800, 300, 20, 'itest')
		RETURNING order_id`, merchantID).Scan(&orderIntID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	orderID := strconv.FormatInt(orderIntID, 10)
	for _, l := range []struct {
		qty, price int
		rate       float64
	}{{2, 600, 10}, {1, 800, 10}, {1, 500, 20}} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, base_price, price, tva_id, tva_rate)
			VALUES ($1, $2, $3, $4, $5, $5, $6, $7)`, orderIntID, productID, merchantID, l.qty, l.price, tvaID, l.rate); err != nil {
			t.Fatalf("seed line: %v", err)
		}
	}
	for _, p := range []struct {
		mop    string
		amount int
	}{{"CB", 2520}, {"CURRENCY", 280}} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, enabled)
			VALUES ($1, 'itest', $2, $3, $4, true)`, merchantID, orderIntID, p.amount, p.mop); err != nil {
			t.Fatalf("seed payment: %v", err)
		}
	}

	svc := NewReceiptService(NewReceiptRepository(db))
	ht := int64(2800)
	inTx := func(fn func(context.Context) error) {
		t.Helper()
		if err := dbutils.RunInTx(ctx, db, fn); err != nil {
			t.Fatal(err)
		}
	}

	// Ticket de vente.
	inTx(func(txCtx context.Context) error {
		return svc.GenerateFiscalReceipt(txCtx, &models.Order{OrderID: orderID, MerchantID: &merchantID, TTC: 2800, HT: &ht}, nil, nil)
	})
	sale, err := svc.GetSaleReceiptByOrderID(ctx, orderID)
	if err != nil {
		t.Fatal(err)
	}
	tax, ok := fiscal.ParseTaxDetails(sale.TaxDetails)
	if !ok {
		t.Fatalf("sale receipt without tax breakdown: %s", sale.TaxDetails)
	}
	// 10 % : 2000 − 200 = 1800 ; 20 % : 800 − 80 = 720.
	if tax.Discount != 280 || len(tax.Lines) != 2 || tax.Lines[0].Rate != 10 || tax.Lines[0].TTC != 1800 ||
		tax.Lines[1].Rate != 20 || tax.Lines[1].TTC != 720 {
		t.Fatalf("sale tax breakdown: %+v", tax)
	}

	// Avoir de 10 € : ventilé au prorata, somme exacte, HT = somme des HT.
	inTx(func(txCtx context.Context) error {
		return svc.GenerateRefundReceipt(txCtx, merchantID, orderID, sale, -1000, "CB")
	})
	refund, err := svc.GetReceiptByOrderID(ctx, orderID)
	if err != nil {
		t.Fatal(err)
	}
	refundTax, ok := fiscal.ParseTaxDetails(refund.TaxDetails)
	if !ok || refundTax.TotalTTC() != -1000 || int64(refund.TotalHT) != refundTax.TotalHT() || len(refundTax.Lines) != 2 {
		t.Fatalf("refund tax breakdown: ok=%v %+v total_ht=%d", ok, refundTax, refund.TotalHT)
	}
	var items []models.SnapshotItem
	if err := json.Unmarshal(refund.ItemsSnapshot, &items); err != nil || len(items) != 2 || items[0].TaxRate != 1000 || items[1].TaxRate != 2000 {
		t.Fatalf("refund items: one line per rate expected, got %s", refund.ItemsSnapshot)
	}

	// Ticket antérieur au lot B (ventilation '{}') : l'avoir reprend les taux
	// de la commande.
	legacy := *sale
	legacy.TaxDetails = []byte(`{}`)
	inTx(func(txCtx context.Context) error {
		return svc.GenerateRefundReceipt(txCtx, merchantID, orderID, &legacy, -500, "CB")
	})
	legacyRefund, err := svc.GetReceiptByOrderID(ctx, orderID)
	if err != nil {
		t.Fatal(err)
	}
	if lt, ok := fiscal.ParseTaxDetails(legacyRefund.TaxDetails); !ok || lt.TotalTTC() != -500 || len(lt.Lines) != 2 {
		t.Fatalf("legacy refund tax breakdown: ok=%v %+v", ok, lt)
	}
}
