//go:build postgres_integration

package bookingevents

import (
	"context"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

const itestMerchant = "itest-bookingevents"

func TestLog_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM booking_events WHERE merchant_id = $1`, itestMerchant)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewRepository(db)
	err := repo.Log(ctx, Event{
		MerchantID: itestMerchant,
		BookingID:  "12345",
		WaitlistID: "wl_test_1",
		EventType:  TypeBookingSeated,
		Source:     SourcePOS,
		Actor:      "user_test",
		Metadata:   map[string]interface{}{"table": 4, "note": "près de la fenêtre"},
	})
	if err != nil {
		t.Fatalf("Log() failed against postgres: %v", err)
	}

	var (
		bookingID int64
		eventType string
		note      string
	)
	err = db.QueryRowContext(ctx, `
		SELECT booking_id, event_type, metadata->>'note'
		FROM booking_events WHERE merchant_id = $1`, itestMerchant,
	).Scan(&bookingID, &eventType, &note)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if bookingID != 12345 || eventType != TypeBookingSeated || note != "près de la fenêtre" {
		t.Fatalf("unexpected row: booking_id=%d event_type=%s note=%s", bookingID, eventType, note)
	}
}
