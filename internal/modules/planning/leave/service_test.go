package leave

import (
	"context"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/modules/auth"
	employeespkg "welloresto-api/internal/modules/planning/employees"
)

type stubEmployeeReader struct {
	employee         *employeespkg.Employee
	employeeErr      error
	memberEmployeeID string
	memberErr        error
}

func (s stubEmployeeReader) GetEmployeeByID(ctx context.Context, merchantID, employeeID string) (*employeespkg.Employee, error) {
	return s.employee, s.employeeErr
}

func (s stubEmployeeReader) GetEmployeeIDByMemberID(ctx context.Context, merchantID, memberID string) (string, error) {
	return s.memberEmployeeID, s.memberErr
}

func TestServiceCreatePlanningLeaveRequestResolvesCurrentMemberEmployee(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubEmployeeReader{
		employee:         &employeespkg.Employee{ID: "emp_1"},
		memberEmployeeID: "emp_1",
	})

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO planning_leave_requests (
			id, merchant_id, employee_id, leave_type, start_date, end_date, status, reason, manager_note,
			requested_by_user_id, processed_by_user_id, processed_at, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`)).
		WithArgs(sqlmock.AnyArg(), "merchant_1", "emp_1", "paid", sqlmock.AnyArg(), sqlmock.AnyArg(), "pending", nil, nil, "user_1", nil, nil, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1", MerchantRightsID: "42"})
	item, err := svc.CreatePlanningLeaveRequest(ctx, PlanningLeaveRequestCreateRequest{
		EmployeeID: "me",
		LeaveType:  "paid",
		StartDate:  "2026-05-30",
		EndDate:    "2026-05-31",
	})
	if err != nil {
		t.Fatalf("CreatePlanningLeaveRequest() error = %v", err)
	}
	if item == nil || item.EmployeeID != "emp_1" {
		t.Fatalf("CreatePlanningLeaveRequest() resolved employee_id = %#v, want emp_1", item)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}
