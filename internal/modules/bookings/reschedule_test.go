package bookings

import (
	"context"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/zap"
)

func TestRescheduleBooking_QueryShapeAndArgs(t *testing.T) {
	db, mock, capture := newCapturingMockDB(t)
	defer db.Close()

	repo := NewBookingsRepository(db, zap.NewNop())

	partySize := 6
	mock.ExpectExec("anything").
		WithArgs("2026-07-10 20:00:00", "2026-07-10 21:30:00", "2026-07-10 20:00:00", "2026-07-10 21:30:00", &partySize, "42", "m_1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.RescheduleBooking(context.Background(), "m_1", "42", "2026-07-10 20:00:00", "2026-07-10 21:30:00", &partySize)
	if err != nil {
		t.Fatalf("RescheduleBooking() error = %v", err)
	}

	for _, fragment := range []string{
		"UPDATE bookings",
		"booking_date_from = ?",
		"booking_date_to = ?",
		"sequence_number = sequence_number + 1",
	} {
		if !strings.Contains(capture.lastSQL, fragment) {
			t.Fatalf("expected query to contain %q, got: %s", fragment, capture.lastSQL)
		}
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCheckCapacityForWindow_RejectsInvalidWindow(t *testing.T) {
	db, _, _ := newCapturingMockDB(t)
	defer db.Close()

	repo := NewBookingsRepository(db, zap.NewNop())

	cases := []struct {
		name     string
		dateFrom string
		dateTo   string
	}{
		{"malformed dateFrom", "not-a-date", "2026-07-10 21:00:00"},
		{"malformed dateTo", "2026-07-10 19:00:00", "not-a-date"},
		{"dateTo equal to dateFrom", "2026-07-10 19:00:00", "2026-07-10 19:00:00"},
		{"dateTo before dateFrom", "2026-07-10 20:00:00", "2026-07-10 19:00:00"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			available, err := repo.CheckCapacityForWindow(context.Background(), "m_1", tc.dateFrom, tc.dateTo, 4, "")
			if tc.name == "dateTo equal to dateFrom" || tc.name == "dateTo before dateFrom" {
				if err != nil {
					t.Fatalf("unexpected error = %v", err)
				}
				if available {
					t.Fatal("expected window to be rejected without hitting the database")
				}
				return
			}
			if err == nil {
				t.Fatal("expected a parse error")
			}
		})
	}
}

// expectAvailabilityQueryChain mocke, dans l'ordre, les 4 requetes chainees
// par CheckCapacityForWindow : merchant params, hours_of_operation,
// existing bookings du jour, duration rules. Les arguments ne sont pas
// verifies ici (couverts par les tests dedies loadHoursOfOperation /
// loadExistingBookings existants) — seul le resultat du calcul de capacite
// est sous test.
func expectAvailabilityQueryChain(mock sqlmock.Sqlmock, existingPartySize int, hasExisting bool) {
	mock.ExpectQuery("anything").WillReturnRows(sqlmock.NewRows([]string{
		"id", "timezone", "default_booking_duration", "slot_interval_minutes",
		"auto_accept_reserve_bookings", "reserve_maximum_party_size", "reserve_minimum_party_size",
		"first_booking_offset_minutes", "last_booking_offset_minutes", "cancelable_by_customer",
		"cancel_booking_limit_offset_hours", "enabled", "overbooking_percent",
		"max_booking_horizon_days", "pending_expiration_hours", "logo_url", "fullName",
	}).AddRow(1, "Europe/Paris", 90, 15, 0, 8, 1, 0, 60, 1, 48, 1, 0, 90, 24, "", "Le Test"))

	mock.ExpectQuery("anything").WillReturnRows(sqlmock.NewRows([]string{
		"id", "hour_from", "hour_to", "booking_capacity", "first_booking_time", "last_booking_time",
	}).AddRow(1, "18:00:00", "23:00:00", 10, nil, nil))

	existingRows := sqlmock.NewRows([]string{"party_size", "booking_date_from", "booking_date_to", "booking_duration", "status"})
	if hasExisting {
		existingRows = existingRows.AddRow(existingPartySize, "2026-07-10 19:00:00", "2026-07-10 20:30:00", 90, "confirmed")
	}
	mock.ExpectQuery("anything").WillReturnRows(existingRows)

	mock.ExpectQuery("anything").WillReturnRows(sqlmock.NewRows([]string{
		"min_party_size", "max_party_size", "duration_minutes", "enabled",
	}))
}

func TestCheckCapacityForWindow_CapacityExceeded(t *testing.T) {
	db, mock, _ := newCapturingMockDB(t)
	defer db.Close()

	repo := NewBookingsRepository(db, zap.NewNop())
	expectAvailabilityQueryChain(mock, 8, true)

	// Fenetre 19:30-21:00, capacite 10, 8 couverts deja occupes sur ce
	// creneau (booking existante 19:00-20:30) : +4 depasse la capacite.
	available, err := repo.CheckCapacityForWindow(context.Background(), "m_1", "2026-07-10 19:30:00", "2026-07-10 21:00:00", 4, "999")
	if err != nil {
		t.Fatalf("CheckCapacityForWindow() error = %v", err)
	}
	if available {
		t.Fatal("expected window to be unavailable (capacity exceeded)")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCheckCapacityForWindow_CapacityOk(t *testing.T) {
	db, mock, _ := newCapturingMockDB(t)
	defer db.Close()

	repo := NewBookingsRepository(db, zap.NewNop())
	expectAvailabilityQueryChain(mock, 8, true)

	// Meme occupation (8/10), mais on ne demande qu'1 couvert supplementaire :
	// 8+1 <= 10, la fenetre reste disponible.
	available, err := repo.CheckCapacityForWindow(context.Background(), "m_1", "2026-07-10 19:30:00", "2026-07-10 21:00:00", 1, "999")
	if err != nil {
		t.Fatalf("CheckCapacityForWindow() error = %v", err)
	}
	if !available {
		t.Fatal("expected window to remain available")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCheckCapacityForWindow_NoCoveringRange(t *testing.T) {
	db, mock, _ := newCapturingMockDB(t)
	defer db.Close()

	repo := NewBookingsRepository(db, zap.NewNop())
	expectAvailabilityQueryChain(mock, 0, false)

	// La fenetre demandee (07:00-08:00) tombe hors du service 18:00-23:00.
	available, err := repo.CheckCapacityForWindow(context.Background(), "m_1", "2026-07-10 07:00:00", "2026-07-10 08:00:00", 2, "")
	if err != nil {
		t.Fatalf("CheckCapacityForWindow() error = %v", err)
	}
	if available {
		t.Fatal("expected no covering hours_of_operation range")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
