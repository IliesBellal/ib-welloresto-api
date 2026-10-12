//go:build postgres_integration

package subscriptioninvoices

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// À lancer après application de la migration 179 :
//
//	POSTGRES_URL=... go test -tags postgres_integration ./internal/modules/subscriptioninvoices/
func TestSubscriptionInvoiceDocuments_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	repo := NewRepository(db)

	cleanup := func() {
		if _, err := db.ExecContext(ctx, `DELETE FROM subscription_invoice_documents WHERE stripe_invoice_id LIKE 'in_itest_%'`); err != nil {
			t.Fatalf("cleanup: %v", err)
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	ref := Ref{InvoiceID: "in_itest_1", MerchantID: "2", Number: "ITEST-0001", AmountPaid: 7900, Currency: "eur",
		PeriodStart: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), PeriodEnd: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		PaidAt: time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)}

	// File d'attente : une facture rejouée n'est enregistrée qu'une fois.
	for i := 0; i < 2; i++ {
		if err := repo.InsertPending(ctx, ref); err != nil {
			t.Fatalf("InsertPending: %v", err)
		}
	}
	pending, err := repo.ListPending(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, p := range pending {
		if p.InvoiceID == ref.InvoiceID {
			found++
			if p.AmountPaid != 7900 || p.MerchantID != "2" || p.Attempts != 0 {
				t.Errorf("pending = %+v", p)
			}
		}
	}
	if found != 1 {
		t.Fatalf("facture en file %d fois, want 1", found)
	}

	// Pas listée au back-office tant qu'elle n'est pas envoyée.
	if listed, _ := repo.ListForMerchant(ctx, "2", 10); len(listed) != 0 {
		for _, l := range listed {
			if l.InvoiceID == ref.InvoiceID {
				t.Fatal("une facture non envoyée ne doit pas être listée")
			}
		}
	}

	if err := repo.RecordFailure(ctx, ref.InvoiceID, "PDF pas prêt", false); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkDone(ctx, ref.InvoiceID, Done{Number: "ITEST-0001", Recipient: "a@b.fr", PDFKey: "k1"}); err != nil {
		t.Fatal(err)
	}

	listed, err := repo.ListForMerchant(ctx, "2", 10)
	if err != nil {
		t.Fatal(err)
	}
	var seen *Listed
	for i := range listed {
		if listed[i].InvoiceID == ref.InvoiceID {
			seen = &listed[i]
		}
	}
	if seen == nil || !seen.HasPDF || seen.AmountPaid != 7900 || seen.Number != "ITEST-0001" {
		t.Fatalf("facture listée = %+v", seen)
	}
	if other, _ := repo.GetForMerchant(ctx, "999999", ref.InvoiceID); other != nil {
		t.Errorf("un autre établissement ne doit pas voir cette facture : %+v", other)
	}
	if mine, err := repo.GetForMerchant(ctx, "2", ref.InvoiceID); err != nil || mine == nil || mine.PDFKey != "k1" {
		t.Errorf("GetForMerchant = %+v, %v", mine, err)
	}
}
