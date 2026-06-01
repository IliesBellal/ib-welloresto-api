package shifttemplates

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

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"
	"welloresto-api/internal/middleware"
	authpkg "welloresto-api/internal/modules/auth"
	employeespkg "welloresto-api/internal/modules/planning/employees"
)

type contractPositionReaderStub struct {
	positions map[string]bool
}

func (s *contractPositionReaderStub) GetEmployeePositionByID(ctx context.Context, merchantID, positionID string) (*employeespkg.EmployeePosition, error) {
	if s.positions[positionID] {
		return &employeespkg.EmployeePosition{ID: positionID, MerchantID: merchantID, Active: true}, nil
	}
	return nil, sql.ErrNoRows
}

type contractFixture struct {
	db     *sql.DB
	mock   sqlmock.Sqlmock
	router http.Handler
}

func newContractFixture(t *testing.T, positions map[string]bool) *contractFixture {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock new: %v", err)
	}

	repo := NewRepository(db)
	svc := NewService(repo, &contractPositionReaderStub{positions: positions})
	h := NewHandler(svc)
	r := chi.NewRouter()
	r.Get("/planning/shift-templates", h.ListShiftTemplates)
	r.Post("/planning/shift-templates", h.CreateShiftTemplate)
	r.Patch("/planning/shift-templates/{id}", h.UpdateShiftTemplate)
	r.Delete("/planning/shift-templates/{id}", h.DeleteShiftTemplate)

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

