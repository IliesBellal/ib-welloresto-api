package bookingcore

import (
	"context"
	"database/sql"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/modules/customers"
)

func TestCreateBooking_ReusesExistingCustomerFoundByPhone(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	customerRepo := customers.NewCustomerRepository(db)
	start := time.Date(2026, 7, 11, 19, 0, 0, 0, time.UTC)
	end := start.Add(90 * time.Minute)
	customerName := "Jean Dupont"
	firstName := "Jean"
	lastName := "Dupont"
	phone := "06 12 34 56 78"

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT customer_id, customer_first_name, customer_last_name, customer_email, customer_tel").
		WithArgs("+33612345678", "merchant_1").
		WillReturnRows(sqlmock.NewRows([]string{"customer_id", "customer_first_name", "customer_last_name", "customer_email", "customer_tel"}).
			AddRow("42", "Jean", "Dupont", "client@example.fr", "+33612345678"))
	mock.ExpectExec("UPDATE customer").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT booking_id FROM bookings WHERE merchant_id = \\? AND booking_number = \\?").
		WithArgs("merchant_1", sqlmock.AnyArg()).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("INSERT INTO bookings").
		WithArgs(
			sqlmock.AnyArg(),
			"confirmed",
			"web",
			"merchant_1",
			2,
			"42",
			nil,
			"2026-07-11 19:00:00",
			"2026-07-11 20:30:00",
			90,
			"WR_ONLINE_BOOKING",
		).
		WillReturnResult(sqlmock.NewResult(99, 1))
	mock.ExpectCommit()

	bookingID, bookingNumber, err := CreateBooking(context.Background(), db, customerRepo, CreateBookingParams{
		MerchantID: "merchant_1",
		Source:     "web",
		CreatedBy:  "WR_ONLINE_BOOKING",
		Status:     "confirmed",
		PartySize:  2,
		StartLocal: start,
		EndLocal:   end,
		Customer: CustomerUpsert{
			CustomerName:      &customerName,
			CustomerFirstName: &firstName,
			CustomerLastName:  &lastName,
			CustomerTel:       &phone,
		},
	})
	if err != nil {
		t.Fatalf("CreateBooking() error = %v", err)
	}
	if bookingID != "99" {
		t.Fatalf("CreateBooking() bookingID = %q, want 99", bookingID)
	}
	if bookingNumber == "" {
		t.Fatal("CreateBooking() bookingNumber is empty")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

// TestCreateBooking_RejectsInvalidStatus vérifie la défense en profondeur :
// un appelant qui atteint bookingcore.CreateBooking avec un statut vide ou
// non normalisé (legacy, faute de frappe, valeur oubliée) doit être rejeté
// avant toute écriture SQL, plutôt que d'insérer silencieusement une valeur
// arbitraire (cf. status="" historique du flux staff).
func TestCreateBooking_RejectsInvalidStatus(t *testing.T) {
	cases := []string{"", "PENDING_APPROVAL", "not_a_status", "Confirmed"}

	for _, status := range cases {
		t.Run("status_"+status, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New() error = %v", err)
			}
			defer db.Close()

			customerRepo := customers.NewCustomerRepository(db)
			start := time.Date(2026, 7, 11, 19, 0, 0, 0, time.UTC)
			end := start.Add(90 * time.Minute)

			_, _, err = CreateBooking(context.Background(), db, customerRepo, CreateBookingParams{
				MerchantID: "merchant_1",
				Source:     "staff",
				CreatedBy:  "itest",
				Status:     status,
				PartySize:  2,
				StartLocal: start,
				EndLocal:   end,
			})
			if err == nil {
				t.Fatalf("CreateBooking() with status %q: expected error, got nil", status)
			}

			// Aucune requête SQL ne doit avoir été tentée : le rejet intervient
			// avant l'ouverture de la transaction.
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("expected no SQL calls for invalid status %q: %v", status, err)
			}
		})
	}
}
