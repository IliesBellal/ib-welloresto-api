package leave

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	authpkg "welloresto-api/internal/modules/auth"
	employeespkg "welloresto-api/internal/modules/planning/employees"
)

// leaveColumns are the columns returned by scanPlanningLeaveRequest.
var leaveColumns = []string{
	"id", "merchant_id", "employee_id", "leave_type",
	"start_date", "end_date", "status", "reason",
	"manager_note", "requested_by_user_id", "processed_by_user_id",
	"processed_at", "created_at", "updated_at", "deleted_at",
}

func newSelfLeaveCtx() context.Context {
	return middleware.WithUser(context.Background(), &authpkg.UserLoginRow{
		UserID:           "u-1",
		MerchantID:       "m-1",
		MerchantRightsID: "member-1",
	})
}

func newSelfLeaveService(t *testing.T, employee *employeespkg.Employee, memberEmployeeID string) (*Service, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	repo := NewRepository(db)
	svc := NewService(repo, stubEmployeeReader{
		employee:         employee,
		memberEmployeeID: memberEmployeeID,
	})
	cleanup := func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet SQL expectations: %v", err)
		}
		_ = db.Close()
	}
	return svc, mock, cleanup
}

// expectSelfLeaveQueries mocks the COUNT + SELECT pair for ListPlanningLeaveRequests
// filtered by a single employeeID.
func expectSelfLeaveQueries(mock sqlmock.Sqlmock, merchantID, employeeID string, rows *sqlmock.Rows) {
	mock.ExpectQuery("SELECT COUNT").
		WithArgs(merchantID, employeeID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(10))
	mock.ExpectQuery("SELECT id, merchant_id, employee_id").
		WithArgs(merchantID, employeeID, 200, 0).
		WillReturnRows(rows)
}

func addLeaveRow(rows *sqlmock.Rows, id, merchantID, employeeID, leaveType, startDate, endDate, status string, reason, managerNote, reqByUserID, procByUserID interface{}, processedAt interface{}, createdAt time.Time) {
	rows.AddRow(
		id, merchantID, employeeID, leaveType,
		mustParseDate(startDate), mustParseDate(endDate),
		status, reason, managerNote, reqByUserID, procByUserID,
		processedAt, createdAt, createdAt, nil,
	)
}

func mustParseDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

// ---------------------------------------------------------------------------
// Service tests
// ---------------------------------------------------------------------------

