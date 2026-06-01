package users

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

func (r *UsersRepository) ListMerchantUsers(ctx context.Context, merchantID string, filters MerchantUserListFilters) ([]MerchantUserListItem, int, error) {
	db := dbutils.GetDB(ctx, r.database)
	baseQuery := `
		FROM users_rights ur
		INNER JOIN users u ON u.user_id = ur.user_id
		LEFT JOIN (
			SELECT merchant_id, user_id, MIN(id) AS employee_id, MIN(CONCAT(first_name, ' ', last_name)) AS employee_name
			FROM employees
			WHERE enabled = 1 AND user_id IS NOT NULL
			GROUP BY merchant_id, user_id
		) employee_link ON employee_link.merchant_id = ur.merchant_id AND employee_link.user_id = ur.user_id
		WHERE ur.merchant_id = ? AND ur.enabled = 1
	`
	args := []interface{}{merchantID}

	if search := strings.TrimSpace(filters.Search); search != "" {
		baseQuery += ` AND (u.first_name LIKE ? OR u.last_name LIKE ? OR u.email LIKE ? OR u.tel LIKE ?)`
		pattern := "%" + search + "%"
		args = append(args, pattern, pattern, pattern, pattern)
	}
	if filters.Active != nil {
		baseQuery += ` AND u.enabled = ?`
		args = append(args, *filters.Active)
	}
	if filters.LinkedEmployee != nil {
		if *filters.LinkedEmployee {
			baseQuery += ` AND employee_link.employee_id IS NOT NULL`
		} else {
			baseQuery += ` AND employee_link.employee_id IS NULL`
		}
	}
	if filters.Admin != nil {
		baseQuery += ` AND ur.admin = ?`
		args = append(args, *filters.Admin)
	}

	var totalItems int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(1) `+baseQuery, args...).Scan(&totalItems); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT
			u.user_id,
			u.first_name,
			u.last_name,
			u.email,
			u.tel,
			u.profile_picture,
			u.created_at,
			u.last_login_at,
			u.enabled,
			COALESCE(ur.login_enabled, TRUE),
			ur.id,
			ur.admin,
			ur.access_wrreception,
			ur.access_wrdelivery,
			ur.access_wrwaiter,
			ur.print_merchant_cash_report,
			ur.open_cash_drawer,
			COALESCE(ur.manage_menu, FALSE),
			COALESCE(ur.manage_plannings, FALSE),
			COALESCE(ur.manage_users, FALSE),
			COALESCE(ur.manage_settings, FALSE),
			COALESCE(ur.manage_haccp, FALSE),
			COALESCE(ur.view_reports, FALSE),
			COALESCE(ur.export_reports, FALSE),
			COALESCE(ur.view_financials, FALSE),
			COALESCE(ur.export_financials, FALSE),
			COALESCE(ur.manage_customers, FALSE),
			COALESCE(ur.export_customers, FALSE),
			employee_link.employee_id,
			employee_link.employee_name
	` + baseQuery + `
		ORDER BY u.last_name ASC, u.first_name ASC
		LIMIT ? OFFSET ?
	`
	dataArgs := append([]interface{}{}, args...)
	dataArgs = append(dataArgs, filters.PageSize, (filters.Page-1)*filters.PageSize)

	rows, err := db.QueryContext(ctx, query, dataArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]MerchantUserListItem, 0)
	for rows.Next() {
		item, scanErr := scanMerchantUserListItem(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		items = append(items, *item)
	}

	return items, totalItems, rows.Err()
}

func (r *UsersRepository) GetMerchantUserByID(ctx context.Context, merchantID, userID string) (*MerchantUserDetail, error) {
	db := dbutils.GetDB(ctx, r.database)
	row := db.QueryRowContext(ctx, `
		SELECT
			u.user_id,
			u.first_name,
			u.last_name,
			u.email,
			u.tel,
			u.profile_picture,
			u.created_at,
			u.last_login_at,
			u.enabled,
			COALESCE(ur.login_enabled, TRUE),
			ur.id,
			ur.admin,
			ur.access_wrreception,
			ur.access_wrdelivery,
			ur.access_wrwaiter,
			ur.print_merchant_cash_report,
			ur.open_cash_drawer,
			COALESCE(ur.manage_menu, FALSE),
			COALESCE(ur.manage_plannings, FALSE),
			COALESCE(ur.manage_users, FALSE),
			COALESCE(ur.manage_settings, FALSE),
			COALESCE(ur.manage_haccp, FALSE),
			COALESCE(ur.view_reports, FALSE),
			COALESCE(ur.export_reports, FALSE),
			COALESCE(ur.view_financials, FALSE),
			COALESCE(ur.export_financials, FALSE),
			COALESCE(ur.manage_customers, FALSE),
			COALESCE(ur.export_customers, FALSE),
			employee_link.employee_id,
			employee_link.employee_name
		FROM users_rights ur
		INNER JOIN users u ON u.user_id = ur.user_id
		LEFT JOIN (
			SELECT merchant_id, user_id, MIN(id) AS employee_id, MIN(CONCAT(first_name, ' ', last_name)) AS employee_name
			FROM employees
			WHERE enabled = 1 AND user_id IS NOT NULL
			GROUP BY merchant_id, user_id
		) employee_link ON employee_link.merchant_id = ur.merchant_id AND employee_link.user_id = ur.user_id
		WHERE ur.merchant_id = ? AND ur.user_id = ? AND ur.enabled = 1
		LIMIT 1
	`, merchantID, strings.TrimSpace(userID))

	item, err := scanMerchantUserListItem(row)
	if err != nil {
		return nil, err
	}

	return &MerchantUserDetail{MerchantUserListItem: *item}, nil
}

