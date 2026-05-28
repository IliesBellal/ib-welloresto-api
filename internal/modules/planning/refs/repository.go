package refs

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"welloresto-api/internal/utils/dbutils"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListContractTypes(ctx context.Context) ([]SystemRef, error) {
	return r.listSystemRefs(ctx, "sys_contract_types")
}

func (r *Repository) ListAttendanceSources(ctx context.Context) ([]SystemRef, error) {
	return r.listSystemRefs(ctx, "sys_attendance_sources")
}

func (r *Repository) AttendanceSourceExists(ctx context.Context, code string) (bool, error) {
	db := dbutils.GetDB(ctx, r.db)
	query := `
		SELECT COUNT(1)
		FROM sys_attendance_sources
		WHERE code = ? AND active = 1
	`
	var count int
	if err := db.QueryRowContext(ctx, query, strings.TrimSpace(code)).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *Repository) ListPlanningEventTypes(ctx context.Context) ([]SystemRef, error) {
	return r.listSystemRefs(ctx, "sys_planning_event_types")
}

func (r *Repository) listSystemRefs(ctx context.Context, tableName string) ([]SystemRef, error) {
	db := dbutils.GetDB(ctx, r.db)
	query := fmt.Sprintf(`
		SELECT code, label, sort_order, active
		FROM %s
		ORDER BY sort_order ASC, code ASC
	`, tableName)

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]SystemRef, 0)
	for rows.Next() {
		var item SystemRef
		if err := rows.Scan(&item.Code, &item.Label, &item.SortOrder, &item.Active); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
