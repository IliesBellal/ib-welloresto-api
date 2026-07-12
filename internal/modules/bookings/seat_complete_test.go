package bookings

import (
	"testing"

	"welloresto-api/internal/modules/bookingcore"
)

// TestSeatBooking_TransitionGuards documente, au niveau du module bookings,
// les gardes de transition exploitees par SeatBooking/CompleteBooking. La
// couverture exhaustive de la machine a etats vit dans
// bookingcore.TestCanTransition ; ce test verifie les cas specifiques a ces
// deux actions (notamment pending -> seated, qui doit passer par accept
// d'abord).
func TestSeatBooking_TransitionGuards(t *testing.T) {
	if err := bookingcore.CanTransition(bookingcore.StatusPending, bookingcore.StatusSeated); err == nil {
		t.Fatal("expected pending -> seated to be rejected (accept the booking first)")
	}
	if err := bookingcore.CanTransition(bookingcore.StatusConfirmed, bookingcore.StatusSeated); err != nil {
		t.Fatalf("expected confirmed -> seated to be allowed, got %v", err)
	}
	if err := bookingcore.CanTransition(bookingcore.StatusCancelled, bookingcore.StatusSeated); err == nil {
		t.Fatal("expected cancelled -> seated to be rejected (terminal state)")
	}
}

func TestCompleteBooking_TransitionGuards(t *testing.T) {
	if err := bookingcore.CanTransition(bookingcore.StatusConfirmed, bookingcore.StatusCompleted); err == nil {
		t.Fatal("expected confirmed -> completed to be rejected (seat the booking first)")
	}
	if err := bookingcore.CanTransition(bookingcore.StatusSeated, bookingcore.StatusCompleted); err != nil {
		t.Fatalf("expected seated -> completed to be allowed, got %v", err)
	}
}
