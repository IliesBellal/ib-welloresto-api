package weektemplates

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	authpkg "welloresto-api/internal/modules/auth"
	employeespkg "welloresto-api/internal/modules/planning/employees"
	leavepkg "welloresto-api/internal/modules/planning/leave"
	schedulepkg "welloresto-api/internal/modules/planning/schedule"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"
)

type contractEmployeeReaderStub struct {
	employees map[string]bool
	positions map[string]bool
}

func (s *contractEmployeeReaderStub) GetEmployeeByID(ctx context.Context, merchantID, employeeID string) (*employeespkg.Employee, error) {
	if s.employees[employeeID] {
		return &employeespkg.Employee{ID: employeeID, MerchantID: merchantID}, nil
	}
	return nil, sql.ErrNoRows
}

func (s *contractEmployeeReaderStub) GetEmployeePositionByID(ctx context.Context, merchantID, id string) (*employeespkg.EmployeePosition, error) {
	if s.positions[id] {
		return &employeespkg.EmployeePosition{ID: id, MerchantID: merchantID, Active: true}, nil
	}
	return nil, sql.ErrNoRows
}

func (s *contractEmployeeReaderStub) GetEmployeePositionByLabel(ctx context.Context, merchantID, label, excludeID string) (*employeespkg.EmployeePosition, error) {
	if s.positions[label] {
		return &employeespkg.EmployeePosition{ID: "pos-" + strings.ReplaceAll(strings.ToLower(label), " ", "-"), MerchantID: merchantID, Active: true}, nil
	}
	return nil, sql.ErrNoRows
}

type contractWeekSourceReaderStub struct {
	week           *schedulepkg.PlanningWeek
	weeksByStart   map[string]*schedulepkg.PlanningWeek
	shifts         []schedulepkg.PlanningShift
	err            error
	createWeekErr  error
	createShiftErr error
	deleteShiftErr error
}

func (s *contractWeekSourceReaderStub) GetPlanningWeekByID(ctx context.Context, merchantID, weekID string) (*schedulepkg.PlanningWeek, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.week == nil {
		return nil, sql.ErrNoRows
	}
	return s.week, nil
}

func (s *contractWeekSourceReaderStub) ListPlanningShifts(ctx context.Context, merchantID, weekID string) ([]schedulepkg.PlanningShift, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.shifts, nil
}

func (s *contractWeekSourceReaderStub) GetPlanningWeekByStartDate(ctx context.Context, merchantID string, startDate time.Time, excludeWeekID string) (*schedulepkg.PlanningWeek, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.weeksByStart == nil {
		return nil, sql.ErrNoRows
	}
	item, ok := s.weeksByStart[startDate.Format("2006-01-02")]
	if !ok || item == nil {
		return nil, sql.ErrNoRows
	}
	return item, nil
}

func (s *contractWeekSourceReaderStub) CreatePlanningWeek(ctx context.Context, merchantID string, week schedulepkg.PlanningWeek) (*schedulepkg.PlanningWeek, error) {
	if s.createWeekErr != nil {
		return nil, s.createWeekErr
	}
	if week.ID == "" {
		week.ID = "wk-created"
	}
	created := week
	if s.weeksByStart == nil {
		s.weeksByStart = map[string]*schedulepkg.PlanningWeek{}
	}
	s.weeksByStart[week.StartDate.Format("2006-01-02")] = &created
	return &created, nil
}

func (s *contractWeekSourceReaderStub) CreatePlanningShift(ctx context.Context, merchantID string, shift schedulepkg.PlanningShift) (*schedulepkg.PlanningShift, error) {
	if s.createShiftErr != nil {
		return nil, s.createShiftErr
	}
	if shift.ID == "" {
		shift.ID = "sh-created"
	}
	created := shift
	return &created, nil
}

func (s *contractWeekSourceReaderStub) SoftDeletePlanningShift(ctx context.Context, merchantID, shiftID string) error {
	if s.deleteShiftErr != nil {
		return s.deleteShiftErr
	}
	return nil
}

type contractLeaveReaderStub struct {
	leaves []leavepkg.PlanningLeaveRequest
	err    error
}

