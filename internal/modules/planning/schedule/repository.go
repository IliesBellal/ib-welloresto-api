package schedule

import (
	"context"
	"database/sql"
	"strings"
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

func (r *Repository) ListPlanningWeeks(ctx context.Context, merchantID string) ([]PlanningWeek, error) {
	db := dbutils.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `
		SELECT id, merchant_id, label, start_date, end_date, status, notes, created_at, updated_at, deleted_at
		FROM planning_weeks
		WHERE merchant_id = ? AND enabled = 1
		ORDER BY start_date DESC, created_at DESC
	`, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]PlanningWeek, 0)
	for rows.Next() {
		item, err := scanPlanningWeek(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetPlanningWeekByID(ctx context.Context, merchantID, weekID string) (*PlanningWeek, error) {
	db := dbutils.GetDB(ctx, r.db)
	row := db.QueryRowContext(ctx, `
		SELECT id, merchant_id, label, start_date, end_date, status, notes, created_at, updated_at, deleted_at
		FROM planning_weeks
		WHERE merchant_id = ? AND id = ? AND enabled = 1
		LIMIT 1
	`, merchantID, weekID)
	return scanPlanningWeekRow(row)
}

func (r *Repository) CreatePlanningWeek(ctx context.Context, merchantID string, week PlanningWeek) (*PlanningWeek, error) {
	db := dbutils.GetDB(ctx, r.db)
	now := time.Now().UTC()
	week.ID = helpers.GeneratePrefixedID(helpers.PlanningWeekIDPrefix)
	week.MerchantID = merchantID
	week.CreatedAt = now
	week.UpdatedAt = now
	_, err := db.ExecContext(ctx, `
		INSERT INTO planning_weeks (
			id, merchant_id, label, start_date, end_date, status, notes, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`, week.ID, week.MerchantID, week.Label, week.StartDate, week.EndDate, week.Status, week.Notes, week.CreatedAt, week.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &week, nil
}

func (r *Repository) UpdatePlanningWeek(ctx context.Context, merchantID, weekID string, week PlanningWeek) (*PlanningWeek, error) {
	db := dbutils.GetDB(ctx, r.db)
	current, err := r.GetPlanningWeekByID(ctx, merchantID, weekID)
	if err != nil {
		return nil, err
	}
	if week.Label != nil {
		current.Label = week.Label
	}
	if !week.StartDate.IsZero() {
		current.StartDate = week.StartDate
	}
	if !week.EndDate.IsZero() {
		current.EndDate = week.EndDate
	}
	if strings.TrimSpace(week.Status) != "" {
		current.Status = strings.TrimSpace(week.Status)
	}
	if week.Notes != nil {
		current.Notes = week.Notes
	}
	current.UpdatedAt = time.Now().UTC()
	_, err = db.ExecContext(ctx, `
		UPDATE planning_weeks
		SET label = ?, start_date = ?, end_date = ?, status = ?, notes = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = 1
	`, current.Label, current.StartDate, current.EndDate, current.Status, current.Notes, current.UpdatedAt, merchantID, weekID)
	if err != nil {
		return nil, err
	}
	return current, nil
}

func (r *Repository) SoftDeletePlanningWeek(ctx context.Context, merchantID, weekID string) error {
	db := dbutils.GetDB(ctx, r.db)
	now := time.Now().UTC()
	res, err := db.ExecContext(ctx, `
		UPDATE planning_weeks
		SET enabled = 0, deleted_at = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = 1
	`, now, now, merchantID, weekID)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *Repository) ListPlanningShifts(ctx context.Context, merchantID, weekID string) ([]PlanningShift, error) {
	db := dbutils.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `
		SELECT id, merchant_id, week_id, employee_id, title, shift_date, start_time, end_time, break_minutes,
			position, location, notes, status, created_at, updated_at, deleted_at
		FROM planning_shifts
		WHERE merchant_id = ? AND week_id = ? AND enabled = 1
		ORDER BY shift_date ASC, start_time ASC, created_at ASC
	`, merchantID, weekID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]PlanningShift, 0)
	for rows.Next() {
		item, err := scanPlanningShift(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetPlanningShiftByID(ctx context.Context, merchantID, shiftID string) (*PlanningShift, error) {
	db := dbutils.GetDB(ctx, r.db)
	row := db.QueryRowContext(ctx, `
		SELECT id, merchant_id, week_id, employee_id, title, shift_date, start_time, end_time, break_minutes,
			position, location, notes, status, created_at, updated_at, deleted_at
		FROM planning_shifts
		WHERE merchant_id = ? AND id = ? AND enabled = 1
		LIMIT 1
	`, merchantID, shiftID)
	return scanPlanningShiftRow(row)
}

func (r *Repository) ListEmployeeShiftsByDate(ctx context.Context, merchantID, employeeID string, shiftDate time.Time, excludeShiftID string) ([]PlanningShift, error) {
	db := dbutils.GetDB(ctx, r.db)
	query := `
		SELECT id, merchant_id, week_id, employee_id, title, shift_date, start_time, end_time, break_minutes,
			position, location, notes, status, created_at, updated_at, deleted_at
		FROM planning_shifts
		WHERE merchant_id = ? AND employee_id = ? AND shift_date = ? AND enabled = 1
	`
	args := []interface{}{merchantID, employeeID, shiftDate.Format("2006-01-02")}
	if strings.TrimSpace(excludeShiftID) != "" {
		query += ` AND id <> ?`
		args = append(args, excludeShiftID)
	}
	query += ` ORDER BY start_time ASC, created_at ASC`
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]PlanningShift, 0)
	for rows.Next() {
		item, err := scanPlanningShift(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) CreatePlanningShift(ctx context.Context, merchantID string, shift PlanningShift) (*PlanningShift, error) {
	db := dbutils.GetDB(ctx, r.db)
	now := time.Now().UTC()
	shift.ID = helpers.GeneratePrefixedID(helpers.PlanningShiftIDPrefix)
	shift.MerchantID = merchantID
	shift.CreatedAt = now
	shift.UpdatedAt = now
	_, err := db.ExecContext(ctx, `
		INSERT INTO planning_shifts (
			id, merchant_id, week_id, employee_id, title, shift_date, start_time, end_time, break_minutes,
			position, location, notes, status, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`, shift.ID, shift.MerchantID, shift.WeekID, shift.EmployeeID, shift.Title, shift.ShiftDate, shift.StartTime, shift.EndTime, shift.BreakMinutes, shift.Position, shift.Location, shift.Notes, shift.Status, shift.CreatedAt, shift.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &shift, nil
}

func (r *Repository) UpdatePlanningShift(ctx context.Context, merchantID, shiftID string, shift PlanningShift) (*PlanningShift, error) {
	db := dbutils.GetDB(ctx, r.db)
	shift.UpdatedAt = time.Now().UTC()
	res, err := db.ExecContext(ctx, `
		UPDATE planning_shifts
		SET week_id = ?, employee_id = ?, title = ?, shift_date = ?, start_time = ?, end_time = ?, break_minutes = ?,
			position = ?, location = ?, notes = ?, status = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = 1
	`, shift.WeekID, shift.EmployeeID, shift.Title, shift.ShiftDate, shift.StartTime, shift.EndTime, shift.BreakMinutes, shift.Position, shift.Location, shift.Notes, shift.Status, shift.UpdatedAt, merchantID, shiftID)
	if err != nil {
		return nil, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, sql.ErrNoRows
	}
	return &shift, nil
}

func (r *Repository) SoftDeletePlanningShift(ctx context.Context, merchantID, shiftID string) error {
	db := dbutils.GetDB(ctx, r.db)
	now := time.Now().UTC()
	res, err := db.ExecContext(ctx, `
		UPDATE planning_shifts
		SET enabled = 0, deleted_at = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = 1
	`, now, now, merchantID, shiftID)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type scannable interface {
	Scan(dest ...any) error
}

type scannableRows interface {
	Scan(dest ...any) error
}

func scanPlanningWeekRow(row scannable) (*PlanningWeek, error) {
	week := &PlanningWeek{}
	var label, notes sql.NullString
	var deletedAt sql.NullTime
	if err := row.Scan(&week.ID, &week.MerchantID, &label, &week.StartDate, &week.EndDate, &week.Status, &notes, &week.CreatedAt, &week.UpdatedAt, &deletedAt); err != nil {
		return nil, err
	}
	if label.Valid {
		week.Label = &label.String
	}
	if notes.Valid {
		week.Notes = &notes.String
	}
	if deletedAt.Valid {
		t := deletedAt.Time
		week.DeletedAt = &t
	}
	return week, nil
}

func scanPlanningWeek(rows scannableRows) (*PlanningWeek, error) {
	return scanPlanningWeekRow(rows)
}

func scanPlanningShiftRow(row scannable) (*PlanningShift, error) {
	shift := &PlanningShift{}
	var employeeID, position, location, notes sql.NullString
	var deletedAt sql.NullTime
	if err := row.Scan(&shift.ID, &shift.MerchantID, &shift.WeekID, &employeeID, &shift.Title, &shift.ShiftDate, &shift.StartTime, &shift.EndTime, &shift.BreakMinutes, &position, &location, &notes, &shift.Status, &shift.CreatedAt, &shift.UpdatedAt, &deletedAt); err != nil {
		return nil, err
	}
	if employeeID.Valid {
		shift.EmployeeID = &employeeID.String
	}
	if position.Valid {
		shift.Position = &position.String
	}
	if location.Valid {
		shift.Location = &location.String
	}
	if notes.Valid {
		shift.Notes = &notes.String
	}
	if deletedAt.Valid {
		t := deletedAt.Time
		shift.DeletedAt = &t
	}
	return shift, nil
}

func scanPlanningShift(rows scannableRows) (*PlanningShift, error) {
	return scanPlanningShiftRow(rows)
}
