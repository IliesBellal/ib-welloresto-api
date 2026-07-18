//go:build postgres_integration

package bookingcore

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

func TestInsertBookingAndNumber_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "999904"
	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM bookings WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	number, err := generateUniqueBookingNumber(ctx, db, merchantID)
	if err != nil {
		t.Fatalf("generateUniqueBookingNumber failed against postgres: %v", err)
	}
	if len(number) != 6 {
		t.Fatalf("expected 6-char booking number, got %q", number)
	}

	comment := "itest booking"
	start := time.Date(2026, 7, 17, 19, 0, 0, 0, time.UTC)
	end := start.Add(90 * time.Minute)
	p := CreateBookingParams{
		MerchantID: merchantID,
		Source:     "staff",
		CreatedBy:  "itest",
		Status:     "confirmed",
		PartySize:  4,
		Comment:    &comment,
	}

	id, err := insertBooking(ctx, db, p, "424242", number,
		start.Format("2006-01-02 15:04:05"), end.Format("2006-01-02 15:04:05"), 90)
	if err != nil {
		t.Fatalf("insertBooking failed against postgres: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected auto-generated booking_id > 0, got %d", id)
	}

	// La collision de numéro doit maintenant être détectée : le même numéro
	// existe, generateUniqueBookingNumber doit en tirer un autre.
	number2, err := generateUniqueBookingNumber(ctx, db, merchantID)
	if err != nil {
		t.Fatalf("generateUniqueBookingNumber (2nd) failed: %v", err)
	}
	if number2 == number {
		t.Fatalf("expected a different booking number on collision, got %q twice", number)
	}

	var gotStatus string
	var creation time.Time
	err = db.QueryRowContext(ctx,
		`SELECT status, creation_date FROM bookings WHERE booking_id = $1`, id).
		Scan(&gotStatus, &creation)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if gotStatus != "confirmed" {
		t.Fatalf("unexpected status %q", gotStatus)
	}
	if time.Since(creation) > 2*time.Minute || time.Since(creation) < -2*time.Minute {
		t.Fatalf("creation_date not close to now(): %v", creation)
	}
}