func TestServiceListCurrentUserLeaveRequestsReturnsOwnLeaveRequests(t *testing.T) {
	svc, mock, cleanup := newSelfLeaveService(t, &employeespkg.Employee{ID: "emp-1"}, "emp-1")
	defer cleanup()

	createdAt := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows(leaveColumns)
	addLeaveRow(rows, "lr-1", "m-1", "emp-1", "paid", "2026-06-10", "2026-06-12", "pending",
		"vacances", nil, "u-1", nil, nil, createdAt)
	addLeaveRow(rows, "lr-2", "m-1", "emp-1", "sick", "2026-05-05", "2026-05-06", "approved",
		nil, "OK", "u-1", "u-2", time.Date(2026, 5, 3, 8, 0, 0, 0, time.UTC), createdAt)

	expectSelfLeaveQueries(mock, "m-1", "emp-1", rows)

	ctx := newSelfLeaveCtx()
	items, err := svc.ListCurrentUserLeaveRequests(ctx, "")
	if err != nil {
		t.Fatalf("ListCurrentUserLeaveRequests() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].ID != "lr-1" || items[1].ID != "lr-2" {
		t.Fatalf("unexpected IDs: %v, %v", items[0].ID, items[1].ID)
	}
	// All results are for this employee only (enforced at SQL level by employee_id filter).
	for _, item := range items {
		if item.EmployeeID != "emp-1" {
			t.Fatalf("expected employee_id emp-1, got %s", item.EmployeeID)
		}
	}
}

func TestServiceListCurrentUserLeaveRequestsManagerNoteHiddenIfPending(t *testing.T) {
	svc, mock, cleanup := newSelfLeaveService(t, &employeespkg.Employee{ID: "emp-1"}, "emp-1")
	defer cleanup()

	createdAt := time.Now().UTC()
	rows := sqlmock.NewRows(leaveColumns)
	addLeaveRow(rows, "lr-pending", "m-1", "emp-1", "paid", "2026-07-01", "2026-07-05", "pending",
		nil, "note confidentielle", "u-1", nil, nil, createdAt)

	expectSelfLeaveQueries(mock, "m-1", "emp-1", rows)

	ctx := newSelfLeaveCtx()
	items, err := svc.ListCurrentUserLeaveRequests(ctx, "")
	if err != nil {
		t.Fatalf("ListCurrentUserLeaveRequests() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}

	payload, _ := json.Marshal(items[0])
	body := string(payload)

	// manager_note must NOT appear for pending requests.
	if strings.Contains(body, "manager_note") {
		t.Fatalf("manager_note must be absent for pending request, got: %s", body)
	}
	if strings.Contains(body, "note confidentielle") {
		t.Fatalf("manager_note value must be hidden for pending request, got: %s", body)
	}
}

func TestServiceListCurrentUserLeaveRequestsManagerNoteVisibleIfApproved(t *testing.T) {
	svc, mock, cleanup := newSelfLeaveService(t, &employeespkg.Employee{ID: "emp-1"}, "emp-1")
	defer cleanup()

	createdAt := time.Now().UTC()
	processedAt := time.Now().UTC()
	rows := sqlmock.NewRows(leaveColumns)
	addLeaveRow(rows, "lr-approved", "m-1", "emp-1", "paid", "2026-07-01", "2026-07-05", "approved",
		nil, "Approuvé sans réserve", "u-1", "u-mgr", processedAt, createdAt)

	expectSelfLeaveQueries(mock, "m-1", "emp-1", rows)

	ctx := newSelfLeaveCtx()
	items, err := svc.ListCurrentUserLeaveRequests(ctx, "")
	if err != nil {
		t.Fatalf("ListCurrentUserLeaveRequests() error = %v", err)
	}

	payload, _ := json.Marshal(items[0])
	body := string(payload)

	if !strings.Contains(body, `"manager_note"`) {
		t.Fatalf("manager_note must be present for approved request, got: %s", body)
	}
	if !strings.Contains(body, "Approuvé sans réserve") {
		t.Fatalf("manager_note value must be visible for approved request, got: %s", body)
	}
}

func TestServiceListCurrentUserLeaveRequestsManagerNoteVisibleIfRejected(t *testing.T) {
	svc, mock, cleanup := newSelfLeaveService(t, &employeespkg.Employee{ID: "emp-1"}, "emp-1")
	defer cleanup()

	createdAt := time.Now().UTC()
	processedAt := time.Now().UTC()
	rows := sqlmock.NewRows(leaveColumns)
	addLeaveRow(rows, "lr-rejected", "m-1", "emp-1", "sick", "2026-06-15", "2026-06-20", "rejected",
		"besoin de se reposer", "Équipe trop réduite ce jour", "u-1", "u-mgr", processedAt, createdAt)

	expectSelfLeaveQueries(mock, "m-1", "emp-1", rows)

	ctx := newSelfLeaveCtx()
	items, err := svc.ListCurrentUserLeaveRequests(ctx, "")
	if err != nil {
		t.Fatalf("ListCurrentUserLeaveRequests() error = %v", err)
	}

	payload, _ := json.Marshal(items[0])
	body := string(payload)

	if !strings.Contains(body, `"manager_note"`) {
		t.Fatalf("manager_note must be present for rejected request, got: %s", body)
	}
	if !strings.Contains(body, "Équipe trop réduite ce jour") {
		t.Fatalf("manager_note value must be visible for rejected request, got: %s", body)
	}
}

func TestServiceListCurrentUserLeaveRequestsInternalUserIDsNeverExposed(t *testing.T) {
	svc, mock, cleanup := newSelfLeaveService(t, &employeespkg.Employee{ID: "emp-1"}, "emp-1")
	defer cleanup()

	createdAt := time.Now().UTC()
	processedAt := time.Now().UTC()
	rows := sqlmock.NewRows(leaveColumns)
	addLeaveRow(rows, "lr-1", "m-1", "emp-1", "paid", "2026-07-01", "2026-07-05", "approved",
		nil, "ok", "requester-uid", "processor-uid", processedAt, createdAt)

	expectSelfLeaveQueries(mock, "m-1", "emp-1", rows)

	ctx := newSelfLeaveCtx()
	items, err := svc.ListCurrentUserLeaveRequests(ctx, "")
	if err != nil {
		t.Fatalf("ListCurrentUserLeaveRequests() error = %v", err)
	}

	payload, _ := json.Marshal(items[0])
	body := string(payload)

	if strings.Contains(body, "requested_by_user_id") {
		t.Fatalf("requested_by_user_id must never appear in self view, got: %s", body)
	}
	if strings.Contains(body, "processed_by_user_id") {
		t.Fatalf("processed_by_user_id must never appear in self view, got: %s", body)
	}
	if strings.Contains(body, "requester-uid") {
		t.Fatalf("requester user ID value must not be exposed, got: %s", body)
	}
	if strings.Contains(body, "processor-uid") {
		t.Fatalf("processor user ID value must not be exposed, got: %s", body)
	}
}

func TestServiceListCurrentUserLeaveRequestsUnlinkedEmployeeReturnsError(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	// memberEmployeeID = "" → GetEmployeeIDByMemberID returns empty → ErrPlanningEmployeeNotFound
	svc := NewService(repo, stubEmployeeReader{
		employee:         nil,
		employeeErr:      models.ErrPlanningEmployeeNotFound,
		memberEmployeeID: "",
	})

	ctx := newSelfLeaveCtx()
	_, err = svc.ListCurrentUserLeaveRequests(ctx, "")
	if err == nil {
		t.Fatal("expected error for unlinked employee, got nil")
	}
	if !strings.Contains(err.Error(), "planning_employee_not_found") {
		t.Fatalf("expected planning_employee_not_found error, got: %v", err)
	}
}

func TestServiceListCurrentUserLeaveRequestsStatusFilterApplied(t *testing.T) {
	svc, mock, cleanup := newSelfLeaveService(t, &employeespkg.Employee{ID: "emp-1"}, "emp-1")
	defer cleanup()

	createdAt := time.Now().UTC()
	rows := sqlmock.NewRows(leaveColumns)
	addLeaveRow(rows, "lr-pending", "m-1", "emp-1", "paid", "2026-07-01", "2026-07-05", "pending",
		nil, nil, "u-1", nil, nil, createdAt)

	// When ?status=pending is passed, SQL must include both employee_id and status filters.
	mock.ExpectQuery("SELECT COUNT").
		WithArgs("m-1", "emp-1", "pending").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery("SELECT id, merchant_id, employee_id").
		WithArgs("m-1", "emp-1", "pending", 200, 0).
		WillReturnRows(rows)

	ctx := newSelfLeaveCtx()
	items, err := svc.ListCurrentUserLeaveRequests(ctx, "pending")
	if err != nil {
		t.Fatalf("ListCurrentUserLeaveRequests() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Status != "pending" {
		t.Fatalf("expected pending status, got %s", items[0].Status)
	}
}

func TestServiceCreateCurrentUserLeaveRequestForcesPendingForTokenEmployee(t *testing.T) {
	svc, mock, cleanup := newSelfLeaveService(t, &employeespkg.Employee{ID: "emp-1"}, "emp-1")
	defer cleanup()

	mock.ExpectExec("INSERT INTO planning_leave_requests").
		WithArgs(
			sqlmock.AnyArg(),
			"m-1",
			"emp-1",
			"paid",
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			"pending",
			"vacances",
			nil,
			"u-1",
			nil,
			nil,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	ctx := newSelfLeaveCtx()
	reason := "vacances"
	item, err := svc.CreateCurrentUserLeaveRequest(ctx, PlanningLeaveRequestSelfCreateRequest{
		LeaveType: "paid",
		StartDate: "2026-07-01",
		EndDate:   "2026-07-05",
		Reason:    &reason,
	})
	if err != nil {
		t.Fatalf("CreateCurrentUserLeaveRequest() error = %v", err)
	}
	if item == nil {
		t.Fatal("expected created item, got nil")
	}
	if item.EmployeeID != "emp-1" {
		t.Fatalf("expected employee_id emp-1, got %s", item.EmployeeID)
	}
	if item.Status != "pending" {
		t.Fatalf("expected status pending, got %s", item.Status)
	}
}

func TestServiceCreateCurrentUserLeaveRequestInvalidTypeReturnsError(t *testing.T) {
	svc, _, cleanup := newSelfLeaveService(t, &employeespkg.Employee{ID: "emp-1"}, "emp-1")
	defer cleanup()

	ctx := newSelfLeaveCtx()
	_, err := svc.CreateCurrentUserLeaveRequest(ctx, PlanningLeaveRequestSelfCreateRequest{
		LeaveType: "vacation",
		StartDate: "2026-07-01",
		EndDate:   "2026-07-05",
	})
	if err == nil {
		t.Fatal("expected invalid type error, got nil")
	}
	if !strings.Contains(err.Error(), "planning_leave_type_invalid") {
		t.Fatalf("expected planning_leave_type_invalid, got %v", err)
	}
}

func TestServiceCreateCurrentUserLeaveRequestInvalidRangeReturnsError(t *testing.T) {
	svc, _, cleanup := newSelfLeaveService(t, &employeespkg.Employee{ID: "emp-1"}, "emp-1")
	defer cleanup()

	ctx := newSelfLeaveCtx()
	_, err := svc.CreateCurrentUserLeaveRequest(ctx, PlanningLeaveRequestSelfCreateRequest{
		LeaveType: "paid",
		StartDate: "2026-07-10",
		EndDate:   "2026-07-05",
	})
	if err == nil {
		t.Fatal("expected invalid range error, got nil")
	}
	if !strings.Contains(err.Error(), "planning_leave_invalid_range") {
		t.Fatalf("expected planning_leave_invalid_range, got %v", err)
	}
}

func TestServiceCreateCurrentUserLeaveRequestUnlinkedEmployeeReturnsError(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubEmployeeReader{
		employee:         nil,
		employeeErr:      models.ErrPlanningEmployeeNotFound,
		memberEmployeeID: "",
	})

	ctx := newSelfLeaveCtx()
	_, err = svc.CreateCurrentUserLeaveRequest(ctx, PlanningLeaveRequestSelfCreateRequest{
		LeaveType: "paid",
		StartDate: "2026-07-01",
		EndDate:   "2026-07-05",
	})
	if err == nil {
		t.Fatal("expected planning_employee_not_found error, got nil")
	}
	if !strings.Contains(err.Error(), "planning_employee_not_found") {
		t.Fatalf("expected planning_employee_not_found, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Handler tests
// ---------------------------------------------------------------------------

func TestHandlerListCurrentUserLeaveRequestsLinkedEmployee(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	h := NewHandler(NewService(repo, stubEmployeeReader{
		employee:         &employeespkg.Employee{ID: "emp-1"},
		memberEmployeeID: "emp-1",
	}))

	createdAt := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows(leaveColumns)
	addLeaveRow(rows, "lr-1", "m-1", "emp-1", "paid", "2026-07-01", "2026-07-10", "pending",
		"congés annuels", nil, "u-1", nil, nil, createdAt)

	mock.ExpectQuery("SELECT COUNT").
		WithArgs("m-1", "emp-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery("SELECT id, merchant_id, employee_id").
		WithArgs("m-1", "emp-1", 200, 0).
		WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/planning/me/leave-requests", nil)
	ctx := middleware.WithUser(req.Context(), &authpkg.UserLoginRow{
		UserID: "u-1", MerchantID: "m-1", MerchantRightsID: "member-1",
	})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.ListCurrentUserLeaveRequests(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var envelope map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	data, ok := envelope["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing data envelope, body=%s", rr.Body.String())
	}
	if data["status"] != "success" {
		t.Fatalf("expected status success, got %v", data["status"])
	}
	leaveRequests, ok := data["leave_requests"].([]interface{})
	if !ok {
		t.Fatalf("leave_requests not an array, body=%s", rr.Body.String())
	}
	if len(leaveRequests) != 1 {
		t.Fatalf("expected 1 leave request, got %d", len(leaveRequests))
	}

	item := leaveRequests[0].(map[string]interface{})
	if item["id"] != "lr-1" {
		t.Fatalf("expected id lr-1, got %v", item["id"])
	}
	if item["employee_id"] != "emp-1" {
		t.Fatalf("expected employee_id emp-1, got %v", item["employee_id"])
	}
	if item["start_date"] != "2026-07-01" {
		t.Fatalf("expected start_date 2026-07-01, got %v", item["start_date"])
	}
	if item["status"] != "pending" {
		t.Fatalf("expected status pending, got %v", item["status"])
	}
	// manager_note must be absent for pending.
	if _, exists := item["manager_note"]; exists {
		t.Fatalf("manager_note must not appear for pending request")
	}
	// Internal user IDs must never appear.
	if _, exists := item["requested_by_user_id"]; exists {
		t.Fatalf("requested_by_user_id must not appear in self view")
	}
	if _, exists := item["processed_by_user_id"]; exists {
		t.Fatalf("processed_by_user_id must not appear in self view")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestHandlerListCurrentUserLeaveRequestsUnlinkedEmployeeReturns422(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	h := NewHandler(NewService(repo, stubEmployeeReader{
		employee:         nil,
		employeeErr:      models.ErrPlanningEmployeeNotFound,
		memberEmployeeID: "",
	}))

	req := httptest.NewRequest(http.MethodGet, "/planning/me/leave-requests", nil)
	ctx := middleware.WithUser(req.Context(), &authpkg.UserLoginRow{
		UserID: "u-1", MerchantID: "m-1", MerchantRightsID: "member-1",
	})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.ListCurrentUserLeaveRequests(rr, req)

	if rr.Code == http.StatusOK {
		t.Fatalf("expected error status for unlinked employee, got 200")
	}
}

func TestHandlerCreateCurrentUserLeaveRequestIgnoresEmployeeIDAndStatusFromBody(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	h := NewHandler(NewService(repo, stubEmployeeReader{
		employee:         &employeespkg.Employee{ID: "emp-1"},
		memberEmployeeID: "emp-1",
	}))

	mock.ExpectExec("INSERT INTO planning_leave_requests").
		WithArgs(
			sqlmock.AnyArg(),
			"m-1",
			"emp-1",
			"paid",
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			"pending",
			"raison envoyee",
			nil,
			"u-1",
			nil,
			nil,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	body := `{"employee_id":"emp-999","status":"approved","leave_type":"paid","start_date":"2026-07-01","end_date":"2026-07-05","reason":"raison envoyee"}`
	req := httptest.NewRequest(http.MethodPost, "/planning/me/leave-requests", strings.NewReader(body))
	ctx := middleware.WithUser(req.Context(), &authpkg.UserLoginRow{
		UserID: "u-1", MerchantID: "m-1", MerchantRightsID: "member-1",
	})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.CreateCurrentUserLeaveRequest(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}

	var envelope map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	data := envelope["data"].(map[string]interface{})
	item := data["leave_request"].(map[string]interface{})

	if item["employee_id"] != "emp-1" {
		t.Fatalf("expected employee_id from token emp-1, got %v", item["employee_id"])
	}
	if item["status"] != "pending" {
		t.Fatalf("expected forced pending status, got %v", item["status"])
	}
	if _, exists := item["requested_by_user_id"]; exists {
		t.Fatalf("requested_by_user_id must not appear in self response")
	}
	if _, exists := item["processed_by_user_id"]; exists {
		t.Fatalf("processed_by_user_id must not appear in self response")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestHandlerCreateCurrentUserLeaveRequestInvalidTypeReturnsError(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	h := NewHandler(NewService(repo, stubEmployeeReader{
		employee:         &employeespkg.Employee{ID: "emp-1"},
		memberEmployeeID: "emp-1",
	}))

	body := `{"leave_type":"vacation","start_date":"2026-07-01","end_date":"2026-07-05"}`
	req := httptest.NewRequest(http.MethodPost, "/planning/me/leave-requests", strings.NewReader(body))
	ctx := middleware.WithUser(req.Context(), &authpkg.UserLoginRow{
		UserID: "u-1", MerchantID: "m-1", MerchantRightsID: "member-1",
	})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.CreateCurrentUserLeaveRequest(rr, req)

	if rr.Code == http.StatusCreated {
		t.Fatalf("expected error status for invalid type, got 201")
	}
}

func TestHandlerCreateCurrentUserLeaveRequestInvalidRangeReturnsError(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	h := NewHandler(NewService(repo, stubEmployeeReader{
		employee:         &employeespkg.Employee{ID: "emp-1"},
		memberEmployeeID: "emp-1",
	}))

	body := `{"leave_type":"paid","start_date":"2026-07-10","end_date":"2026-07-05"}`
	req := httptest.NewRequest(http.MethodPost, "/planning/me/leave-requests", strings.NewReader(body))
	ctx := middleware.WithUser(req.Context(), &authpkg.UserLoginRow{
		UserID: "u-1", MerchantID: "m-1", MerchantRightsID: "member-1",
	})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.CreateCurrentUserLeaveRequest(rr, req)

	if rr.Code == http.StatusCreated {
		t.Fatalf("expected error status for invalid range, got 201")
	}
}

func TestHandlerCreateCurrentUserLeaveRequestUnlinkedEmployeeReturnsError(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	h := NewHandler(NewService(repo, stubEmployeeReader{
		employee:         nil,
		employeeErr:      models.ErrPlanningEmployeeNotFound,
		memberEmployeeID: "",
	}))

	body := `{"leave_type":"paid","start_date":"2026-07-01","end_date":"2026-07-05"}`
	req := httptest.NewRequest(http.MethodPost, "/planning/me/leave-requests", strings.NewReader(body))
	ctx := middleware.WithUser(req.Context(), &authpkg.UserLoginRow{
		UserID: "u-1", MerchantID: "m-1", MerchantRightsID: "member-1",
	})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.CreateCurrentUserLeaveRequest(rr, req)

	if rr.Code == http.StatusCreated {
		t.Fatalf("expected error status for unlinked employee, got 201")
	}
}

// ---------------------------------------------------------------------------
// DTO unit test — PlanningLeaveRequestSelfView MarshalJSON
// ---------------------------------------------------------------------------

func TestPlanningLeaveRequestSelfViewMarshalJSON_PendingHidesManagerNote(t *testing.T) {
	note := "note du manager"
	v := PlanningLeaveRequestSelfView{
		ID:          "lr-1",
		EmployeeID:  "emp-1",
		LeaveType:   "paid",
		StartDate:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		EndDate:     time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC),
		Status:      "pending",
		ManagerNote: &note,
		CreatedAt:   time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
	}
	payload, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	body := string(payload)
	if strings.Contains(body, "manager_note") {
		t.Fatalf("manager_note must be absent for pending, got: %s", body)
	}
	if !strings.Contains(body, `"start_date":"2026-07-01"`) {
		t.Fatalf("expected start_date YYYY-MM-DD, got: %s", body)
	}
}

func TestPlanningLeaveRequestSelfViewMarshalJSON_ApprovedExposesManagerNote(t *testing.T) {
	note := "OK approuvé"
	v := PlanningLeaveRequestSelfView{
		ID:          "lr-2",
		EmployeeID:  "emp-1",
		LeaveType:   "paid",
		StartDate:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		EndDate:     time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC),
		Status:      "approved",
		ManagerNote: &note,
		CreatedAt:   time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
	}
	payload, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	body := string(payload)
	if !strings.Contains(body, `"manager_note":"OK approuvé"`) {
		t.Fatalf("manager_note must be present for approved, got: %s", body)
	}
}

func TestPlanningLeaveRequestSelfViewMarshalJSON_NeverExposesInternalUserIDs(t *testing.T) {
	v := PlanningLeaveRequestSelfView{
		ID:         "lr-3",
		EmployeeID: "emp-1",
		LeaveType:  "sick",
		StartDate:  time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		EndDate:    time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC),
		Status:     "rejected",
		CreatedAt:  time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
	}
	payload, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	body := string(payload)
	if strings.Contains(body, "requested_by_user_id") {
		t.Fatalf("requested_by_user_id must never appear, got: %s", body)
	}
	if strings.Contains(body, "processed_by_user_id") {
		t.Fatalf("processed_by_user_id must never appear, got: %s", body)
	}
}