func (s *contractLeaveReaderStub) ListApprovedLeavesOverlappingRange(ctx context.Context, merchantID string, employeeIDs []string, startDate, endDate time.Time) ([]leavepkg.PlanningLeaveRequest, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.leaves, nil
}

// Reusable HTTP contract test harness for planning slices.
// Can be copied for other features by changing routes, SQL expectations and JSON contracts.
type contractFixture struct {
	db     *sql.DB
	mock   sqlmock.Sqlmock
	router http.Handler
}

func newContractFixture(t *testing.T, employeeIDs, positionIDs map[string]bool, weekSource WeekSourceReader, leaveReader LeaveReader) *contractFixture {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock new: %v", err)
	}

	repo := NewRepository(db)
	svc := NewService(repo, &contractEmployeeReaderStub{employees: employeeIDs, positions: positionIDs}, weekSource, leaveReader, nil)
	h := NewHandler(svc)
	r := chi.NewRouter()
	r.Get("/planning/week-templates", h.ListWeekTemplates)
	r.Get("/planning/week-templates/{id}", h.GetWeekTemplate)
	r.Post("/planning/week-templates", h.CreateWeekTemplate)
	r.Post("/planning/week-templates/from-week", h.CreateWeekTemplateFromWeek)
	r.Post("/planning/week-templates/{id}/preview", h.PreviewWeekTemplateInstantiation)
	r.Post("/planning/week-templates/{id}/instantiate", h.InstantiateWeekTemplate)
	r.Patch("/planning/week-templates/{id}", h.UpdateWeekTemplate)
	r.Delete("/planning/week-templates/{id}", h.DeleteWeekTemplate)

	return &contractFixture{db: db, mock: mock, router: r}
}

func (f *contractFixture) close(t *testing.T) {
	t.Helper()
	if err := f.mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
	_ = f.db.Close()
}

func authenticatedRequest(t *testing.T, method, path, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	user := &authpkg.UserLoginRow{UserID: "u-1", MerchantID: "m-1"}
	return req.WithContext(middleware.WithUser(req.Context(), user))
}

func decodeBodyToMap(t *testing.T, rr *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var payload map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v, body=%s", err, rr.Body.String())
	}
	return payload
}

