package employees

import (
	"context"
	"errors"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
)

// Seuls les shifts à partir d'aujourd'hui (fuseau de l'établissement) perdent
// leur employé : les shifts passés gardent l'historique.
func TestServiceDeleteEmployeeNullifiesUpcomingShifts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"})

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE employees
		SET active = FALSE, enabled = FALSE, deleted_at = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = TRUE
	`)).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "merchant_1", "emp_1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE planning_shifts
		SET employee_id = NULL, updated_at = ?
		WHERE merchant_id = ? AND employee_id = ? AND enabled = TRUE
			AND shift_date >= (SELECT CAST(now() AT TIME ZONE m.timezone AS DATE) FROM merchant m WHERE CAST(m.id AS TEXT) = ?)
	`)).
		WithArgs(sqlmock.AnyArg(), "merchant_1", "emp_1", "merchant_1").
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectCommit()

	err = svc.DeleteEmployee(ctx, "emp_1")
	if err != nil {
		t.Fatalf("DeleteEmployee() error = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestServiceDeleteEmployeeReturnsNotFoundWhenEmployeeMissing(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"})

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE employees
		SET active = FALSE, enabled = FALSE, deleted_at = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = TRUE
	`)).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "merchant_1", "emp_missing").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err = svc.DeleteEmployee(ctx, "emp_missing")
	if !errors.Is(err, models.ErrPlanningEmployeeNotFound) {
		t.Fatalf("DeleteEmployee() error = %v, want %v", err, models.ErrPlanningEmployeeNotFound)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}
