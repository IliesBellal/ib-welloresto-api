package bookings

import (
	"context"
	"database/sql"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/zap"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
	"welloresto-api/internal/modules/bookingcore"
)

// bookingFetchColumns reflète l'ordre exact des colonnes scannées par
// bookings_fetcher.go (FetchAndBuildBookings), pour rejouer le rechargement
// post-insert avec sqlmock.
var bookingFetchColumns = []string{
	"booking_id", "order_id", "booking_number", "status", "party_size",
	"customer_id", "sequence_number",
	"customer_name", "customer_tel", "customer_email", "comment",
	"booking_date_from", "booking_date_to", "creation_date",
	"customer_nb_orders", "customer_nb_bookings", "code",
	"business_name", "address", "timezone",
	"default_booking_duration", "logo_url", "created_by",
}

// runStaffCreateBooking rejoue le chemin complet emprunté par
// POST /bookings/create (svc.CreateBooking) : lookup fuseau horaire marchand,
// upsert client par téléphone, génération du numéro, INSERT INTO bookings,
// puis rechargement (GetBookingByID) — c'est-à-dire exactement ce que le
// handler renvoie tel quel dans la réponse JSON. wantInsertedStatus fixe la
// valeur attendue du paramètre status lié à l'INSERT ; le test échoue via
// ExpectationsWereMet si le code produit autre chose.
func runStaffCreateBooking(t *testing.T, requestStatus, wantInsertedStatus string) *Booking {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewBookingsRepository(db, zap.NewNop())
	svc := NewBookingsService(repo, db, nil, nil, nil, nil, nil, zap.NewNop())
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{MerchantID: "m_1", UserID: "staff_1"})

	custName := "Jean Dupont"
	custTel := "0612345678"
	req := &BookingObjectRequest{
		Customer: models.Customer{CustomerName: &custName, CustomerTel: &custTel},
		Booking: Booking{
			Status:    requestStatus,
			PartySize: 2,
			StartDate: "2026-07-10 19:00:00",
			EndDate:   "2026-07-10 21:00:00",
		},
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT timezone").
		WillReturnRows(sqlmock.NewRows([]string{"timezone"}).AddRow("UTC"))
	mock.ExpectQuery("SELECT customer_id").
		WillReturnRows(sqlmock.NewRows([]string{"customer_id", "customer_first_name", "customer_last_name", "customer_email", "customer_tel"}).
			AddRow("42", "Jean", "Dupont", "", "0612345678"))
	mock.ExpectExec("UPDATE customer").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT booking_id FROM bookings").WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("INSERT INTO bookings").
		WithArgs(
			sqlmock.AnyArg(), // booking_number généré aléatoirement
			wantInsertedStatus,
			"staff",
			"m_1",
			2,
			"42",
			nil,
			"2026-07-10 19:00:00",
			"2026-07-10 21:00:00",
			120,
			"staff_1",
		).
		WillReturnResult(sqlmock.NewResult(99, 1))

	fetchRow := sqlmock.NewRows(bookingFetchColumns).AddRow(
		"99", nil, "ABC123", wantInsertedStatus, 2,
		"42", 1,
		"Jean Dupont", "0612345678", "", nil,
		time.Date(2026, 7, 10, 19, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 10, 21, 0, 0, 0, time.UTC),
		time.Now(),
		0, 0, "resto-1",
		"Resto Test", "1 rue A", "UTC",
		90, "", "staff_1",
	)
	mock.ExpectQuery("SELECT").WillReturnRows(fetchRow)
	mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows([]string{"booking_id", "location_id", "location_name", "location_desc"}))
	mock.ExpectCommit()

	booking, err := svc.CreateBooking(ctx, req)
	if err != nil {
		t.Fatalf("CreateBooking() error = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations (statut INSERT attendu %q): %v", wantInsertedStatus, err)
	}

	return booking
}

func TestCreateBooking_MissingStatus_DefaultsToConfirmed(t *testing.T) {
	booking := runStaffCreateBooking(t, "", bookingcore.StatusConfirmed)

	if booking.Status != bookingcore.StatusConfirmed {
		t.Fatalf("booking.Status = %q, want %q (réponse renvoyée par le endpoint)", booking.Status, bookingcore.StatusConfirmed)
	}
}

func TestCreateBooking_ExplicitStatus_IsNotOverwritten(t *testing.T) {
	booking := runStaffCreateBooking(t, bookingcore.StatusPending, bookingcore.StatusPending)

	if booking.Status != bookingcore.StatusPending {
		t.Fatalf("booking.Status = %q, want %q (un statut explicite ne doit pas être écrasé par le défaut confirmed)", booking.Status, bookingcore.StatusPending)
	}
}