func mustMap(t *testing.T, parent map[string]interface{}, key string) map[string]interface{} {
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

func mustArray(t *testing.T, parent map[string]interface{}, key string) []interface{} {
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

func requireExactKeys(t *testing.T, object map[string]interface{}, expected ...string) {
	t.Helper()
	actual := make([]string, 0, len(object))
	for k := range object {
		actual = append(actual, k)
	}
	sort.Strings(actual)
	expectedCopy := append([]string(nil), expected...)
	sort.Strings(expectedCopy)
	if strings.Join(actual, ",") != strings.Join(expectedCopy, ",") {
		t.Fatalf("unexpected keys. actual=%v expected=%v", actual, expectedCopy)
	}
}

func requireHasKey(t *testing.T, object map[string]interface{}, key string) {
	t.Helper()
	if _, ok := object[key]; !ok {
		t.Fatalf("missing key %q", key)
	}
}

func requireNoKey(t *testing.T, object map[string]interface{}, key string) {
	t.Helper()
	if _, ok := object[key]; ok {
		t.Fatalf("unexpected key %q", key)
	}
}

func requireHHMM(t *testing.T, value interface{}, field string) {
	t.Helper()
	s, ok := value.(string)
	if !ok {
		t.Fatalf("field %s should be string HH:MM", field)
	}
	if ok := regexp.MustCompile(`^\d{2}:\d{2}$`).MatchString(s); !ok {
		t.Fatalf("field %s should match HH:MM, got %q", field, s)
	}
}

func TestWeekTemplatesHTTPContract_ListWithoutDetailedShifts(t *testing.T) {
	fx := newContractFixture(t, map[string]bool{}, map[string]bool{}, nil, nil)
	defer fx.close(t)

	createdAt := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	cols := []string{"id", "merchant_id", "label", "notes", "active", "shift_count", "created_at", "updated_at"}
	fx.mock.ExpectQuery("SELECT wt.id, wt.merchant_id").
		WithArgs("m-1").
		WillReturnRows(sqlmock.NewRows(cols).
			AddRow("wtmpl_abc123", "m-1", "Semaine type ete", "Forte affluence", true, 12, createdAt, updatedAt).
			AddRow("wtmpl_inactive", "m-1", "Semaine type hiver", nil, false, 0, createdAt, updatedAt))

	rr := httptest.NewRecorder()
	fx.router.ServeHTTP(rr, authenticatedRequest(t, http.MethodGet, "/planning/week-templates", ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	payload := decodeBodyToMap(t, rr)
	requireExactKeys(t, payload, "id", "data")
	data := mustMap(t, payload, "data")
	requireExactKeys(t, data, "status", "week_templates")
	if data["status"] != "success" {
		t.Fatalf("expected data.status=success, got %v", data["status"])
	}

	items := mustArray(t, data, "week_templates")
	if len(items) != 2 {
		t.Fatalf("expected 2 week_templates, got %d", len(items))
	}
	first, ok := items[0].(map[string]interface{})
	if !ok {
		t.Fatal("first week_templates element is not object")
	}
	requireHasKey(t, first, "merchant_id")
	requireHasKey(t, first, "shift_count")
	requireNoKey(t, first, "week_template_shifts")
}

func TestWeekTemplatesHTTPContract_GetDetailWithShiftsCollection(t *testing.T) {
	fx := newContractFixture(t, map[string]bool{}, map[string]bool{}, nil, nil)
	defer fx.close(t)

	createdAt := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	cols := []string{"id", "merchant_id", "label", "notes", "active", "shift_count", "created_at", "updated_at"}
	fx.mock.ExpectQuery("SELECT wt.id, wt.merchant_id").
		WithArgs("m-1", "wtmpl_abc123").
		WillReturnRows(sqlmock.NewRows(cols).AddRow("wtmpl_abc123", "m-1", "Semaine type ete", "Forte affluence", true, 1, createdAt, updatedAt))
	shiftCols := []string{"id", "week_template_id", "day_of_week", "employee_id", "position_id", "title", "start_time", "end_time", "break_minutes", "location", "notes", "created_at", "updated_at"}
	fx.mock.ExpectQuery("SELECT s.id,").
		WithArgs("m-1", "wtmpl_abc123").
		WillReturnRows(sqlmock.NewRows(shiftCols).AddRow("wts_xyz789", "wtmpl_abc123", 1, nil, nil, "Ouverture", "09:00", "17:00", 30, "Salle", nil, createdAt, updatedAt))

	rr := httptest.NewRecorder()
	fx.router.ServeHTTP(rr, authenticatedRequest(t, http.MethodGet, "/planning/week-templates/wtmpl_abc123", ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	payload := decodeBodyToMap(t, rr)
	requireExactKeys(t, payload, "id", "data")
	data := mustMap(t, payload, "data")
	requireExactKeys(t, data, "status", "week_template", "week_template_shifts")
	weekTemplate := mustMap(t, data, "week_template")
	requireHasKey(t, weekTemplate, "merchant_id")
	requireNoKey(t, weekTemplate, "week_template_shifts")
	if shiftCountRaw, ok := weekTemplate["shift_count"]; ok {
		if shiftCountRaw.(float64) != 1 {
			t.Fatalf("expected shift_count coherence 1, got %v", shiftCountRaw)
		}
	}
	shiftItems := mustArray(t, data, "week_template_shifts")
	if len(shiftItems) != 1 {
		t.Fatalf("expected 1 week_template_shift, got %d", len(shiftItems))
	}
	shift, ok := shiftItems[0].(map[string]interface{})
	if !ok {
		t.Fatal("shift is not object")
	}
	requireHasKey(t, shift, "employee_id")
	requireHasKey(t, shift, "position_id")
	if shift["employee_id"] != nil {
		t.Fatalf("expected explicit null employee_id")
	}
	if shift["position_id"] != nil {
		t.Fatalf("expected explicit null position_id")
	}
	requireHHMM(t, shift["start_time"], "start_time")
	requireHHMM(t, shift["end_time"], "end_time")
}

func TestWeekTemplatesHTTPContract_PostCreateWithExactContractPayload(t *testing.T) {
	fx := newContractFixture(t, map[string]bool{"emp-1": true}, map[string]bool{"pos-2": true}, nil, nil)
	defer fx.close(t)

	postPayloadFromContract := `{
  "label": "Semaine type ete",
  "notes": "Forte affluence",
  "active": true,
  "shifts": [
    {
      "day_of_week": 1,
      "employee_id": "emp-1",
      "position_id": "pos-2",
      "title": null,
      "start_time": "09:00",
      "end_time": "17:00",
      "break_minutes": 30,
      "location": null,
      "notes": null
    }
  ]
}`

	fx.mock.ExpectBegin()
	fx.mock.ExpectExec("INSERT INTO planning_week_templates").
		WithArgs(sqlmock.AnyArg(), "m-1", "Semaine type ete", "Forte affluence", true).
		WillReturnResult(sqlmock.NewResult(1, 1))
	fx.mock.ExpectExec("INSERT INTO planning_week_template_shifts").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), 1, "emp-1", "pos-2", nil, "09:00", "17:00", 30, nil, nil).
		WillReturnResult(sqlmock.NewResult(1, 1))
	fx.mock.ExpectCommit()

	rr := httptest.NewRecorder()
	fx.router.ServeHTTP(rr, authenticatedRequest(t, http.MethodPost, "/planning/week-templates", postPayloadFromContract))
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d body=%s", rr.Code, rr.Body.String())
	}

	payload := decodeBodyToMap(t, rr)
	requireExactKeys(t, payload, "id", "data")
	data := mustMap(t, payload, "data")
	requireExactKeys(t, data, "status", "week_template", "week_template_shifts")
	weekTemplate := mustMap(t, data, "week_template")
	requireHasKey(t, weekTemplate, "merchant_id")
	shiftItems := mustArray(t, data, "week_template_shifts")
	if len(shiftItems) != 1 {
		t.Fatalf("expected 1 shift in create response, got %d", len(shiftItems))
	}
	shift := shiftItems[0].(map[string]interface{})
	requireHasKey(t, shift, "employee_id")
	requireHasKey(t, shift, "position_id")
	requireHHMM(t, shift["start_time"], "start_time")
	requireHHMM(t, shift["end_time"], "end_time")
}

func TestWeekTemplatesHTTPContract_PostEmptyShiftsAccepted_AndMissingShiftsRejected(t *testing.T) {
	fx := newContractFixture(t, map[string]bool{}, map[string]bool{}, nil, nil)
	defer fx.close(t)

	fx.mock.ExpectBegin()
	fx.mock.ExpectExec("INSERT INTO planning_week_templates").
		WithArgs(sqlmock.AnyArg(), "m-1", "Semaine type ete", "Forte affluence", true).
		WillReturnResult(sqlmock.NewResult(1, 1))
	fx.mock.ExpectCommit()

	postEmptyShiftsPayload := `{
  "label": "Semaine type ete",
  "notes": "Forte affluence",
  "active": true,
  "shifts": []
}`
	rrOk := httptest.NewRecorder()
	fx.router.ServeHTTP(rrOk, authenticatedRequest(t, http.MethodPost, "/planning/week-templates", postEmptyShiftsPayload))
	if rrOk.Code != http.StatusCreated {
		t.Fatalf("expected status 201 for shifts:[], got %d body=%s", rrOk.Code, rrOk.Body.String())
	}

	postMissingShiftsPayload := `{
  "label": "Semaine type ete",
  "notes": "Forte affluence",
  "active": true
}`
	rrBad := httptest.NewRecorder()
	fx.router.ServeHTTP(rrBad, authenticatedRequest(t, http.MethodPost, "/planning/week-templates", postMissingShiftsPayload))
	if rrBad.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for missing shifts, got %d body=%s", rrBad.Code, rrBad.Body.String())
	}
	badPayload := decodeBodyToMap(t, rrBad)
	requireExactKeys(t, badPayload, "id", "data")
}

func TestWeekTemplatesHTTPContract_PatchReplacesShifts(t *testing.T) {
	fx := newContractFixture(t, map[string]bool{}, map[string]bool{}, nil, nil)
	defer fx.close(t)

	createdAt := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 6, 1, 10, 5, 0, 0, time.UTC)
	templateCols := []string{"id", "merchant_id", "label", "notes", "active", "shift_count", "created_at", "updated_at"}
	shiftCols := []string{"id", "week_template_id", "day_of_week", "employee_id", "position_id", "title", "start_time", "end_time", "break_minutes", "location", "notes", "created_at", "updated_at"}

	fx.mock.ExpectQuery("SELECT wt.id, wt.merchant_id").
		WithArgs("m-1", "wtmpl_abc123").
		WillReturnRows(sqlmock.NewRows(templateCols).AddRow("wtmpl_abc123", "m-1", "Semaine type ete", "Forte affluence", true, 1, createdAt, createdAt))
	fx.mock.ExpectBegin()
	fx.mock.ExpectExec("UPDATE planning_week_templates").
		WithArgs("Renomme", "...", false, "m-1", "wtmpl_abc123").
		WillReturnResult(sqlmock.NewResult(1, 1))
	fx.mock.ExpectExec("DELETE FROM planning_week_template_shifts").
		WithArgs("wtmpl_abc123").
		WillReturnResult(sqlmock.NewResult(1, 1))
	fx.mock.ExpectExec("INSERT INTO planning_week_template_shifts").
		WithArgs(sqlmock.AnyArg(), "wtmpl_abc123", 2, nil, nil, nil, "10:00", "18:00", 0, nil, nil).
		WillReturnResult(sqlmock.NewResult(1, 1))
	fx.mock.ExpectCommit()
	fx.mock.ExpectQuery("SELECT wt.id, wt.merchant_id").
		WithArgs("m-1", "wtmpl_abc123").
		WillReturnRows(sqlmock.NewRows(templateCols).AddRow("wtmpl_abc123", "m-1", "Renomme", "...", false, 1, createdAt, updatedAt))
	fx.mock.ExpectQuery("SELECT s.id,").
		WithArgs("m-1", "wtmpl_abc123").
		WillReturnRows(sqlmock.NewRows(shiftCols).AddRow("wts_new", "wtmpl_abc123", 2, nil, nil, nil, "10:00", "18:00", 0, nil, nil, updatedAt, updatedAt))

	patchPayloadFromContract := `{
  "label": "Renomme",
  "notes": "...",
  "active": false,
  "shifts": [
    {
      "day_of_week": 2,
      "employee_id": null,
      "position_id": null,
      "title": null,
      "start_time": "10:00",
      "end_time": "18:00",
      "break_minutes": 0,
      "location": null,
      "notes": null
    }
  ]
}`
	rrPatch := httptest.NewRecorder()
	fx.router.ServeHTTP(rrPatch, authenticatedRequest(t, http.MethodPatch, "/planning/week-templates/wtmpl_abc123", patchPayloadFromContract))
	if rrPatch.Code != http.StatusOK {
		t.Fatalf("expected status 200 for patch, got %d body=%s", rrPatch.Code, rrPatch.Body.String())
	}

	patchResponse := decodeBodyToMap(t, rrPatch)
	requireExactKeys(t, patchResponse, "id", "data")
	patchData := mustMap(t, patchResponse, "data")
	requireExactKeys(t, patchData, "status", "week_template", "week_template_shifts")
	patchedShifts := mustArray(t, patchData, "week_template_shifts")
	if len(patchedShifts) != 1 {
		t.Fatalf("expected 1 replacement shift, got %d", len(patchedShifts))
	}
	patchedShift := patchedShifts[0].(map[string]interface{})
	if patchedShift["employee_id"] != nil || patchedShift["position_id"] != nil {
		t.Fatalf("expected explicit null employee_id/position_id in patch response")
	}
}

func TestWeekTemplatesHTTPContract_DeleteSoftKeepsReadable(t *testing.T) {
	fx := newContractFixture(t, map[string]bool{}, map[string]bool{}, nil, nil)
	defer fx.close(t)

	createdAt := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 6, 1, 10, 5, 0, 0, time.UTC)
	templateCols := []string{"id", "merchant_id", "label", "notes", "active", "shift_count", "created_at", "updated_at"}
	shiftCols := []string{"id", "week_template_id", "day_of_week", "employee_id", "position_id", "title", "start_time", "end_time", "break_minutes", "location", "notes", "created_at", "updated_at"}

	fx.mock.ExpectExec("UPDATE planning_week_templates").
		WithArgs("m-1", "wtmpl_abc123").
		WillReturnResult(sqlmock.NewResult(1, 1))
	rrDelete := httptest.NewRecorder()
	fx.router.ServeHTTP(rrDelete, authenticatedRequest(t, http.MethodDelete, "/planning/week-templates/wtmpl_abc123", ""))
	if rrDelete.Code != http.StatusOK {
		t.Fatalf("expected status 200 for delete, got %d body=%s", rrDelete.Code, rrDelete.Body.String())
	}
	deletePayload := decodeBodyToMap(t, rrDelete)
	requireExactKeys(t, deletePayload, "id", "data")
	deleteData := mustMap(t, deletePayload, "data")
	requireExactKeys(t, deleteData, "status")

	fx.mock.ExpectQuery("SELECT wt.id, wt.merchant_id").
		WithArgs("m-1", "wtmpl_abc123").
		WillReturnRows(sqlmock.NewRows(templateCols).AddRow("wtmpl_abc123", "m-1", "Renomme", "...", false, 1, createdAt, updatedAt))
	fx.mock.ExpectQuery("SELECT s.id,").
		WithArgs("m-1", "wtmpl_abc123").
		WillReturnRows(sqlmock.NewRows(shiftCols).AddRow("wts_new", "wtmpl_abc123", 2, nil, nil, nil, "10:00", "18:00", 0, nil, nil, updatedAt, updatedAt))

	rrDetailAfterDelete := httptest.NewRecorder()
	fx.router.ServeHTTP(rrDetailAfterDelete, authenticatedRequest(t, http.MethodGet, "/planning/week-templates/wtmpl_abc123", ""))
	if rrDetailAfterDelete.Code != http.StatusOK {
		t.Fatalf("expected status 200 for detail after delete, got %d body=%s", rrDetailAfterDelete.Code, rrDetailAfterDelete.Body.String())
	}
	afterDeletePayload := decodeBodyToMap(t, rrDetailAfterDelete)
	requireExactKeys(t, afterDeletePayload, "id", "data")
	afterDeleteData := mustMap(t, afterDeletePayload, "data")
	requireExactKeys(t, afterDeleteData, "status", "week_template", "week_template_shifts")
	weekTemplate := mustMap(t, afterDeleteData, "week_template")
	if weekTemplate["active"] != false {
		t.Fatalf("expected active=false after soft delete, got %v", weekTemplate["active"])
	}
}

