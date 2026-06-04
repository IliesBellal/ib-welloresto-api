package swaps

// Security tests for POST /planning/me/shift-swap-requests
// Only two tests: wrong shift ownership and body requester_employee_id is ignored.
// No nominal tests — those are verified manually via Postman.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"

	"welloresto-api/internal/middleware"
	authpkg "welloresto-api/internal/modules/auth"
	employeespkg "welloresto-api/internal/modules/planning/employees"
	schedulepkg "welloresto-api/internal/modules/planning/schedule"
	settingspkg "welloresto-api/internal/modules/planning/settings"
)

// newCreateSwapCtx returns a context with a token whose MerchantRightsID resolves to the given memberID.
func newCreateSwapCtx(memberID string) context.Context {
	return middleware.WithUser(context.Background(), &authpkg.UserLoginRow{
		UserID:           "u-create",
		MerchantID:       "m-create",
		MerchantRightsID: memberID,
	})
}

// newCreateSwapHandler builds a Handler wired with stubs for the given scenario.
// tokenEmployeeID: the employee resolved from the token.
// shiftOwnerID: the employee_id stored on requester_shift in the DB.
func newCreateSwapHandler(t *testing.T, tokenEmployeeID string, shiftOwnerID string) (*Handler, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}

	memberID := "member-token"
	tokenEmployee := &employeespkg.Employee{ID: tokenEmployeeID, MemberID: &memberID}
	targetEmployee := &employeespkg.Employee{ID: "emp-target"}

	requesterShift := &schedulepkg.PlanningShift{ID: "shift-req", EmployeeID: &shiftOwnerID}
	targetShift := &schedulepkg.PlanningShift{ID: "shift-tgt", EmployeeID: func() *string { s := "emp-target"; return &s }()}

	svc := NewService(
		NewRepository(db),
		stubEmployeeReader{
			employeesByID: map[string]*employeespkg.Employee{
				tokenEmployeeID: tokenEmployee,
				"emp-target":    targetEmployee,
			},
			memberEmployeeID: tokenEmployeeID,
		},
		stubShiftReader{shifts: map[string]*schedulepkg.PlanningShift{
			"shift-req": requesterShift,
			"shift-tgt": targetShift,
		}},
		stubConflictChecker{},
		stubSettingsReader{mode: settingspkg.ShiftSwapApprovalModeTargetEmployeeRequired},
	)
	cleanup := func() { _ = db.Close() }
	// mock is unused here because service never reaches the DB in the error paths
	_ = mock
	return NewHandler(svc), cleanup
}

// TestCreateCurrentUser_RequesterShiftNotOwnedByToken verifies that when the requester_shift_id
// belongs to a different employee than the one resolved from the token, the service returns
// ErrPlanningShiftSwapInvalid and the handler responds 400.
func TestCreateCurrentUser_RequesterShiftNotOwnedByToken(t *testing.T) {
	const tokenEmployeeID = "emp-token"
	const shiftOwnerID = "emp-other" // shift belongs to a DIFFERENT employee
	h, cleanup := newCreateSwapHandler(t, tokenEmployeeID, shiftOwnerID)
	defer cleanup()

	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"requester_shift_id": "shift-req",
		"target_employee_id": "emp-target",
		"target_shift_id":    "shift-tgt",
	})
	req := httptest.NewRequest(http.MethodPost, "/planning/me/shift-swap-requests", bytes.NewReader(bodyBytes))
	req = req.WithContext(newCreateSwapCtx("member-token"))

	rr := httptest.NewRecorder()
	h.CreateCurrentUserShiftSwapRequest(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
	var envelope map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	data := envelope["data"].(map[string]interface{})
	if data["status"] != "planning_shift_swap_invalid" {
		t.Fatalf("expected planning_shift_swap_invalid, got %v", data["status"])
	}
}

// TestCreateCurrentUser_BodyRequesterEmployeeIDIsIgnored verifies that even when the client
// sends a requester_employee_id in the body that differs from the token employee,
// the handler uses the token-resolved employee — so the request fails when the token employee
// does not own the requester_shift (proving the body field is overridden).
func TestCreateCurrentUser_BodyRequesterEmployeeIDIsIgnored(t *testing.T) {
	const tokenEmployeeID = "emp-token"
	// shift owned by the token employee — so if the body requester were used instead,
	// the body requester_employee_id ("emp-attacker") would fail ownership.
	// We prove token employee is used by making the shift owned by token employee and
	// confirming the request does NOT succeed when another param makes it invalid instead.
	// Simplest proof: shift belongs to "emp-token" (token) but we also pass
	// requester_employee_id="emp-attacker" in body. The handler must ignore it and use
	// "emp-token"; the only way to verify this without a full DB is to confirm the
	// requester_employee_id that reaches the service equals "emp-token".
	// We do this by making requester_shift owned by "emp-token" and target == requester
	// (which triggers ErrPlanningShiftSwapInvalid "same employee"), showing the service
	// received "emp-token" as requester, not "emp-attacker".

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()
	_ = mock

	memberID := "member-token"
	tokenEmp := &employeespkg.Employee{ID: tokenEmployeeID, MemberID: &memberID}
	_ = time.Now() // ensure time import is used

	svc := NewService(
		NewRepository(db),
		stubEmployeeReader{
			employeesByID:    map[string]*employeespkg.Employee{tokenEmployeeID: tokenEmp},
			memberEmployeeID: tokenEmployeeID,
		},
		stubShiftReader{shifts: map[string]*schedulepkg.PlanningShift{
			// shift-req owned by token employee
			"shift-req": {ID: "shift-req", EmployeeID: func() *string { s := tokenEmployeeID; return &s }()},
		}},
		stubConflictChecker{},
		stubSettingsReader{mode: settingspkg.ShiftSwapApprovalModeTargetEmployeeRequired},
	)
	h := NewHandler(svc)

	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"requester_employee_id": "emp-attacker", // must be ignored
		"requester_shift_id":    "shift-req",
		"target_employee_id":    tokenEmployeeID, // same as token employee → triggers same-employee guard
		"target_shift_id":       "shift-req",
	})
	req := httptest.NewRequest(http.MethodPost, "/planning/me/shift-swap-requests", bytes.NewReader(bodyBytes))
	req = req.WithContext(newCreateSwapCtx("member-token"))

	rr := httptest.NewRecorder()
	h.CreateCurrentUserShiftSwapRequest(rr, req)

	// The service should have used "emp-token" (from token) as requester.
	// target_employee_id == "emp-token" == requester → ErrPlanningShiftSwapInvalid (same employee).
	// If the body's "emp-attacker" had been used, requester != target, and the result
	// would be different (ErrPlanningEmployeeNotFound for emp-attacker, or a 400 for another reason).
	// Either way we assert the response is NOT a success (201) and specifically
	// the "same employee" invalid error confirms the token employee was used.
	if rr.Code == http.StatusCreated {
		t.Fatalf("expected non-201, got 201: body requester_employee_id was honoured instead of token employee")
	}
	var envelope map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	data := envelope["data"].(map[string]interface{})
	// "planning_shift_swap_invalid" = same employee guard triggered, proving token employee was used
	if data["status"] != "planning_shift_swap_invalid" {
		t.Fatalf("expected planning_shift_swap_invalid (same-employee guard, proving token was used), got %v — body=%s", data["status"], rr.Body.String())
	}
}

// helper to satisfy chi.URLParam in tests that don't need it
func withChiID(ctx context.Context, id string) context.Context {
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", id)
	return context.WithValue(ctx, chi.RouteCtxKey, rc)
}
