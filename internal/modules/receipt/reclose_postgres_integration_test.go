//go:build postgres_integration

package receipt

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

// Lot C conformité caisse, R2 et R3 (docs/attestation-conformite-03-lot-C-brief.md) :
// à la reclôture d'une commande rouverte, ticket inchangé si la vente est
// identique (paiements exclus), sinon avoir de la vente en vigueur puis
// nouveau ticket ; à l'annulation, avoir de la vente en vigueur.
func TestReceiptReclose_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const siret = "siret-rcpt-reclose"
	var merchantID string
	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM receipts WHERE merchant_id = $1`,
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
		VALUES ('ITest Receipt Reclose', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', 'mtok-rcpt-reclose', 'UTC')
		RETURNING id`, siret).Scan(&mid); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(mid, 10)
	t.Cleanup(func() { cleanupFor(merchantID) })

	var tvaID, productID int64
	if err := db.QueryRowContext(ctx, `SELECT tva_id FROM tva_categories WHERE tva_id <> -1 ORDER BY tva_id LIMIT 1`).Scan(&tvaID); err != nil {
		t.Fatalf("pick tva category: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, price, category, tva_in_id, tva_take_away_id, tva_delivery_id)
		VALUES ($1, 'itest-rcpt-reclose', 0, 'itest', $2, $2, $2) RETURNING product_id`, merchantID, tvaID).Scan(&productID); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	var orderIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, price, TVA, HT, created_by)
		VALUES ($1, 1, 'WELLO_RESTO', 'CLOSED', 'CLOSED', 'IN', 1700, 0, 1700, 'itest')
		RETURNING order_id`, merchantID).Scan(&orderIntID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	orderID := strconv.FormatInt(orderIntID, 10)
	var lineA int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, base_price, price, tva_id, tva_rate)
		VALUES ($1, $2, $3, 2, 600, 600, $4, 10) RETURNING order_item_id`, orderIntID, productID, merchantID, tvaID).Scan(&lineA); err != nil {
		t.Fatalf("seed line: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, base_price, price, tva_id, tva_rate)
		VALUES ($1, $2, $3, 1, 500, 500, $4, 20)`, orderIntID, productID, merchantID, tvaID); err != nil {
		t.Fatalf("seed line: %v", err)
	}

	svc := NewReceiptService(NewReceiptRepository(db))
	inTx := func(fn func(context.Context) error) {
		t.Helper()
		if err := dbutils.RunInTx(ctx, db, fn); err != nil {
			t.Fatal(err)
		}
	}
	close := func(ttc int64, payments []models.SnapshotPayment) {
		t.Helper()
		ht := ttc
		inTx(func(txCtx context.Context) error {
			return svc.GenerateFiscalReceipt(txCtx, &models.Order{OrderID: orderID, MerchantID: &merchantID, TTC: ttc, HT: &ht}, payments)
		})
	}
	type row struct {
		number   string
		ttc      int
		payments string
		items    []models.SnapshotItem
		tax      fiscal.TaxDetails
	}
	receipts := func() []row {
		t.Helper()
		rows, err := db.QueryContext(ctx, `
			SELECT receipt_number, total_ttc, payments_snapshot::text, items_snapshot::text, tax_details::text
			FROM receipts WHERE order_id = $1 ORDER BY created_at, receipt_number`, orderIntID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []row
		for rows.Next() {
			var r row
			var items, tax string
			if err := rows.Scan(&r.number, &r.ttc, &r.payments, &items, &tax); err != nil {
				t.Fatal(err)
			}
			_ = json.Unmarshal([]byte(items), &r.items)
			r.tax, _ = fiscal.ParseTaxDetails([]byte(tax))
			out = append(out, r)
		}
		return out
	}

	cash := []models.SnapshotPayment{{Amount: 1700, MOP: "ES"}}
	card := []models.SnapshotPayment{{Amount: 1700, MOP: "CB"}}

	// 1. Première clôture : un ticket.
	close(1700, cash)
	if got := receipts(); len(got) != 1 || got[0].ttc != 1700 {
		t.Fatalf("first close: %+v", got)
	}

	// 2. Reclôture identique, paiement changé (R3) : aucun ticket.
	close(1700, card)
	if got := receipts(); len(got) != 1 {
		t.Fatalf("identical reclose issued %d receipts, want 1", len(got))
	}

	// 3. Ligne modifiée : avoir de la vente (TVA ventilée au prorata, sans
	// paiement) puis nouveau ticket.
	if _, err := db.ExecContext(ctx, `UPDATE orderitems SET quantity = 3 WHERE order_item_id = $1`, lineA); err != nil {
		t.Fatal(err)
	}
	close(2300, card)
	got := receipts()
	if len(got) != 3 || got[1].ttc != -1700 || got[2].ttc != 2300 {
		t.Fatalf("changed reclose: %+v", got)
	}
	if got[1].payments != "[]" || got[1].tax.TotalTTC() != -1700 || len(got[1].tax.Lines) != 2 ||
		len(got[1].items) != 2 || got[1].items[0].Name != "Avoir sur facture "+got[0].number {
		t.Fatalf("correction credit note: %+v", got[1])
	}

	// 4. Remboursement partiel de 3 €, puis nouvelle modification : l'avoir
	// ne porte que sur ce qu'il reste de la vente (2 000).
	sale2, err := svc.GetSaleReceiptByOrderID(ctx, orderID)
	if err != nil {
		t.Fatal(err)
	}
	inTx(func(txCtx context.Context) error {
		return svc.GenerateRefundReceipt(txCtx, merchantID, orderID, sale2, -300, "CB")
	})
	if _, err := db.ExecContext(ctx, `UPDATE orderitems SET quantity = 1 WHERE order_item_id = $1`, lineA); err != nil {
		t.Fatal(err)
	}
	close(1100, card)
	got = receipts()
	if len(got) != 6 || got[3].ttc != -300 || got[4].ttc != -2000 || got[5].ttc != 1100 {
		t.Fatalf("reclose after partial refund: %+v", got)
	}

	// 5. Annulation de la commande rouverte : avoir de la vente en vigueur,
	// une seule fois.
	inTx(func(txCtx context.Context) error { return svc.CancelSaleReceipt(txCtx, merchantID, orderID) })
	inTx(func(txCtx context.Context) error { return svc.CancelSaleReceipt(txCtx, merchantID, orderID) })
	got = receipts()
	if len(got) != 7 || got[6].ttc != -1100 || got[6].payments != "[]" {
		t.Fatalf("cancel after reopen: %+v", got)
	}

	// 6. Vente entièrement annulée puis reclose à l'identique : elle ne compte
	// plus, nouveau ticket.
	close(1100, card)
	if got = receipts(); len(got) != 8 || got[7].ttc != 1100 {
		t.Fatalf("reclose after full cancel: %+v", got)
	}

	// 7. Chaîne des tickets de l'établissement : numéros qui se suivent,
	// chaque ticket re-scellé à l'identique sur le précédent.
	rows, err := db.QueryContext(ctx, `
		SELECT receipt_number, order_id::text, created_at, total_ttc, total_ht, tax_details::text, items_snapshot::text,
		       payments_snapshot::text, prev_hash, hash, signature
		FROM receipts WHERE merchant_id = $1 ORDER BY created_at, receipt_number`, merchantID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	prev, n, lastSeq := fiscal.GenesisHash, 0, 0
	for rows.Next() {
		var number, order, tax, items, pays, prevHash, hash string
		var sig sql.NullString
		var at time.Time
		var ttc, ht int
		if err := rows.Scan(&number, &order, &at, &ttc, &ht, &tax, &items, &pays, &prevHash, &hash, &sig); err != nil {
			t.Fatal(err)
		}
		seq, _ := strconv.Atoi(number[len(number)-6:])
		if n > 0 && seq != lastSeq+1 {
			t.Fatalf("receipt %s: number not consecutive after %d", number, lastSeq)
		}
		if prevHash != prev {
			t.Fatalf("receipt %s: parent %s, want %s", number, prevHash, prev)
		}
		p, err := fiscal.NewReceiptPayload(merchantID, number, order, at, ttc, ht, []byte(tax), []byte(items), []byte(pays))
		if err != nil {
			t.Fatal(err)
		}
		if h, s, _ := fiscal.Seal(fiscal.ChainReceipts, prevHash, p); h != hash || s != sig.String {
			t.Fatalf("receipt %s does not re-seal identically", number)
		}
		prev, lastSeq, n = hash, seq, n+1
	}
	if n != 8 {
		t.Fatalf("%d receipts in the chain, want 8", n)
	}
}
