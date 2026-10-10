//go:build postgres_integration

package payouts

import (
	"context"
	"sync"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// À lancer après application de la migration 178 :
//
//	POSTGRES_URL=... go test -tags postgres_integration ./internal/modules/payouts/
func TestPayoutDocuments_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	repo := NewRepository(db)

	const payoutID, otherPayoutID = "po_itest_1", "po_itest_2"
	cleanup := func() {
		for _, q := range []string{
			`DELETE FROM payout_documents WHERE payout_id LIKE 'po_itest_%'`,
			`DELETE FROM commission_invoices WHERE payout_id LIKE 'po_itest_%'`,
			`DELETE FROM invoice_counters WHERE series = 'ITEST'`,
		} {
			if _, err := db.ExecContext(ctx, q); err != nil {
				t.Fatalf("cleanup: %v", err)
			}
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	// File d'attente : un payout rejoué n'est enregistré qu'une fois.
	ref := Ref{PayoutID: payoutID, AccountID: "acct_itest", Amount: 12345, Currency: "eur", ArrivalDate: time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)}
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
		if p.PayoutID == payoutID {
			found++
			if p.Amount != 12345 || p.AccountID != "acct_itest" || p.Attempts != 0 {
				t.Errorf("pending = %+v", p)
			}
		}
	}
	if found != 1 {
		t.Fatalf("payout en file %d fois, want 1", found)
	}

	// Un échec compte une tentative et laisse le payout en attente ; l'abandon le sort de la file.
	if err := repo.RecordFailure(ctx, payoutID, "pas rapproché", false); err != nil {
		t.Fatal(err)
	}
	pending, _ = repo.ListPending(ctx, 100)
	for _, p := range pending {
		if p.PayoutID == payoutID && p.Attempts != 1 {
			t.Errorf("tentatives = %d, want 1", p.Attempts)
		}
	}
	if err := repo.MarkDone(ctx, payoutID, Done{MerchantID: "2", Recipient: "a@b.fr", StatementKey: "k1"}); err != nil {
		t.Fatal(err)
	}
	pending, _ = repo.ListPending(ctx, 100)
	for _, p := range pending {
		if p.PayoutID == payoutID {
			t.Error("un payout traité ne doit plus être en file")
		}
	}

	// Back-office : le payout traité est listé pour son établissement, et pour lui seul.
	listed, err := repo.ListForMerchant(ctx, "2", 10)
	if err != nil {
		t.Fatal(err)
	}
	var seen *Listed
	for i := range listed {
		if listed[i].PayoutID == payoutID {
			seen = &listed[i]
		}
	}
	if seen == nil || !seen.HasStatement || seen.HasInvoice || seen.Amount != 12345 {
		t.Fatalf("payout listé = %+v", seen)
	}
	if other, _ := repo.GetForMerchant(ctx, "999999", payoutID); other != nil {
		t.Errorf("un autre établissement ne doit pas voir ce payout : %+v", other)
	}
	if mine, err := repo.GetForMerchant(ctx, "2", payoutID); err != nil || mine == nil || mine.StatementKey != "k1" {
		t.Errorf("GetForMerchant = %+v, %v", mine, err)
	}

	// Numérotation : séquentielle par (série, année), et une facture par payout.
	in := NewInvoice{Series: "ITEST", MerchantID: "2", PayoutID: payoutID, AmountTTC: 120,
		PeriodStart: ref.ArrivalDate.AddDate(0, 0, -30), PeriodEnd: ref.ArrivalDate, IssuedAt: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)}
	first, err := repo.CreateInvoice(ctx, in)
	if err != nil {
		t.Fatalf("CreateInvoice: %v", err)
	}
	if first.Number != "ITEST-2026-000001" || first.AmountHT != 100 || first.AmountVAT != 20 {
		t.Errorf("facture = %+v", first)
	}
	again, err := repo.CreateInvoice(ctx, in)
	if err != nil || again.Number != first.Number {
		t.Fatalf("même payout : %+v, %v — doit renvoyer la première facture", again, err)
	}
	in.PayoutID = otherPayoutID
	second, err := repo.CreateInvoice(ctx, in)
	if err != nil || second.Number != "ITEST-2026-000002" {
		t.Fatalf("second payout : %+v, %v — numéro attendu ITEST-2026-000002 (sans trou malgré le doublon)", second, err)
	}

	// Émissions concurrentes pour un même payout : une seule facture, un seul numéro consommé.
	in.PayoutID = "po_itest_3"
	var wg sync.WaitGroup
	numbers := make([]string, 5)
	for i := range numbers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			inv, err := repo.CreateInvoice(ctx, in)
			if err != nil {
				t.Errorf("CreateInvoice concurrent: %v", err)
				return
			}
			numbers[i] = inv.Number
		}(i)
	}
	wg.Wait()
	for _, n := range numbers {
		if n != "ITEST-2026-000003" {
			t.Errorf("numéros = %v, want un seul ITEST-2026-000003", numbers)
			break
		}
	}
}
