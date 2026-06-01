package weektemplates

import (
	"context"
	"database/sql"
	"fmt"
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

func (r *Repository) ListWeekTemplates(ctx context.Context, merchantID string) ([]WeekTemplate, error) {
	query := `
		SELECT wt.id, wt.merchant_id, wt.label, wt.notes, wt.active, COUNT(wts.id) AS shift_count, wt.created_at, wt.updated_at
		FROM planning_week_templates wt
		LEFT JOIN planning_week_template_shifts wts ON wts.week_template_id = wt.id
		WHERE wt.merchant_id = ?
		GROUP BY wt.id, wt.merchant_id, wt.label, wt.notes, wt.active, wt.created_at, wt.updated_at
		ORDER BY wt.created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, merchantID)
	if err != nil {
		return nil, fmt.Errorf("list week templates: %w", err)
	}
	defer rows.Close()

	templates := make([]WeekTemplate, 0)
	for rows.Next() {
		template, err := scanWeekTemplate(rows)
		if err != nil {
			return nil, err
		}
		templates = append(templates, template)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate week templates: %w", err)
	}

	return templates, nil
}

func (r *Repository) GetWeekTemplateByID(ctx context.Context, merchantID, id string) (*WeekTemplate, error) {
	query := `
		SELECT wt.id, wt.merchant_id, wt.label, wt.notes, wt.active, COUNT(wts.id) AS shift_count, wt.created_at, wt.updated_at
		FROM planning_week_templates wt
		LEFT JOIN planning_week_template_shifts wts ON wts.week_template_id = wt.id
		WHERE wt.merchant_id = ? AND wt.id = ?
		GROUP BY wt.id, wt.merchant_id, wt.label, wt.notes, wt.active, wt.created_at, wt.updated_at
		LIMIT 1
	`

	row := r.db.QueryRowContext(ctx, query, merchantID, id)
	tpl, err := scanWeekTemplate(row)
	if err != nil {
		return nil, err
	}
	return &tpl, nil
}

func (r *Repository) ListWeekTemplateShifts(ctx context.Context, merchantID, weekTemplateID string) ([]WeekTemplateShift, error) {
	query := `
		SELECT s.id,
			s.week_template_id,
			s.day_of_week,
			s.employee_id,
			s.position_id,
			s.title,
			TIME_FORMAT(s.start_time, '%H:%i') AS start_time,
			TIME_FORMAT(s.end_time, '%H:%i') AS end_time,
			s.break_minutes,
			s.location,
			s.notes,
			s.created_at,
			s.updated_at
		FROM planning_week_template_shifts s
		INNER JOIN planning_week_templates wt ON wt.id = s.week_template_id
		WHERE wt.merchant_id = ? AND s.week_template_id = ?
		ORDER BY s.day_of_week ASC, s.start_time ASC, s.created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query, merchantID, weekTemplateID)
	if err != nil {
		return nil, fmt.Errorf("list week template shifts: %w", err)
	}
	defer rows.Close()

	shifts := make([]WeekTemplateShift, 0)
	for rows.Next() {
		shift, err := scanWeekTemplateShift(rows)
		if err != nil {
			return nil, err
		}
		shifts = append(shifts, shift)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate week template shifts: %w", err)
	}

	return shifts, nil
}

