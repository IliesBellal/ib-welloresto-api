package swaps

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
