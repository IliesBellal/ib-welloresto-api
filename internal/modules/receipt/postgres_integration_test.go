//go:build postgres_integration

package receipt

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

func TestReceiptRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "999902" // colonne integer en cible — chaîne numérique castée par PG
	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM receipts WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewReceiptRepository(db)

	// GetLastReceiptData prend le verrou de la chaîne receipts de l'établissement : elle
	// s'appelle dans la transaction qui insère le ticket suivant.
	lastReceipt := func() (num, hash string, err error) {
		err = dbutils.RunInTx(ctx, db, func(txCtx context.Context) error {
			var e error
			num, hash, e = repo.GetLastReceiptData(txCtx, merchantID)
			return e
		})
		return
	}

	// Premier reçu : la table est vide pour ce marchand
	lastNum, lastHash, err := lastReceipt()
	if err != nil {
		t.Fatalf("GetLastReceiptData (empty) failed: %v", err)
	}
	if lastNum != "" || lastHash != "" {
		t.Fatalf("expected empty last receipt, got %q/%q", lastNum, lastHash)
	}

	receipt := &models.Receipt{
		ReceiptID:        "itest-rcpt-1",
		MerchantID:       merchantID,
		OrderID:          "888801",
		ReceiptNumber:    "2026-000001",
		TotalTTC:         1250,
		TotalHT:          1136,
		TaxDetails:       []byte(`{"tva10": 114}`),
		ItemsSnapshot:    []byte(`[{"name": "Burger", "qty": 1}]`),
		PaymentsSnapshot: []byte(`[{"method": "card", "amount": 1250}]`),
		CreatedAt:        time.Now().UTC(),
		PrevHash:         "",
		Hash:             "abc123",
		Signature:        "sig-test",
	}
	if err := repo.InsertReceipt(ctx, receipt); err != nil {
		t.Fatalf("InsertReceipt failed against postgres: %v", err)
	}

	lastNum, lastHash, err = lastReceipt()
	if err != nil {
		t.Fatalf("GetLastReceiptData failed: %v", err)
	}
	if lastNum != "2026-000001" || lastHash != "abc123" {
		t.Fatalf("unexpected last receipt: %q/%q", lastNum, lastHash)
	}

	got, err := repo.GetReceiptByOrderID(ctx, "888801")
	if err != nil {
		t.Fatalf("GetReceiptByOrderID failed: %v", err)
	}
	if got.ReceiptID != "itest-rcpt-1" || got.TotalTTC != 1250 {
		t.Fatalf("unexpected receipt: %+v", got)
	}
}
