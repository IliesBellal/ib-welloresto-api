package bookings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	_ "github.com/go-sql-driver/mysql"
	"go.uber.org/zap"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
	"welloresto-api/internal/utils/dbutils"
)

// captureQueryMatcher accepte n'importe quelle requête mais conserve le dernier
// SQL exécuté pour vérifier la forme de la requête de conflit (même approche
// que customers/repository_test.go).
type captureQueryMatcher struct {
	lastSQL string
}

func (c *captureQueryMatcher) Match(expectedSQL, actualSQL string) error {
	c.lastSQL = actualSQL
	return nil
}

func newCapturingMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *captureQueryMatcher) {
	t.Helper()
	capture := &captureQueryMatcher{}
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(capture.Match)))
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	return db, mock, capture
}

func TestFindConflictingBookings_NoLocations_NoQuery(t *testing.T) {
	db, mock, _ := newCapturingMockDB(t)
	defer db.Close()

	repo := NewBookingsRepository(db, zap.NewNop())

	conflicts, err := repo.FindConflictingBookings(context.Background(), "m_1", nil,
		"2026-07-10 19:00:00", "2026-07-10 21:00:00", "")
	if err != nil {
		t.Fatalf("FindConflictingBookings() error = %v", err)
	}
	if conflicts != nil {
		t.Fatalf("expected nil conflicts without locations, got %v", conflicts)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("no query expected: %v", err)
	}
}

func TestFindConflictingBookings_QueryShapeAndArgs(t *testing.T) {
	db, mock, capture := newCapturingMockDB(t)
	defer db.Close()

	repo := NewBookingsRepository(db, zap.NewNop())

	mock.ExpectQuery("anything").
		WithArgs("12", "14", "m_1", "2026-07-10 21:00:00", "2026-07-10 19:00:00", "0").
		WillReturnRows(sqlmock.NewRows([]string{"booking_id", "location_id"}).AddRow("42", "12"))

	conflicts, err := repo.FindConflictingBookings(context.Background(), "m_1",
		[]string{"12", "14"}, "2026-07-10 19:00:00", "2026-07-10 21:00:00", "")
	if err != nil {
		t.Fatalf("FindConflictingBookings() error = %v", err)
	}

	if len(conflicts) != 1 || conflicts[0].BookingID != "42" || conflicts[0].LocationID != "12" {
		t.Fatalf("unexpected conflicts: %+v", conflicts)
	}

	for _, fragment := range []string{
		"FOR UPDATE",
		"IN (?,?)",
		"'PENDING_APPROVAL','ACCEPTED','ORDER_OPEN'",
		"b.booking_date_from < ?",
		"b.booking_id <> ?",
	} {
		if !strings.Contains(capture.lastSQL, fragment) {
			t.Fatalf("expected query to contain %q, got: %s", fragment, capture.lastSQL)
		}
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestHasRuleOverlap(t *testing.T) {
	rules := []BookingDurationRule{
		{RuleID: "r1", MinPartySize: 1, MaxPartySize: 4, DurationMinutes: 90, Enabled: true},
		{RuleID: "r2", MinPartySize: 5, MaxPartySize: 8, DurationMinutes: 120, Enabled: true},
	}

	if hasRuleOverlap(rules, 3, 6, "") == false {
		t.Fatal("expected overlap for intersecting range")
	}
	if hasRuleOverlap(rules, 9, 12, "") == true {
		t.Fatal("did not expect overlap for disjoint range")
	}
	if hasRuleOverlap(rules, 5, 8, "r2") == true {
		t.Fatal("self exclusion should ignore matching rule id")
	}
}

func TestPutBookingSettings_Validation(t *testing.T) {
	svc := &BookingsService{}
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{MerchantID: "m_1"})

	invalidCases := []PutBookingSettingsRequest{
		{MinBookingNoticeMinutes: -1, MaxBookingHorizonDays: 30, OverbookingPercent: 0, ReserveMinimumPartySize: 1, ReserveMaximumPartySize: 8},
		{MinBookingNoticeMinutes: 0, MaxBookingHorizonDays: 0, OverbookingPercent: 0, ReserveMinimumPartySize: 1, ReserveMaximumPartySize: 8},
		{MinBookingNoticeMinutes: 0, MaxBookingHorizonDays: 30, OverbookingPercent: 101, ReserveMinimumPartySize: 1, ReserveMaximumPartySize: 8},
		{MinBookingNoticeMinutes: 0, MaxBookingHorizonDays: 30, OverbookingPercent: 0, ReserveMinimumPartySize: 9, ReserveMaximumPartySize: 8},
	}

	for _, req := range invalidCases {
		if _, err := svc.PutBookingSettings(ctx, "tok", &req); !errors.Is(err, models.ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput, got %v for req %+v", err, req)
		}
	}
}

func TestPutBookingHours_Validation(t *testing.T) {
	svc := &BookingsService{}
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{MerchantID: "m_1"})

	first := "12:00:00"
	last := "11:00:00"
	capacity := 10
	_, err := svc.PutBookingHours(ctx, "tok", &PutBookingSettingsHoursRequest{
		Hours: []models.POSHoursOfOperationPatch{{
			DayOfWeekFrom:    1,
			DayOfWeekTo:      1,
			HourFrom:         "10:00:00",
			HourTo:           "14:00:00",
			BookingCapacity:  &capacity,
			FirstBookingTime: &first,
			LastBookingTime:  &last,
		}},
	})
	if !errors.Is(err, models.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for invalid hours payload, got %v", err)
	}
}

