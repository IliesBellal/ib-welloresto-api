package bookings

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/zap"
)

// Le POS envoie l'heure murale du marchand ; la base stocke de l'UTC. Paris
// en novembre = UTC+1 : 20:00 saisi doit être stocké 19:00 (et non 20:00,
// qui s'affichait 21:00 — bug de la replanification).
func TestToUTCWindow_ConvertsMerchantWallClockToUTC(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewBookingsRepository(db, zap.NewNop())
	svc := NewBookingsService(repo, db, nil, nil, nil, nil, nil, zap.NewNop())

	mock.ExpectQuery("SELECT timezone").
		WillReturnRows(sqlmock.NewRows([]string{"timezone"}).AddRow("Europe/Paris"))

	from, to, err := svc.toUTCWindow(context.Background(), "2", "2026-11-12 20:00:00", "2026-11-12 21:30:00")
	if err != nil {
		t.Fatalf("toUTCWindow() error = %v", err)
	}
	if from != "2026-11-12 19:00:00" || to != "2026-11-12 20:30:00" {
		t.Fatalf("toUTCWindow() = (%s, %s), want (2026-11-12 19:00:00, 2026-11-12 20:30:00)", from, to)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
