package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
)

type AuthRepository struct {
	database *sql.DB
}

func NewAuthRepository(db *sql.DB) AuthRepository {
	return AuthRepository{database: db}
}

// authMerchantJoinCast returns the SQL fragment used to join varchar
// merchant_id columns (users_rights, merchant_parameters, subscriptions,
// scannorder_settings, integration_uber_eats, integration_uber_direct,
// integration_deliveroo) against merchant.id, which is an integer identity —
// merchant_id is carried as a string everywhere else in the Go code (see
// 12-merchant-id-unification.md). MySQL implicitly casts across the join,
// Postgres requires an explicit one, and CAST syntax itself differs per
// dialect (CHAR vs TEXT). Shared by GetUserByToken/Login/GetUserByPIN, which
// all join the same set of tables identically.
func authMerchantJoinCast() string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "CAST(m.id AS TEXT)"
	}
	return "CAST(m.id AS CHAR)"
}

func (r *AuthRepository) GetUserByToken(ctx context.Context, token string) (*UserLoginRow, error) {
	if strings.TrimSpace(token) == "" {
		return nil, nil // ou une erreur métier type ErrUnauthorized
	}

	db := dbx.GetDB(ctx, r.database)
	joinCast := authMerchantJoinCast()
	query := fmt.Sprintf(`
SELECT
    u.user_id,
    u.password,
    u.name,
    u.first_name,
    u.last_name,
    u.email,
    u.tel,
    u.enabled,
    u.profile_picture,
	u.email_verified_at,

	ur.id AS merchant_rights_id,
    ur.token AS rights_token,
    ur.access_wrreception,
    ur.access_wrdelivery,
    ur.access_wrwaiter,
    ur.print_merchant_cash_report,
    ur.open_cash_drawer,
    ur.admin,
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
    ur.merchant_id,
	u.mfa_type,
	u.mfa_status,
	u.mfa_verified_at,
	u.mfa_otp_sent_at,

    m.fullName,
    m.merchantTel,
    m.lat,
    m.lng,
    m.timezone,
    CONCAT(m.street_number,' ',m.street,', ',m.zip_code,' ',m.city,', ',m.country),
    m.logo,
    m.web_site,

    mp.delivery_fees,
    mp.delivery_fees_limit,
    mp.delivery_distance_limit,
    mp.manage_on_site,
    mp.manage_take_away,
    mp.manage_delivery,
    mp.kitchen_show_only_paid,
    mp.service_required_for_ordering,
    mp.warning_new_order_not_paid,
    mp.disable_components_under_safety_stock,
    mp.customer_form_requirements,
    mp.currency,
    mp.is_open,
	mp.pos_upsell_enabled,
	mp.pos_covers_count_required,
	mp.waiter_app_can_cash_in,

    p.allow_waiter_account,
    p.allow_delivery_account,
    p.scannorder_ready,
    p.stock_management,
    p.hr_management,
	COALESCE(s.planning_enabled, p.planning_enabled, p.hr_management, FALSE) AS planning_enabled,
	COALESCE(s.haccp_enabled, p.haccp_enabled, TRUE) AS haccp_enabled,
	COALESCE(s.stock_enabled, p.stock_enabled, CASE WHEN p.stock_management > 0 THEN TRUE ELSE FALSE END) AS stock_enabled,
	COALESCE(s.scannorder_enabled, p.scannorder_enabled, p.scannorder_ready, FALSE) AS scannorder_enabled,
	COALESCE(s.bookings_enabled, p.bookings_enabled, TRUE) AS bookings_enabled,
	COALESCE(s.kiosks_enabled, p.kiosks_enabled, TRUE) AS kiosks_enabled,
	COALESCE(s.delivery_enabled, p.delivery_enabled, TRUE) AS delivery_enabled,

    sset.activated,

    iue.store_id,
    iue.estimated_preparation_time,
    iue.delay_until,
    iue.delay_duration,
    iue.closed_until,
	iue.commission_rate,

    iud.customer_id,

    ind.location_id,
	ind.commission_rate

FROM users u
INNER JOIN users_rights ur ON ur.user_id = u.user_id
INNER JOIN merchant m ON %[1]s = ur.merchant_id
LEFT JOIN merchant_parameters mp ON mp.merchant_id = %[1]s
LEFT JOIN subscriptions s ON s.merchant_id = %[1]s
LEFT JOIN packages p ON p.id = s.package_id
LEFT JOIN scannorder_settings sset ON sset.merchant_id = %[1]s
LEFT JOIN integration_uber_eats iue ON iue.merchant_id = %[1]s AND iue.bearer_token IS NOT NULL
LEFT JOIN integration_uber_direct iud ON iud.merchant_id = %[1]s AND iud.bearer_token IS NOT NULL
LEFT JOIN integration_deliveroo ind ON ind.merchant_id = %[1]s

WHERE ur.token = ?
  AND ur.enabled = TRUE
  AND ur.login_enabled = TRUE
LIMIT 1;
`, joinCast)

	row := db.QueryRowContext(ctx, query, token)
	return scanUserLoginRow(row)
}