func TestFindConflictingBookings_ExcludeSelf(t *testing.T) {
	db, mock, _ := newCapturingMockDB(t)
	defer db.Close()

	repo := NewBookingsRepository(db, zap.NewNop())

	// L'ID de réattribution est passé tel quel comme dernier argument.
	mock.ExpectQuery("anything").
		WithArgs("12", "m_1", "2026-07-10 21:00:00", "2026-07-10 19:00:00", "42").
		WillReturnRows(sqlmock.NewRows([]string{"booking_id", "location_id"}))

	conflicts, err := repo.FindConflictingBookings(context.Background(), "m_1",
		[]string{"12"}, "2026-07-10 19:00:00", "2026-07-10 21:00:00", "42")
	if err != nil {
		t.Fatalf("FindConflictingBookings() error = %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflicts, got %+v", conflicts)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateBooking_TableConflict_RollsBackWith409Sentinel(t *testing.T) {
	db, mock, _ := newCapturingMockDB(t)
	defer db.Close()

	repo := NewBookingsRepository(db, zap.NewNop())
	svc := NewBookingsService(repo, db, nil, nil, nil, nil, nil, zap.NewNop())

	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{MerchantID: "m_1"})

	mock.ExpectBegin()
	mock.ExpectQuery("anything").
		WillReturnRows(sqlmock.NewRows([]string{"booking_id", "location_id"}).AddRow("42", "12"))
	mock.ExpectRollback()

	req := &BookingObjectRequest{
		Booking: Booking{
			StartDate: "2026-07-10 19:00:00",
			EndDate:   "2026-07-10 21:00:00",
			PartySize: 4,
			Locations: []BookingLocation{{LocationID: "12"}},
		},
	}

	_, err := svc.CreateBooking(ctx, req)
	if err == nil {
		t.Fatal("expected table_conflict error, got nil")
	}

	var conflictErr *TableConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("expected *TableConflictError, got %T: %v", err, err)
	}
	if len(conflictErr.Conflicts) != 1 || conflictErr.Conflicts[0].BookingID != "42" {
		t.Fatalf("unexpected conflicts payload: %+v", conflictErr.Conflicts)
	}
	if !errors.Is(err, models.ErrTableConflict) {
		t.Fatal("expected error to match models.ErrTableConflict sentinel (mapping 409)")
	}

	// Rollback attendu : aucune écriture (INSERT bookings/booked_location) ne
	// doit avoir été tentée après le conflit.
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations (rollback attendu, pas d'INSERT): %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests d'intégration MySQL réels (chevauchement + concurrence).
// Sautés par défaut : définir BOOKINGS_TEST_MYSQL_DSN, ex.
//   root@tcp(127.0.0.1:33077)/bookings_test?multiStatements=true
// La base est réinitialisée via les migrations 050/051/052.
// ---------------------------------------------------------------------------

func openRealDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("BOOKINGS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("BOOKINGS_TEST_MYSQL_DSN non défini — test MySQL réel sauté")
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(4) // > 1 pour exercer réellement la concurrence

	for _, table := range []string{"booked_location", "bookings", "bookings_settings", "hours_of_operation", "floor_areas", "locations", "floors", "order_location"} {
		if _, err := db.Exec("DROP TABLE IF EXISTS " + table); err != nil {
			t.Fatalf("drop %s: %v", table, err)
		}
	}

	for _, migration := range []string{
		"050_baseline_floorplan.up.sql",
		"051_baseline_bookings.up.sql",
		"052_booked_location_unique.up.sql",
	} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", migration))
		if err != nil {
			t.Fatalf("lecture %s: %v", migration, err)
		}
		if _, err := db.Exec(string(raw)); err != nil {
			t.Fatalf("application %s: %v", migration, err)
		}
	}

	return db
}

func seedBooking(t *testing.T, db *sql.DB, merchantID, status, from, to string, locationIDs ...string) string {
	t.Helper()

	res, err := db.Exec(`
        INSERT INTO bookings (booking_number, status, merchant_id, party_size, booking_date_from, booking_date_to, booking_duration, creation_date)
        VALUES ('TEST01', ?, ?, 4, ?, ?, 120, UTC_TIMESTAMP)`,
		status, merchantID, from, to)
	if err != nil {
		t.Fatalf("seed booking: %v", err)
	}
	id, _ := res.LastInsertId()

	for _, locID := range locationIDs {
		if _, err := db.Exec(`INSERT INTO booked_location (booking_id, location_id) VALUES (?, ?)`, id, locID); err != nil {
			t.Fatalf("seed booked_location: %v", err)
		}
	}

	return fmt.Sprintf("%d", id)
}

func TestFindConflictingBookings_MySQL_OverlapSemantics(t *testing.T) {
	db := openRealDB(t)
	defer db.Close()

	repo := NewBookingsRepository(db, zap.NewNop())
	ctx := context.Background()

	bookingID := seedBooking(t, db, "m_1", "ACCEPTED", "2026-07-10 19:00:00", "2026-07-10 21:00:00", "12")

	cases := []struct {
		name         string
		from, to     string
		exclude      string
		wantConflict bool
	}{
		{"chevauchement partiel", "2026-07-10 20:00:00", "2026-07-10 22:00:00", "", true},
		{"englobant", "2026-07-10 18:00:00", "2026-07-10 22:00:00", "", true},
		{"inclus", "2026-07-10 19:30:00", "2026-07-10 20:30:00", "", true},
		{"disjoint apres", "2026-07-10 21:30:00", "2026-07-10 23:00:00", "", false},
		{"dos a dos (fin = debut)", "2026-07-10 17:00:00", "2026-07-10 19:00:00", "", false},
		{"exclusion self (reattribution)", "2026-07-10 19:00:00", "2026-07-10 21:00:00", bookingID, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conflicts, err := repo.FindConflictingBookings(ctx, "m_1", []string{"12"}, tc.from, tc.to, tc.exclude)
			if err != nil {
				t.Fatalf("FindConflictingBookings: %v", err)
			}
			if got := len(conflicts) > 0; got != tc.wantConflict {
				t.Fatalf("conflit = %v, attendu %v (conflicts: %+v)", got, tc.wantConflict, conflicts)
			}
		})
	}

	// Une table différente sur le même créneau ne conflicte pas.
	conflicts, err := repo.FindConflictingBookings(ctx, "m_1", []string{"99"}, "2026-07-10 19:00:00", "2026-07-10 21:00:00", "")
	if err != nil {
		t.Fatalf("FindConflictingBookings: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("aucun conflit attendu sur une autre table, got %+v", conflicts)
	}

	// Scoping multi-tenant : le même créneau vu par un autre marchand ne
	// remonte pas le conflit (et n'expose pas ses données).
	conflicts, err = repo.FindConflictingBookings(ctx, "m_2", []string{"12"}, "2026-07-10 19:00:00", "2026-07-10 21:00:00", "")
	if err != nil {
		t.Fatalf("FindConflictingBookings: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("aucun conflit attendu pour un autre marchand, got %+v", conflicts)
	}
}

func TestFindConflictingBookings_MySQL_Concurrency(t *testing.T) {
	db := openRealDB(t)
	defer db.Close()

	repo := NewBookingsRepository(db, zap.NewNop())

	// Deux "créations" simultanées sur la même table 33, même créneau :
	// chacune rejoue la séquence du service (check conflit puis INSERT, dans
	// une transaction). Résultat attendu : jamais deux réservations
	// chevauchantes persistées (l'une passe, l'autre voit le conflit ou est
	// victime du deadlock InnoDB provoqué par les gap locks du FOR UPDATE).
	attempt := func() error {
		return dbutils.RunInTx(context.Background(), db, func(txCtx context.Context) error {
			conflicts, err := repo.FindConflictingBookings(txCtx, "m_1", []string{"33"},
				"2026-07-11 19:00:00", "2026-07-11 21:00:00", "")
			if err != nil {
				return err
			}
			if len(conflicts) > 0 {
				return &TableConflictError{Conflicts: conflicts}
			}

			tx := dbutils.GetDB(txCtx, db)
			res, err := tx.ExecContext(txCtx, `
                INSERT INTO bookings (booking_number, status, merchant_id, party_size, booking_date_from, booking_date_to, booking_duration, creation_date)
                VALUES ('CONC01', 'ACCEPTED', 'm_1', 4, '2026-07-11 19:00:00', '2026-07-11 21:00:00', 120, UTC_TIMESTAMP)`)
			if err != nil {
				return err
			}
			id, _ := res.LastInsertId()
			_, err = tx.ExecContext(txCtx, `INSERT INTO booked_location (booking_id, location_id) VALUES (?, 33)`, id)
			return err
		})
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = attempt()
		}(i)
	}
	wg.Wait()

	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
		}
	}
	if successes < 1 {
		t.Fatalf("au moins une création devait aboutir, erreurs: %v / %v", errs[0], errs[1])
	}

	var overlapping int
	err := db.QueryRow(`
        SELECT COUNT(*)
        FROM booked_location bl
        JOIN bookings b ON b.booking_id = bl.booking_id
        WHERE bl.location_id = 33
          AND b.status IN ('PENDING_APPROVAL','ACCEPTED','ORDER_OPEN')
          AND b.booking_date_from < '2026-07-11 21:00:00'
          AND b.booking_date_to   > '2026-07-11 19:00:00'`).Scan(&overlapping)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if overlapping != 1 {
		t.Fatalf("double réservation détectée : %d affectations chevauchantes persistées (attendu 1)", overlapping)
	}
}
