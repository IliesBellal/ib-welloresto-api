package swaps

import (
	"context"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
	employeespkg "welloresto-api/internal/modules/planning/employees"
	schedulepkg "welloresto-api/internal/modules/planning/schedule"
	settingspkg "welloresto-api/internal/modules/planning/settings"
)

type stubEmployeeReader struct {
	employeesByID     map[string]*employeespkg.Employee
	employee          *employeespkg.Employee
	err               error
	memberEmployeeID  string
	memberEmployeeErr error
}

func (s stubEmployeeReader) GetEmployeeByID(ctx context.Context, merchantID, employeeID string) (*employeespkg.Employee, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.employeesByID != nil {
		if employee, ok := s.employeesByID[employeeID]; ok {
			return employee, nil
		}
		return nil, nil
	}
	return s.employee, s.err
}

func (s stubEmployeeReader) GetEmployeeIDByMemberID(ctx context.Context, merchantID, memberID string) (string, error) {
	return s.memberEmployeeID, s.memberEmployeeErr
}

type stubShiftReader struct {
	shifts map[string]*schedulepkg.PlanningShift
	err    error
}

func (s stubShiftReader) GetPlanningShiftByID(ctx context.Context, merchantID, shiftID string) (*schedulepkg.PlanningShift, error) {
	if s.err != nil {
		return nil, s.err
	}
	if shift, ok := s.shifts[shiftID]; ok {
		return shift, nil
	}
	return nil, nil
}

type stubConflictChecker struct{}

func (stubConflictChecker) EnsureShiftHasNoConflicts(ctx context.Context, merchantID string, excludeShiftID *string, employeeID string, shiftDate models.DateOnly, startTime string, endTime string) error {
	return nil
}

type stubSettingsReader struct {
	mode string
}

func (s stubSettingsReader) GetOrCreateSettings(ctx context.Context, merchantID string) (*settingspkg.PlanningSettings, error) {
	return &settingspkg.PlanningSettings{MerchantID: merchantID, ShiftSwapApprovalMode: s.mode}, nil
}

