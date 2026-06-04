package swaps

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"

	"welloresto-api/internal/middleware"
	authpkg "welloresto-api/internal/modules/auth"
	employeespkg "welloresto-api/internal/modules/planning/employees"
	settingspkg "welloresto-api/internal/modules/planning/settings"
)

type stubEmployeeReaderForAccept struct {
	memberID string
}

func (s stubEmployeeReaderForAccept) GetEmployeeByID(ctx context.Context, merchantID, employeeID string) (*employeespkg.Employee, error) {
	m := s.memberID
	return &employeespkg.Employee{ID: employeeID, MemberID: &m}, nil
}
func (s stubEmployeeReaderForAccept) GetEmployeeIDByMemberID(ctx context.Context, merchantID, memberID string) (string, error) {
	return "", nil
}

type stubSettingsReaderForAccept struct {
	mode string
}

func (s stubSettingsReaderForAccept) GetOrCreateSettings(ctx context.Context, merchantID string) (*settingspkg.PlanningSettings, error) {
	return &settingspkg.PlanningSettings{ShiftSwapApprovalMode: s.mode}, nil
}

func TestHandlerAccept_NonTargetIsForbidden(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubEmployeeReaderForAccept{memberID: "member-target"}, nil, nil, stubSettingsReaderForAccept{mode: settingspkg.ShiftSwapApprovalModeTargetEmployeeRequired})
	h := NewHandler(svc)

	// prepare DB row for GetPlanningShiftSwapRequestByID
	cols := []string{"id", "merchant_id", "requester_employee_id", "requester_shift_id", "target_employee_id", "target_shift_id", "status", "reason", "manager_note", "requested_by_user_id", "processed_by_user_id", "processed_at", "created_at", "updated_at", "deleted_at"}
	rows := sqlmock.NewRows(cols)
	now := time.Now().UTC()
	rows.AddRow("swap-1", "m-1", "emp-1", "shift-r1", "emp-2", "shift-t1", "pending", nil, nil, nil, nil, nil, now, now, nil)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, merchant_id, requester_employee_id")).
		WithArgs("m-1", "swap-1").
		WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodPost, "/planning/me/shift-swap-requests/swap-1/accept", nil)
	req = req.WithContext(middleware.WithUser(context.Background(), &authpkg.UserLoginRow{UserID: "u-1", MerchantID: "m-1", MerchantRightsID: "member-actor"}))
	// set chi route param
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", "swap-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
	rr := httptest.NewRecorder()

	h.AcceptPlanningShiftSwapRequest(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rr.Code, rr.Body.String())
	}
	var envelope map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	data, ok := envelope["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing data envelope, body=%s", rr.Body.String())
	}
	if data["status"] != "planning_shift_swap_approval_forbidden" {
		t.Fatalf("expected planning_shift_swap_approval_forbidden, got %v", data["status"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestHandlerAccept_ManagerModeIsForbiddenForTarget(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubEmployeeReaderForAccept{memberID: "member-actor"}, nil, nil, stubSettingsReaderForAccept{mode: settingspkg.ShiftSwapApprovalModeManagerRequired})
	h := NewHandler(svc)

	// prepare DB row for GetPlanningShiftSwapRequestByID
	cols := []string{"id", "merchant_id", "requester_employee_id", "requester_shift_id", "target_employee_id", "target_shift_id", "status", "reason", "manager_note", "requested_by_user_id", "processed_by_user_id", "processed_at", "created_at", "updated_at", "deleted_at"}
	rows := sqlmock.NewRows(cols)
	now := time.Now().UTC()
	rows.AddRow("swap-2", "m-1", "emp-1", "shift-r1", "emp-2", "shift-t1", "pending", nil, nil, nil, nil, nil, now, now, nil)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, merchant_id, requester_employee_id")).
		WithArgs("m-1", "swap-2").
		WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodPost, "/planning/me/shift-swap-requests/swap-2/accept", nil)
	req = req.WithContext(middleware.WithUser(context.Background(), &authpkg.UserLoginRow{UserID: "u-1", MerchantID: "m-1", MerchantRightsID: "member-actor"}))
	// set chi route param
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", "swap-2")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
	rr := httptest.NewRecorder()

	h.AcceptPlanningShiftSwapRequest(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rr.Code, rr.Body.String())
	}
	var envelope map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	data, ok := envelope["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing data envelope, body=%s", rr.Body.String())
	}
	if data["status"] != "planning_shift_swap_approval_forbidden" {
		t.Fatalf("expected planning_shift_swap_approval_forbidden, got %v", data["status"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
