//go:build postgres_integration

package receipt

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

// GET /orders/{id}/receipt (lot E, phase 2) : lecture des tickets d'une
// commande, cloisonnée par établissement.
func TestGetOrderReceipts_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const merchantID = "itest-printable"
	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM receipts WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	newOrder := func(state string) string {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, order_source, price, TVA, HT, created_by)
			VALUES ($1, 1, 'WELLO_RESTO', $2, $2, 'IN', 'WELLO_RESTO_POS', 1100, 100, 1000, 'itest') RETURNING order_id`,
			merchantID, state).Scan(&id); err != nil {
			t.Fatalf("seed order: %v", err)
		}
		return strconv.FormatInt(id, 10)
	}
	closed, open := newOrder("CLOSED"), newOrder("OPEN")
	at := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	for i, ttc := range []int{1100, -1100, 1100} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO receipts (receipt_id, merchant_id, order_id, receipt_number, total_ttc, total_ht, tax_details, items_snapshot, payments_snapshot, created_at, prev_hash, hash, signature)
			VALUES ($1, $2, $3, $4, $5, $6, $7, '[]', '[]', $8, 'p', $9, 's')`,
			"itest-printable-"+strconv.Itoa(i), merchantID, closed, "F-2026-90000"+strconv.Itoa(i+1), ttc, ttc*10/11,
			`{"lines":[{"rate":10,"ttc":`+strconv.Itoa(ttc)+`,"ht":`+strconv.Itoa(ttc*10/11)+`,"tva":`+strconv.Itoa(ttc-ttc*10/11)+`}],"discount":0}`,
			at.Add(time.Duration(i)*time.Hour), "h"+strconv.Itoa(i)); err != nil {
			t.Fatalf("seed receipt: %v", err)
		}
	}

	svc := NewReceiptService(NewReceiptRepository(db))
	r, err := svc.GetOrderReceipts(ctx, merchantID, closed)
	if err != nil || !r.Closed || len(r.Receipts) != 3 || r.CurrentReceiptNumber == nil || *r.CurrentReceiptNumber != "F-2026-900003" ||
		r.Receipts[1].Type != PrintableRefund || len(r.Receipts[2].VAT) != 1 || r.Receipts[0].Hash != "h0" {
		t.Fatalf("closed order = (%+v, %v)", r, err)
	}
	if r, err := svc.GetOrderReceipts(ctx, merchantID, open); err != nil || r.Closed || len(r.Receipts) != 0 || r.CurrentReceiptNumber != nil {
		t.Fatalf("open order = (%+v, %v)", r, err)
	}
	if _, err := svc.GetOrderReceipts(ctx, "itest-printable-other", closed); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("other merchant: err = %v", err)
	}
}
