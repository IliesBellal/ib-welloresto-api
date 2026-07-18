//go:build postgres_integration

package printers

import (
	"context"
	"errors"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

func TestPrintersRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const (
		merchantID = "itest-printers-m1"
		printerID  = "itest-printer-1"
	)
	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM printers WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewRepository(db)

	ip := "192.168.1.50"
	ids := []string{"prod-1", "prod-2"}
	created, err := repo.CreatePrinter(ctx, merchantID, &CreatePrinterRequest{
		Name:                 "ITest Kitchen",
		ConnectionType:       "network",
		IPAddress:            &ip,
		Role:                 "kitchen",
		ProductionProductIDs: &ids,
	}, printerID, "fr")
	if err != nil {
		t.Fatalf("CreatePrinter failed against postgres: %v", err)
	}
	if !created.Enabled || created.IPAddress == nil || *created.IPAddress != ip || len(created.ProductionProductIDs) != 2 {
		t.Fatalf("unexpected created printer: %+v", created)
	}

	// Doublon de PK : doit être détecté cross-dialecte (dbx.IsDuplicateEntry → ErrInvalidInput)
	_, err = repo.CreatePrinter(ctx, merchantID, &CreatePrinterRequest{
		Name: "ITest Dup", ConnectionType: "network", Role: "kitchen",
	}, printerID, "fr")
	if !errors.Is(err, models.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on duplicate printer_id, got %v", err)
	}

	list, err := repo.ListPrinters(ctx, merchantID)
	if err != nil {
		t.Fatalf("ListPrinters failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 printer, got %d", len(list))
	}

	newName := "ITest Renamed"
	newRole := "bar"
	updated, err := repo.UpdatePrinter(ctx, merchantID, printerID, &UpdatePrinterRequest{
		Name: &newName,
		Role: &newRole,
	})
	if err != nil {
		t.Fatalf("UpdatePrinter failed against postgres: %v", err)
	}
	if updated.Name != newName || updated.Role != newRole {
		t.Fatalf("update not applied: %+v", updated)
	}

	if err := repo.DeletePrinter(ctx, merchantID, printerID); err != nil {
		t.Fatalf("DeletePrinter failed against postgres: %v", err)
	}
	if _, err := repo.GetPrinter(ctx, merchantID, printerID); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after soft delete, got %v", err)
	}
}
