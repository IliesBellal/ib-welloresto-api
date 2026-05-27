package leave

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

func (r *Repository) ListPlanningLeaveRequests(ctx context.Context, merchantID string, filters PlanningLeaveRequestListFilters) ([]PlanningLeaveRequest, error) {
	db := dbutils.GetDB(ctx, r.db)
	query := `
		SELECT id, merchant_id, employee_id, leave_type, start_date, end_date, status, reason,
			manager_note, requested_by_user_id, processed_by_user_id, processed_at, created_at, updated_at, deleted_at
		FROM planning_leave_requests
		WHERE merchant_id = ? AND enabled = 1
	`
	args := []interface{}{merchantID}
	if strings.TrimSpace(filters.EmployeeID) != "" {
		query += ` AND employee_id = ?`
		args = append(args, strings.TrimSpace(filters.EmployeeID))
	}
	if strings.TrimSpace(filters.Status) != "" {
		query += ` AND status = ?`
		args = append(args, strings.TrimSpace(filters.Status))
	}
	query += ` ORDER BY start_date DESC, created_at DESC`
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]PlanningLeaveRequest, 0)
	for rows.Next() {
		item, scanErr := scanPlanningLeaveRequest(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetPlanningLeaveRequestByID(ctx context.Context, merchantID, requestID string) (*PlanningLeaveRequest, error) {
	db := dbutils.GetDB(ctx, r.db)
	row := db.QueryRowContext(ctx, `
		SELECT id, merchant_id, employee_id, leave_type, start_date, end_date, status, reason,
			manager_note, requested_by_user_id, processed_by_user_id, processed_at, created_at, updated_at, deleted_at
		FROM planning_leave_requests
		WHERE merchant_id = ? AND id = ? AND enabled = 1
		LIMIT 1
	`, merchantID, requestID)
	return scanPlanningLeaveRequestRow(row)
}

func (r *Repository) CreatePlanningLeaveRequest(ctx context.Context, merchantID string, request PlanningLeaveRequest) (*PlanningLeaveRequest, error) {
	db := dbutils.GetDB(ctx, r.db)
	now := time.Now().UTC()
	request.ID = helpers.GeneratePrefixedID(helpers.PlanningLeaveRequestIDPrefix)
	request.MerchantID = merchantID
	request.CreatedAt = now
	request.UpdatedAt = now
	_, err := db.ExecContext(ctx, `
		INSERT INTO planning_leave_requests (
			id, merchant_id, employee_id, leave_type, start_date, end_date, status, reason, manager_note,
			requested_by_user_id, processed_by_user_id, processed_at, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`, request.ID, request.MerchantID, request.EmployeeID, request.LeaveType, request.StartDate, request.EndDate, request.Status, request.Reason, request.ManagerNote, request.RequestedByUserID, request.ProcessedByUserID, request.ProcessedAt, request.CreatedAt, request.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &request, nil
}

func (r *Repository) UpdatePlanningLeaveRequest(ctx context.Context, merchantID, requestID string, request PlanningLeaveRequest) (*PlanningLeaveRequest, error) {
	db := dbutils.GetDB(ctx, r.db)
	request.UpdatedAt = time.Now().UTC()
	res, err := db.ExecContext(ctx, `
		UPDATE planning_leave_requests
		SET leave_type = ?, start_date = ?, end_date = ?, status = ?, reason = ?, manager_note = ?,
			processed_by_user_id = ?, processed_at = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = 1
	`, request.LeaveType, request.StartDate, request.EndDate, request.Status, request.Reason, request.ManagerNote, request.ProcessedByUserID, request.ProcessedAt, request.UpdatedAt, merchantID, requestID)
	if err != nil {
		return nil, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, sql.ErrNoRows
	}
	return &request, nil
}

func (r *Repository) SoftDeletePlanningLeaveRequest(ctx context.Context, merchantID, requestID string) error {
	db := dbutils.GetDB(ctx, r.db)
	now := time.Now().UTC()
	res, err := db.ExecContext(ctx, `
		UPDATE planning_leave_requests
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

func (r *Repository) CountEmployeeAssignedShiftsInRange(ctx context.Context, merchantID, employeeID string, startDate, endDate time.Time) (int, error) {
	db := dbutils.GetDB(ctx, r.db)
	query := `
		SELECT COUNT(1)
		FROM planning_shifts
		WHERE merchant_id = ? AND employee_id = ? AND enabled = 1 AND status <> 'cancelled'
			AND shift_date >= ? AND shift_date <= ?
	`
	var count int
	if err := db.QueryRowContext(ctx, query, merchantID, employeeID, startDate.Format("2006-01-02"), endDate.Format("2006-01-02")).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

type scannable interface {
	Scan(dest ...any) error
}

type scannableRows interface {
	Scan(dest ...any) error
}

func scanPlanningLeaveRequestRow(row scannable) (*PlanningLeaveRequest, error) {
	item := &PlanningLeaveRequest{}
	var reason, managerNote, requestedByUserID, processedByUserID sql.NullString
	var processedAt, deletedAt sql.NullTime
	if err := row.Scan(&item.ID, &item.MerchantID, &item.EmployeeID, &item.LeaveType, &item.StartDate, &item.EndDate, &item.Status, &reason, &managerNote, &requestedByUserID, &processedByUserID, &processedAt, &item.CreatedAt, &item.UpdatedAt, &deletedAt); err != nil {
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

func scanPlanningLeaveRequest(rows scannableRows) (*PlanningLeaveRequest, error) {
	return scanPlanningLeaveRequestRow(rows)
}