// scanUserLoginRow scans a row produced by the shared SELECT used by GetUserByToken
// and GetUserByPIN (74 columns, same order). Shared to keep both queries in sync.
func scanUserLoginRow(row *sql.Row) (*UserLoginRow, error) {
	data := &UserLoginRow{}
	var ueDelayUntil sql.NullTime
	var ueClosedUntil sql.NullTime

	err := row.Scan(
		&data.UserID, &data.Password, &data.Name, &data.FirstName, &data.LastName, &data.Email, &data.Tel,
		&data.Enabled, &data.ProfilePicture, &data.EmailVerifiedAt,

		&data.MerchantRightsID,
		&data.Token, &data.Rights.AccessReception, &data.Rights.AccessDelivery, &data.Rights.AccessWaiter,
		&data.Rights.PrintMerchantCashReport, &data.Rights.OpenCashDrawer, &data.Rights.Admin,
		&data.Rights.CanManageMenu, &data.Rights.CanManagePlannings, &data.Rights.CanManageUsers,
		&data.Rights.CanManageSettings, &data.Rights.CanManageHACCP, &data.Rights.CanViewReports,
		&data.Rights.CanExportReports, &data.Rights.CanViewFinancials, &data.Rights.CanExportFinancials,
		&data.Rights.CanManageCustomers, &data.Rights.CanExportCustomers, &data.MerchantID,
		&data.MFAType, &data.MFAStatus, &data.MFAVerifiedAt, &data.MFAOTPSentAt,

		&data.MerchantName, &data.MerchantTel, &data.MerchantLat, &data.MerchantLng, &data.TimeZone,
		&data.MerchantAddress, &data.MerchantLogo, &data.WebSite,

		&data.DeliveryFees, &data.DeliveryFeesLimit, &data.DeliveryDistanceLimit,
		&data.ManageOnSite, &data.ManageTakeAway, &data.ManageDelivery,
		&data.KitchenShowOnlyPaid, &data.ServiceRequiredForOrdering,
		&data.WarningNewOrderNotPaid, &data.DisableSafetyStock,
		&data.CustomerFormRequirements,
		&data.Currency, &data.IsOpen, &data.POSUpsellEnabled,
		&data.POSCoversCountRequired, &data.MobilePaymentEnabled,

		&data.AllowWaiterAccount, &data.AllowDeliveryAccount,
		&data.ScanNOrderReady, &data.StockManagement, &data.HrManagement,
		&data.PlanningEnabled, &data.HACCPEnabled, &data.StockEnabled, &data.ScanNOrderEnabled, &data.BookingsEnabled,
		&data.KiosksEnabled, &data.DeliveryEnabled,

		&data.SNOActivated,

		&data.UEStoreID, &data.UEPrepTime, &ueDelayUntil, &data.UEDelayDuration, &ueClosedUntil, &data.UECommissionRate,

		&data.UDCustomerID,
		&data.DrooLocationID,
		&data.DrooCommissionRate,
	)

	data.UEDelayUntil = helpers.NullTimeToNullUnixInt(ueDelayUntil)
	data.UEClosedUntil = helpers.NullTimeToNullUnixInt(ueClosedUntil)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	return data, err
}

