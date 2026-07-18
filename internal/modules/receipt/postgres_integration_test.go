//go:build postgres_integration

package receipt

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
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

	// Premier reçu : la table est vide pour ce marchand
	lastNum, lastHash, err := repo.GetLastReceiptData(ctx, merchantID)
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

	lastNum, lastHash, err = repo.GetLastReceiptData(ctx, merchantID)
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