func (r *Repository) CreateWeekTemplateWithShifts(ctx context.Context, template WeekTemplate, shifts []WeekTemplateShift) error {
	return dbutils.RunInTx(ctx, r.db, func(txCtx context.Context) error {
		db := dbutils.GetDB(txCtx, r.db)
		if err := r.insertWeekTemplate(txCtx, db, template); err != nil {
			return err
		}
		for _, shift := range shifts {
			if err := r.insertWeekTemplateShift(txCtx, db, shift); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Repository) UpdateWeekTemplateWithOptionalShifts(ctx context.Context, merchantID, id string, template WeekTemplate, replaceShifts bool, shifts []WeekTemplateShift) error {
	return dbutils.RunInTx(ctx, r.db, func(txCtx context.Context) error {
		db := dbutils.GetDB(txCtx, r.db)
		query := `
			UPDATE planning_week_templates
			SET label = ?, notes = ?, active = ?, updated_at = NOW()
			WHERE merchant_id = ? AND id = ?
		`
		result, err := db.ExecContext(txCtx, query, template.Label, template.Notes, template.Active, merchantID, id)
		if err != nil {
			return fmt.Errorf("update week template: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("week template rows affected: %w", err)
		}
		if affected == 0 {
			return sql.ErrNoRows
		}

		if !replaceShifts {
			return nil
		}

		if _, err := db.ExecContext(txCtx, `DELETE FROM planning_week_template_shifts WHERE week_template_id = ?`, id); err != nil {
			return fmt.Errorf("delete week template shifts: %w", err)
		}
		for _, shift := range shifts {
			if err := r.insertWeekTemplateShift(txCtx, db, shift); err != nil {
				return err
			}
		}

		return nil
	})
}

func (r *Repository) SoftDeleteWeekTemplate(ctx context.Context, merchantID, id string) error {
	query := `
		UPDATE planning_week_templates
		SET active = 0, updated_at = NOW()
		WHERE merchant_id = ? AND id = ?
	`
	result, err := r.db.ExecContext(ctx, query, merchantID, id)
	if err != nil {
		return fmt.Errorf("soft delete week template: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("soft delete rows affected: %w", err)
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *Repository) insertWeekTemplate(ctx context.Context, db dbutils.DBTX, template WeekTemplate) error {
	query := `
		INSERT INTO planning_week_templates (id, merchant_id, label, notes, active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, NOW(), NOW())
	`
	_, err := db.ExecContext(ctx, query, template.ID, template.MerchantID, template.Label, template.Notes, template.Active)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return fmt.Errorf("insert week template duplicate: %w", err)
		}
		return fmt.Errorf("insert week template: %w", err)
	}
	return nil
}

func (r *Repository) insertWeekTemplateShift(ctx context.Context, db dbutils.DBTX, shift WeekTemplateShift) error {
	query := `
		INSERT INTO planning_week_template_shifts (
			id,
			week_template_id,
			day_of_week,
			employee_id,
			position_id,
			title,
			start_time,
			end_time,
			break_minutes,
			location,
			notes,
			created_at,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())
	`

	if shift.ID == "" {
		shift.ID = helpers.GeneratePrefixedID(helpers.PlanningWeekTemplateShiftIDPrefix)
	}
	_, err := db.ExecContext(
		ctx,
		query,
		shift.ID,
		shift.WeekTemplateID,
		shift.DayOfWeek,
		shift.EmployeeID,
		shift.PositionID,
		shift.Title,
		shift.StartTime,
		shift.EndTime,
		shift.BreakMinutes,
		shift.Location,
		shift.Notes,
	)
	if err != nil {
		return fmt.Errorf("insert week template shift: %w", err)
	}

	return nil
}

type weekTemplateScanner interface {
	Scan(dest ...interface{}) error
}

func scanWeekTemplate(scanner weekTemplateScanner) (WeekTemplate, error) {
	var tpl WeekTemplate
	var notes sql.NullString
	if err := scanner.Scan(
		&tpl.ID,
		&tpl.MerchantID,
		&tpl.Label,
		&notes,
		&tpl.Active,
		&tpl.ShiftCount,
		&tpl.CreatedAt,
		&tpl.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return WeekTemplate{}, sql.ErrNoRows
		}
		return WeekTemplate{}, fmt.Errorf("scan week template: %w", err)
	}
	tpl.Notes = nullStringPtr(notes)
	return tpl, nil
}

func scanWeekTemplateShift(scanner weekTemplateScanner) (WeekTemplateShift, error) {
	var shift WeekTemplateShift
	var employeeID sql.NullString
	var positionID sql.NullString
	var title sql.NullString
	var location sql.NullString
	var notes sql.NullString
	if err := scanner.Scan(
		&shift.ID,
		&shift.WeekTemplateID,
		&shift.DayOfWeek,
		&employeeID,
		&positionID,
		&title,
		&shift.StartTime,
		&shift.EndTime,
		&shift.BreakMinutes,
		&location,
		&notes,
		&shift.CreatedAt,
		&shift.UpdatedAt,
	); err != nil {
		return WeekTemplateShift{}, fmt.Errorf("scan week template shift: %w", err)
	}
	shift.EmployeeID = nullStringPtr(employeeID)
	shift.PositionID = nullStringPtr(positionID)
	shift.Title = nullStringPtr(title)
	shift.Location = nullStringPtr(location)
	shift.Notes = nullStringPtr(notes)
	return shift, nil
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	v := value.String
	return &v
}

func nowUTC() time.Time { return time.Now().UTC() }