func (r *UsersRepository) SearchLinkableUsers(ctx context.Context, merchantID string, filters LinkableUserSearchFilters) ([]LinkableUser, int, error) {
	db := dbutils.GetDB(ctx, r.database)
	search := strings.TrimSpace(filters.Search)
	if search == "" {
		return []LinkableUser{}, 0, nil
	}

	baseQuery := `
		FROM users u
		WHERE NOT EXISTS (
			SELECT 1
			FROM users_rights ur
			WHERE ur.user_id = u.user_id
			  AND ur.merchant_id = ?
			  AND ur.enabled = 1
		)
		AND (u.first_name LIKE ? OR u.last_name LIKE ? OR u.email LIKE ? OR u.tel LIKE ?)
	`
	pattern := "%" + search + "%"
	args := []interface{}{merchantID, pattern, pattern, pattern, pattern}

	var totalItems int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(1) `+baseQuery, args...).Scan(&totalItems); err != nil {
		return nil, 0, err
	}

	rows, err := db.QueryContext(ctx, `
		SELECT u.user_id, u.first_name, u.last_name, u.email, u.tel, u.profile_picture, u.created_at, u.last_login_at, u.enabled
	`+baseQuery+`
		ORDER BY u.last_name ASC, u.first_name ASC
		LIMIT ? OFFSET ?
	`, append(args, filters.PageSize, (filters.Page-1)*filters.PageSize)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]LinkableUser, 0)
	for rows.Next() {
		item, scanErr := scanLinkableUser(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		items = append(items, *item)
	}

	return items, totalItems, rows.Err()
}

func (r *UsersRepository) GetMerchantUserRights(ctx context.Context, merchantID, userID string) (*MerchantUserRights, error) {
	db := dbutils.GetDB(ctx, r.database)
	row := db.QueryRowContext(ctx, `
		SELECT
			id,
			merchant_id,
			user_id,
			admin,
			COALESCE(login_enabled, TRUE),
			access_wrreception,
			access_wrdelivery,
			access_wrwaiter,
			print_merchant_cash_report,
			open_cash_drawer,
			COALESCE(manage_menu, FALSE),
			COALESCE(manage_plannings, FALSE),
			COALESCE(manage_users, FALSE),
			COALESCE(manage_settings, FALSE),
			COALESCE(manage_haccp, FALSE),
			COALESCE(view_reports, FALSE),
			COALESCE(export_reports, FALSE),
			COALESCE(view_financials, FALSE),
			COALESCE(export_financials, FALSE),
			COALESCE(manage_customers, FALSE),
			COALESCE(export_customers, FALSE)
		FROM users_rights
		WHERE merchant_id = ? AND user_id = ? AND enabled = 1
		LIMIT 1
	`, merchantID, strings.TrimSpace(userID))

	return scanMerchantUserRights(row)
}

func (r *UsersRepository) UpsertMerchantUserRights(ctx context.Context, userID, merchantID, token string, rights MerchantUserRightsUpsertRequest) (int64, error) {
	db := dbutils.GetDB(ctx, r.database)

	var existingID int64
	var existingEnabled bool
	row := db.QueryRowContext(ctx, `
		SELECT id, enabled
		FROM users_rights
		WHERE merchant_id = ? AND user_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, merchantID, userID)
	if err := row.Scan(&existingID, &existingEnabled); err != nil && err != sql.ErrNoRows {
		return 0, err
	} else if err == nil {
		_, updateErr := db.ExecContext(ctx, `
			UPDATE users_rights
			SET token = ?,
				enabled = 1,
				admin = ?,
				access_wrreception = ?,
				access_wrdelivery = ?,
				access_wrwaiter = ?,
				print_merchant_cash_report = ?,
				open_cash_drawer = ?,
				manage_menu = ?,
				manage_plannings = ?,
				manage_users = ?,
				manage_settings = ?,
				manage_haccp = ?,
				view_reports = ?,
				export_reports = ?,
				view_financials = ?,
				export_financials = ?,
				manage_customers = ?,
				export_customers = ?
			WHERE id = ?
		`, token, rights.Admin,
			rights.Permissions.AccessReception,
			rights.Permissions.AccessDelivery,
			rights.Permissions.AccessWaiter,
			rights.Permissions.PrintMerchantCashReport,
			rights.Permissions.OpenCashDrawer,
			rights.Permissions.ManageMenu,
			rights.Permissions.ManagePlannings,
			rights.Permissions.ManageUsers,
			rights.Permissions.ManageSettings,
			rights.Permissions.ManageHACCP,
			rights.Permissions.ViewReports,
			rights.Permissions.ExportReports,
			rights.Permissions.ViewFinancials,
			rights.Permissions.ExportFinancials,
			rights.Permissions.ManageCustomers,
			rights.Permissions.ExportCustomers,
			existingID,
		)
		if updateErr != nil {
			return 0, updateErr
		}
		return existingID, nil
	}

	res, err := db.ExecContext(ctx, `
		INSERT INTO users_rights (
			user_id,
			merchant_id,
			token,
			admin,
			access_wrreception,
			access_wrdelivery,
			access_wrwaiter,
			print_merchant_cash_report,
			open_cash_drawer,
			manage_menu,
			manage_plannings,
			manage_users,
			manage_settings,
			manage_haccp,
			view_reports,
			export_reports,
			view_financials,
			export_financials,
			manage_customers,
			export_customers,
			enabled
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
	`, userID, merchantID, token, rights.Admin,
		rights.Permissions.AccessReception,
		rights.Permissions.AccessDelivery,
		rights.Permissions.AccessWaiter,
		rights.Permissions.PrintMerchantCashReport,
		rights.Permissions.OpenCashDrawer,
		rights.Permissions.ManageMenu,
		rights.Permissions.ManagePlannings,
		rights.Permissions.ManageUsers,
		rights.Permissions.ManageSettings,
		rights.Permissions.ManageHACCP,
		rights.Permissions.ViewReports,
		rights.Permissions.ExportReports,
		rights.Permissions.ViewFinancials,
		rights.Permissions.ExportFinancials,
		rights.Permissions.ManageCustomers,
		rights.Permissions.ExportCustomers,
	)
	if err != nil {
		return 0, err
	}

	insertID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	return insertID, nil
}

