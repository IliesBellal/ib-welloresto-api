package employees

import (
	"bytes"
	"context"
	"database/sql"
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
)

func TestServiceCreateEmployeePositionRejectsInvalidColor(t *testing.T) {
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"})
	svc := NewService(&Repository{})

	tests := []struct {
		name  string
		color string
	}{
		{name: "missing color", color: ""},
		{name: "invalid hex", color: "blue"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := svc.CreateEmployeePosition(ctx, EmployeePositionCreateRequest{Label: "Barman", Color: test.color})
			if !errors.Is(err, models.ErrInvalidData) {
				t.Fatalf("CreateEmployeePosition() error = %v, want %v", err, models.ErrInvalidData)
			}
		})
	}
}

func TestServiceCreateEmployeePositionPersistsColor(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"})

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT p.id, p.merchant_id, p.label, p.color, p.sort_order, p.active, COUNT(e.id) AS employee_count,
			p.created_at, p.updated_at, p.deleted_at
		FROM planning_positions p
		LEFT JOIN employees e ON e.position_id = p.id AND e.merchant_id = p.merchant_id AND e.enabled = TRUE
		WHERE p.merchant_id = ? AND LOWER(p.label) = LOWER(?) AND p.enabled = TRUE
		 GROUP BY p.id, p.merchant_id, p.label, p.color, p.sort_order, p.active, p.created_at, p.updated_at, p.deleted_at
	`)).
		WithArgs("merchant_1", "Barman").
		WillReturnError(sql.ErrNoRows)

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO planning_positions (
			id, merchant_id, label, color, sort_order, active, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)).
		WithArgs(sqlmock.AnyArg(), "merchant_1", "Barman", "#ec4899", 10, true, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	item, err := svc.CreateEmployeePosition(ctx, EmployeePositionCreateRequest{
		Label:     "Barman",
		Color:     "#ec4899",
		SortOrder: intPtr(10),
		Active:    boolPtr(true),
	})
	if err != nil {
		t.Fatalf("CreateEmployeePosition() error = %v", err)
	}
	if item.Color != "#ec4899" {
		t.Fatalf("CreateEmployeePosition() color = %q, want %q", item.Color, "#ec4899")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestHandlerListEmployeePositionsIncludesColor(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo)
	handler := NewHandler(svc)
	now := time.Now().UTC()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT p.id, p.merchant_id, p.label, p.color, p.sort_order, p.active, COUNT(e.id) AS employee_count,
			p.created_at, p.updated_at, p.deleted_at
		FROM planning_positions p
		LEFT JOIN employees e ON e.position_id = p.id AND e.merchant_id = p.merchant_id AND e.enabled = TRUE
		WHERE p.merchant_id = ? AND p.enabled = TRUE
		 GROUP BY p.id, p.merchant_id, p.label, p.color, p.sort_order, p.active, p.created_at, p.updated_at, p.deleted_at ORDER BY p.sort_order ASC, p.label ASC
	`)).
		WithArgs("merchant_1").
		WillReturnRows(employeePositionRows().AddRow(
			"pos_1", "merchant_1", "Serveur", "#10b981", 0, true, 2, now, now, nil,
		))

	req := httptest.NewRequest(http.MethodGet, "/planning/positions", nil)
	req = req.WithContext(middleware.WithUser(req.Context(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"}))
	rec := httptest.NewRecorder()

	handler.ListEmployeePositions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ListEmployeePositions() status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"color":"#10b981"`) {
		t.Fatalf("response = %s, want color field", rec.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestHandlerUpdateEmployeePositionAcceptsColorOnly(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo)
	handler := NewHandler(svc)
	now := time.Now().UTC()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT p.id, p.merchant_id, p.label, p.color, p.sort_order, p.active, COUNT(e.id) AS employee_count,
			p.created_at, p.updated_at, p.deleted_at
		FROM planning_positions p
		LEFT JOIN employees e ON e.position_id = p.id AND e.merchant_id = p.merchant_id AND e.enabled = TRUE
		WHERE p.merchant_id = ? AND p.id = ? AND p.enabled = TRUE
		GROUP BY p.id, p.merchant_id, p.label, p.color, p.sort_order, p.active, p.created_at, p.updated_at, p.deleted_at
	`)).
		WithArgs("merchant_1", "pos_1").
		WillReturnRows(employeePositionRows().AddRow(
			"pos_1", "merchant_1", "Serveur", "#10b981", 0, true, 2, now, now, nil,
		))

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE planning_positions
		SET label = ?, color = ?, sort_order = ?, active = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = TRUE
	`)).
		WithArgs("Serveur", "#f59e0b", 0, true, sqlmock.AnyArg(), "merchant_1", "pos_1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	body := bytes.NewBufferString(`{"color":"#f59e0b"}`)
	req := httptest.NewRequest(http.MethodPatch, "/planning/positions/pos_1", body)
	req = req.WithContext(middleware.WithUser(req.Context(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"}))
	req = withEmployeeRouteParam(req, "id", "pos_1")
	rec := httptest.NewRecorder()

	handler.UpdateEmployeePosition(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("UpdateEmployeePosition() status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"color":"#f59e0b"`) {
		t.Fatalf("response = %s, want updated color", rec.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func employeePositionRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "merchant_id", "label", "color", "sort_order", "active", "employee_count", "created_at", "updated_at", "deleted_at",
	})
}

func intPtr(value int) *int {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}
