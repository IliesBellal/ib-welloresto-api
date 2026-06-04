package swaps

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListPlanningShiftSwapRequests(ctx context.Context, merchantID string, filters PlanningShiftSwapRequestListFilters) ([]PlanningShiftSwapRequest, int, error) {
	db := dbutils.GetDB(ctx, r.db)
	countQuery := `
		SELECT COUNT(1)
		FROM planning_shift_swap_requests
		WHERE merchant_id = ? AND enabled = 1
	`
	args := []interface{}{merchantID}
	if strings.TrimSpace(filters.RequesterEmployeeID) != "" {
		countQuery += ` AND requester_employee_id = ?`
		args = append(args, strings.TrimSpace(filters.RequesterEmployeeID))
	}
	if strings.TrimSpace(filters.TargetEmployeeID) != "" {
		countQuery += ` AND target_employee_id = ?`
		args = append(args, strings.TrimSpace(filters.TargetEmployeeID))
	}
	if strings.TrimSpace(filters.Status) != "" {
		countQuery += ` AND status = ?`
		args = append(args, strings.TrimSpace(filters.Status))
	}
	var totalItems int
	if err := db.QueryRowContext(ctx, countQuery, args...).Scan(&totalItems); err != nil {
		return nil, 0, err
	}
	query := `
		SELECT id, merchant_id, requester_employee_id, requester_shift_id, target_employee_id, target_shift_id,
			status, reason, manager_note, requested_by_user_id, processed_by_user_id, processed_at, created_at, updated_at, deleted_at
		FROM planning_shift_swap_requests
		WHERE merchant_id = ? AND enabled = 1
	`
	if strings.TrimSpace(filters.RequesterEmployeeID) != "" {
		query += ` AND requester_employee_id = ?`
	}
	if strings.TrimSpace(filters.TargetEmployeeID) != "" {
		query += ` AND target_employee_id = ?`
	}
	if strings.TrimSpace(filters.Status) != "" {
		query += ` AND status = ?`
	}
	query += ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	dataArgs := append([]interface{}{}, args...)
	dataArgs = append(dataArgs, filters.PageSize, (filters.Page-1)*filters.PageSize)
	rows, err := db.QueryContext(ctx, query, dataArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]PlanningShiftSwapRequest, 0)
	for rows.Next() {
		item, scanErr := scanPlanningShiftSwapRequest(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		items = append(items, *item)
	}
	return items, totalItems, rows.Err()
}