func TestWeekTemplatesHTTPContract_PostFromWeekReturnsContractShape(t *testing.T) {
	weekSource := &contractWeekSourceReaderStub{
		week: &schedulepkg.PlanningWeek{ID: "w-current", MerchantID: "m-1"},
		shifts: []schedulepkg.PlanningShift{
			{
				ShiftDate:    models.NewDateOnly(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
				EmployeeID:   ptrString("emp-1"),
				PositionID:   ptrString("pos-2"),
				StartTime:    "09:00:00",
				EndTime:      "17:00:00",
				BreakMinutes: 30,
			},
		},
	}
	fx := newContractFixture(t, map[string]bool{}, map[string]bool{}, weekSource, nil)
	defer fx.close(t)

	payloadFromContract := `{
  "week_id": "w-current",
  "label": "Modele — semaine du 1er juin",
  "notes": null
}`

	fx.mock.ExpectBegin()
	fx.mock.ExpectExec("INSERT INTO planning_week_templates").
		WithArgs(sqlmock.AnyArg(), "m-1", "Modele — semaine du 1er juin", nil, true).
		WillReturnResult(sqlmock.NewResult(1, 1))
	fx.mock.ExpectExec("INSERT INTO planning_week_template_shifts").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), 1, "emp-1", "pos-2", nil, "09:00", "17:00", 30, nil, nil).
		WillReturnResult(sqlmock.NewResult(1, 1))
	fx.mock.ExpectCommit()

	rr := httptest.NewRecorder()
	fx.router.ServeHTTP(rr, authenticatedRequest(t, http.MethodPost, "/planning/week-templates/from-week", payloadFromContract))
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d body=%s", rr.Code, rr.Body.String())
	}

	resp := decodeBodyToMap(t, rr)
	requireExactKeys(t, resp, "id", "data")
	data := mustMap(t, resp, "data")
	requireExactKeys(t, data, "status", "week_template", "week_template_shifts")
	shiftItems := mustArray(t, data, "week_template_shifts")
	if len(shiftItems) != 1 {
		t.Fatalf("expected 1 mapped shift, got %d", len(shiftItems))
	}
	shift := shiftItems[0].(map[string]interface{})
	if shift["employee_id"] != "emp-1" {
		t.Fatalf("expected employee_id preserved, got %v", shift["employee_id"])
	}
	if shift["title"] != nil {
		t.Fatalf("expected empty source title to become null")
	}
	requireHHMM(t, shift["start_time"], "start_time")
	requireHHMM(t, shift["end_time"], "end_time")
}

