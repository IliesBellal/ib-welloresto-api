package bookings

import (
	"context"
	"database/sql"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/zap"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/modules/auth"
	"welloresto-api/internal/modules/bookingcomm"
	"welloresto-api/internal/modules/bookingcore"
)

// setupBookingFetch enfile les deux requêtes d'un GetBookingByID
// (FetchAndBuildBookings : booking principal + tables) sur le mock
// capturant, avec le statut demandé et un email/téléphone client renseignés
// (nécessaires pour que notifyBookingMessage ne s'arrête pas faute de
// contact).
func setupBookingFetch(mock sqlmock.Sqlmock, status string) {
	mainRow := sqlmock.NewRows(bookingFetchColumns).AddRow(
		"99", nil, "ABC123", status, 2,
		"42", 1,
		"Jean Dupont", "0612345678", "jean@example.com", nil,
		time.Date(2026, 7, 10, 19, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 10, 21, 0, 0, 0, time.UTC),
		time.Now(),
		0, 0, "resto-1",
		"Resto Test", "1 rue A", "UTC",
		90, "", "staff_1",
	)
	mock.ExpectQuery("anything").WillReturnRows(mainRow)
	mock.ExpectQuery("anything").WillReturnRows(sqlmock.NewRows([]string{"booking_id", "location_id", "location_name", "location_desc"}))
}

// newNotifyTestService instancie un BookingsService avec un mailer factice
// (compte les envois) et sans événements/notifier — comme les tests
// existants (cf. TestCreateBooking_TableConflict_RollsBackWith409Sentinel).
func newNotifyTestService(db *sql.DB) (*BookingsService, *mockReminderMailer) {
	repo := NewBookingsRepository(db, zap.NewNop())
	mail := &mockReminderMailer{}
	comm := bookingcomm.New(mail, nil, "https://rsv.welloresto.fr", nil, nil)
	return NewBookingsService(repo, db, nil, nil, nil, nil, comm, zap.NewNop()), mail
}

func notifyTestCtx() context.Context {
	return middleware.WithUser(context.Background(), &auth.UserLoginRow{MerchantID: "m_1", UserID: "staff_1"})
}

func TestAcceptBooking_FromPending_SendsConfirmationOnce(t *testing.T) {
	db, mock, _ := newCapturingMockDB(t)
	defer db.Close()

	svc, mail := newNotifyTestService(db)
	ctx := notifyTestCtx()

	setupBookingFetch(mock, bookingcore.StatusPending)
	mock.ExpectExec("anything").WillReturnResult(sqlmock.NewResult(0, 1))
	setupBookingFetch(mock, bookingcore.StatusConfirmed)
	// notifyBookingMessage : GetBookingSettings échoue (ErrNoRows) -> settings
	// nil, mais l'email part quand même (il ne dépend pas des settings).
	mock.ExpectQuery("anything").WillReturnError(sql.ErrNoRows)

	if _, err := svc.AcceptBooking(ctx, "tok", "99"); err != nil {
		t.Fatalf("AcceptBooking() error = %v", err)
	}
	if mail.calls != 1 {
		t.Fatalf("expected 1 confirmation email on pending->confirmed, got %d", mail.calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAcceptBooking_AlreadyConfirmed_NoOpDoesNotResendConfirmation(t *testing.T) {
	db, mock, _ := newCapturingMockDB(t)
	defer db.Close()

	svc, mail := newNotifyTestService(db)
	ctx := notifyTestCtx()

	setupBookingFetch(mock, bookingcore.StatusConfirmed)
	mock.ExpectExec("anything").WillReturnResult(sqlmock.NewResult(0, 1))
	setupBookingFetch(mock, bookingcore.StatusConfirmed)
	// Pas de requête GetBookingSettings attendue : le garde-fou coupe avant
	// notifyBookingMessage sur le no-op confirmed -> confirmed.

	if _, err := svc.AcceptBooking(ctx, "tok", "99"); err != nil {
		t.Fatalf("AcceptBooking() sur no-op confirmed->confirmed error = %v, want succès silencieux", err)
	}
	if mail.calls != 0 {
		t.Fatalf("expected 0 confirmation email on confirmed->confirmed no-op, got %d", mail.calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDenyBooking_FromPending_SendsCancellationOnce(t *testing.T) {
	db, mock, _ := newCapturingMockDB(t)
	defer db.Close()

	svc, mail := newNotifyTestService(db)
	ctx := notifyTestCtx()

	setupBookingFetch(mock, bookingcore.StatusPending)
	mock.ExpectExec("anything").WillReturnResult(sqlmock.NewResult(0, 1))
	setupBookingFetch(mock, bookingcore.StatusDenied)
	mock.ExpectQuery("anything").WillReturnError(sql.ErrNoRows)

	if _, err := svc.DenyBooking(ctx, "tok", "99", nil); err != nil {
		t.Fatalf("DenyBooking() error = %v", err)
	}
	if mail.calls != 1 {
		t.Fatalf("expected 1 cancellation email on pending->denied, got %d", mail.calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDenyBooking_AlreadyDenied_NoOpDoesNotResendCancellation(t *testing.T) {
	db, mock, _ := newCapturingMockDB(t)
	defer db.Close()

	svc, mail := newNotifyTestService(db)
	ctx := notifyTestCtx()

	setupBookingFetch(mock, bookingcore.StatusDenied)
	mock.ExpectExec("anything").WillReturnResult(sqlmock.NewResult(0, 1))
	setupBookingFetch(mock, bookingcore.StatusDenied)

	if _, err := svc.DenyBooking(ctx, "tok", "99", nil); err != nil {
		t.Fatalf("DenyBooking() sur no-op denied->denied error = %v, want succès silencieux", err)
	}
	if mail.calls != 0 {
		t.Fatalf("expected 0 cancellation email on denied->denied no-op, got %d", mail.calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCancelBooking_FromConfirmed_SendsCancellationOnce(t *testing.T) {
	db, mock, _ := newCapturingMockDB(t)
	defer db.Close()

	svc, mail := newNotifyTestService(db)
	ctx := notifyTestCtx()

	setupBookingFetch(mock, bookingcore.StatusConfirmed)
	mock.ExpectExec("anything").WillReturnResult(sqlmock.NewResult(0, 1))
	setupBookingFetch(mock, bookingcore.StatusCancelled)
	mock.ExpectQuery("anything").WillReturnError(sql.ErrNoRows)

	if _, err := svc.CancelBooking(ctx, "tok", "99", nil); err != nil {
		t.Fatalf("CancelBooking() error = %v", err)
	}
	if mail.calls != 1 {
		t.Fatalf("expected 1 cancellation email on confirmed->cancelled, got %d", mail.calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCancelBooking_AlreadyCancelled_NoOpDoesNotResendCancellation(t *testing.T) {
	db, mock, _ := newCapturingMockDB(t)
	defer db.Close()

	svc, mail := newNotifyTestService(db)
	ctx := notifyTestCtx()

	setupBookingFetch(mock, bookingcore.StatusCancelled)
	mock.ExpectExec("anything").WillReturnResult(sqlmock.NewResult(0, 1))
	setupBookingFetch(mock, bookingcore.StatusCancelled)

	if _, err := svc.CancelBooking(ctx, "tok", "99", nil); err != nil {
		t.Fatalf("CancelBooking() sur no-op cancelled->cancelled error = %v, want succès silencieux", err)
	}
	if mail.calls != 0 {
		t.Fatalf("expected 0 cancellation email on cancelled->cancelled no-op, got %d", mail.calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