func (r *UsersRepository) UpdateMerchantUserRights(ctx context.Context, merchantID, userID string, rights MerchantUserRightsUpsertRequest) error {
	db := dbutils.GetDB(ctx, r.database)
	res, err := db.ExecContext(ctx, `
		UPDATE users_rights
		SET admin = ?,
			access_wrreception = ?,
			access_wrdelivery = ?,
			access_wrwaiter = ?,
			print_merchant_cash_report = ?,
			open_cash_drawer = ?,
			manage_menu = ?,
			manage_plannings = ?,
			manage_users = ?,
			manage_settings = ?,
			manage_haccp = ?,
			view_reports = ?,
			export_reports = ?,
			view_financials = ?,
			export_financials = ?,
			manage_customers = ?,
			export_customers = ?,
			login_enabled = ?
		WHERE merchant_id = ? AND user_id = ? AND enabled = 1
	`, rights.Admin,
		rights.Permissions.AccessReception,
		rights.Permissions.AccessDelivery,
		rights.Permissions.AccessWaiter,
		rights.Permissions.PrintMerchantCashReport,
		rights.Permissions.OpenCashDrawer,
		rights.Permissions.ManageMenu,
		rights.Permissions.ManagePlannings,
		rights.Permissions.ManageUsers,
		rights.Permissions.ManageSettings,
		rights.Permissions.ManageHACCP,
		rights.Permissions.ViewReports,
		rights.Permissions.ExportReports,
		rights.Permissions.ViewFinancials,
		rights.Permissions.ExportFinancials,
		rights.Permissions.ManageCustomers,
		rights.Permissions.ExportCustomers,
		rights.LoginEnabled,
		merchantID,
		userID,
	)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return models.ErrMerchantUserNotFound
	}
	return nil
}

