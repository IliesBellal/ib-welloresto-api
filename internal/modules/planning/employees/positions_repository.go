package employees

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/database/dbx"
)

func (r *Repository) ListEmployeePositions(ctx context.Context, merchantID string, filters EmployeePositionListFilters) ([]EmployeePosition, error) {
	db := dbx.GetDB(ctx, r.db)
	query := `
		SELECT p.id, p.merchant_id, p.label, p.color, p.sort_order, p.active, COUNT(e.id) AS employee_count,
			p.created_at, p.updated_at, p.deleted_at
		FROM planning_positions p
		LEFT JOIN employees e ON e.position_id = p.id AND e.merchant_id = p.merchant_id AND e.enabled = TRUE
		WHERE p.merchant_id = ? AND p.enabled = TRUE
	`
	args := []interface{}{merchantID}
	if strings.TrimSpace(filters.Search) != "" {
		query += ` AND p.label LIKE ?`
		args = append(args, "%"+strings.TrimSpace(filters.Search)+"%")
	}
	if filters.Active != nil {
		query += ` AND p.active = ?`
		args = append(args, *filters.Active)
	}
	query += ` GROUP BY p.id, p.merchant_id, p.label, p.color, p.sort_order, p.active, p.created_at, p.updated_at, p.deleted_at`
	query += ` ORDER BY p.sort_order ASC, p.label ASC`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]EmployeePosition, 0)
	for rows.Next() {
		item, scanErr := scanEmployeePosition(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetEmployeePositionByID(ctx context.Context, merchantID, positionID string) (*EmployeePosition, error) {
	db := dbx.GetDB(ctx, r.db)
	row := db.QueryRowContext(ctx, `
		SELECT p.id, p.merchant_id, p.label, p.color, p.sort_order, p.active, COUNT(e.id) AS employee_count,
			p.created_at, p.updated_at, p.deleted_at
		FROM planning_positions p
		LEFT JOIN employees e ON e.position_id = p.id AND e.merchant_id = p.merchant_id AND e.enabled = TRUE
		WHERE p.merchant_id = ? AND p.id = ? AND p.enabled = TRUE
		GROUP BY p.id, p.merchant_id, p.label, p.color, p.sort_order, p.active, p.created_at, p.updated_at, p.deleted_at
	`, merchantID, positionID)
	return scanEmployeePositionRow(row)
}

func (r *Repository) GetEmployeePositionByLabel(ctx context.Context, merchantID, label, excludeID string) (*EmployeePosition, error) {
	db := dbx.GetDB(ctx, r.db)
	query := `
		SELECT p.id, p.merchant_id, p.label, p.color, p.sort_order, p.active, COUNT(e.id) AS employee_count,
			p.created_at, p.updated_at, p.deleted_at
		FROM planning_positions p
		LEFT JOIN employees e ON e.position_id = p.id AND e.merchant_id = p.merchant_id AND e.enabled = TRUE
		WHERE p.merchant_id = ? AND LOWER(p.label) = LOWER(?) AND p.enabled = TRUE
	`
	args := []interface{}{merchantID, strings.TrimSpace(label)}
	if strings.TrimSpace(excludeID) != "" {
		query += ` AND p.id <> ?`
		args = append(args, strings.TrimSpace(excludeID))
	}
	query += ` GROUP BY p.id, p.merchant_id, p.label, p.color, p.sort_order, p.active, p.created_at, p.updated_at, p.deleted_at`
	row := db.QueryRowContext(ctx, query, args...)
	return scanEmployeePositionRow(row)
}

func (r *Repository) NextEmployeePositionSortOrder(ctx context.Context, merchantID string) (int, error) {
	db := dbx.GetDB(ctx, r.db)
	var next int
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(sort_order), 0) + 10
		FROM planning_positions
		WHERE merchant_id = ? AND enabled = TRUE
	`, merchantID).Scan(&next); err != nil {
		return 0, err
	}
	return next, nil
}

func (r *Repository) CreateEmployeePosition(ctx context.Context, merchantID string, req EmployeePositionCreateRequest) (*EmployeePosition, error) {
	db := dbx.GetDB(ctx, r.db)
	now := time.Now().UTC()
	position := EmployeePosition{
		ID:         helpers.GeneratePrefixedID(helpers.PlanningPositionIDPrefix),
		MerchantID: merchantID,
		Label:      strings.TrimSpace(req.Label),
		Color:      req.Color,
		SortOrder:  0,
		Active:     true,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if req.SortOrder != nil {
		position.SortOrder = *req.SortOrder
	}
	if req.Active != nil {
		position.Active = *req.Active
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO planning_positions (
			id, merchant_id, label, color, sort_order, active, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, position.ID, position.MerchantID, position.Label, position.Color, position.SortOrder, position.Active, position.CreatedAt, position.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &position, nil
}

func (r *Repository) UpdateEmployeePosition(ctx context.Context, merchantID, positionID string, position EmployeePosition) (*EmployeePosition, error) {
	db := dbx.GetDB(ctx, r.db)
	position.UpdatedAt = time.Now().UTC()
	res, err := db.ExecContext(ctx, `
		UPDATE planning_positions
		SET label = ?, color = ?, sort_order = ?, active = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = TRUE
	`, position.Label, position.Color, position.SortOrder, position.Active, position.UpdatedAt, merchantID, positionID)
	if err != nil {
		return nil, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, sql.ErrNoRows
	}
	position.ID = positionID
	position.MerchantID = merchantID
	position.EmployeeCount = 0
	return &position, nil
}

func (r *Repository) CountEmployeesByPositionID(ctx context.Context, merchantID, positionID string) (int, error) {
	db := dbx.GetDB(ctx, r.db)
	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM employees
		WHERE merchant_id = ? AND position_id = ? AND enabled = TRUE
	`, merchantID, positionID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (r *Repository) SoftDeleteEmployeePosition(ctx context.Context, merchantID, positionID string) error {
	db := dbx.GetDB(ctx, r.db)
	now := time.Now().UTC()
	res, err := db.ExecContext(ctx, `
		UPDATE planning_positions
		SET active = FALSE, enabled = FALSE, deleted_at = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = TRUE
	`, now, now, merchantID, positionID)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func scanEmployeePositionRow(row scannable) (*EmployeePosition, error) {
	item := &EmployeePosition{}
	var active sql.NullBool
	var employeeCount sql.NullInt64
	var deletedAt sql.NullTime
	if err := row.Scan(&item.ID, &item.MerchantID, &item.Label, &item.Color, &item.SortOrder, &active, &employeeCount, &item.CreatedAt, &item.UpdatedAt, &deletedAt); err != nil {
		return nil, err
	}
	item.Active = active.Bool
	item.EmployeeCount = int(employeeCount.Int64)
	if deletedAt.Valid {
		t := deletedAt.Time
		item.DeletedAt = &t
	}
	return item, nil
}

func scanEmployeePosition(rows scannableRows) (*EmployeePosition, error) {
	item := &EmployeePosition{}
	var active sql.NullBool
	var employeeCount sql.NullInt64
	var deletedAt sql.NullTime
	if err := rows.Scan(&item.ID, &item.MerchantID, &item.Label, &item.Color, &item.SortOrder, &active, &employeeCount, &item.CreatedAt, &item.UpdatedAt, &deletedAt); err != nil {
		return nil, err
	}
	item.Active = active.Bool
	item.EmployeeCount = int(employeeCount.Int64)
	if deletedAt.Valid {
		t := deletedAt.Time
		item.DeletedAt = &t
	}
	return item, nil
}
