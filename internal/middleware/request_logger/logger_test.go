package requestlogger

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/zap"
)

// Régression : flush() envoyait des placeholders `?` (MySQL) même quand le
// dialecte actif est Postgres, où pgx exige `$1, $2, ...`. Vérifié sans base
// réelle via sqlmock — le test d'intégration Postgres complet vit dans
// postgres_integration_test.go (tag postgres_integration).
func TestFlush_Postgres_RebindsPlaceholders(t *testing.T) {
	t.Setenv("DB_DIALECT", "postgres")

	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer mockDB.Close()

	merchantID := "m1"
	userID := int64(1)

	mock.ExpectExec(`INSERT INTO api_request_logs`).
		WithArgs(userID, merchantID, "GET", "/url", []byte(`{"ok":true}`), 200, "1.2.3.4").
		WillReturnResult(sqlmock.NewResult(1, 1))

	l := &Logger{db: mockDB, log: zap.NewNop()}
	l.flush([]LogEntry{
		{
			UserID:     &userID,
			MerchantID: &merchantID,
			Method:     "GET",
			URL:        "/url",
			Payload:    []byte(`{"ok":true}`),
			StatusCode: 200,
			IP:         "1.2.3.4",
		},
	})

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("query did not use $N placeholders as expected on Postgres: %v", err)
	}
	if l.failureStreak != 0 {
		t.Fatalf("expected flush to succeed, failureStreak = %d", l.failureStreak)
	}
}

func TestFlush_MySQL_KeepsQuestionMarkPlaceholders(t *testing.T) {
	t.Setenv("DB_DIALECT", "mysql")

	mockDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer mockDB.Close()

	merchantID := "m1"
	userID := int64(1)

	mock.ExpectExec(`VALUES \(\?, \?, \?, \?, \?, \?, \?\)`).
		WithArgs(userID, merchantID, "GET", "/url", []byte(`{"ok":true}`), 200, "1.2.3.4").
		WillReturnResult(sqlmock.NewResult(1, 1))

	l := &Logger{db: mockDB, log: zap.NewNop()}
	l.flush([]LogEntry{
		{
			UserID:     &userID,
			MerchantID: &merchantID,
			Method:     "GET",
			URL:        "/url",
			Payload:    []byte(`{"ok":true}`),
			StatusCode: 200,
			IP:         "1.2.3.4",
		},
	})

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("MySQL flush must keep ? placeholders unchanged: %v", err)
	}
}