func TestServiceUpdatePlanningShiftSwapRequestAllowsTargetEmployeeApprovalByMemberID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	memberID := "42"
	svc := NewService(
		repo,
		stubEmployeeReader{employee: &employeespkg.Employee{ID: "emp_target", MemberID: &memberID}},
		nil,
		nil,
		stubSettingsReader{mode: settingspkg.ShiftSwapApprovalModeTargetEmployeeRequired},
	)

	now := time.Now().UTC()
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, requester_employee_id, requester_shift_id, target_employee_id, target_shift_id,
			status, reason, manager_note, requested_by_user_id, processed_by_user_id, processed_at, created_at, updated_at, deleted_at
		FROM planning_shift_swap_requests
		WHERE merchant_id = ? AND id = ? AND enabled = TRUE
		LIMIT 1
	`)).
		WithArgs("merchant_1", "swap_1").
		WillReturnRows(sqlmock.NewRows(swapColumns()).AddRow(
			"swap_1", "merchant_1", "emp_req", "shift_req", "emp_target", "shift_target", "pending", nil, nil, nil, nil, nil, now, now, nil,
		))

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE planning_shift_swap_requests
		SET status = ?, reason = ?, manager_note = ?, processed_by_user_id = ?, processed_at = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = TRUE
	`)).
		WithArgs("rejected", nil, nil, "user_1", sqlmock.AnyArg(), sqlmock.AnyArg(), "merchant_1", "swap_1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	status := "rejected"
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1", MerchantRightsID: memberID})
	item, err := svc.UpdatePlanningShiftSwapRequest(ctx, "swap_1", PlanningShiftSwapRequestUpdateRequest{Status: &status})
	if err != nil {
		t.Fatalf("UpdatePlanningShiftSwapRequest() error = %v", err)
	}
	if item == nil || item.Status != "rejected" {
		t.Fatalf("UpdatePlanningShiftSwapRequest() returned unexpected status: %#v", item)
	}
	if item.ProcessedByUserID == nil || *item.ProcessedByUserID != "user_1" {
		t.Fatalf("UpdatePlanningShiftSwapRequest() did not set processed_by_user_id")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestServiceUpdatePlanningShiftSwapRequestRejectsMismatchedMemberID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	targetMemberID := "7"
	svc := NewService(
		repo,
		stubEmployeeReader{employee: &employeespkg.Employee{ID: "emp_target", MemberID: &targetMemberID}},
		nil,
		nil,
		stubSettingsReader{mode: settingspkg.ShiftSwapApprovalModeTargetEmployeeRequired},
	)

	now := time.Now().UTC()
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, requester_employee_id, requester_shift_id, target_employee_id, target_shift_id,
			status, reason, manager_note, requested_by_user_id, processed_by_user_id, processed_at, created_at, updated_at, deleted_at
		FROM planning_shift_swap_requests
		WHERE merchant_id = ? AND id = ? AND enabled = TRUE
		LIMIT 1
	`)).
		WithArgs("merchant_1", "swap_1").
		WillReturnRows(sqlmock.NewRows(swapColumns()).AddRow(
			"swap_1", "merchant_1", "emp_req", "shift_req", "emp_target", "shift_target", "pending", nil, nil, nil, nil, nil, now, now, nil,
		))

	status := "rejected"
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1", MerchantRightsID: "42"})
	_, err = svc.UpdatePlanningShiftSwapRequest(ctx, "swap_1", PlanningShiftSwapRequestUpdateRequest{Status: &status})
	if err != models.ErrPlanningShiftSwapApprovalForbidden {
		t.Fatalf("UpdatePlanningShiftSwapRequest() error = %v, want %v", err, models.ErrPlanningShiftSwapApprovalForbidden)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestServiceCreatePlanningShiftSwapRequestResolvesCurrentMemberEmployee(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	memberID := "42"
	requesterEmployeeID := "emp_requester"
	targetEmployeeID := "emp_target"
	svc := NewService(
		repo,
		stubEmployeeReader{
			memberEmployeeID: requesterEmployeeID,
			employeesByID: map[string]*employeespkg.Employee{
				requesterEmployeeID: {ID: requesterEmployeeID, MemberID: &memberID},
				targetEmployeeID:    {ID: targetEmployeeID},
			},
		},
		stubShiftReader{shifts: map[string]*schedulepkg.PlanningShift{
			"shift_requester": {ID: "shift_requester", EmployeeID: &requesterEmployeeID},
			"shift_target":    {ID: "shift_target", EmployeeID: &targetEmployeeID},
		}},
		nil,
		stubSettingsReader{mode: settingspkg.ShiftSwapApprovalModeManagerRequired},
	)

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO planning_shift_swap_requests (
			id, merchant_id, requester_employee_id, requester_shift_id, target_employee_id, target_shift_id,
			status, reason, manager_note, requested_by_user_id, processed_by_user_id, processed_at, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, TRUE, ?, ?)
	`)).
		WithArgs(sqlmock.AnyArg(), "merchant_1", requesterEmployeeID, "shift_requester", targetEmployeeID, "shift_target", "pending", nil, nil, "user_1", nil, nil, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1", MerchantRightsID: memberID})
	item, err := svc.CreatePlanningShiftSwapRequest(ctx, PlanningShiftSwapRequestCreateRequest{
		RequesterEmployeeID: "me",
		RequesterShiftID:    "shift_requester",
		TargetEmployeeID:    targetEmployeeID,
		TargetShiftID:       "shift_target",
	})
	if err != nil {
		t.Fatalf("CreatePlanningShiftSwapRequest() error = %v", err)
	}
	if item == nil || item.RequesterEmployeeID != requesterEmployeeID {
		t.Fatalf("CreatePlanningShiftSwapRequest() resolved requester_employee_id = %#v, want %s", item, requesterEmployeeID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func swapColumns() []string {
	return []string{
		"id", "merchant_id", "requester_employee_id", "requester_shift_id", "target_employee_id", "target_shift_id",
		"status", "reason", "manager_note", "requested_by_user_id", "processed_by_user_id", "processed_at", "created_at", "updated_at", "deleted_at",
	}
}
