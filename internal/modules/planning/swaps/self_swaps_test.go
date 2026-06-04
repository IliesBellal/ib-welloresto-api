package swaps

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	authpkg "welloresto-api/internal/modules/auth"
	employeespkg "welloresto-api/internal/modules/planning/employees"
)

var selfSwapColumns = []string{
	"id", "requester_employee_id", "requester_employee_name", "requester_shift_id",
	"target_employee_id", "target_employee_name", "target_shift_id", "status",
	"reason", "manager_note", "processed_at", "created_at",
	"requester_shift_join_id", "requester_shift_employee_id", "requester_shift_position_id", "requester_shift_title",
	"requester_shift_date", "requester_shift_start_time", "requester_shift_end_time", "requester_shift_position", "requester_shift_position_color",
	"target_shift_join_id", "target_shift_employee_id", "target_shift_position_id", "target_shift_title",
	"target_shift_date", "target_shift_start_time", "target_shift_end_time", "target_shift_position", "target_shift_position_color",
}

func newSelfSwapCtx() context.Context {
	return middleware.WithUser(context.Background(), &authpkg.UserLoginRow{
		UserID:           "u-1",
		MerchantID:       "m-1",
		MerchantRightsID: "member-1",
	})
}

func newSelfSwapService(t *testing.T, memberEmployeeID string) (*Service, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	repo := NewRepository(db)
	svc := NewService(repo, stubEmployeeReader{
		employee:         &employeespkg.Employee{ID: memberEmployeeID},
		memberEmployeeID: memberEmployeeID,
	}, nil, nil, nil)
	cleanup := func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet SQL expectations: %v", err)
		}
		_ = db.Close()
	}
	return svc, mock, cleanup
}

