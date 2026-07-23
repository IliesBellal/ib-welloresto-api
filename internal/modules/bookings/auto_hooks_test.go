package bookings

import (
	"context"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/zap"

	"welloresto-api/internal/modules/bookingcore"
)

func TestFindConfirmedBookingForAutoSeat_NoLocations_NoQuery(t *testing.T) {
	db, mock, _ := newCapturingMockDB(t)
	defer db.Close()

	repo := NewBookingsRepository(db, zap.NewNop())

	bookingID, err := repo.FindConfirmedBookingForAutoSeat(context.Background(), "m_1", nil)
	if err != nil {
		t.Fatalf("FindConfirmedBookingForAutoSeat() error = %v", err)
	}
	if bookingID != "" {
		t.Fatalf("expected empty bookingID without locations, got %q", bookingID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("no query expected: %v", err)
	}
}

func TestFindConfirmedBookingForAutoSeat_MatchQueryShape(t *testing.T) {
	db, mock, capture := newCapturingMockDB(t)
	defer db.Close()

	repo := NewBookingsRepository(db, zap.NewNop())

	mock.ExpectQuery("anything").
		WithArgs("12", "14", "m_1").
		WillReturnRows(sqlmock.NewRows([]string{"booking_id"}).AddRow("42"))

	bookingID, err := repo.FindConfirmedBookingForAutoSeat(context.Background(), "m_1", []string{"12", "14"})
	if err != nil {
		t.Fatalf("FindConfirmedBookingForAutoSeat() error = %v", err)
	}
	if bookingID != "42" {
		t.Fatalf("expected booking_id 42, got %q", bookingID)
	}

	for _, fragment := range []string{
		"status = 'confirmed'",
		"INTERVAL '30' MINUTE",
		"ORDER BY ABS(TIMESTAMPDIFF(SECOND",
		"LIMIT 1",
	} {
		if !strings.Contains(capture.lastSQL, fragment) {
			t.Fatalf("expected query to contain %q, got: %s", fragment, capture.lastSQL)
		}
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFindConfirmedBookingForAutoSeat_NoMatch(t *testing.T) {
	db, mock, _ := newCapturingMockDB(t)
	defer db.Close()

	repo := NewBookingsRepository(db, zap.NewNop())

	mock.ExpectQuery("anything").
		WithArgs("12", "m_1").
		WillReturnRows(sqlmock.NewRows([]string{"booking_id"}))

	bookingID, err := repo.FindConfirmedBookingForAutoSeat(context.Background(), "m_1", []string{"12"})
	if err != nil {
		t.Fatalf("FindConfirmedBookingForAutoSeat() error = %v", err)
	}
	if bookingID != "" {
		t.Fatalf("expected no match, got %q", bookingID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAutoSeatForOrder_NoMatch_NoWrite(t *testing.T) {
	db, mock, _ := newCapturingMockDB(t)
	defer db.Close()

	repo := NewBookingsRepository(db, zap.NewNop())
	svc := NewBookingsService(repo, db, nil, nil, nil, nil, nil, zap.NewNop())

	mock.ExpectQuery("anything").
		WithArgs("12", "m_1").
		WillReturnRows(sqlmock.NewRows([]string{"booking_id"}))

	if err := svc.AutoSeatForOrder(context.Background(), "m_1", "order_1", []string{"12"}); err != nil {
		t.Fatalf("AutoSeatForOrder() error = %v", err)
	}

	// Aucune ecriture attendue : le mock echouerait sur un ExecContext non
	// planifie si SetBookingSeatedWithOrder etait appele a tort.
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations (no write expected on no-match): %v", err)
	}
}

func TestAutoSeatForOrder_Match_SeatsAndLinksOrder(t *testing.T) {
	db, mock, capture := newCapturingMockDB(t)
	defer db.Close()

	repo := NewBookingsRepository(db, zap.NewNop())
	svc := NewBookingsService(repo, db, nil, nil, nil, nil, nil, zap.NewNop())

	mock.ExpectQuery("anything").
		WithArgs("12", "m_1").
		WillReturnRows(sqlmock.NewRows([]string{"booking_id"}).AddRow("42"))
	mock.ExpectExec("anything").
		WithArgs(bookingcore.StatusSeated, "order_1", "42", "m_1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := svc.AutoSeatForOrder(context.Background(), "m_1", "order_1", []string{"12"}); err != nil {
		t.Fatalf("AutoSeatForOrder() error = %v", err)
	}

	if !strings.Contains(capture.lastSQL, "order_id = ?") {
		t.Fatalf("expected update to set order_id, got: %s", capture.lastSQL)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAutoCompleteForOrder_NoMatch_NoWrite(t *testing.T) {
	db, mock, _ := newCapturingMockDB(t)
	defer db.Close()

	repo := NewBookingsRepository(db, zap.NewNop())
	svc := NewBookingsService(repo, db, nil, nil, nil, nil, nil, zap.NewNop())

	mock.ExpectQuery("anything").
		WithArgs("m_1", "order_1", bookingcore.StatusSeated).
		WillReturnRows(sqlmock.NewRows([]string{"booking_id"}))

	if err := svc.AutoCompleteForOrder(context.Background(), "m_1", "order_1"); err != nil {
		t.Fatalf("AutoCompleteForOrder() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations (no write expected on no-match): %v", err)
	}
}

func TestAutoCompleteForOrder_Match_Completes(t *testing.T) {
	db, mock, _ := newCapturingMockDB(t)
	defer db.Close()

	repo := NewBookingsRepository(db, zap.NewNop())
	svc := NewBookingsService(repo, db, nil, nil, nil, nil, nil, zap.NewNop())

	mock.ExpectQuery("anything").
		WithArgs("m_1", "order_1", bookingcore.StatusSeated).
		WillReturnRows(sqlmock.NewRows([]string{"booking_id"}).AddRow("42"))
	mock.ExpectExec("anything").
		WithArgs(bookingcore.StatusCompleted, "42", "m_1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := svc.AutoCompleteForOrder(context.Background(), "m_1", "order_1"); err != nil {
		t.Fatalf("AutoCompleteForOrder() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
