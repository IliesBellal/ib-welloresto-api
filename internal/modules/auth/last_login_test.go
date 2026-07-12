package auth

import (
	"context"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/models"
)

func TestAuthServiceLoginMarksLastLoginAt(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewAuthRepository(db)
	svc := NewAuthService(repo, nil, nil, nil, "")
	token := "rights-token"

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT
    u.user_id,`)).
		WithArgs("john@example.com", "john@example.com", token).
		WillReturnRows(sqlmock.NewRows(makeColumns(83)).AddRow(
			// user (0-10)
			"user_1", "John Doe", "John", "Doe", "john@example.com", "+33123456789", true, nil, true, "ignored", nil,
			// rights (11-34)
			"1", token, true, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, "merchant_1", nil, nil, nil, nil,
			// merchant (35-42)
			"Merchant A", "+33999999999", 1.0, 2.0, "Europe/Paris", "1 rue", nil, nil,
			// merchant params (43-62)
			0, 0, 0, true, true, true, false, "", "", false, false, 5, false, false, false, false, nil, "EUR", true, false,
			// package (61-70)
			true, true, false, 0, false, true, true, true, false, true,
			// SNO (71)
			false,
			// uber eats (72-77)
			nil, nil, nil, nil, nil, nil,
			// uber direct (78)
			nil,
			// deliveroo (79-80)
			nil, nil,
		))

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET mfa_status = ? WHERE user_id = ?`)).
		WithArgs(models.MFAStatusVerified, "user_1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET last_login_at = UTC_TIMESTAMP() WHERE user_id = ?`)).
		WithArgs("user_1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT 
    m.id,`)).
		WithArgs("user_1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "fullName", "lat", "lng", "address", "city", "country", "zip_code", "logo_url", "token"}).AddRow("merchant_1", "Merchant A", 1.0, 2.0, "1 rue", "Paris", "France", "75000", "", token))

	resp, err := svc.Login(context.Background(), LoginRequestPayload{Email: "john@example.com"}, token, false)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if resp == nil || resp.Session == nil || resp.Session.Token != token {
		t.Fatalf("Login() did not return the expected session token")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func makeColumns(count int) []string {
	columns := make([]string, count)
	for index := range columns {
		columns[index] = "c" + string(rune('a'+(index%26)))
	}
	return columns
}
