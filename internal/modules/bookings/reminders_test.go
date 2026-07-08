package bookings

import (
	"context"
	"strings"
	"testing"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/modules/bookingcomm"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/zap"
)

// mockReminderMailer capture les envois d'email sans effectuer d'appel réel.
type mockReminderMailer struct {
	mailer.Service
	calls int
}

func (m *mockReminderMailer) SendAsync(fromName, fromEmail, to, subject, templateName string, data interface{}) {
	m.calls++
}

func TestListBookingsForReminder_QueryShape(t *testing.T) {
	db, mock, capture := newCapturingMockDB(t)
	defer db.Close()

	repo := NewBookingsRepository(db, zap.NewNop())

	mock.ExpectQuery("anything").
		WithArgs("confirmed", 24).
		WillReturnRows(sqlmock.NewRows([]string{
			"booking_id", "merchant_id", "booking_number", "party_size", "start_date",
			"merchant_name", "merchant_slug", "timezone", "sms_enabled",
			"customer_name", "customer_email", "customer_phone",
		}).AddRow("1", "m_1", "AB12CD", 4, "2026-07-10 20:00:00", "Le Bistrot", "le-bistrot", "Europe/Paris", 1, "Jean", "jean@example.com", "+33612345678"))

	rows, err := repo.ListBookingsForReminder(context.Background(), 24)
	if err != nil {
		t.Fatalf("ListBookingsForReminder() error = %v", err)
	}
	if len(rows) != 1 || rows[0].BookingNumber != "AB12CD" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
	if !rows[0].SMSEnabled {
		t.Fatalf("expected SMSEnabled=true")
	}

	// La déduplication repose sur reminder_sent_at IS NULL, et le statut
	// filtré doit être confirmed (pas pending) — cf. brief Bloc 4.
	for _, fragment := range []string{
		"reminder_sent_at IS NULL",
		"b.status = ?",
		"INTERVAL ? HOUR",
	} {
		if !strings.Contains(capture.lastSQL, fragment) {
			t.Fatalf("expected query to contain %q, got: %s", fragment, capture.lastSQL)
		}
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestMarkReminderSent_UpdatesColumn(t *testing.T) {
	db, mock, capture := newCapturingMockDB(t)
	defer db.Close()

	repo := NewBookingsRepository(db, zap.NewNop())

	mock.ExpectExec("anything").WithArgs("1").WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.MarkReminderSent(context.Background(), "1"); err != nil {
		t.Fatalf("MarkReminderSent() error = %v", err)
	}
	if !strings.Contains(capture.lastSQL, "reminder_sent_at = UTC_TIMESTAMP()") {
		t.Fatalf("expected UPDATE to set reminder_sent_at, got: %s", capture.lastSQL)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestSendBookingReminders_SendsMarksAndSkipsAlreadySent(t *testing.T) {
	db, mock, _ := newCapturingMockDB(t)
	defer db.Close()

	repo := NewBookingsRepository(db, zap.NewNop())
	mail := &mockReminderMailer{}
	comm := bookingcomm.New(mail, nil, "https://rsv.welloresto.fr", nil)
	svc := NewBookingsService(repo, db, nil, nil, nil, nil, comm, zap.NewNop())

	// 1er appel : une réservation à rappeler -> email envoyé + reminder_sent_at posé.
	mock.ExpectQuery("anything").
		WithArgs("confirmed", 24).
		WillReturnRows(sqlmock.NewRows([]string{
			"booking_id", "merchant_id", "booking_number", "party_size", "start_date",
			"merchant_name", "merchant_slug", "timezone", "sms_enabled",
			"customer_name", "customer_email", "customer_phone",
		}).AddRow("1", "m_1", "AB12CD", 4, "2026-07-10 20:00:00", "Le Bistrot", "le-bistrot", "Europe/Paris", 0, "Jean", "jean@example.com", "+33612345678"))
	mock.ExpectExec("anything").WithArgs("1").WillReturnResult(sqlmock.NewResult(0, 1))

	sent, err := svc.SendBookingReminders(context.Background())
	if err != nil {
		t.Fatalf("SendBookingReminders() error = %v", err)
	}
	if sent != 1 {
		t.Fatalf("expected 1 reminder sent, got %d", sent)
	}
	if mail.calls != 1 {
		t.Fatalf("expected 1 email sent, got %d", mail.calls)
	}

	// 2e appel : la réservation a déjà reminder_sent_at renseigné -> la requête
	// (filtrée sur reminder_sent_at IS NULL) ne la retourne plus, donc aucun
	// nouvel envoi ni marquage.
	mock.ExpectQuery("anything").
		WithArgs("confirmed", 24).
		WillReturnRows(sqlmock.NewRows([]string{
			"booking_id", "merchant_id", "booking_number", "party_size", "start_date",
			"merchant_name", "merchant_slug", "timezone", "sms_enabled",
			"customer_name", "customer_email", "customer_phone",
		}))

	sent, err = svc.SendBookingReminders(context.Background())
	if err != nil {
		t.Fatalf("SendBookingReminders() second call error = %v", err)
	}
	if sent != 0 {
		t.Fatalf("expected 0 reminders on second call (already sent), got %d", sent)
	}
	if mail.calls != 1 {
		t.Fatalf("expected still 1 email sent total, got %d", mail.calls)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
