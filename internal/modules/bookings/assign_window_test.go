package bookings

import (
	"testing"
	"time"
)

// Régression : l'attribution de tables passait Booking.StartDate/EndDate
// (jamais remplis, donc "") au contrôle de conflit, ce que Postgres rejette
// (invalid input syntax for type timestamp) — toute attribution échouait.
func TestBookingUTCWindow_FromTimestamps(t *testing.T) {
	from := time.Date(2026, 11, 28, 19, 0, 0, 0, time.UTC)
	b := &Booking{
		BookingDateFrom: from.Unix(),
		BookingDateTo:   from.Add(90 * time.Minute).Unix(),
	}
	gotFrom, gotTo := bookingUTCWindow(b)
	if gotFrom != "2026-11-28 19:00:00" || gotTo != "2026-11-28 20:30:00" {
		t.Fatalf("bookingUTCWindow() = (%s, %s)", gotFrom, gotTo)
	}
}

func TestBookingUTCWindow_MissingEndFallsBackToDefaultDuration(t *testing.T) {
	from := time.Date(2026, 11, 28, 19, 0, 0, 0, time.UTC)
	b := &Booking{BookingDateFrom: from.Unix()}
	b.Merchant.DefaultBookingDuration = 120
	if _, gotTo := bookingUTCWindow(b); gotTo != "2026-11-28 21:00:00" {
		t.Fatalf("end = %s, want 2026-11-28 21:00:00", gotTo)
	}

	b.Merchant.DefaultBookingDuration = 0
	if _, gotTo := bookingUTCWindow(b); gotTo != "2026-11-28 20:30:00" {
		t.Fatalf("end = %s, want 2026-11-28 20:30:00 (90 min)", gotTo)
	}
}
