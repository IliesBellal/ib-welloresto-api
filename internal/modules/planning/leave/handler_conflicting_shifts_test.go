package leave

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
)

func TestHandlerListPlanningLeaveRequestConflictingShifts_ReturnsAssignedOverlaps(t *testing.T) {
	h, mock, cleanup := newLeaveHandlerFixture(t)
	defer cleanup()

	expectLeaveByID(mock, "m-1", "lr-1", "emp-1", "2026-06-05", "2026-06-10")
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, week_id, shift_date, start_time, end_time, position_id, position
		FROM planning_shifts
	
		WHERE merchant_id = ? AND employee_id = ? AND enabled = TRUE AND status <> 'cancelled'
			AND shift_date >= ? AND shift_date <= ?
	
		ORDER BY shift_date ASC, start_time ASC
	`)).
		WithArgs("m-1", "emp-1", "2026-06-05", "2026-06-10").
		WillReturnRows(sqlmock.NewRows([]string{"id", "week_id", "shift_date", "start_time", "end_time", "position_id", "position"}).
			AddRow("sh-1", "wk-1", "2026-06-05", "09:00:00", "17:00:00", "pos-1", "Salle").
			AddRow("sh-2", "wk-1", "2026-06-07", "10:00:00", "18:00:00", nil, nil))

	rr := httptest.NewRecorder()
	req := newLeaveAuthedRequest(t, http.MethodGet, "/planning/leave-requests/lr-1/conflicting-shifts", "lr-1")
	h.ListPlanningLeaveRequestConflictingShifts(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	payload := decodeJSONMap(t, rr.Body.Bytes())
	data := mustMapValue(t, payload, "data")
	if data["status"] != "success" {
		t.Fatalf("expected status success, got %v", data["status"])
	}
	items := mustArrayValue(t, data, "conflicting_shifts")
	if len(items) != 2 {
		t.Fatalf("expected 2 conflicting_shifts, got %d", len(items))
	}

	first := mustMapFromArray(t, items, 0)
	if first["id"] != "sh-1" {
		t.Fatalf("expected first id sh-1, got %v", first["id"])
	}
	if first["shift_date"] != "2026-06-05" {
		t.Fatalf("expected shift_date YYYY-MM-DD, got %v", first["shift_date"])
	}
	if first["week_id"] != "wk-1" {
		t.Fatalf("expected week_id wk-1, got %v", first["week_id"])
	}
}

func TestHandlerListPlanningLeaveRequestConflictingShifts_NoAssignedShiftReturnsEmpty(t *testing.T) {
	h, mock, cleanup := newLeaveHandlerFixture(t)
	defer cleanup()

	expectLeaveByID(mock, "m-1", "lr-2", "emp-1", "2026-06-05", "2026-06-10")
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, week_id, shift_date, start_time, end_time, position_id, position
		FROM planning_shifts
	
		WHERE merchant_id = ? AND employee_id = ? AND enabled = TRUE AND status <> 'cancelled'
			AND shift_date >= ? AND shift_date <= ?
	
		ORDER BY shift_date ASC, start_time ASC
	`)).
		WithArgs("m-1", "emp-1", "2026-06-05", "2026-06-10").
		WillReturnRows(sqlmock.NewRows([]string{"id", "week_id", "shift_date", "start_time", "end_time", "position_id", "position"}))

	rr := httptest.NewRecorder()
	req := newLeaveAuthedRequest(t, http.MethodGet, "/planning/leave-requests/lr-2/conflicting-shifts", "lr-2")
	h.ListPlanningLeaveRequestConflictingShifts(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	payload := decodeJSONMap(t, rr.Body.Bytes())
	data := mustMapValue(t, payload, "data")
	items := mustArrayValue(t, data, "conflicting_shifts")
	if len(items) != 0 {
		t.Fatalf("expected empty conflicting_shifts, got %d", len(items))
	}
}