func (r *Repository) ListCurrentEmployeePlanningShiftSwapRequests(ctx context.Context, merchantID, employeeID, status string) ([]PlanningShiftSwapRequestSelfView, error) {
	db := dbutils.GetDB(ctx, r.db)
	query := `
		SELECT ss.id, ss.requester_employee_id,
			NULLIF(TRIM(CONCAT(COALESCE(re.first_name, ''), ' ', COALESCE(re.last_name, ''))), '') AS requester_employee_name,
			ss.requester_shift_id, ss.target_employee_id,
			NULLIF(TRIM(CONCAT(COALESCE(te.first_name, ''), ' ', COALESCE(te.last_name, ''))), '') AS target_employee_name,
			ss.target_shift_id, ss.status, ss.reason, ss.manager_note, ss.processed_at, ss.created_at,
			rs.id, rs.employee_id, rs.position_id, rs.title, rs.shift_date, rs.start_time, rs.end_time, rs.position, rp.color,
			ts.id, ts.employee_id, ts.position_id, ts.title, ts.shift_date, ts.start_time, ts.end_time, ts.position, tp.color
		FROM planning_shift_swap_requests ss
		LEFT JOIN employees re ON re.id = ss.requester_employee_id AND re.merchant_id = ss.merchant_id AND re.enabled = 1
		LEFT JOIN employees te ON te.id = ss.target_employee_id AND te.merchant_id = ss.merchant_id AND te.enabled = 1
		LEFT JOIN planning_shifts rs ON rs.id = ss.requester_shift_id AND rs.merchant_id = ss.merchant_id AND rs.enabled = 1
		LEFT JOIN planning_positions rp ON rp.id = rs.position_id AND rp.merchant_id = rs.merchant_id AND rp.enabled = 1
		LEFT JOIN planning_shifts ts ON ts.id = ss.target_shift_id AND ts.merchant_id = ss.merchant_id AND ts.enabled = 1
		LEFT JOIN planning_positions tp ON tp.id = ts.position_id AND tp.merchant_id = ts.merchant_id AND tp.enabled = 1
		WHERE ss.merchant_id = ? AND ss.enabled = 1 AND (ss.requester_employee_id = ? OR ss.target_employee_id = ?)
	`
	args := []interface{}{merchantID, employeeID, employeeID}
	if strings.TrimSpace(status) != "" {
		query += ` AND ss.status = ?`
		args = append(args, strings.TrimSpace(status))
	}
	query += ` ORDER BY ss.created_at DESC`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]PlanningShiftSwapRequestSelfView, 0)
	for rows.Next() {
		item, scanErr := scanPlanningShiftSwapRequestSelfView(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetPlanningShiftSwapRequestByID(ctx context.Context, merchantID, requestID string) (*PlanningShiftSwapRequest, error) {
	db := dbutils.GetDB(ctx, r.db)
	row := db.QueryRowContext(ctx, `
		SELECT id, merchant_id, requester_employee_id, requester_shift_id, target_employee_id, target_shift_id,
			status, reason, manager_note, requested_by_user_id, processed_by_user_id, processed_at, created_at, updated_at, deleted_at
		FROM planning_shift_swap_requests
		WHERE merchant_id = ? AND id = ? AND enabled = 1
		LIMIT 1
	`, merchantID, requestID)
	return scanPlanningShiftSwapRequestRow(row)
}

func (r *Repository) CreatePlanningShiftSwapRequest(ctx context.Context, merchantID string, request PlanningShiftSwapRequest) (*PlanningShiftSwapRequest, error) {
	db := dbutils.GetDB(ctx, r.db)
	now := time.Now().UTC()
	request.ID = helpers.GeneratePrefixedID(helpers.PlanningShiftSwapRequestIDPrefix)
	request.MerchantID = merchantID
	request.CreatedAt = now
	request.UpdatedAt = now
	_, err := db.ExecContext(ctx, `
		INSERT INTO planning_shift_swap_requests (
			id, merchant_id, requester_employee_id, requester_shift_id, target_employee_id, target_shift_id,
			status, reason, manager_note, requested_by_user_id, processed_by_user_id, processed_at, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`, request.ID, request.MerchantID, request.RequesterEmployeeID, request.RequesterShiftID, request.TargetEmployeeID, request.TargetShiftID, request.Status, request.Reason, request.ManagerNote, request.RequestedByUserID, request.ProcessedByUserID, request.ProcessedAt, request.CreatedAt, request.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &request, nil
}

func (r *Repository) UpdatePlanningShiftSwapRequest(ctx context.Context, merchantID, requestID string, request PlanningShiftSwapRequest) (*PlanningShiftSwapRequest, error) {
	db := dbutils.GetDB(ctx, r.db)
	request.UpdatedAt = time.Now().UTC()
	res, err := db.ExecContext(ctx, `
		UPDATE planning_shift_swap_requests
		SET status = ?, reason = ?, manager_note = ?, processed_by_user_id = ?, processed_at = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = 1
	`, request.Status, request.Reason, request.ManagerNote, request.ProcessedByUserID, request.ProcessedAt, request.UpdatedAt, merchantID, requestID)
	if err != nil {
		return nil, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, sql.ErrNoRows
	}
	return &request, nil
}

func (r *Repository) ApprovePlanningShiftSwapRequest(ctx context.Context, merchantID, requestID string, request PlanningShiftSwapRequest) (*PlanningShiftSwapRequest, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	now := time.Now().UTC()
	if _, err = tx.ExecContext(ctx, `
		UPDATE planning_shifts
		SET employee_id = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = 1
	`, request.TargetEmployeeID, now, merchantID, request.RequesterShiftID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE planning_shifts
		SET employee_id = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = 1
	`, request.RequesterEmployeeID, now, merchantID, request.TargetShiftID); err != nil {
		return nil, err
	}
	request.Status = "approved"
	request.UpdatedAt = now
	if _, err = tx.ExecContext(ctx, `
		UPDATE planning_shift_swap_requests
		SET status = ?, reason = ?, manager_note = ?, processed_by_user_id = ?, processed_at = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = 1
	`, request.Status, request.Reason, request.ManagerNote, request.ProcessedByUserID, request.ProcessedAt, request.UpdatedAt, merchantID, requestID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &request, nil
}

func (r *Repository) SoftDeletePlanningShiftSwapRequest(ctx context.Context, merchantID, requestID string) error {
	db := dbutils.GetDB(ctx, r.db)
	now := time.Now().UTC()
	res, err := db.ExecContext(ctx, `
		UPDATE planning_shift_swap_requests
		SET enabled = 0, deleted_at = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = 1
	`, now, now, merchantID, requestID)
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

func scanPlanningShiftSwapRequestRow(row scannable) (*PlanningShiftSwapRequest, error) {
	item := &PlanningShiftSwapRequest{}
	var reason, managerNote, requestedByUserID, processedByUserID sql.NullString
	var processedAt, deletedAt sql.NullTime
	if err := row.Scan(&item.ID, &item.MerchantID, &item.RequesterEmployeeID, &item.RequesterShiftID, &item.TargetEmployeeID, &item.TargetShiftID, &item.Status, &reason, &managerNote, &requestedByUserID, &processedByUserID, &processedAt, &item.CreatedAt, &item.UpdatedAt, &deletedAt); err != nil {
		return nil, err
	}
	if reason.Valid {
		item.Reason = &reason.String
	}
	if managerNote.Valid {
		item.ManagerNote = &managerNote.String
	}
	if requestedByUserID.Valid {
		item.RequestedByUserID = &requestedByUserID.String
	}
	if processedByUserID.Valid {
		item.ProcessedByUserID = &processedByUserID.String
	}
	if processedAt.Valid {
		t := processedAt.Time
		item.ProcessedAt = &t
	}
	if deletedAt.Valid {
		t := deletedAt.Time
		item.DeletedAt = &t
	}
	return item, nil
}

func scanPlanningShiftSwapRequest(rows scannableRows) (*PlanningShiftSwapRequest, error) {
	return scanPlanningShiftSwapRequestRow(rows)
}

func scanPlanningShiftSwapRequestSelfView(row scannable) (*PlanningShiftSwapRequestSelfView, error) {
	item := &PlanningShiftSwapRequestSelfView{}
	var requesterEmployeeName, targetEmployeeName sql.NullString
	var reason, managerNote sql.NullString
	var processedAt sql.NullTime

	var requesterShiftID sql.NullString
	var requesterShiftEmployeeID, requesterShiftPositionID, requesterShiftTitle sql.NullString
	var requesterShiftDate sql.NullTime
	var requesterShiftStartTime, requesterShiftEndTime, requesterShiftPosition, requesterShiftPositionColor sql.NullString

	var targetShiftID sql.NullString
	var targetShiftEmployeeID, targetShiftPositionID, targetShiftTitle sql.NullString
	var targetShiftDate sql.NullTime
	var targetShiftStartTime, targetShiftEndTime, targetShiftPosition, targetShiftPositionColor sql.NullString

	if err := row.Scan(
		&item.ID,
		&item.RequesterEmployeeID,
		&requesterEmployeeName,
		&item.RequesterShiftID,
		&item.TargetEmployeeID,
		&targetEmployeeName,
		&item.TargetShiftID,
		&item.Status,
		&reason,
		&managerNote,
		&processedAt,
		&item.CreatedAt,
		&requesterShiftID,
		&requesterShiftEmployeeID,
		&requesterShiftPositionID,
		&requesterShiftTitle,
		&requesterShiftDate,
		&requesterShiftStartTime,
		&requesterShiftEndTime,
		&requesterShiftPosition,
		&requesterShiftPositionColor,
		&targetShiftID,
		&targetShiftEmployeeID,
		&targetShiftPositionID,
		&targetShiftTitle,
		&targetShiftDate,
		&targetShiftStartTime,
		&targetShiftEndTime,
		&targetShiftPosition,
		&targetShiftPositionColor,
	); err != nil {
		return nil, err
	}

	if requesterEmployeeName.Valid {
		item.RequesterEmployeeName = &requesterEmployeeName.String
	}
	if targetEmployeeName.Valid {
		item.TargetEmployeeName = &targetEmployeeName.String
	}
	if reason.Valid {
		item.Reason = &reason.String
	}
	if managerNote.Valid {
		item.ManagerNote = &managerNote.String
	}
	if processedAt.Valid {
		t := processedAt.Time
		item.ProcessedAt = &t
	}

	item.RequesterShift = buildPlanningShiftSwapRequestSelfShiftView(
		item.RequesterShiftID,
		requesterShiftID,
		requesterShiftEmployeeID,
		requesterShiftPositionID,
		requesterShiftTitle,
		requesterShiftDate,
		requesterShiftStartTime,
		requesterShiftEndTime,
		requesterShiftPosition,
		requesterShiftPositionColor,
	)
	item.TargetShift = buildPlanningShiftSwapRequestSelfShiftView(
		item.TargetShiftID,
		targetShiftID,
		targetShiftEmployeeID,
		targetShiftPositionID,
		targetShiftTitle,
		targetShiftDate,
		targetShiftStartTime,
		targetShiftEndTime,
		targetShiftPosition,
		targetShiftPositionColor,
	)

	return item, nil
}

func buildPlanningShiftSwapRequestSelfShiftView(
	fallbackID string,
	shiftID sql.NullString,
	employeeID sql.NullString,
	positionID sql.NullString,
	title sql.NullString,
	shiftDate sql.NullTime,
	startTime sql.NullString,
	endTime sql.NullString,
	position sql.NullString,
	positionColor sql.NullString,
) PlanningShiftSwapRequestSelfShiftView {
	view := PlanningShiftSwapRequestSelfShiftView{ID: fallbackID}
	if shiftID.Valid && strings.TrimSpace(shiftID.String) != "" {
		view.ID = shiftID.String
	}
	if employeeID.Valid {
		view.EmployeeID = &employeeID.String
	}
	if positionID.Valid {
		view.PositionID = &positionID.String
	}
	if title.Valid {
		view.Title = &title.String
	}
	if shiftDate.Valid {
		dateOnly := models.NewDateOnly(shiftDate.Time)
		view.ShiftDate = &dateOnly
	}
	if startTime.Valid {
		view.StartTime = &startTime.String
	}
	if endTime.Valid {
		view.EndTime = &endTime.String
	}
	if position.Valid {
		view.Position = &position.String
	}
	if positionColor.Valid {
		view.PositionColor = &positionColor.String
	}
	return view
}