func mustParseSwapDate(value string) time.Time {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func addSelfSwapRow(rows *sqlmock.Rows, id, requesterEmployeeID, requesterEmployeeName, requesterShiftID, targetEmployeeID, targetEmployeeName, targetShiftID, status string, reason, managerNote interface{}, processedAt interface{}, createdAt time.Time, requesterShiftDate, targetShiftDate string) {
	rows.AddRow(
		id,
		requesterEmployeeID,
		requesterEmployeeName,
		requesterShiftID,
		targetEmployeeID,
		targetEmployeeName,
		targetShiftID,
		status,
		reason,
		managerNote,
		processedAt,
		createdAt,
		requesterShiftID,
		requesterEmployeeID,
		"pos-req",
		"Service midi",
		mustParseSwapDate(requesterShiftDate),
		"09:00:00",
		"13:00:00",
		"Cuisine",
		"#ef4444",
		targetShiftID,
		targetEmployeeID,
		"pos-target",
		"Service soir",
		mustParseSwapDate(targetShiftDate),
		"18:00:00",
		"22:00:00",
		"Salle",
		"#3b82f6",
	)
}

func TestServiceListCurrentUserShiftSwapRequestsReturnsRequesterAndTargetRows(t *testing.T) {
	svc, mock, cleanup := newSelfSwapService(t, "emp-1")
	defer cleanup()

	createdAt := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows(selfSwapColumns)
	addSelfSwapRow(rows, "swap-req", "emp-1", "Alice Martin", "shift-r1", "emp-2", "Bob Leroy", "shift-t1", "pending", "permute ?", "note manager pending", nil, createdAt, "2026-06-10", "2026-06-11")
	addSelfSwapRow(rows, "swap-target", "emp-3", "Carla Dupond", "shift-r2", "emp-1", "Alice Martin", "shift-t2", "approved", nil, "visible", time.Date(2026, 6, 3, 8, 0, 0, 0, time.UTC), createdAt, "2026-06-12", "2026-06-13")

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT ss.id, ss.requester_employee_id,
			NULLIF(TRIM(CONCAT(COALESCE(re.first_name, ''), ' ', COALESCE(re.last_name, ''))), '') AS requester_employee_name,
			ss.requester_shift_id, ss.target_employee_id,
			NULLIF(TRIM(CONCAT(COALESCE(te.first_name, ''), ' ', COALESCE(te.last_name, ''))), '') AS target_employee_name,
			ss.target_shift_id, ss.status, ss.reason, ss.manager_note, ss.processed_at, ss.created_at,
			rs.id, rs.employee_id, rs.position_id, rs.title, rs.shift_date, rs.start_time, rs.end_time, rs.position, rp.color,
			ts.id, ts.employee_id, ts.position_id, ts.title, ts.shift_date, ts.start_time, ts.end_time, ts.position, tp.color
		FROM planning_shift_swap_requests ss
		LEFT JOIN employees re ON re.id = ss.requester_employee_id AND re.merchant_id = ss.merchant_id AND re.enabled = 1
		LEFT JOIN employees te ON te.id = ss.target_employee_id AND te.merchant_id = ss.merchant_id AND te.enabled = 1
		LEFT JOIN planning_shifts rs ON rs.id = ss.requester_shift_id AND rs.merchant_id = ss.merchant_id AND rs.enabled = 1
		LEFT JOIN planning_positions rp ON rp.id = rs.position_id AND rp.merchant_id = rs.merchant_id AND rp.enabled = 1
		LEFT JOIN planning_shifts ts ON ts.id = ss.target_shift_id AND ts.merchant_id = ss.merchant_id AND ts.enabled = 1
		LEFT JOIN planning_positions tp ON tp.id = ts.position_id AND tp.merchant_id = ts.merchant_id AND tp.enabled = 1
		WHERE ss.merchant_id = ? AND ss.enabled = 1 AND (ss.requester_employee_id = ? OR ss.target_employee_id = ?)
		 ORDER BY ss.created_at DESC`)).
		WithArgs("m-1", "emp-1", "emp-1").
		WillReturnRows(rows)

	currentEmployeeID, items, err := svc.ListCurrentUserShiftSwapRequests(newSelfSwapCtx(), "")
	if err != nil {
		t.Fatalf("ListCurrentUserShiftSwapRequests() error = %v", err)
	}
	if currentEmployeeID != "emp-1" {
		t.Fatalf("currentEmployeeID = %q, want emp-1", currentEmployeeID)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].RequesterEmployeeID != "emp-1" {
		t.Fatalf("first item should be requester-side for emp-1, got %#v", items[0])
	}
	if items[1].TargetEmployeeID != "emp-1" {
		t.Fatalf("second item should be target-side for emp-1, got %#v", items[1])
	}
	if items[0].RequesterShift.ShiftDate == nil || items[0].RequesterShift.ShiftDate.String() != "2026-06-10" {
		t.Fatalf("missing requester shift date enrichment: %#v", items[0].RequesterShift)
	}
	if items[1].TargetShift.Position == nil || *items[1].TargetShift.Position != "Salle" {
		t.Fatalf("missing target shift position enrichment: %#v", items[1].TargetShift)
	}
}

func TestServiceListCurrentUserShiftSwapRequestsAppliesOptionalStatusFilter(t *testing.T) {
	svc, mock, cleanup := newSelfSwapService(t, "emp-1")
	defer cleanup()

	rows := sqlmock.NewRows(selfSwapColumns)
	addSelfSwapRow(rows, "swap-pending", "emp-1", "Alice Martin", "shift-r1", "emp-2", "Bob Leroy", "shift-t1", "pending", nil, nil, nil, time.Now().UTC(), "2026-06-10", "2026-06-11")

	mock.ExpectQuery(`SELECT ss.id, ss.requester_employee_id`).
		WithArgs("m-1", "emp-1", "emp-1", "pending").
		WillReturnRows(rows)

	_, items, err := svc.ListCurrentUserShiftSwapRequests(newSelfSwapCtx(), "pending")
	if err != nil {
		t.Fatalf("ListCurrentUserShiftSwapRequests() error = %v", err)
	}
	if len(items) != 1 || items[0].Status != "pending" {
		t.Fatalf("expected one pending item, got %#v", items)
	}
}

func TestServiceListCurrentUserShiftSwapRequestsHidesInternalIDsAndPendingManagerNote(t *testing.T) {
	svc, mock, cleanup := newSelfSwapService(t, "emp-1")
	defer cleanup()

	rows := sqlmock.NewRows(selfSwapColumns)
	addSelfSwapRow(rows, "swap-1", "emp-1", "Alice Martin", "shift-r1", "emp-2", "Bob Leroy", "shift-t1", "pending", nil, "secret", nil, time.Now().UTC(), "2026-06-10", "2026-06-11")

	mock.ExpectQuery(`SELECT ss.id, ss.requester_employee_id`).
		WithArgs("m-1", "emp-1", "emp-1").
		WillReturnRows(rows)

	_, items, err := svc.ListCurrentUserShiftSwapRequests(newSelfSwapCtx(), "")
	if err != nil {
		t.Fatalf("ListCurrentUserShiftSwapRequests() error = %v", err)
	}
	payload, _ := json.Marshal(items[0])
	body := string(payload)
	if strings.Contains(body, "requested_by_user_id") {
		t.Fatalf("requested_by_user_id must not appear in self DTO, got: %s", body)
	}
	if strings.Contains(body, "processed_by_user_id") {
		t.Fatalf("processed_by_user_id must not appear in self DTO, got: %s", body)
	}
	if strings.Contains(body, "manager_note") {
		t.Fatalf("manager_note must be absent for pending swap, got: %s", body)
	}
	if strings.Contains(body, "secret") {
		t.Fatalf("pending manager_note value must not be exposed, got: %s", body)
	}
	if !strings.Contains(body, `"shift_date":"2026-06-10"`) {
		t.Fatalf("shift_date must be marshaled as YYYY-MM-DD, got: %s", body)
	}
}

func TestHandlerListCurrentUserShiftSwapRequestsIncludesCurrentEmployeeIDAndProcessedManagerNote(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	h := NewHandler(NewService(repo, stubEmployeeReader{
		employee:         &employeespkg.Employee{ID: "emp-1"},
		memberEmployeeID: "emp-1",
	}, nil, nil, nil))

	rows := sqlmock.NewRows(selfSwapColumns)
	addSelfSwapRow(rows, "swap-1", "emp-1", "Alice Martin", "shift-r1", "emp-2", "Bob Leroy", "shift-t1", "approved", "ok", "Approuvé", time.Date(2026, 6, 3, 8, 0, 0, 0, time.UTC), time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC), "2026-06-10", "2026-06-11")

	mock.ExpectQuery(`SELECT ss.id, ss.requester_employee_id`).
		WithArgs("m-1", "emp-1", "emp-1").
		WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/planning/me/shift-swap-requests", nil)
	req = req.WithContext(newSelfSwapCtx())
	rr := httptest.NewRecorder()

	h.ListCurrentUserShiftSwapRequests(rr, req)

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
		t.Fatalf("expected success status, got %v", data["status"])
	}
	if data["current_employee_id"] != "emp-1" {
		t.Fatalf("expected current_employee_id emp-1, got %v", data["current_employee_id"])
	}
	items, ok := data["shift_swap_requests"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("expected one shift_swap_request, got %#v", data["shift_swap_requests"])
	}
	item := items[0].(map[string]interface{})
	if _, exists := item["requested_by_user_id"]; exists {
		t.Fatalf("requested_by_user_id must not appear in self response")
	}
	if _, exists := item["processed_by_user_id"]; exists {
		t.Fatalf("processed_by_user_id must not appear in self response")
	}
	if item["manager_note"] != "Approuvé" {
		t.Fatalf("expected visible processed manager_note, got %v", item["manager_note"])
	}
	requesterShift := item["requester_shift"].(map[string]interface{})
	if requesterShift["shift_date"] != "2026-06-10" {
		t.Fatalf("expected requester_shift.shift_date 2026-06-10, got %v", requesterShift["shift_date"])
	}
	if requesterShift["position_color"] != "#ef4444" {
		t.Fatalf("expected requester_shift.position_color enrichment, got %v", requesterShift["position_color"])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestServiceListCurrentUserShiftSwapRequestsRequiresLinkedEmployee(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := NewService(NewRepository(db), stubEmployeeReader{memberEmployeeID: ""}, nil, nil, nil)

	_, _, err = svc.ListCurrentUserShiftSwapRequests(newSelfSwapCtx(), "")
	if err != models.ErrPlanningEmployeeNotFound {
		t.Fatalf("ListCurrentUserShiftSwapRequests() error = %v, want %v", err, models.ErrPlanningEmployeeNotFound)
	}
}
