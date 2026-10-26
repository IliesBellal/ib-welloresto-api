package kiosk

import (
	"context"
	"errors"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/models"
)

const testSession = "7d3f2a64-5b1e-4c8a-9f0d-2e6b8c4a1f35"

func int64p(v int64) *int64 { return &v }

func TestValidKioskEvents_KeepsWellFormedOnly(t *testing.T) {
	rows := validKioskEvents([]KioskEvent{
		{Type: "unknown", SessionID: testSession},
		{Type: EventSessionStart, SessionID: "pas-un-uuid"},
		{Type: EventProductView, SessionID: testSession},                              // produit manquant
		{Type: EventPaymentFailed, SessionID: testSession},                            // raison manquante
		{Type: EventFulfillmentChosen, SessionID: testSession, Detail: "IN avec espace"}, // détail invalide
		{Type: EventSessionStart, SessionID: testSession, ElapsedMs: int64p(0)},
		{Type: EventAddToCart, SessionID: testSession, ProductID: " 42 "},
		{Type: EventOrderCreated, SessionID: testSession, ElapsedMs: int64p(95_000)},
	})
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3: %+v", len(rows), rows)
	}
	if rows[1].ProductID != "42" {
		t.Errorf("product id = %q, want trimmed 42", rows[1].ProductID)
	}
	if rows[2].ElapsedMs == nil || *rows[2].ElapsedMs != 95_000 {
		t.Errorf("elapsed = %v, want 95000", rows[2].ElapsedMs)
	}
}

func TestValidKioskEvents_OutOfRangeValuesBecomeNull(t *testing.T) {
	rows := validKioskEvents([]KioskEvent{
		{Type: EventOrderCreated, SessionID: testSession, ElapsedMs: int64p(-1)},
		{Type: EventOrderCreated, SessionID: testSession, ElapsedMs: int64p(maxElapsedMs + 1)},
		{Type: EventSessionAbandoned, SessionID: testSession, Detail: "inactivity", AmountCents: int64p(maxAmountCents + 1)},
		// Un montant n'a de sens que pour un abandon.
		{Type: EventSummaryView, SessionID: testSession, AmountCents: int64p(1200)},
		// Un produit n'est gardé que sur les événements produit.
		{Type: EventSessionStart, SessionID: testSession, ProductID: "42"},
	})
	if len(rows) != 5 {
		t.Fatalf("rows = %d, want 5", len(rows))
	}
	for i, row := range rows {
		if row.ElapsedMs != nil || row.AmountCents != nil || row.ProductID != "" {
			t.Errorf("row %d = %+v, want elapsed, amount and product empty", i, row)
		}
	}
}

func TestRecordEvents_DisabledStoresNothing(t *testing.T) {
	svc, mock := newReclaimTestService(t)
	svc.cfg.AudienceEnabled = false

	err := svc.RecordEvents(context.Background(), &AuthenticatedKiosk{KioskID: "kiosk-1", MerchantID: "merch-1"},
		KioskEventsRequest{Events: []KioskEvent{{Type: EventSessionStart, SessionID: testSession}}})
	if err != nil {
		t.Fatalf("RecordEvents: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRecordEvents_RejectsOversizedBatch(t *testing.T) {
	svc, _ := newReclaimTestService(t)
	svc.cfg.AudienceEnabled = true

	events := make([]KioskEvent, maxKioskEventsPerBatch+1)
	err := svc.RecordEvents(context.Background(), &AuthenticatedKiosk{KioskID: "kiosk-1", MerchantID: "merch-1"}, KioskEventsRequest{Events: events})
	if !errors.Is(err, models.ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}

// La borne et l'établissement viennent du jeton, jamais du corps.
func TestRecordEvents_InsertsWithAuthenticatedKiosk(t *testing.T) {
	svc, mock := newReclaimTestService(t)
	svc.cfg.AudienceEnabled = true

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO kiosk_events (event_type, merchant_id, kiosk_id, session_id, elapsed_ms, product_id, amount_cents, detail, step) VALUES`)).
		WithArgs(
			EventSessionStart, "merch-1", "kiosk-1", testSession, int64p(0), nil, nil, nil, nil,
			EventSessionAbandoned, "merch-1", "kiosk-1", testSession, int64p(64_000), nil, int64p(1890), "inactivity", "cart",
		).
		WillReturnResult(sqlmock.NewResult(0, 2))

	err := svc.RecordEvents(context.Background(), &AuthenticatedKiosk{KioskID: "kiosk-1", MerchantID: "merch-1"}, KioskEventsRequest{Events: []KioskEvent{
		{Type: EventSessionStart, SessionID: testSession, ElapsedMs: int64p(0)},
		{Type: "unknown", SessionID: testSession},
		{Type: EventSessionAbandoned, SessionID: testSession, ElapsedMs: int64p(64_000), AmountCents: int64p(1890), Detail: "inactivity", Step: "cart"},
	}})
	if err != nil {
		t.Fatalf("RecordEvents: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
