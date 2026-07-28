package schedule

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/utils/dbutils"
)

func (r *Repository) ListPlanningShiftsForPublication(ctx context.Context, merchantID, weekID string) ([]publishedShiftSnapshot, error) {
	db := dbx.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `
		SELECT s.employee_id, s.shift_date, s.start_time, s.end_time, s.title,
			COALESCE(NULLIF(TRIM(s.position), ''), COALESCE(p.label, '')) AS position_label
		FROM planning_shifts s
		LEFT JOIN planning_positions p ON p.id = s.position_id AND p.merchant_id = s.merchant_id AND p.enabled = TRUE
		WHERE s.merchant_id = ? AND s.week_id = ? AND s.employee_id IS NOT NULL AND s.enabled = TRUE
		ORDER BY s.employee_id ASC, s.shift_date ASC, s.start_time ASC, s.created_at ASC
	`, merchantID, weekID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]publishedShiftSnapshot, 0)
	for rows.Next() {
		var item publishedShiftSnapshot
		if err := rows.Scan(&item.EmployeeID, &item.ShiftDate, &item.StartTime, &item.EndTime, &item.Title, &item.PositionLabel); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListPublishedShiftSnapshots(ctx context.Context, merchantID, weekID string) ([]publishedShiftSnapshot, error) {
	db := dbx.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `
		SELECT employee_id, shift_date, start_time, end_time, title, position_label
		FROM planning_published_shift_snapshots
		WHERE merchant_id = ? AND week_id = ?
		ORDER BY employee_id ASC, shift_date ASC, start_time ASC
	`, merchantID, weekID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]publishedShiftSnapshot, 0)
	for rows.Next() {
		var item publishedShiftSnapshot
		if err := rows.Scan(&item.EmployeeID, &item.ShiftDate, &item.StartTime, &item.EndTime, &item.Title, &item.PositionLabel); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) PublishPlanningWeekWithSnapshots(ctx context.Context, merchantID, weekID string, publishedAt time.Time, snapshots []publishedShiftSnapshot) (*PlanningWeek, error) {
	if err := dbutils.RunInTx(ctx, r.db, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, r.db)
		res, err := db.ExecContext(txCtx, `
			UPDATE planning_weeks
			SET status = 'published', published_at = COALESCE(published_at, ?), updated_at = ?
			WHERE merchant_id = ? AND id = ? AND enabled = TRUE
		`, publishedAt, publishedAt, merchantID, weekID)
		if err != nil {
			return err
		}
		if affected, _ := res.RowsAffected(); affected == 0 {
			return sql.ErrNoRows
		}
		if _, err := db.ExecContext(txCtx, `
			DELETE FROM planning_published_shift_snapshots
			WHERE merchant_id = ? AND week_id = ?
		`, merchantID, weekID); err != nil {
			return err
		}
		for _, snapshot := range snapshots {
			if _, err := db.ExecContext(txCtx, `
				INSERT INTO planning_published_shift_snapshots (
					id, merchant_id, week_id, employee_id, shift_date, start_time, end_time, title, position_label, published_at, created_at
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			`, snapshotID(), merchantID, weekID, snapshot.EmployeeID, snapshot.ShiftDate, snapshot.StartTime, snapshot.EndTime, snapshot.Title, snapshot.PositionLabel, publishedAt, publishedAt); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return r.GetPlanningWeekByID(ctx, merchantID, weekID)
}

func (r *Repository) GetMerchantDisplayName(ctx context.Context, merchantID string) (string, error) {
	db := dbx.GetDB(ctx, r.db)
	var fullName string
	if err := db.QueryRowContext(ctx, `
		SELECT fullName
		FROM merchant
		WHERE id = ?
		LIMIT 1
	`, merchantID).Scan(&fullName); err != nil {
		return "", err
	}
	return strings.TrimSpace(fullName), nil
}

func (r *Repository) ListPlanningNotificationRecipients(ctx context.Context, merchantID string, employeeIDs []string) ([]planningNotificationRecipient, error) {
	if len(employeeIDs) == 0 {
		return nil, nil
	}
	db := dbx.GetDB(ctx, r.db)
	placeholders := make([]string, len(employeeIDs))
	args := make([]interface{}, 0, len(employeeIDs)+2)
	args = append(args, merchantID, merchantID)
	for index, employeeID := range employeeIDs {
		placeholders[index] = "?"
		args = append(args, employeeID)
	}
	query := fmt.Sprintf(`
		SELECT e.id, e.first_name, e.last_name, e.email, e.phone, u.last_login_at, du.last_used_at
		FROM employees e
		LEFT JOIN users u ON u.user_id = e.user_id AND u.enabled = TRUE
		LEFT JOIN (
			SELECT user_id, MAX(last_used) AS last_used_at
			FROM users_devices
			WHERE merchant_id = ?
			GROUP BY user_id
		) du ON du.user_id = e.user_id
		WHERE e.merchant_id = ? AND e.enabled = TRUE AND e.id IN (%s)
		ORDER BY e.id ASC
	`, strings.Join(placeholders, ","))
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]planningNotificationRecipient, 0, len(employeeIDs))
	for rows.Next() {
		var item planningNotificationRecipient
		var email, phone sql.NullString
		var lastLoginAt, lastUsedAt sql.NullTime
		if err := rows.Scan(&item.EmployeeID, &item.FirstName, &item.LastName, &email, &phone, &lastLoginAt, &lastUsedAt); err != nil {
			return nil, err
		}
		if email.Valid {
			item.Email = &email.String
		}
		if phone.Valid {
			item.Phone = &phone.String
		}
		if lastLoginAt.Valid {
			item.LastLoginAt = &lastLoginAt.Time
		}
		if lastUsedAt.Valid {
			item.LastDeviceUsedAt = &lastUsedAt.Time
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
