package shifttemplates

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
	employeespkg "welloresto-api/internal/modules/planning/employees"
)

type stubPositionReader struct {
	position *employeespkg.EmployeePosition
	err      error
}

func (s stubPositionReader) GetEmployeePositionByID(ctx context.Context, merchantID, positionID string) (*employeespkg.EmployeePosition, error) {
	return s.position, s.err
}

func TestShiftTemplateJSONIncludesExplicitNullPositionID(t *testing.T) {
	template := ShiftTemplate{
		ID:           "tmpl_1",
		Label:        "Service midi",
		StartTime:    "11:00",
		EndTime:      "15:00",
		BreakMinutes: 0,
		PositionID:   nil,
		Color:        "#10b981",
		SortOrder:    0,
		Active:       true,
		CreatedAt:    time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC),
		UpdatedAt:    time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC),
	}

	payload, err := json.Marshal(template)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	jsonBody := string(payload)
	if !strings.Contains(jsonBody, `"position_id":null`) {
		t.Fatalf("json = %s, want explicit null position_id", jsonBody)
	}
	if strings.Contains(jsonBody, "merchant_id") {
		t.Fatalf("json = %s, did not expect merchant_id", jsonBody)
	}
}

func TestServiceCreateShiftTemplateDefaultsSortOrderAndActive(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubPositionReader{})
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"})

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT COALESCE(MAX(sort_order), -1) + 1
		FROM planning_shift_templates
		WHERE merchant_id = ?
	`)).
		WithArgs("merchant_1").
		WillReturnRows(sqlmock.NewRows([]string{"next_sort_order"}).AddRow(2))

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO planning_shift_templates (
			id, merchant_id, label, start_time, end_time, break_minutes, position_id, color, sort_order, active, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)).
		WithArgs(sqlmock.AnyArg(), "merchant_1", "Service midi", "11:00", "15:00", 0, nil, "#10b981", 2, true, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	item, err := svc.CreateShiftTemplate(ctx, ShiftTemplateCreateRequest{
		Label:        " Service midi ",
		StartTime:    "11:00:00",
		EndTime:      "15:00",
		BreakMinutes: intPtr(0),
		Color:        "#10B981",
	})
	if err != nil {
		t.Fatalf("CreateShiftTemplate() error = %v", err)
	}
	if item.SortOrder != 2 {
		t.Fatalf("CreateShiftTemplate() sort_order = %d, want %d", item.SortOrder, 2)
	}
	if !item.Active {
		t.Fatal("CreateShiftTemplate() active = false, want true")
	}
	if item.StartTime != "11:00" || item.EndTime != "15:00" {
		t.Fatalf("CreateShiftTemplate() time range = %s-%s, want 11:00-15:00", item.StartTime, item.EndTime)
	}
	if item.Color != "#10b981" {
		t.Fatalf("CreateShiftTemplate() color = %q, want %q", item.Color, "#10b981")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestServiceCreateShiftTemplateRejectsInvalidInputs(t *testing.T) {
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"})
	svc := NewService(nil, stubPositionReader{})

	tests := []struct {
		name string
		req  ShiftTemplateCreateRequest
		want error
	}{
		{
			name: "empty label",
			req:  ShiftTemplateCreateRequest{Label: " ", StartTime: "11:00", EndTime: "15:00", BreakMinutes: intPtr(0), Color: "#10b981", SortOrder: intPtr(0)},
			want: models.ErrPlanningShiftTemplateLabelRequired,
		},
		{
			name: "invalid color",
			req:  ShiftTemplateCreateRequest{Label: "Service", StartTime: "11:00", EndTime: "15:00", BreakMinutes: intPtr(0), Color: "green", SortOrder: intPtr(0)},
			want: models.ErrInvalidData,
		},
		{
			name: "negative break",
			req:  ShiftTemplateCreateRequest{Label: "Service", StartTime: "11:00", EndTime: "15:00", BreakMinutes: intPtr(-1), Color: "#10b981", SortOrder: intPtr(0)},
			want: models.ErrInvalidData,
		},
		{
			name: "end before start",
			req:  ShiftTemplateCreateRequest{Label: "Service", StartTime: "15:00", EndTime: "11:00", BreakMinutes: intPtr(0), Color: "#10b981", SortOrder: intPtr(0)},
			want: models.ErrPlanningShiftTemplateInvalidRange,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := svc.CreateShiftTemplate(ctx, test.req)
			if !errors.Is(err, test.want) {
				t.Fatalf("CreateShiftTemplate() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestServiceCreateShiftTemplateRejectsUnknownPosition(t *testing.T) {
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"})
	svc := NewService(nil, stubPositionReader{err: sql.ErrNoRows})

	_, err := svc.CreateShiftTemplate(ctx, ShiftTemplateCreateRequest{
		Label:        "Service midi",
		StartTime:    "11:00",
		EndTime:      "15:00",
		BreakMinutes: intPtr(0),
		PositionID:   stringPtr("pos_missing"),
		Color:        "#10b981",
		SortOrder:    intPtr(0),
	})
	if !errors.Is(err, models.ErrPlanningPositionNotFound) {
		t.Fatalf("CreateShiftTemplate() error = %v, want %v", err, models.ErrPlanningPositionNotFound)
	}
}

func TestServiceUpdateShiftTemplatePositionIDTriState(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubPositionReader{})
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"})
	now := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, label, TIME_FORMAT(start_time, '%H:%i') AS start_time, TIME_FORMAT(end_time, '%H:%i') AS end_time,
			break_minutes, position_id, color, sort_order, active, created_at, updated_at
		FROM planning_shift_templates
		WHERE merchant_id = ? AND id = ?
		LIMIT 1
	`)).
		WithArgs("merchant_1", "tmpl_1").
		WillReturnRows(shiftTemplateRows().AddRow("tmpl_1", "Service midi", "11:00", "15:00", 0, "pos_1", "#10b981", 1, true, now, now))

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE planning_shift_templates
		SET label = ?, start_time = ?, end_time = ?, break_minutes = ?, position_id = ?, color = ?, sort_order = ?, active = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ?
	`)).
		WithArgs("Service midi", "11:00", "15:00", 0, nil, "#10b981", 1, true, sqlmock.AnyArg(), "merchant_1", "tmpl_1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	item, err := svc.UpdateShiftTemplate(ctx, "tmpl_1", ShiftTemplateUpdateRequest{PositionID: NullableStringPatchField{Present: true, Value: nil}})
	if err != nil {
		t.Fatalf("UpdateShiftTemplate() error = %v", err)
	}
	if item.PositionID != nil {
		t.Fatalf("UpdateShiftTemplate() position_id = %#v, want nil", item.PositionID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestServiceDeleteShiftTemplateSoftDeletesToInactive(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubPositionReader{})
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"})
	now := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, label, TIME_FORMAT(start_time, '%H:%i') AS start_time, TIME_FORMAT(end_time, '%H:%i') AS end_time,
			break_minutes, position_id, color, sort_order, active, created_at, updated_at
		FROM planning_shift_templates
		WHERE merchant_id = ? AND id = ?
		LIMIT 1
	`)).
		WithArgs("merchant_1", "tmpl_1").
		WillReturnRows(shiftTemplateRows().AddRow("tmpl_1", "Service midi", "11:00", "15:00", 0, nil, "#10b981", 1, true, now, now))

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE planning_shift_templates
		SET label = ?, start_time = ?, end_time = ?, break_minutes = ?, position_id = ?, color = ?, sort_order = ?, active = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ?
	`)).
		WithArgs("Service midi", "11:00", "15:00", 0, nil, "#10b981", 1, false, sqlmock.AnyArg(), "merchant_1", "tmpl_1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = svc.DeleteShiftTemplate(ctx, "tmpl_1")
	if err != nil {
		t.Fatalf("DeleteShiftTemplate() error = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestHandlerListShiftTemplatesIncludesInactiveItems(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubPositionReader{})
	handler := NewHandler(svc)
	now := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, label, TIME_FORMAT(start_time, '%H:%i') AS start_time, TIME_FORMAT(end_time, '%H:%i') AS end_time,
			break_minutes, position_id, color, sort_order, active, created_at, updated_at
		FROM planning_shift_templates
		WHERE merchant_id = ?
		ORDER BY sort_order ASC, label ASC, created_at ASC
	`)).
		WithArgs("merchant_1").
		WillReturnRows(shiftTemplateRows().AddRow("tmpl_1", "Service midi", "11:00", "15:00", 0, nil, "#10b981", 1, false, now, now))

	req := httptest.NewRequest(http.MethodGet, "/planning/shift-templates", nil)
	req = req.WithContext(middleware.WithUser(req.Context(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"}))
	rec := httptest.NewRecorder()

	handler.ListShiftTemplates(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ListShiftTemplates() status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, expected := range []string{`"shift_templates"`, `"active":false`, `"position_id":null`, `"start_time":"11:00"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("response = %s, want %s", body, expected)
		}
	}
	if strings.Contains(body, "merchant_id") {
		t.Fatalf("response = %s, did not expect merchant_id", body)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestHandlerCreateShiftTemplateUsesSingularKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubPositionReader{})
	handler := NewHandler(svc)

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO planning_shift_templates (
			id, merchant_id, label, start_time, end_time, break_minutes, position_id, color, sort_order, active, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)).
		WithArgs(sqlmock.AnyArg(), "merchant_1", "Service midi", "11:00", "15:00", 0, nil, "#10b981", 0, true, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	body := bytes.NewBufferString(`{"label":"Service midi","start_time":"11:00","end_time":"15:00","break_minutes":0,"position_id":null,"color":"#10b981","sort_order":0}`)
	req := httptest.NewRequest(http.MethodPost, "/planning/shift-templates", body)
	req = req.WithContext(middleware.WithUser(req.Context(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"}))
	rec := httptest.NewRecorder()

	handler.CreateShiftTemplate(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("CreateShiftTemplate() status = %d, want %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"shift_template":{`) {
		t.Fatalf("response = %s, want singular shift_template key", rec.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func shiftTemplateRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "label", "start_time", "end_time", "break_minutes", "position_id", "color", "sort_order", "active", "created_at", "updated_at",
	})
}

func intPtr(value int) *int {
	return &value
}

func stringPtr(value string) *string {
	return &value
}