func (r *AuthRepository) Login(ctx context.Context, username, plainPwd, token string) (*UserLoginRow, error) {
	db := dbx.GetDB(ctx, r.database)
	joinCast := authMerchantJoinCast()
	query := fmt.Sprintf(`
SELECT
    u.user_id,
    u.name,
    u.first_name,
    u.last_name,
    u.email,
    u.tel,
    u.enabled,
    u.profile_picture,
    u.terms_of_use_accepted,
    u.password,
	u.email_verified_at,

	ur.id AS merchant_rights_id,
    ur.token AS rights_token,
    ur.access_wrreception,
    ur.access_wrdelivery,
    ur.access_wrwaiter,
    ur.print_merchant_cash_report,
    ur.open_cash_drawer,
    ur.admin,
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
	ur.merchant_id,
	u.mfa_type,
	u.mfa_status,
	u.mfa_verified_at,
	u.mfa_otp_sent_at,

    m.fullName,
    m.merchantTel,
    m.lat,
    m.lng,
    m.timezone,
    CONCAT(m.street_number,' ',m.street,', ',m.zip_code,' ',m.city,', ',m.country),
    m.logo,
    m.web_site,

    mp.delivery_fees,
    mp.delivery_fees_limit,
    mp.delivery_distance_limit,
    mp.manage_on_site,
    mp.manage_take_away,
    mp.manage_delivery,
    mp.kitchen_show_only_paid,
    mp.kitchen_distribution_mode,
    mp.production_display_mode,
    mp.pager_number_required,
    mp.pos_auto_lock_enabled,
    mp.pos_auto_lock_delay_minutes,
    mp.service_required_for_ordering,
    mp.cash_register_required_for_ordering,
    mp.warning_new_order_not_paid,
    mp.disable_components_under_safety_stock,
    mp.customer_form_requirements,
    mp.currency,
    mp.is_open,
	mp.pos_upsell_enabled,
	mp.pos_covers_count_required,
	mp.waiter_app_can_cash_in,

    p.allow_waiter_account,
    p.allow_delivery_account,
    p.scannorder_ready,
    p.stock_management,
    p.hr_management,
	COALESCE(s.planning_enabled, p.planning_enabled, p.hr_management, FALSE) AS planning_enabled,
	COALESCE(s.haccp_enabled, p.haccp_enabled, TRUE) AS haccp_enabled,
	COALESCE(s.stock_enabled, p.stock_enabled, CASE WHEN p.stock_management > 0 THEN TRUE ELSE FALSE END) AS stock_enabled,
	COALESCE(s.scannorder_enabled, p.scannorder_enabled, p.scannorder_ready, FALSE) AS scannorder_enabled,
	COALESCE(s.bookings_enabled, p.bookings_enabled, TRUE) AS bookings_enabled,
	COALESCE(s.kiosks_enabled, p.kiosks_enabled, TRUE) AS kiosks_enabled,
	COALESCE(s.delivery_enabled, p.delivery_enabled, TRUE) AS delivery_enabled,

    sset.activated,

    iue.store_id,
    iue.estimated_preparation_time,
    iue.delay_until,
    iue.delay_duration,
    iue.closed_until,
	iue.commission_rate,

    iud.customer_id,

    ind.location_id,
	ind.commission_rate

FROM users u
INNER JOIN users_rights ur ON ur.user_id = u.user_id
INNER JOIN merchant m ON %[1]s = ur.merchant_id
LEFT JOIN merchant_parameters mp ON mp.merchant_id = %[1]s
LEFT JOIN subscriptions s ON s.merchant_id = %[1]s
LEFT JOIN packages p ON p.id = s.package_id
LEFT JOIN scannorder_settings sset ON sset.merchant_id = %[1]s
LEFT JOIN integration_uber_eats iue ON iue.merchant_id = %[1]s AND iue.bearer_token IS NOT NULL
LEFT JOIN integration_uber_direct iud ON iud.merchant_id = %[1]s AND iud.bearer_token IS NOT NULL
LEFT JOIN integration_deliveroo ind ON ind.merchant_id = %[1]s

WHERE
    (
        (UPPER(u.name)=UPPER(?) AND u.name <> '' AND u.name IS NOT NULL)
        OR (UPPER(u.email)=UPPER(?) AND u.email <> '' AND u.email IS NOT NULL)
        OR ur.token = ?
    )
LIMIT 1;
`, joinCast)

	row := db.QueryRowContext(ctx, query,
		username,
		username,
		token,
	)

	data := &UserLoginRow{}

	var ueDelayUntil sql.NullTime
	var ueClosedUntil sql.NullTime

	err := row.Scan(
		&data.UserID, &data.Name, &data.FirstName, &data.LastName, &data.Email,
		&data.Tel, &data.Enabled, &data.ProfilePicture,
		&data.TermsOfUseAccepted, &data.Password, &data.EmailVerifiedAt,

		&data.MerchantRightsID,
		&data.Token, &data.Rights.AccessReception, &data.Rights.AccessDelivery, &data.Rights.AccessWaiter,
		&data.Rights.PrintMerchantCashReport, &data.Rights.OpenCashDrawer, &data.Rights.Admin,
		&data.Rights.CanManageMenu, &data.Rights.CanManagePlannings, &data.Rights.CanManageUsers,
		&data.Rights.CanManageSettings, &data.Rights.CanManageHACCP, &data.Rights.CanViewReports,
		&data.Rights.CanExportReports, &data.Rights.CanViewFinancials, &data.Rights.CanExportFinancials,
		&data.Rights.CanManageCustomers, &data.Rights.CanExportCustomers, &data.MerchantID,
		&data.MFAType, &data.MFAStatus, &data.MFAVerifiedAt, &data.MFAOTPSentAt,

		&data.MerchantName, &data.MerchantTel, &data.MerchantLat, &data.MerchantLng, &data.TimeZone,
		&data.MerchantAddress, &data.MerchantLogo, &data.WebSite,

		&data.DeliveryFees, &data.DeliveryFeesLimit, &data.DeliveryDistanceLimit,
		&data.ManageOnSite, &data.ManageTakeAway, &data.ManageDelivery,
		&data.KitchenShowOnlyPaid, &data.KitchenDistributionMode, &data.ProductionDisplayMode, &data.PagerNumberRequired,
		&data.POSAutoLockEnabled, &data.POSAutoLockDelayMinutes, &data.ServiceRequiredForOrdering,
		&data.CashRegisterRequiredForOrdering, &data.WarningNewOrderNotPaid, &data.DisableSafetyStock,
		&data.CustomerFormRequirements,
		&data.Currency, &data.IsOpen, &data.POSUpsellEnabled,
		&data.POSCoversCountRequired, &data.MobilePaymentEnabled,

		&data.AllowWaiterAccount, &data.AllowDeliveryAccount,
		&data.ScanNOrderReady, &data.StockManagement, &data.HrManagement,
		&data.PlanningEnabled, &data.HACCPEnabled, &data.StockEnabled, &data.ScanNOrderEnabled, &data.BookingsEnabled,
		&data.KiosksEnabled, &data.DeliveryEnabled,

		&data.SNOActivated,

		&data.UEStoreID,
		&data.UEPrepTime,
		&ueDelayUntil,
		&data.UEDelayDuration,
		&ueClosedUntil,
		&data.UECommissionRate,

		&data.UDCustomerID,
		&data.DrooLocationID,
		&data.DrooCommissionRate,
	)

	if err == sql.ErrNoRows {
		logger.FromContext(ctx).Warn("No user found for '" + username + "' with token '" + token + "'")
		return nil, models.ErrUserNotFound
	}
	if err != nil {
		logger.FromContext(ctx).Error("Login query failed " + err.Error())
		return nil, err
	}

	data.UEDelayUntil = helpers.NullTimeToNullUnixInt(ueDelayUntil)
	data.UEClosedUntil = helpers.NullTimeToNullUnixInt(ueClosedUntil)

	loggedByToken := token != "" && token == data.Token
	if !loggedByToken {
		if !helpers.PasswordMatches(plainPwd, data.Password) {
			return nil, models.ErrUserNotFound
		}

		// Migration automatique vers bcrypt pour les mots de passe legacy
		if !strings.HasPrefix(data.Password, "$2") {
			if newHash, err := helpers.HashPassword(plainPwd); err == nil {
				if err := r.UpdatePassword(ctx, data.UserID, newHash); err == nil {
					data.Password = newHash
				}
			}
		}
	}

	return data, err
}

