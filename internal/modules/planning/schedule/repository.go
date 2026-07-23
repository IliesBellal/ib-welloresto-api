package schedule

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/utils/dbutils"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListPlanningWeeks(ctx context.Context, merchantID string) ([]PlanningWeek, error) {
	db := dbx.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `
		SELECT id, merchant_id, label, start_date, end_date, status, published_at, notes, created_at, updated_at, deleted_at
		FROM planning_weeks
		WHERE merchant_id = ? AND enabled = TRUE
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
	db := dbx.GetDB(ctx, r.db)
	row := db.QueryRowContext(ctx, `
		SELECT id, merchant_id, label, start_date, end_date, status, published_at, notes, created_at, updated_at, deleted_at
		FROM planning_weeks
		WHERE merchant_id = ? AND id = ? AND enabled = TRUE
		LIMIT 1
	`, merchantID, weekID)
	return scanPlanningWeekRow(row)
}

func (r *Repository) GetPlanningWeekByStartDate(ctx context.Context, merchantID string, startDate time.Time, excludeWeekID string) (*PlanningWeek, error) {
	db := dbx.GetDB(ctx, r.db)
	query := `
		SELECT id, merchant_id, label, start_date, end_date, status, published_at, notes, created_at, updated_at, deleted_at
		FROM planning_weeks
		WHERE merchant_id = ? AND start_date = ? AND enabled = TRUE
	`
	args := []interface{}{merchantID, startDate.Format("2006-01-02")}
	if strings.TrimSpace(excludeWeekID) != "" {
		query += ` AND id <> ?`
		args = append(args, strings.TrimSpace(excludeWeekID))
	}
	query += ` ORDER BY created_at DESC LIMIT 1`
	row := db.QueryRowContext(ctx, query, args...)
	return scanPlanningWeekRow(row)
}

func (r *Repository) CreatePlanningWeek(ctx context.Context, merchantID string, week PlanningWeek) (*PlanningWeek, error) {
	db := dbx.GetDB(ctx, r.db)
	now := time.Now().UTC()
	week.ID = helpers.GeneratePrefixedID(helpers.PlanningWeekIDPrefix)
	week.MerchantID = merchantID
	week.CreatedAt = now
	week.UpdatedAt = now
	_, err := db.ExecContext(ctx, `
		INSERT INTO planning_weeks (
			id, merchant_id, label, start_date, end_date, status, published_at, notes, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, TRUE, ?, ?)
	`, week.ID, week.MerchantID, week.Label, week.StartDate, week.EndDate, week.Status, week.PublishedAt, week.Notes, week.CreatedAt, week.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &week, nil
}

func (r *Repository) UpdatePlanningWeek(ctx context.Context, merchantID, weekID string, week PlanningWeek) (*PlanningWeek, error) {
	db := dbx.GetDB(ctx, r.db)
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
	if week.Status == "draft" {
		current.PublishedAt = nil
	}
	if week.Status == "published" {
		if week.PublishedAt != nil {
			current.PublishedAt = week.PublishedAt
		} else if current.PublishedAt == nil {
			now := time.Now().UTC()
			current.PublishedAt = &now
		}
	}
	if week.Notes != nil {
		current.Notes = week.Notes
	}
	current.UpdatedAt = time.Now().UTC()
	_, err = db.ExecContext(ctx, `
		UPDATE planning_weeks
		SET label = ?, start_date = ?, end_date = ?, status = ?, published_at = ?, notes = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = TRUE
	`, current.Label, current.StartDate, current.EndDate, current.Status, current.PublishedAt, current.Notes, current.UpdatedAt, merchantID, weekID)
	if err != nil {
		return nil, err
	}
	return current, nil
}

func (r *Repository) PublishPlanningWeek(ctx context.Context, merchantID, weekID string, publishedAt time.Time) (*PlanningWeek, error) {
	db := dbx.GetDB(ctx, r.db)
	res, err := db.ExecContext(ctx, `
		UPDATE planning_weeks
		SET status = 'published', published_at = COALESCE(published_at, ?), updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = TRUE
	`, publishedAt, publishedAt, merchantID, weekID)
	if err != nil {
		return nil, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, sql.ErrNoRows
	}
	return r.GetPlanningWeekByID(ctx, merchantID, weekID)
}

func (r *Repository) UnpublishPlanningWeek(ctx context.Context, merchantID, weekID string) (*PlanningWeek, error) {
	db := dbx.GetDB(ctx, r.db)
	now := time.Now().UTC()
	res, err := db.ExecContext(ctx, `
		UPDATE planning_weeks
		SET status = 'draft', published_at = NULL, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = TRUE
	`, now, merchantID, weekID)
	if err != nil {
		return nil, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, sql.ErrNoRows
	}
	return r.GetPlanningWeekByID(ctx, merchantID, weekID)
}

func (r *Repository) SoftDeletePlanningWeek(ctx context.Context, merchantID, weekID string) error {
	return dbutils.RunInTx(ctx, r.db, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, r.db)
		now := time.Now().UTC()

		res, err := db.ExecContext(txCtx, `
			UPDATE planning_weeks
			SET enabled = FALSE, deleted_at = ?, updated_at = ?
			WHERE merchant_id = ? AND id = ? AND enabled = TRUE
		`, now, now, merchantID, weekID)
		if err != nil {
			return err
		}
		if affected, _ := res.RowsAffected(); affected == 0 {
			return sql.ErrNoRows
		}

		_, err = db.ExecContext(txCtx, `
			UPDATE planning_shifts
			SET enabled = FALSE, deleted_at = ?, updated_at = ?
			WHERE merchant_id = ? AND week_id = ? AND enabled = TRUE
		`, now, now, merchantID, weekID)
		return err
	})
}

func (r *Repository) ListPlanningShifts(ctx context.Context, merchantID, weekID string) ([]PlanningShift, error) {
	db := dbx.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `
		SELECT id, merchant_id, week_id, employee_id, position_id, title, shift_date, start_time, end_time, break_minutes,
			position, location, notes, status, created_at, updated_at, deleted_at
		FROM planning_shifts
		WHERE merchant_id = ? AND week_id = ? AND enabled = TRUE
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