func TestShiftTemplatesHTTPContract_GetListIncludesInactive(t *testing.T) {
	fx := newContractFixture(t, map[string]bool{"pos_bar": true})
	defer fx.close(t)

	createdAt := time.Date(2026, 5, 25, 8, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 5, 25, 8, 0, 0, 0, time.UTC)
	cols := []string{"id", "label", "start_time", "end_time", "break_minutes", "position_id", "color", "sort_order", "active", "created_at", "updated_at"}
	fx.mock.ExpectQuery("SELECT id, label, TIME_FORMAT").
		WithArgs("m-1").
		WillReturnRows(sqlmock.NewRows(cols).
			AddRow("tmpl_01", "Service midi", "11:00", "15:00", 0, nil, "#10b981", 0, true, createdAt, updatedAt).
			AddRow("tmpl_02", "Coupure bar", "11:00", "23:00", 180, "pos_bar", "#f59e0b", 1, false, createdAt, updatedAt))

	rr := httptest.NewRecorder()
	fx.router.ServeHTTP(rr, authenticatedRequest(t, http.MethodGet, "/planning/shift-templates", ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	payload := decodeBodyToMap(t, rr)
	requireExactKeys(t, payload, "id", "data")
	data := mustMap(t, payload, "data")
	requireExactKeys(t, data, "status", "shift_templates")
	items := mustArray(t, data, "shift_templates")
	if len(items) != 2 {
		t.Fatalf("expected 2 shift_templates, got %d", len(items))
	}

	first := items[0].(map[string]interface{})
	requireNoKey(t, first, "merchant_id")
	requireHasKey(t, first, "color")
	requireHasKey(t, first, "sort_order")
	requireHasKey(t, first, "position_id")
	if first["position_id"] != nil {
		t.Fatalf("expected explicit null position_id for first item")
	}
	requireHHMM(t, first["start_time"], "start_time")
	requireHHMM(t, first["end_time"], "end_time")

	second := items[1].(map[string]interface{})
	if second["active"] != false {
		t.Fatalf("expected second template active=false, got %v", second["active"])
	}
}

func TestShiftTemplatesHTTPContract_PostWithContractPayloadAndColorRequired(t *testing.T) {
	fx := newContractFixture(t, map[string]bool{})
	defer fx.close(t)

	postPayloadFromContract := `{
  "label": "Service midi",
  "start_time": "11:00",
  "end_time": "15:00",
  "break_minutes": 0,
  "position_id": null,
  "color": "#10b981"
}`

	fx.mock.ExpectQuery("SELECT COALESCE").
		WithArgs("m-1").
		WillReturnRows(sqlmock.NewRows([]string{"next"}).AddRow(0))
	fx.mock.ExpectExec("INSERT INTO planning_shift_templates").
		WithArgs(sqlmock.AnyArg(), "m-1", "Service midi", "11:00", "15:00", 0, nil, "#10b981", 0, true, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	rr := httptest.NewRecorder()
	fx.router.ServeHTTP(rr, authenticatedRequest(t, http.MethodPost, "/planning/shift-templates", postPayloadFromContract))
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d body=%s", rr.Code, rr.Body.String())
	}

	payload := decodeBodyToMap(t, rr)
	requireExactKeys(t, payload, "id", "data")
	data := mustMap(t, payload, "data")
	requireExactKeys(t, data, "status", "shift_template")
	item := mustMap(t, data, "shift_template")
	requireNoKey(t, item, "merchant_id")
	requireHasKey(t, item, "position_id")
	if item["position_id"] != nil {
		t.Fatalf("expected explicit null position_id")
	}
	requireHasKey(t, item, "color")
	requireHasKey(t, item, "sort_order")
	requireHHMM(t, item["start_time"], "start_time")
	requireHHMM(t, item["end_time"], "end_time")

	missingColorPayload := `{
  "label": "Service midi",
  "start_time": "11:00",
  "end_time": "15:00",
  "break_minutes": 0,
  "position_id": null
}`
	rrMissingColor := httptest.NewRecorder()
	fx.router.ServeHTTP(rrMissingColor, authenticatedRequest(t, http.MethodPost, "/planning/shift-templates", missingColorPayload))
	if rrMissingColor.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 when color is missing, got %d body=%s", rrMissingColor.Code, rrMissingColor.Body.String())
	}
}

func TestShiftTemplatesHTTPContract_PatchPartial(t *testing.T) {
	fx := newContractFixture(t, map[string]bool{"pos_bar": true})
	defer fx.close(t)

	createdAt := time.Date(2026, 5, 25, 8, 0, 0, 0, time.UTC)
	beforeUpdate := time.Date(2026, 5, 25, 8, 0, 0, 0, time.UTC)
	cols := []string{"id", "label", "start_time", "end_time", "break_minutes", "position_id", "color", "sort_order", "active", "created_at", "updated_at"}
	fx.mock.ExpectQuery("SELECT id, label, TIME_FORMAT").
		WithArgs("m-1", "tmpl_02").
		WillReturnRows(sqlmock.NewRows(cols).AddRow("tmpl_02", "Coupure bar", "11:00", "23:00", 180, "pos_bar", "#f59e0b", 1, true, createdAt, beforeUpdate))
	fx.mock.ExpectExec("UPDATE planning_shift_templates").
		WithArgs("Coupure bar update", "12:00", "22:00", 120, "pos_bar", "#f59e0b", 2, true, sqlmock.AnyArg(), "m-1", "tmpl_02").
		WillReturnResult(sqlmock.NewResult(1, 1))

	patchPayload := `{
  "label": "Coupure bar update",
  "start_time": "12:00",
  "end_time": "22:00",
  "break_minutes": 120,
  "sort_order": 2
}`
	rr := httptest.NewRecorder()
	fx.router.ServeHTTP(rr, authenticatedRequest(t, http.MethodPatch, "/planning/shift-templates/tmpl_02", patchPayload))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	payload := decodeBodyToMap(t, rr)
	requireExactKeys(t, payload, "id", "data")
	data := mustMap(t, payload, "data")
	requireExactKeys(t, data, "status", "shift_template")
	item := mustMap(t, data, "shift_template")
	requireNoKey(t, item, "merchant_id")
	requireHasKey(t, item, "color")
	requireHasKey(t, item, "sort_order")
	requireHHMM(t, item["start_time"], "start_time")
	requireHHMM(t, item["end_time"], "end_time")
}

func TestShiftTemplatesHTTPContract_DeleteSoftStillVisibleInList(t *testing.T) {
	fx := newContractFixture(t, map[string]bool{"pos_bar": true})
	defer fx.close(t)

	createdAt := time.Date(2026, 5, 25, 8, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 5, 25, 9, 0, 0, 0, time.UTC)
	cols := []string{"id", "label", "start_time", "end_time", "break_minutes", "position_id", "color", "sort_order", "active", "created_at", "updated_at"}

	fx.mock.ExpectQuery("SELECT id, label, TIME_FORMAT").
		WithArgs("m-1", "tmpl_02").
		WillReturnRows(sqlmock.NewRows(cols).AddRow("tmpl_02", "Coupure bar", "11:00", "23:00", 180, "pos_bar", "#f59e0b", 1, true, createdAt, createdAt))
	fx.mock.ExpectExec("UPDATE planning_shift_templates").
		WithArgs("Coupure bar", "11:00", "23:00", 180, "pos_bar", "#f59e0b", 1, false, sqlmock.AnyArg(), "m-1", "tmpl_02").
		WillReturnResult(sqlmock.NewResult(1, 1))

	rrDelete := httptest.NewRecorder()
	fx.router.ServeHTTP(rrDelete, authenticatedRequest(t, http.MethodDelete, "/planning/shift-templates/tmpl_02", ""))
	if rrDelete.Code != http.StatusOK {
		t.Fatalf("expected status 200 for delete, got %d body=%s", rrDelete.Code, rrDelete.Body.String())
	}

	fx.mock.ExpectQuery("SELECT id, label, TIME_FORMAT").
		WithArgs("m-1").
		WillReturnRows(sqlmock.NewRows(cols).AddRow("tmpl_02", "Coupure bar", "11:00", "23:00", 180, "pos_bar", "#f59e0b", 1, false, createdAt, updatedAt))

	rrList := httptest.NewRecorder()
	fx.router.ServeHTTP(rrList, authenticatedRequest(t, http.MethodGet, "/planning/shift-templates", ""))
	if rrList.Code != http.StatusOK {
		t.Fatalf("expected status 200 for list after delete, got %d body=%s", rrList.Code, rrList.Body.String())
	}

	payload := decodeBodyToMap(t, rrList)
	data := mustMap(t, payload, "data")
	items := mustArray(t, data, "shift_templates")
	if len(items) != 1 {
		t.Fatalf("expected 1 shift_template, got %d", len(items))
	}
	item := items[0].(map[string]interface{})
	if item["active"] != false {
		t.Fatalf("expected template to stay visible with active=false, got %v", item["active"])
	}
}