// UpdatePassword overwrites a user's stored password hash.
func (r *AuthRepository) UpdatePassword(ctx context.Context, userID, newHash string) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `UPDATE users SET password = ? WHERE user_id = ?`, newHash, userID)
	return err
}

// ---------------------------------------------------------------------------
// Password reset ("mot de passe oublié") — see docs/PASSWORD_RESET.md
// ---------------------------------------------------------------------------

// GetUserForPasswordReset resolves a login (username OR email) to the account
// that should receive a reset link. Returns (nil, nil) when nothing matches —
// the caller must not turn that into a distinguishable HTTP response.
//
// The name/email predicate is a deliberate copy of the one in Login so that
// "whatever works to sign in also works here". Two extra conditions apply:
// the account must be enabled, and it must have a deliverable email.
func (r *AuthRepository) GetUserForPasswordReset(ctx context.Context, login string) (*PasswordResetUser, error) {
	login = strings.TrimSpace(login)
	if login == "" {
		return nil, nil
	}

	db := dbx.GetDB(ctx, r.database)
	row := db.QueryRowContext(ctx, `
SELECT u.user_id, u.email, u.first_name, u.last_name
FROM users u
INNER JOIN users_rights ur ON ur.user_id = u.user_id
WHERE
    (
        (UPPER(u.name) = UPPER(?) AND u.name <> '' AND u.name IS NOT NULL)
        OR (UPPER(u.email) = UPPER(?) AND u.email <> '' AND u.email IS NOT NULL)
    )
    AND u.enabled = TRUE
    AND u.email IS NOT NULL
    AND u.email <> ''
LIMIT 1`, login, login)

	user := &PasswordResetUser{}
	err := row.Scan(&user.UserID, &user.Email, &user.FirstName, &user.LastName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return user, nil
}

// CountPasswordResetsSince counts a user's reset requests since a cutoff.
// Backs the per-account rate limit, which lives in SQL rather than Redis so it
// keeps working when the cache is down.
func (r *AuthRepository) CountPasswordResetsSince(ctx context.Context, userID string, since time.Time) (int, error) {
	db := dbx.GetDB(ctx, r.database)

	var count int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM password_resets WHERE user_id = ? AND created_at >= ?`,
		userID, since,
	).Scan(&count)

	return count, err
}

// InsertPasswordReset stores a new reset request. tokenHash is the sha256 hex
// of the token mailed to the user — the clear token is never persisted.
func (r *AuthRepository) InsertPasswordReset(ctx context.Context, id, userID, tokenHash string, expiresAt time.Time, requestedIP string) error {
	db := dbx.GetDB(ctx, r.database)

	var ip interface{}
	if strings.TrimSpace(requestedIP) != "" {
		ip = requestedIP
	}

	_, err := db.ExecContext(ctx, `
INSERT INTO password_resets (id, user_id, token_hash, expires_at, requested_ip)
VALUES (?, ?, ?, ?, ?)`, id, userID, tokenHash, expiresAt, ip)

	return err
}

// ConsumePasswordResetToken atomically marks a token as used and returns the
// user it belongs to. Returns ErrInvalidResetToken when the token is unknown,
// expired, or already consumed — the three are not distinguished on purpose.
//
// The single UPDATE ... RETURNING is what makes the single-use guarantee hold:
// a SELECT followed by an UPDATE would leave a window in which two concurrent
// clicks on the same link both pass. Postgres-only by design — password_resets
// never existed in MySQL (see docs/PASSWORD_RESET.md, decision D8).
func (r *AuthRepository) ConsumePasswordResetToken(ctx context.Context, tokenHash string) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	var userID string
	err := db.QueryRowContext(ctx, `
UPDATE password_resets
SET used_at = `+dbx.UTCNow()+`
WHERE token_hash = ?
  AND used_at IS NULL
  AND expires_at > `+dbx.UTCNow()+`
RETURNING user_id`, tokenHash).Scan(&userID)

	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInvalidResetToken
	}
	if err != nil {
		return "", err
	}

	return userID, nil
}

// RotateRightsTokensForUser issues a fresh session token for every merchant
// link of a user and returns the tokens it replaced.
//
// This is what actually signs the user out everywhere. Deleting the Redis
// entries is not enough: GetUserByToken falls back to `WHERE ur.token = ?` in
// the database, so a cache eviction is silently repaired by the next request.
// Callers should purge the returned tokens from Redis afterwards.
func (r *AuthRepository) RotateRightsTokensForUser(ctx context.Context, userID string) ([]string, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx, `SELECT id, token FROM users_rights WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}

	type rightsRow struct {
		id    string
		token string
	}

	var links []rightsRow
	for rows.Next() {
		var link rightsRow
		if err := rows.Scan(&link.id, &link.token); err != nil {
			rows.Close()
			return nil, err
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	oldTokens := make([]string, 0, len(links))
	for _, link := range links {
		newToken, err := helpers.GenerateToken(32)
		if err != nil {
			return oldTokens, err
		}
		if _, err := db.ExecContext(ctx,
			`UPDATE users_rights SET token = ? WHERE id = ?`, newToken, link.id); err != nil {
			return oldTokens, err
		}
		if strings.TrimSpace(link.token) != "" {
			oldTokens = append(oldTokens, link.token)
		}
	}

	return oldTokens, nil
}

// GetUserByPIN looks up the employee whose PIN matches within a merchant.
// Requires ur.enabled = true AND ur.login_enabled = true so a deactivated link
// cannot authenticate even if its pin_hash was not cleared.
func (r *AuthRepository) GetUserByPIN(ctx context.Context, merchantID, pinHash string) (*UserLoginRow, error) {
	db := dbx.GetDB(ctx, r.database)
	joinCast := authMerchantJoinCast()
	query := fmt.Sprintf(`
SELECT
    u.user_id,
    u.password,
    u.name,
    u.first_name,
    u.last_name,
    u.email,
    u.tel,
    u.enabled,
    u.profile_picture,
	u.email_verified_at,

	ur.id AS merchant_rights_id,
    ur.token AS rights_token,
    ur.access_wrreception,
    ur.access_wrdelivery,
    ur.access_wrwaiter,
    ur.print_merchant_cash_report,
    ur.open_cash_drawer,
    ur.admin,
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
    ur.merchant_id,
	u.mfa_type,
	u.mfa_status,
	u.mfa_verified_at,
	u.mfa_otp_sent_at,

    m.fullName,
    m.merchantTel,
    m.lat,
    m.lng,
    m.timezone,
    CONCAT(m.street_number,' ',m.street,', ',m.zip_code,' ',m.city,', ',m.country),
    m.logo,
    m.web_site,

    mp.delivery_fees,
    mp.delivery_fees_limit,
    mp.delivery_distance_limit,
    mp.manage_on_site,
    mp.manage_take_away,
    mp.manage_delivery,
    mp.kitchen_show_only_paid,
    mp.service_required_for_ordering,
    mp.warning_new_order_not_paid,
    mp.disable_components_under_safety_stock,
    mp.customer_form_requirements,
    mp.currency,
    mp.is_open,
	mp.pos_upsell_enabled,
	mp.pos_covers_count_required,
	mp.waiter_app_can_cash_in,

    p.allow_waiter_account,
    p.allow_delivery_account,
    p.scannorder_ready,
    p.stock_management,
    p.hr_management,
	COALESCE(s.planning_enabled, p.planning_enabled, p.hr_management, FALSE) AS planning_enabled,
	COALESCE(s.haccp_enabled, p.haccp_enabled, TRUE) AS haccp_enabled,
	COALESCE(s.stock_enabled, p.stock_enabled, CASE WHEN p.stock_management > 0 THEN TRUE ELSE FALSE END) AS stock_enabled,
	COALESCE(s.scannorder_enabled, p.scannorder_enabled, p.scannorder_ready, FALSE) AS scannorder_enabled,
	COALESCE(s.bookings_enabled, p.bookings_enabled, TRUE) AS bookings_enabled,
	COALESCE(s.kiosks_enabled, p.kiosks_enabled, TRUE) AS kiosks_enabled,
	COALESCE(s.delivery_enabled, p.delivery_enabled, TRUE) AS delivery_enabled,

    sset.activated,

    iue.store_id,
    iue.estimated_preparation_time,
    iue.delay_until,
    iue.delay_duration,
    iue.closed_until,
	iue.commission_rate,

    iud.customer_id,

    ind.location_id,
	ind.commission_rate

FROM users u
INNER JOIN users_rights ur ON ur.user_id = u.user_id
INNER JOIN merchant m ON %[1]s = ur.merchant_id
LEFT JOIN merchant_parameters mp ON mp.merchant_id = %[1]s
LEFT JOIN subscriptions s ON s.merchant_id = %[1]s
LEFT JOIN packages p ON p.id = s.package_id
LEFT JOIN scannorder_settings sset ON sset.merchant_id = %[1]s
LEFT JOIN integration_uber_eats iue ON iue.merchant_id = %[1]s AND iue.bearer_token IS NOT NULL
LEFT JOIN integration_uber_direct iud ON iud.merchant_id = %[1]s AND iud.bearer_token IS NOT NULL
LEFT JOIN integration_deliveroo ind ON ind.merchant_id = %[1]s

WHERE ur.merchant_id = ? AND ur.pin_hash = ? AND ur.enabled = true AND ur.login_enabled = true
LIMIT 1;
`, joinCast)
	row := db.QueryRowContext(ctx, query, merchantID, pinHash)
	return scanUserLoginRow(row)
}

func (r *AuthRepository) SetPINHash(ctx context.Context, merchantID, userID string, pinHash *string) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx,
		`UPDATE users_rights SET pin_hash = ? WHERE merchant_id = ? AND user_id = ?`,
		pinHash, merchantID, userID)
	return err
}

