package shifttemplates

import (
	"context"
	"database/sql"
	"time"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/database/dbx"
)

type Repository struct {
	db *sql.DB
}

// plnTimeHHMM formate une colonne time en HH:MM selon le dialecte
// (TIME_FORMAT n'existe pas en Postgres).
func plnTimeHHMM(col string) string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "to_char(" + col + ", 'HH24:MI')"
	}
	return "TIME_FORMAT(" + col + ", '%H:%i')"
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListShiftTemplates(ctx context.Context, merchantID string) ([]ShiftTemplate, error) {
	db := dbx.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `
		SELECT id, label, ` + plnTimeHHMM("start_time") + ` AS start_time, ` + plnTimeHHMM("end_time") + ` AS end_time,
			break_minutes, position_id, color, sort_order, active, created_at, updated_at
		FROM planning_shift_templates
		WHERE merchant_id = ?
		ORDER BY sort_order ASC, label ASC, created_at ASC
	`, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ShiftTemplate, 0)
	for rows.Next() {
		item, scanErr := scanShiftTemplate(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetShiftTemplateByID(ctx context.Context, merchantID, templateID string) (*ShiftTemplate, error) {
	db := dbx.GetDB(ctx, r.db)
	row := db.QueryRowContext(ctx, `
		SELECT id, label, ` + plnTimeHHMM("start_time") + ` AS start_time, ` + plnTimeHHMM("end_time") + ` AS end_time,
			break_minutes, position_id, color, sort_order, active, created_at, updated_at
		FROM planning_shift_templates
		WHERE merchant_id = ? AND id = ?
		LIMIT 1
	`, merchantID, templateID)
	return scanShiftTemplateRow(row)
}

func (r *Repository) NextShiftTemplateSortOrder(ctx context.Context, merchantID string) (int, error) {
	db := dbx.GetDB(ctx, r.db)
	var next int
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(sort_order), -1) + 1
		FROM planning_shift_templates
		WHERE merchant_id = ?
	`, merchantID).Scan(&next); err != nil {
		return 0, err
	}
	return next, nil
}

func (r *Repository) CreateShiftTemplate(ctx context.Context, merchantID string, template ShiftTemplate) (*ShiftTemplate, error) {
	db := dbx.GetDB(ctx, r.db)
	now := time.Now().UTC()
	template.ID = helpers.GeneratePrefixedID(helpers.PlanningShiftTemplateIDPrefix)
	template.CreatedAt = now
	template.UpdatedAt = now
	_, err := db.ExecContext(ctx, `
		INSERT INTO planning_shift_templates (
			id, merchant_id, label, start_time, end_time, break_minutes, position_id, color, sort_order, active, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, template.ID, merchantID, template.Label, template.StartTime, template.EndTime, template.BreakMinutes, template.PositionID, template.Color, template.SortOrder, template.Active, template.CreatedAt, template.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &template, nil
}

func (r *Repository) UpdateShiftTemplate(ctx context.Context, merchantID, templateID string, template ShiftTemplate) (*ShiftTemplate, error) {
	db := dbx.GetDB(ctx, r.db)
	template.UpdatedAt = time.Now().UTC()
	res, err := db.ExecContext(ctx, `
		UPDATE planning_shift_templates
		SET label = ?, start_time = ?, end_time = ?, break_minutes = ?, position_id = ?, color = ?, sort_order = ?, active = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ?
	`, template.Label, template.StartTime, template.EndTime, template.BreakMinutes, template.PositionID, template.Color, template.SortOrder, template.Active, template.UpdatedAt, merchantID, templateID)
	if err != nil {
		return nil, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, sql.ErrNoRows
	}
	template.ID = templateID
	return &template, nil
}

type scannable interface {
	Scan(dest ...any) error
}

type scannableRows interface {
	Scan(dest ...any) error
}

func scanShiftTemplateRow(row scannable) (*ShiftTemplate, error) {
	item := &ShiftTemplate{}
	var positionID sql.NullString
	var active sql.NullBool
	if err := row.Scan(&item.ID, &item.Label, &item.StartTime, &item.EndTime, &item.BreakMinutes, &positionID, &item.Color, &item.SortOrder, &active, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	if positionID.Valid {
		item.PositionID = &positionID.String
	}
	item.Active = active.Bool
	return item, nil
}

func scanShiftTemplate(rows scannableRows) (*ShiftTemplate, error) {
	return scanShiftTemplateRow(rows)
}