func TestHandlerListPlanningLeaveRequestConflictingShifts_UsesAssignedEmployeeFilter(t *testing.T) {
	h, mock, cleanup := newLeaveHandlerFixture(t)
	defer cleanup()

	expectLeaveByID(mock, "m-1", "lr-3", "emp-1", "2026-06-05", "2026-06-10")
	mock.ExpectQuery(`WHERE merchant_id = \? AND employee_id = \? AND enabled = TRUE AND status <> 'cancelled'`).
		WithArgs("m-1", "emp-1", "2026-06-05", "2026-06-10").
		WillReturnRows(sqlmock.NewRows([]string{"id", "week_id", "shift_date", "start_time", "end_time", "position_id", "position"}))

	rr := httptest.NewRecorder()
	req := newLeaveAuthedRequest(t, http.MethodGet, "/planning/leave-requests/lr-3/conflicting-shifts", "lr-3")
	h.ListPlanningLeaveRequestConflictingShifts(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	payload := decodeJSONMap(t, rr.Body.Bytes())
	data := mustMapValue(t, payload, "data")
	items := mustArrayValue(t, data, "conflicting_shifts")
	if len(items) != 0 {
		t.Fatalf("expected empty conflicting_shifts, got %d", len(items))
	}
}

func newLeaveHandlerFixture(t *testing.T) (*Handler, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	repo := NewRepository(db)
	h := NewHandler(NewService(repo, stubEmployeeReader{}))
	cleanup := func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet SQL expectations: %v", err)
		}
		_ = db.Close()
	}
	return h, mock, cleanup
}

func expectLeaveByID(mock sqlmock.Sqlmock, merchantID, requestID, employeeID, startDateISO, endDateISO string) {
	createdAt := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC)
	startDate, err := time.Parse("2006-01-02", startDateISO)
	if err != nil {
		panic(err)
	}
	endDate, err := time.Parse("2006-01-02", endDateISO)
	if err != nil {
		panic(err)
	}
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, employee_id, leave_type, start_date, end_date, status, reason,
			manager_note, requested_by_user_id, processed_by_user_id, processed_at, created_at, updated_at, deleted_at
		FROM planning_leave_requests
		WHERE merchant_id = ? AND id = ? AND enabled = TRUE
		LIMIT 1
	`)).
		WithArgs(merchantID, requestID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "merchant_id", "employee_id", "leave_type", "start_date", "end_date", "status", "reason", "manager_note", "requested_by_user_id", "processed_by_user_id", "processed_at", "created_at", "updated_at", "deleted_at"}).
			AddRow(requestID, merchantID, employeeID, "paid", startDate, endDate, "pending", nil, nil, "u-1", nil, nil, createdAt, updatedAt, nil))
}

func newLeaveAuthedRequest(t *testing.T, method, path, leaveID string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	ctx := middleware.WithUser(req.Context(), &authpkg.UserLoginRow{UserID: "u-1", MerchantID: "m-1"})
	req = req.WithContext(ctx)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", leaveID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func decodeJSONMap(t *testing.T, payload []byte) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatalf("json decode: %v body=%s", err, string(payload))
	}
	return out
}

func mustMapValue(t *testing.T, parent map[string]interface{}, key string) map[string]interface{} {
	t.Helper()
	raw, ok := parent[key]
	if !ok {
		t.Fatalf("missing key %q", key)
	}
	obj, ok := raw.(map[string]interface{})
	if !ok {
		t.Fatalf("key %q is not object", key)
	}
	return obj
}

func mustArrayValue(t *testing.T, parent map[string]interface{}, key string) []interface{} {
	t.Helper()
	raw, ok := parent[key]
	if !ok {
		t.Fatalf("missing key %q", key)
	}
	arr, ok := raw.([]interface{})
	if !ok {
		t.Fatalf("key %q is not array", key)
	}
	return arr
}

func mustMapFromArray(t *testing.T, arr []interface{}, idx int) map[string]interface{} {
	t.Helper()
	obj, ok := arr[idx].(map[string]interface{})
	if !ok {
		t.Fatalf("item at %d is not object", idx)
	}
	return obj
}