func (r *Repository) ListPlanningShiftsByDateRange(ctx context.Context, merchantID string, startDate, endDate time.Time) ([]PlanningShift, error) {
	db := dbx.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `
		SELECT id, merchant_id, week_id, employee_id, position_id, title, shift_date, start_time, end_time, break_minutes,
			position, location, notes, status, created_at, updated_at, deleted_at
		FROM planning_shifts
		WHERE merchant_id = ? AND enabled = TRUE AND shift_date >= ? AND shift_date <= ?
		ORDER BY shift_date ASC, start_time ASC, created_at ASC
	`, merchantID, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]PlanningShift, 0)
	for rows.Next() {
		item, scanErr := scanPlanningShift(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) ListPlanningShiftsTeamWeekView(ctx context.Context, merchantID, weekID string) ([]PlanningShiftTeamWeekView, error) {
	db := dbx.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `
		SELECT s.id, s.merchant_id, s.week_id, s.employee_id,
			NULLIF(TRIM(CONCAT(COALESCE(e.first_name, ''), ' ', COALESCE(e.last_name, ''))), '') AS employee_name,
			s.position_id, s.title, s.shift_date, s.start_time, s.end_time, s.break_minutes,
			s.position, p.color, s.location, s.notes, s.status, s.created_at, s.updated_at, s.deleted_at
		FROM planning_shifts s
		LEFT JOIN employees e ON e.id = s.employee_id AND e.merchant_id = s.merchant_id AND e.enabled = TRUE
		LEFT JOIN planning_positions p ON p.id = s.position_id AND p.merchant_id = s.merchant_id AND p.enabled = TRUE
		WHERE s.merchant_id = ? AND s.week_id = ? AND s.enabled = TRUE
		ORDER BY s.shift_date ASC, s.start_time ASC, s.created_at ASC
	`, merchantID, weekID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]PlanningShiftTeamWeekView, 0)
	for rows.Next() {
		item, scanErr := scanPlanningShiftTeamWeekView(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetPlanningShiftByID(ctx context.Context, merchantID, shiftID string) (*PlanningShift, error) {
	db := dbx.GetDB(ctx, r.db)
	row := db.QueryRowContext(ctx, `
		SELECT id, merchant_id, week_id, employee_id, position_id, title, shift_date, start_time, end_time, break_minutes,
			position, location, notes, status, created_at, updated_at, deleted_at
		FROM planning_shifts
		WHERE merchant_id = ? AND id = ? AND enabled = TRUE
		LIMIT 1
	`, merchantID, shiftID)
	return scanPlanningShiftRow(row)
}

func (r *Repository) ListEmployeeShiftsByDate(ctx context.Context, merchantID, employeeID string, shiftDate time.Time, excludeShiftID string) ([]PlanningShift, error) {
	db := dbx.GetDB(ctx, r.db)
	query := `
		SELECT id, merchant_id, week_id, employee_id, position_id, title, shift_date, start_time, end_time, break_minutes,
			position, location, notes, status, created_at, updated_at, deleted_at
		FROM planning_shifts
		WHERE merchant_id = ? AND employee_id = ? AND shift_date = ? AND enabled = TRUE
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
	db := dbx.GetDB(ctx, r.db)
	now := time.Now().UTC()
	shift.ID = helpers.GeneratePrefixedID(helpers.PlanningShiftIDPrefix)
	shift.MerchantID = merchantID
	shift.CreatedAt = now
	shift.UpdatedAt = now
	_, err := db.ExecContext(ctx, `
		INSERT INTO planning_shifts (
			id, merchant_id, week_id, employee_id, position_id, shift_date, start_time, end_time, break_minutes,
			position, location, notes, status, title, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, TRUE, ?, ?)
	`, shift.ID, shift.MerchantID, shift.WeekID, shift.EmployeeID, shift.PositionID, shift.ShiftDate, shift.StartTime, shift.EndTime, shift.BreakMinutes, shift.Position, shift.Location, shift.Notes, shift.Status, shift.Title, shift.CreatedAt, shift.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &shift, nil
}

func (r *Repository) UpdatePlanningShift(ctx context.Context, merchantID, shiftID string, shift PlanningShift) (*PlanningShift, error) {
	db := dbx.GetDB(ctx, r.db)
	shift.UpdatedAt = time.Now().UTC()
	res, err := db.ExecContext(ctx, `
		UPDATE planning_shifts
		SET week_id = ?, employee_id = ?, position_id = ?, title = ?, shift_date = ?, start_time = ?, end_time = ?, break_minutes = ?,
			position = ?, location = ?, notes = ?, status = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = TRUE
	`, shift.WeekID, shift.EmployeeID, shift.PositionID, shift.Title, shift.ShiftDate, shift.StartTime, shift.EndTime, shift.BreakMinutes, shift.Position, shift.Location, shift.Notes, shift.Status, shift.UpdatedAt, merchantID, shiftID)
	if err != nil {
		return nil, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, sql.ErrNoRows
	}
	return &shift, nil
}

func (r *Repository) SoftDeletePlanningShift(ctx context.Context, merchantID, shiftID string) error {
	db := dbx.GetDB(ctx, r.db)
	now := time.Now().UTC()
	res, err := db.ExecContext(ctx, `
		UPDATE planning_shifts
		SET enabled = FALSE, deleted_at = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = TRUE
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
	var publishedAt sql.NullTime
	var deletedAt sql.NullTime
	if err := row.Scan(&week.ID, &week.MerchantID, &label, &week.StartDate, &week.EndDate, &week.Status, &publishedAt, &notes, &week.CreatedAt, &week.UpdatedAt, &deletedAt); err != nil {
		return nil, err
	}
	if label.Valid {
		week.Label = &label.String
	}
	if publishedAt.Valid {
		t := publishedAt.Time
		week.PublishedAt = &t
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
	var employeeID, positionID, position, location, notes sql.NullString
	var deletedAt sql.NullTime
	if err := row.Scan(&shift.ID, &shift.MerchantID, &shift.WeekID, &employeeID, &positionID, &shift.Title, &shift.ShiftDate, &shift.StartTime, &shift.EndTime, &shift.BreakMinutes, &position, &location, &notes, &shift.Status, &shift.CreatedAt, &shift.UpdatedAt, &deletedAt); err != nil {
		return nil, err
	}
	if employeeID.Valid {
		shift.EmployeeID = &employeeID.String
	}
	if positionID.Valid {
		shift.PositionID = &positionID.String
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

func scanPlanningShiftTeamWeekViewRow(row scannable) (*PlanningShiftTeamWeekView, error) {
	shift := &PlanningShiftTeamWeekView{}
	var employeeID, employeeName, positionID, position, positionColor, location, notes sql.NullString
	var deletedAt sql.NullTime
	if err := row.Scan(
		&shift.ID,
		&shift.MerchantID,
		&shift.WeekID,
		&employeeID,
		&employeeName,
		&positionID,
		&shift.Title,
		&shift.ShiftDate,
		&shift.StartTime,
		&shift.EndTime,
		&shift.BreakMinutes,
		&position,
		&positionColor,
		&location,
		&notes,
		&shift.Status,
		&shift.CreatedAt,
		&shift.UpdatedAt,
		&deletedAt,
	); err != nil {
		return nil, err
	}
	if employeeID.Valid {
		shift.EmployeeID = &employeeID.String
	}
	if employeeName.Valid {
		shift.EmployeeName = &employeeName.String
	}
	if positionID.Valid {
		shift.PositionID = &positionID.String
	}
	if position.Valid {
		shift.Position = &position.String
	}
	if positionColor.Valid {
		shift.PositionColor = &positionColor.String
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

func scanPlanningShiftTeamWeekView(rows scannableRows) (*PlanningShiftTeamWeekView, error) {
	return scanPlanningShiftTeamWeekViewRow(rows)
}
