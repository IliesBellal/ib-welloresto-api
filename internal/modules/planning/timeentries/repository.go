package timeentries

import (
	"context"
	"database/sql"
	"time"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/utils/dbutils"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListEmployeeTimeEntries(ctx context.Context, merchantID, employeeID string, filters PlanningTimeEntryListFilters) ([]PlanningTimeEntry, int, error) {
	db := dbutils.GetDB(ctx, r.db)
	var totalItems int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM planning_time_entries
		WHERE merchant_id = ? AND employee_id = ? AND enabled = 1
	`, merchantID, employeeID).Scan(&totalItems); err != nil {
		return nil, 0, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, merchant_id, employee_id, shift_id, attendance_source, clock_in_at, clock_out_at,
			clock_in_note, clock_out_note, created_at, updated_at, deleted_at
		FROM planning_time_entries
		WHERE merchant_id = ? AND employee_id = ? AND enabled = 1
		ORDER BY clock_in_at DESC, created_at DESC
		LIMIT ? OFFSET ?
	`, merchantID, employeeID, filters.PageSize, (filters.Page-1)*filters.PageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]PlanningTimeEntry, 0)
	for rows.Next() {
		item, scanErr := scanPlanningTimeEntry(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		items = append(items, *item)
	}
	return items, totalItems, rows.Err()
}

func (r *Repository) GetPlanningTimeEntryByID(ctx context.Context, merchantID, entryID string) (*PlanningTimeEntry, error) {
	db := dbutils.GetDB(ctx, r.db)
	row := db.QueryRowContext(ctx, `
		SELECT id, merchant_id, employee_id, shift_id, attendance_source, clock_in_at, clock_out_at,
			clock_in_note, clock_out_note, created_at, updated_at, deleted_at
		FROM planning_time_entries
		WHERE merchant_id = ? AND id = ? AND enabled = 1
		LIMIT 1
	`, merchantID, entryID)
	return scanPlanningTimeEntryRow(row)
}

func (r *Repository) GetOpenPlanningTimeEntryForEmployee(ctx context.Context, merchantID, employeeID string) (*PlanningTimeEntry, error) {
	db := dbutils.GetDB(ctx, r.db)
	row := db.QueryRowContext(ctx, `
		SELECT id, merchant_id, employee_id, shift_id, attendance_source, clock_in_at, clock_out_at,
			clock_in_note, clock_out_note, created_at, updated_at, deleted_at
		FROM planning_time_entries
		WHERE merchant_id = ? AND employee_id = ? AND clock_out_at IS NULL AND enabled = 1
		ORDER BY clock_in_at DESC, created_at DESC
		LIMIT 1
	`, merchantID, employeeID)
	return scanPlanningTimeEntryRow(row)
}

func (r *Repository) CreatePlanningTimeEntry(ctx context.Context, merchantID string, entry PlanningTimeEntry) (*PlanningTimeEntry, error) {
	db := dbutils.GetDB(ctx, r.db)
	now := time.Now().UTC()
	entry.ID = helpers.GeneratePrefixedID(helpers.PlanningTimeEntryIDPrefix)
	entry.MerchantID = merchantID
	entry.CreatedAt = now
	entry.UpdatedAt = now
	_, err := db.ExecContext(ctx, `
		INSERT INTO planning_time_entries (
			id, merchant_id, employee_id, shift_id, attendance_source, clock_in_at, clock_out_at,
			clock_in_note, clock_out_note, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`, entry.ID, entry.MerchantID, entry.EmployeeID, entry.ShiftID, entry.AttendanceSource, entry.ClockInAt, entry.ClockOutAt, entry.ClockInNote, entry.ClockOutNote, entry.CreatedAt, entry.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

func (r *Repository) ClosePlanningTimeEntry(ctx context.Context, merchantID, entryID string, clockOutAt time.Time, clockOutNote *string) (*PlanningTimeEntry, error) {
	db := dbutils.GetDB(ctx, r.db)
	current, err := r.GetPlanningTimeEntryByID(ctx, merchantID, entryID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	res, err := db.ExecContext(ctx, `
		UPDATE planning_time_entries
		SET clock_out_at = ?, clock_out_note = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = 1 AND clock_out_at IS NULL
	`, clockOutAt, clockOutNote, now, merchantID, entryID)
	if err != nil {
		return nil, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, sql.ErrNoRows
	}
	current.ClockOutAt = &clockOutAt
	current.ClockOutNote = clockOutNote
	current.UpdatedAt = now
	return current, nil
}

type scannable interface {
	Scan(dest ...any) error
}

type scannableRows interface {
	Scan(dest ...any) error
}

func scanPlanningTimeEntryRow(row scannable) (*PlanningTimeEntry, error) {
	item := &PlanningTimeEntry{}
	var shiftID, clockInNote, clockOutNote sql.NullString
	var clockOutAt sql.NullTime
	var deletedAt sql.NullTime
	if err := row.Scan(&item.ID, &item.MerchantID, &item.EmployeeID, &shiftID, &item.AttendanceSource, &item.ClockInAt, &clockOutAt, &clockInNote, &clockOutNote, &item.CreatedAt, &item.UpdatedAt, &deletedAt); err != nil {
		return nil, err
	}
	if shiftID.Valid {
		item.ShiftID = &shiftID.String
	}
	if clockOutAt.Valid {
		t := clockOutAt.Time
		item.ClockOutAt = &t
	}
	if clockInNote.Valid {
		item.ClockInNote = &clockInNote.String
	}
	if clockOutNote.Valid {
		item.ClockOutNote = &clockOutNote.String
	}
	if deletedAt.Valid {
		t := deletedAt.Time
		item.DeletedAt = &t
	}
	return item, nil
}

func scanPlanningTimeEntry(rows scannableRows) (*PlanningTimeEntry, error) {
	return scanPlanningTimeEntryRow(rows)
}