func TestWeekTemplatesHTTPContract_PostFromWeekRejectsInvalidWeekID(t *testing.T) {
	fx := newContractFixture(t, map[string]bool{}, map[string]bool{}, &contractWeekSourceReaderStub{week: nil}, nil)
	defer fx.close(t)

	rr := httptest.NewRecorder()
	fx.router.ServeHTTP(rr, authenticatedRequest(t, http.MethodPost, "/planning/week-templates/from-week", `{"week_id":"missing","label":"X"}`))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown week_id, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func ptrString(v string) *string { return &v }

func TestWeekTemplatesHTTPContract_PreviewReturnsContractShape(t *testing.T) {
	weekStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	fx := newContractFixture(
		t,
		map[string]bool{"emp-1": true},
		map[string]bool{},
		&contractWeekSourceReaderStub{
			weeksByStart: map[string]*schedulepkg.PlanningWeek{"2026-06-01": {ID: "wk-1", MerchantID: "m-1", StartDate: weekStart}},
			shifts:       []schedulepkg.PlanningShift{{ID: "sh-overlap", EmployeeID: ptrString("emp-1"), ShiftDate: models.NewDateOnly(weekStart), StartTime: "09:30:00", EndTime: "11:30:00"}},
		},
		&contractLeaveReaderStub{},
	)
	defer fx.close(t)

	tplCols := []string{"id", "merchant_id", "label", "notes", "active", "shift_count", "created_at", "updated_at"}
	shiftCols := []string{"id", "week_template_id", "day_of_week", "employee_id", "position_id", "title", "start_time", "end_time", "break_minutes", "location", "notes", "created_at", "updated_at"}
	createdAt := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	fx.mock.ExpectQuery("SELECT wt.id, wt.merchant_id").
		WithArgs("m-1", "wt-1").
		WillReturnRows(sqlmock.NewRows(tplCols).AddRow("wt-1", "m-1", "Template", nil, true, 1, createdAt, createdAt))
	fx.mock.ExpectQuery("SELECT s.id,").
		WithArgs("m-1", "wt-1").
		WillReturnRows(sqlmock.NewRows(shiftCols).AddRow("ts-1", "wt-1", 1, "emp-1", nil, "A", "09:00", "11:00", 0, nil, nil, createdAt, createdAt))

	rr := httptest.NewRecorder()
	fx.router.ServeHTTP(rr, authenticatedRequest(t, http.MethodPost, "/planning/week-templates/wt-1/preview", `{"target_week_starts":["2026-06-01"]}`))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	payload := decodeBodyToMap(t, rr)
	requireExactKeys(t, payload, "id", "data")
	data := mustMap(t, payload, "data")
	requireExactKeys(t, data, "status", "preview")
	preview := mustMap(t, data, "preview")
	requireHasKey(t, preview, "to_create_count")
	requireHasKey(t, preview, "conflicts")
	requireHasKey(t, preview, "impacted_employee_count")
	requireHasKey(t, preview, "auto_unassigned_count")
	requireHasKey(t, preview, "idempotent_skipped_count")
}

func TestWeekTemplatesHTTPContract_PreviewRejectsRangeTooLarge(t *testing.T) {
	fx := newContractFixture(t, map[string]bool{}, map[string]bool{}, &contractWeekSourceReaderStub{}, &contractLeaveReaderStub{})
	defer fx.close(t)

	starts := make([]string, 0, MaxPreviewTargetWeeks+1)
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i <= MaxPreviewTargetWeeks; i++ {
		starts = append(starts, `"`+base.AddDate(0, 0, 7*i).Format("2006-01-02")+`"`)
	}
	body := `{"target_week_starts":[` + strings.Join(starts, ",") + `]}`

	rr := httptest.NewRecorder()
	fx.router.ServeHTTP(rr, authenticatedRequest(t, http.MethodPost, "/planning/week-templates/wt-1/preview", body))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestWeekTemplatesHTTPContract_InstantiateReturnsContractShape(t *testing.T) {
	weekStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	fx := newContractFixture(
		t,
		map[string]bool{"emp-1": true},
		map[string]bool{},
		&contractWeekSourceReaderStub{
			weeksByStart: map[string]*schedulepkg.PlanningWeek{"2026-06-01": {ID: "wk-1", MerchantID: "m-1", StartDate: weekStart}},
			shifts:       []schedulepkg.PlanningShift{{ID: "sh-overlap", EmployeeID: ptrString("emp-1"), ShiftDate: models.NewDateOnly(weekStart), StartTime: "09:30:00", EndTime: "11:30:00"}},
		},
		&contractLeaveReaderStub{},
	)
	defer fx.close(t)

	tplCols := []string{"id", "merchant_id", "label", "notes", "active", "shift_count", "created_at", "updated_at"}
	shiftCols := []string{"id", "week_template_id", "day_of_week", "employee_id", "position_id", "title", "start_time", "end_time", "break_minutes", "location", "notes", "created_at", "updated_at"}
	createdAt := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	fx.mock.ExpectQuery("SELECT wt.id, wt.merchant_id").
		WithArgs("m-1", "wt-1").
		WillReturnRows(sqlmock.NewRows(tplCols).AddRow("wt-1", "m-1", "Template", nil, true, 1, createdAt, createdAt))
	fx.mock.ExpectQuery("SELECT s.id,").
		WithArgs("m-1", "wt-1").
		WillReturnRows(sqlmock.NewRows(shiftCols).AddRow("ts-1", "wt-1", 1, "emp-1", nil, "A", "09:00", "11:00", 0, nil, nil, createdAt, createdAt))
	fx.mock.ExpectBegin()
	fx.mock.ExpectCommit()

	rr := httptest.NewRecorder()
	fx.router.ServeHTTP(rr, authenticatedRequest(t, http.MethodPost, "/planning/week-templates/wt-1/instantiate", `{"target_week_starts":["2026-06-01"],"conflict_mode":"template_to_unassigned"}`))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	payload := decodeBodyToMap(t, rr)
	requireExactKeys(t, payload, "id", "data")
	data := mustMap(t, payload, "data")
	requireExactKeys(t, data, "status", "result")
	result := mustMap(t, data, "result")
	requireHasKey(t, result, "created_count")
	requireHasKey(t, result, "assigned_count")
	requireHasKey(t, result, "unassigned_count")
	requireHasKey(t, result, "replaced_count")
	requireHasKey(t, result, "skipped_count")
	requireHasKey(t, result, "per_week")
}

func TestWeekTemplatesHTTPContract_InstantiateRejectsInvalidConflictMode(t *testing.T) {
	fx := newContractFixture(t, map[string]bool{}, map[string]bool{}, &contractWeekSourceReaderStub{}, &contractLeaveReaderStub{})
	defer fx.close(t)

	rr := httptest.NewRecorder()
	fx.router.ServeHTTP(rr, authenticatedRequest(t, http.MethodPost, "/planning/week-templates/wt-1/instantiate", `{"target_week_starts":["2026-06-01"],"conflict_mode":"invalid"}`))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestWeekTemplatesHTTPContract_InstantiateRejectsRangeTooLarge(t *testing.T) {
	fx := newContractFixture(t, map[string]bool{}, map[string]bool{}, &contractWeekSourceReaderStub{}, &contractLeaveReaderStub{})
	defer fx.close(t)

	starts := make([]string, 0, MaxPreviewTargetWeeks+1)
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i <= MaxPreviewTargetWeeks; i++ {
		starts = append(starts, `"`+base.AddDate(0, 0, 7*i).Format("2006-01-02")+`"`)
	}
	body := `{"target_week_starts":[` + strings.Join(starts, ",") + `],"conflict_mode":"keep_existing"}`

	rr := httptest.NewRecorder()
	fx.router.ServeHTTP(rr, authenticatedRequest(t, http.MethodPost, "/planning/week-templates/wt-1/instantiate", body))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}