func (r *UsersRepository) UserExists(ctx context.Context, userID string) (bool, error) {
	db := dbutils.GetDB(ctx, r.database)
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM users WHERE user_id = ?`, strings.TrimSpace(userID)).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *UsersRepository) MerchantUserLinkExists(ctx context.Context, merchantID, userID string) (bool, error) {
	db := dbutils.GetDB(ctx, r.database)
	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM users_rights
		WHERE merchant_id = ? AND user_id = ? AND enabled = 1
	`, merchantID, strings.TrimSpace(userID)).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *UsersRepository) DisableMerchantUserLink(ctx context.Context, merchantID, userID string) (bool, error) {
	db := dbutils.GetDB(ctx, r.database)
	res, err := db.ExecContext(ctx, `
		UPDATE users_rights
		SET enabled = 0
		WHERE merchant_id = ? AND user_id = ? AND enabled = 1
	`, merchantID, strings.TrimSpace(userID))
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (r *UsersRepository) ClearMerchantEmployeeLinks(ctx context.Context, merchantID, userID string) (int, error) {
	db := dbutils.GetDB(ctx, r.database)
	res, err := db.ExecContext(ctx, `
		UPDATE employees
		SET user_id = NULL
		WHERE merchant_id = ? AND user_id = ? AND enabled = 1
	`, merchantID, strings.TrimSpace(userID))
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(affected), nil
}

type merchantUserScanner interface {
	Scan(dest ...any) error
}

func scanMerchantUserListItem(scanner merchantUserScanner) (*MerchantUserListItem, error) {
	item := &MerchantUserListItem{}
	var email sql.NullString
	var tel sql.NullString
	var profilePicture sql.NullString
	var lastLoginAt sql.NullTime
	var employeeID sql.NullString
	var employeeName sql.NullString
	if err := scanner.Scan(
		&item.UserID,
		&item.FirstName,
		&item.LastName,
		&email,
		&tel,
		&profilePicture,
		&item.CreatedAt,
		&lastLoginAt,
		&item.Enabled,
		&item.LoginEnabled,
		&item.MerchantRightsID,
		&item.Admin,
		&item.Permissions.AccessReception,
		&item.Permissions.AccessDelivery,
		&item.Permissions.AccessWaiter,
		&item.Permissions.PrintMerchantCashReport,
		&item.Permissions.OpenCashDrawer,
		&item.Permissions.ManageMenu,
		&item.Permissions.ManagePlannings,
		&item.Permissions.ManageUsers,
		&item.Permissions.ManageSettings,
		&item.Permissions.ManageHACCP,
		&item.Permissions.ViewReports,
		&item.Permissions.ExportReports,
		&item.Permissions.ViewFinancials,
		&item.Permissions.ExportFinancials,
		&item.Permissions.ManageCustomers,
		&item.Permissions.ExportCustomers,
		&employeeID,
		&employeeName,
	); err != nil {
		return nil, err
	}
	item.Email = nullableStringPtr(email)
	item.Tel = nullableStringPtr(tel)
	item.ProfilePicture = nullableStringPtr(profilePicture)
	item.LastLoginAt = nullableTimePtr(lastLoginAt)
	item.EmployeeID = nullableStringPtr(employeeID)
	item.EmployeeName = nullableStringPtr(employeeName)
	item.Status = userStatus(item.Enabled)
	return item, nil
}

func scanLinkableUser(scanner merchantUserScanner) (*LinkableUser, error) {
	item := &LinkableUser{}
	var email sql.NullString
	var tel sql.NullString
	var profilePicture sql.NullString
	var lastLoginAt sql.NullTime
	if err := scanner.Scan(&item.UserID, &item.FirstName, &item.LastName, &email, &tel, &profilePicture, &item.CreatedAt, &lastLoginAt, &item.Enabled); err != nil {
		return nil, err
	}
	item.Email = nullableStringPtr(email)
	item.Tel = nullableStringPtr(tel)
	item.ProfilePicture = nullableStringPtr(profilePicture)
	item.LastLoginAt = nullableTimePtr(lastLoginAt)
	item.Status = userStatus(item.Enabled)
	return item, nil
}

func scanMerchantUserRights(scanner merchantUserScanner) (*MerchantUserRights, error) {
	rights := &MerchantUserRights{}
	if err := scanner.Scan(
		&rights.MerchantRightsID,
		&rights.MerchantID,
		&rights.UserID,
		&rights.Admin,
		&rights.LoginEnabled,
		&rights.Permissions.AccessReception,
		&rights.Permissions.AccessDelivery,
		&rights.Permissions.AccessWaiter,
		&rights.Permissions.PrintMerchantCashReport,
		&rights.Permissions.OpenCashDrawer,
		&rights.Permissions.ManageMenu,
		&rights.Permissions.ManagePlannings,
		&rights.Permissions.ManageUsers,
		&rights.Permissions.ManageSettings,
		&rights.Permissions.ManageHACCP,
		&rights.Permissions.ViewReports,
		&rights.Permissions.ExportReports,
		&rights.Permissions.ViewFinancials,
		&rights.Permissions.ExportFinancials,
		&rights.Permissions.ManageCustomers,
		&rights.Permissions.ExportCustomers,
	); err != nil {
		return nil, err
	}
	return rights, nil
}

func nullableStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	trimmed := strings.TrimSpace(value.String)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func nullableTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	timestamp := value.Time
	return &timestamp
}

func userStatus(enabled bool) string {
	if enabled {
		return "active"
	}
	return "disabled"
}
