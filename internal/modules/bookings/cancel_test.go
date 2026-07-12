package bookings

import (
	"context"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/zap"

	"welloresto-api/internal/modules/bookingcore"
)

func TestCancelBooking_QueryShapeAndArgs(t *testing.T) {
	db, mock, capture := newCapturingMockDB(t)
	defer db.Close()

	repo := NewBookingsRepository(db, zap.NewNop())

	reasonID := "dr_1"
	mock.ExpectExec("anything").
		WithArgs(bookingcore.StatusCancelled, "u_1", reasonID, "42", "m_1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.CancelBooking(context.Background(), "m_1", "42", "u_1", &CancelBookingRequest{DeletionReasonID: &reasonID})
	if err != nil {
		t.Fatalf("CancelBooking() error = %v", err)
	}

	for _, fragment := range []string{
		"UPDATE bookings",
		"SET status = ?, cancelled_by = ?, deletion_reason_id = ?",
	} {
		if !strings.Contains(capture.lastSQL, fragment) {
			t.Fatalf("expected query to contain %q, got: %s", fragment, capture.lastSQL)
		}
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCancelBooking_NilReasonIsNullable(t *testing.T) {
	db, mock, _ := newCapturingMockDB(t)
	defer db.Close()

	repo := NewBookingsRepository(db, zap.NewNop())

	mock.ExpectExec("anything").
		WithArgs(bookingcore.StatusCancelled, "u_1", nil, "42", "m_1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.CancelBooking(context.Background(), "m_1", "42", "u_1", &CancelBookingRequest{}); err != nil {
		t.Fatalf("CancelBooking() error = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// TestCancelBooking_RejectsPendingTransition verifie que la transition
// pending -> cancelled reste rejetee par la machine a etats partagee (une
// demande pending se traite via deny, pas via cancel — addendum §7.9). La
// couverture exhaustive de la machine a etats vit dans
// bookingcore.TestCanTransition ; ce test documente au niveau du module
// bookings que CancelBooking (service.go) s'appuie sur cette meme garde
// avant toute ecriture.
func TestCancelBooking_RejectsPendingTransition(t *testing.T) {
	if err := bookingcore.CanTransition(bookingcore.StatusPending, bookingcore.StatusCancelled); err == nil {
		t.Fatal("expected pending -> cancelled to be rejected by the shared status machine")
	}
}