func (r *AuthRepository) CheckPINConflict(ctx context.Context, merchantID, pinHash, excludeUserID string) (bool, error) {
	db := dbx.GetDB(ctx, r.database)
	var exists int
	err := db.QueryRowContext(ctx,
		`SELECT 1 FROM users_rights WHERE merchant_id = ? AND pin_hash = ? AND user_id != ? LIMIT 1`,
		merchantID, pinHash, excludeUserID).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (r *AuthRepository) GetMerchants(ctx context.Context, userID string) ([]MerchantRow, error) {
	db := dbx.GetDB(ctx, r.database)
	query := fmt.Sprintf(`
SELECT
    m.id,
    m.fullName,
    m.lat,
    m.lng,
    CONCAT(m.street_number,' ',m.street,', ',m.zip_code,' ',m.city,', ',m.country),
    m.city,
    m.country,
    m.zip_code,
	m.logo_url,
    ur.token
FROM merchant m
INNER JOIN users_rights ur ON ur.merchant_id = %s
WHERE ur.user_id IS NOT NULL AND ur.user_id = ?
`, authMerchantJoinCast())
	rows, err := db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var list []MerchantRow
	for rows.Next() {
		var m MerchantRow
		rows.Scan(&m.MerchantID, &m.BusinessName, &m.Lat, &m.Lng, &m.Address, &m.City, &m.Country, &m.ZipCode, &m.LogoURL, &m.Token)
		list = append(list, m)
	}
	return list, nil
}

func (r *AuthRepository) CheckAppVersion(ctx context.Context, currentVersion int, app, merchantID string) (map[string]interface{}, error) {
	db := dbx.GetDB(ctx, r.database)

	// Step 1: get highest version > currentVersion
	q1 := fmt.Sprintf(`
SELECT id, version_code, download_url
FROM app_version
WHERE app_id = ?
  AND version_code > ?
  AND release_date < %s
ORDER BY version_code DESC
LIMIT 1;
`, dbx.UTCNow())
	row := db.QueryRowContext(ctx, q1, app, currentVersion)

	var versionID int
	var versionCode int
	var downloadURL string

	err := row.Scan(&versionID, &versionCode, &downloadURL)
	if err == sql.ErrNoRows {
		return map[string]interface{}{"status": "no_update"}, nil
	}
	if err != nil {
		return nil, err
	}

	// Step 2: check if version is restricted
	q2 := `
SELECT 1 FROM app_version_merchant
WHERE version_code = ?
LIMIT 1;
`
	var restricted int
	err = db.QueryRowContext(ctx, q2, versionCode).Scan(&restricted)
	if err == sql.ErrNoRows {
		// Not restricted → update available
		return map[string]interface{}{
			"status":       "update_available",
			"download_url": downloadURL,
		}, nil
	}
	if err != nil {
		return nil, err
	}

	// Step 3: restricted → check if merchant allowed
	q3 := `
SELECT 1 FROM app_version_merchant
WHERE version_code = ?
  AND merchant_id = ?
LIMIT 1;
`

	var allowed int
	err = db.QueryRowContext(ctx, q3, versionCode, merchantID).Scan(&allowed)
	if err == sql.ErrNoRows {
		return map[string]interface{}{"status": "no_update"}, nil
	}
	if err != nil {
		return nil, err
	}

	// Allowed → update available
	return map[string]interface{}{
		"status":       "update_available",
		"download_url": downloadURL,
	}, nil
}

func (r *AuthRepository) SaveDevice(ctx context.Context, userID, merchantID, app, deviceID, fcmToken string) error {
	db := dbx.GetDB(ctx, r.database)

	// No syntax common to both dialects: ON DUPLICATE KEY UPDATE (MySQL) vs
	// ON CONFLICT ... DO UPDATE (Postgres, on the PK device_id).
	q := fmt.Sprintf(`
INSERT INTO users_devices
(user_id, merchant_id, app, device_id, fcm_token, last_used)
VALUES (?, ?, ?, ?, ?, %[1]s)
ON DUPLICATE KEY UPDATE
    fcm_token = VALUES(fcm_token),
    last_used = %[1]s,
    user_id = VALUES(user_id),
    merchant_id = VALUES(merchant_id)
`, dbx.UTCNow())
	if dbx.ActiveDialect() == dbx.Postgres {
		q = fmt.Sprintf(`
INSERT INTO users_devices
(user_id, merchant_id, app, device_id, fcm_token, last_used)
VALUES (?, ?, ?, ?, ?, %[1]s)
ON CONFLICT (device_id) DO UPDATE SET
    fcm_token = EXCLUDED.fcm_token,
    last_used = %[1]s,
    user_id = EXCLUDED.user_id,
    merchant_id = EXCLUDED.merchant_id
`, dbx.UTCNow())
	}

	_, execErr := db.ExecContext(ctx, q, userID, merchantID, app, deviceID, fcmToken)
	if execErr != nil {
		return execErr
	}

	return nil
}

// UpdateMFAStatus met à jour le statut MFA dans users_rights pour un token donné
func (r *AuthRepository) UpdateMFAStatus(ctx context.Context, userID string, status string) error {
	db := dbx.GetDB(ctx, r.database)
	query := `UPDATE users SET mfa_status = ? WHERE user_id = ?`

	_, err := db.ExecContext(ctx, query, status, userID)
	if err != nil {
		return err
	}

	return nil
}

func (r *AuthRepository) MarkAsOTPSent(ctx context.Context, userID string) error {
	db := dbx.GetDB(ctx, r.database)
	query := fmt.Sprintf(`UPDATE users SET mfa_otp_sent_at = %s WHERE user_id = ?`, dbx.UTCNow())

	_, err := db.ExecContext(ctx, query, userID)
	if err != nil {
		return err
	}

	return nil
}

func (r *AuthRepository) MarkAsMFAVerified(ctx context.Context, userID string) error {
	db := dbx.GetDB(ctx, r.database)
	query := fmt.Sprintf(`UPDATE users SET mfa_status = ?, mfa_verified_at = %s WHERE user_id = ?`, dbx.UTCNow())

	_, err := db.ExecContext(ctx, query, models.MFAStatusVerified, userID)
	if err != nil {
		return err
	}

	return nil
}

func (r *AuthRepository) MarkLastLoginAt(ctx context.Context, userID string) error {
	db := dbx.GetDB(ctx, r.database)
	query := fmt.Sprintf(`UPDATE users SET last_login_at = %s WHERE user_id = ?`, dbx.UTCNow())
	_, err := db.ExecContext(ctx, query, userID)
	return err
}

// MarkAsVerified met à jour la date de validation pour l'email ou le téléphone
func (r *AuthRepository) MarkAsVerified(ctx context.Context, token string, mode string) error {
	db := dbx.GetDB(ctx, r.database)
	var column string

	// On détermine quelle colonne mettre à jour selon le mode
	switch strings.ToUpper(mode) {
	case "EMAIL":
		column = "email_verified_at"
	case "SMS", "TEL":
		column = "tel_verified_at"
	default:
		return errors.New("mode de vérification invalide")
	}

	// UPDATE...JOIN rewritten as EXISTS (portable MySQL/Postgres). Postgres
	// does not allow qualifying SET target columns with the table alias.
	query := fmt.Sprintf(`
		UPDATE users u
		SET %s = NOW()
		WHERE EXISTS (SELECT 1 FROM users_rights ur WHERE ur.user_id = u.user_id AND ur.token = ?)`, column)

	result, err := db.ExecContext(ctx, query, token)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return errors.New("aucun utilisateur trouvé pour ce token")
	}

	return nil
}
