package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

type AuthRepository struct {
	database *sql.DB
}

func NewAuthRepository(db *sql.DB) AuthRepository {
	return AuthRepository{database: db}
}

func (r *AuthRepository) GetUserByToken(ctx context.Context, token string) (*UserLoginRow, error) {
	if strings.TrimSpace(token) == "" {
		return nil, nil // ou une erreur métier type ErrUnauthorized
	}

	query := `
SELECT
    u.user_id,
    u.password,
    u.name,
    u.first_name,
    u.last_name,
    u.email,
    u.tel,
    u.enabled,
    u.pin_code,
    u.profile_picture,
	u.email_verified_at,

    ur.token AS rights_token,
    ur.access_wrreception,
    ur.access_wrdelivery,
    ur.access_wrwaiter,
    ur.print_merchant_cash_report,
    ur.open_cash_drawer,
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
    mp.currency,
    mp.is_open,

    p.allow_waiter_account,
    p.allow_delivery_account,
    p.scannorder_ready,
    p.stock_management,
    p.hr_management,

    sset.activated,

    iue.store_id,
    iue.estimated_preparation_time,
    iue.delay_until,
    iue.delay_duration,
    iue.closed_until,

    iud.customer_id,

    ind.location_id

FROM users u
INNER JOIN users_rights ur ON ur.user_id = u.user_id
INNER JOIN merchant m ON m.id = ur.merchant_id
LEFT JOIN merchant_parameters mp ON mp.merchant_id = m.id
LEFT JOIN subscriptions s ON s.merchant_id = m.id
LEFT JOIN packages p ON p.id = s.package_id
LEFT JOIN scannorder_settings sset ON sset.merchant_id = m.id
LEFT JOIN integration_uber_eats iue ON iue.merchant_id = m.id AND iue.bearer_token IS NOT NULL
LEFT JOIN integration_uber_direct iud ON iud.merchant_id = m.id AND iud.bearer_token IS NOT NULL
LEFT JOIN integration_deliveroo ind ON ind.merchant_id = m.id

WHERE ur.token = ? 
LIMIT 1;
`

	row := r.database.QueryRowContext(ctx, query, token)

	data := &UserLoginRow{}

	var ueDelayUntil sql.NullTime
	var ueClosedUntil sql.NullTime

	err := row.Scan(
		&data.UserID, &data.Password, &data.Name, &data.FirstName, &data.LastName, &data.Email, &data.Tel,
		&data.Enabled, &data.PinCode, &data.ProfilePicture, &data.EmailVerifiedAt,

		&data.Token, &data.Rights.AccessReception, &data.Rights.AccessDelivery, &data.Rights.AccessWaiter,
		&data.Rights.PrintMerchantCashReport, &data.Rights.OpenCashDrawer, &data.MerchantID,
		&data.MFAType, &data.MFAStatus, &data.MFAVerifiedAt, &data.MFAOTPSentAt,

		&data.MerchantName, &data.MerchantTel, &data.MerchantLat, &data.MerchantLng, &data.TimeZone,
		&data.MerchantAddress, &data.MerchantLogo, &data.WebSite,

		&data.DeliveryFees, &data.DeliveryFeesLimit, &data.DeliveryDistanceLimit,
		&data.ManageOnSite, &data.ManageTakeAway, &data.ManageDelivery,
		&data.KitchenShowOnlyPaid, &data.ServiceRequiredForOrdering,
		&data.WarningNewOrderNotPaid, &data.DisableSafetyStock,
		&data.Currency, &data.IsOpen,

		&data.AllowWaiterAccount, &data.AllowDeliveryAccount,
		&data.ScanNOrderReady, &data.StockManagement, &data.HrManagement,

		&data.SNOActivated,

		&data.UEStoreID, &data.UEPrepTime, &ueDelayUntil, &data.UEDelayDuration, &ueClosedUntil,

		&data.UDCustomerID,
		&data.DrooLocationID,
	)

	data.UEDelayUntil = helpers.NullTimeToNullUnixInt(ueDelayUntil)
	data.UEClosedUntil = helpers.NullTimeToNullUnixInt(ueClosedUntil)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	return data, err
}

func (r *AuthRepository) Login(ctx context.Context, username, plainPwd, token string) (*UserLoginRow, error) {
	query := `
SELECT
    u.user_id,
    u.name,
    u.first_name,
    u.last_name,
    u.email,
    u.tel,
    u.enabled,
    u.pin_code,
    u.profile_picture,
    u.terms_of_use_accepted,
    u.password,
	u.email_verified_at,

    ur.token AS rights_token,
    ur.access_wrreception,
    ur.access_wrdelivery,
    ur.access_wrwaiter,
    ur.print_merchant_cash_report,
    ur.open_cash_drawer,
    ur.merchant_id,
    ur.admin,
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
    mp.service_required_for_ordering,
    mp.cash_register_required_for_ordering,
    mp.warning_new_order_not_paid,
    mp.disable_components_under_safety_stock,
    mp.currency,
    mp.is_open,

    p.allow_waiter_account,
    p.allow_delivery_account,
    p.scannorder_ready,
    p.stock_management,
    p.hr_management,

    sset.activated,

    iue.store_id,
    iue.estimated_preparation_time,
    iue.delay_until,
    iue.delay_duration,
    iue.closed_until,

    iud.customer_id,

    ind.location_id

FROM users u
INNER JOIN users_rights ur ON ur.user_id = u.user_id
INNER JOIN merchant m ON m.id = ur.merchant_id
LEFT JOIN merchant_parameters mp ON mp.merchant_id = m.id
LEFT JOIN subscriptions s ON s.merchant_id = m.id
LEFT JOIN packages p ON p.id = s.package_id
LEFT JOIN scannorder_settings sset ON sset.merchant_id = m.id
LEFT JOIN integration_uber_eats iue ON iue.merchant_id = m.id AND iue.bearer_token IS NOT NULL
LEFT JOIN integration_uber_direct iud ON iud.merchant_id = m.id AND iud.bearer_token IS NOT NULL
LEFT JOIN integration_deliveroo ind ON ind.merchant_id = m.id

WHERE 
    (
        UPPER(u.name)=UPPER(?)
        OR UPPER(u.email)=UPPER(?)
        OR ur.token = ?
    )
LIMIT 1;
`

	row := r.database.QueryRowContext(ctx, query,
		username,
		username,
		token,
	)

	data := &UserLoginRow{}

	var ueDelayUntil sql.NullTime
	var ueClosedUntil sql.NullTime

	err := row.Scan(
		&data.UserID, &data.Name, &data.FirstName, &data.LastName, &data.Email,
		&data.Tel, &data.Enabled, &data.PinCode, &data.ProfilePicture,
		&data.TermsOfUseAccepted, &data.Password, &data.EmailVerifiedAt,

		&data.Token, &data.Rights.AccessReception, &data.Rights.AccessDelivery, &data.Rights.AccessWaiter,
		&data.Rights.PrintMerchantCashReport, &data.Rights.OpenCashDrawer, &data.MerchantID, &data.Rights.Admin,
		&data.MFAType, &data.MFAStatus, &data.MFAVerifiedAt, &data.MFAOTPSentAt,

		&data.MerchantName, &data.MerchantTel, &data.MerchantLat, &data.MerchantLng, &data.TimeZone,
		&data.MerchantAddress, &data.MerchantLogo, &data.WebSite,

		&data.DeliveryFees, &data.DeliveryFeesLimit, &data.DeliveryDistanceLimit,
		&data.ManageOnSite, &data.ManageTakeAway, &data.ManageDelivery,
		&data.KitchenShowOnlyPaid, &data.KitchenDistributionMode, &data.ProductionDisplayMode, &data.PagerNumberRequired, &data.ServiceRequiredForOrdering,
		&data.CashRegisterRequiredForOrdering, &data.WarningNewOrderNotPaid, &data.DisableSafetyStock,
		&data.Currency, &data.IsOpen,

		&data.AllowWaiterAccount, &data.AllowDeliveryAccount,
		&data.ScanNOrderReady, &data.StockManagement, &data.HrManagement,

		&data.SNOActivated,

		&data.UEStoreID,
		&data.UEPrepTime,
		&ueDelayUntil,
		&data.UEDelayDuration,
		&ueClosedUntil,

		&data.UDCustomerID,
		&data.DrooLocationID,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	data.UEDelayUntil = helpers.NullTimeToNullUnixInt(ueDelayUntil)
	data.UEClosedUntil = helpers.NullTimeToNullUnixInt(ueClosedUntil)

	loggedByToken := token != "" && token == data.Token
	if !loggedByToken {
		if !loggedByToken && !strings.HasPrefix(data.Password, "$2") {
			/*
				conversion automatique en hash
				newHash, err := HashPassword(plainPwd)
				if err == nil {
					_ = r.userRepo.UpdatePassword(ctx, data.UserID, newHash)
				}
			*/
		}

		if !helpers.PasswordMatches(plainPwd, data.Password) {
			return nil, errors.New("invalid_credentials")
		}
	}

	if err == sql.ErrNoRows {
		return nil, nil
	}
	return data, err
}

func (r *AuthRepository) GetMerchants(ctx context.Context, userID string) ([]MerchantRow, error) {
	query := `
SELECT 
    m.id,
    m.fullName,
    m.fullName,
    m.lat,
    m.lng,
    CONCAT(m.street_number,' ',m.street,', ',m.zip_code,' ',m.city,', ',m.country),
    m.city,
    m.country,
    m.zip_code,
    ur.token
FROM merchant m
INNER JOIN users_rights ur ON ur.merchant_id = m.id
WHERE ur.user_id IS NOT NULL AND ur.user_id = ?
`
	rows, err := r.database.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var list []MerchantRow
	for rows.Next() {
		var m MerchantRow
		rows.Scan(&m.MerchantID, &m.FullName, &m.BusinessName, &m.Lat, &m.Lng, &m.Address, &m.City, &m.Country, &m.ZipCode, &m.Token)
		list = append(list, m)
	}
	return list, nil
}

func (r *AuthRepository) CheckAppVersion(ctx context.Context, currentVersion int, app, merchantID string) (map[string]interface{}, error) {

	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	// Step 1: get highest version > currentVersion
	q1 := `
SELECT id, version_code, download_url
FROM app_version
WHERE app_id = ?
  AND version_code > ?
  AND release_date < UTC_TIMESTAMP()
ORDER BY version_code DESC
LIMIT 1;
`
	row := tx.QueryRowContext(ctx, q1, app, currentVersion)

	var versionID int
	var versionCode int
	var downloadURL string

	err = row.Scan(&versionID, &versionCode, &downloadURL)
	if err == sql.ErrNoRows {
		tx.Commit()
		return map[string]interface{}{"status": "no_update"}, nil
	}
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	// Step 2: check if version is restricted
	q2 := `
SELECT 1 FROM app_version_merchant
WHERE version_code = ?
LIMIT 1;
`
	var restricted int
	err = tx.QueryRowContext(ctx, q2, versionCode).Scan(&restricted)
	if err == sql.ErrNoRows {
		// Not restricted → update available
		tx.Commit()
		return map[string]interface{}{
			"status":       "update_available",
			"download_url": downloadURL,
		}, nil
	}
	if err != nil {
		tx.Rollback()
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
	err = tx.QueryRowContext(ctx, q3, versionCode, merchantID).Scan(&allowed)
	if err == sql.ErrNoRows {
		tx.Commit()
		return map[string]interface{}{"status": "no_update"}, nil
	}
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	// Allowed → update available
	tx.Commit()
	return map[string]interface{}{
		"status":       "update_available",
		"download_url": downloadURL,
	}, nil
}

func (r *AuthRepository) SaveDevice(ctx context.Context, userID, merchantID, app, deviceID, fcmToken string) error {

	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	q := `
INSERT INTO users_devices
(user_id, merchant_id, app, device_id, fcm_token, last_used)
VALUES (?, ?, ?, ?, ?, UTC_TIMESTAMP())
ON DUPLICATE KEY UPDATE
    fcm_token = VALUES(fcm_token),
    last_used = UTC_TIMESTAMP(),
    user_id = VALUES(user_id),
    merchant_id = VALUES(merchant_id)
`

	_, execErr := tx.ExecContext(ctx, q, userID, merchantID, app, deviceID, fcmToken)
	if execErr != nil {
		tx.Rollback()
		return execErr
	}

	return tx.Commit()
}

// UpdateMFAStatus met à jour le statut MFA dans users_rights pour un token donné
func (r *AuthRepository) UpdateMFAStatus(ctx context.Context, userID string, status string) error {
	query := `UPDATE users SET mfa_status = ? WHERE user_id = ?`

	_, err := r.database.ExecContext(ctx, query, status, userID)
	if err != nil {
		return err
	}

	if err != nil {
		return err
	}

	return nil
}

func (r *AuthRepository) MarkAsOTPSent(ctx context.Context, userID string) error {
	query := `UPDATE users SET mfa_otp_sent_at = UTC_TIMESTAMP() WHERE user_id = ?`

	_, err := r.database.ExecContext(ctx, query, userID)
	if err != nil {
		return err
	}

	return nil
}

func (r *AuthRepository) MarkAsMFAVerified(ctx context.Context, userID string) error {
	query := `UPDATE users SET mfa_status = ?, mfa_verified_at = UTC_TIMESTAMP() WHERE user_id = ?`

	_, err := r.database.ExecContext(ctx, query, models.MFAStatusVerified, userID)
	if err != nil {
		return err
	}

	if err != nil {
		return err
	}

	return nil
}

// MarkAsVerified met à jour la date de validation pour l'email ou le téléphone
func (r *AuthRepository) MarkAsVerified(ctx context.Context, token string, mode string) error {
	var column string

	// On détermine quelle colonne mettre à jour selon le mode
	switch strings.ToUpper(mode) {
	case "EMAIL":
		column = "u.email_verified_at"
	case "SMS", "TEL":
		column = "u.tel_verified_at"
	default:
		return errors.New("mode de vérification invalide")
	}

	// On joint users et users_rights pour identifier l'user via son token
	query := fmt.Sprintf(`
		UPDATE users u
		INNER JOIN users_rights ur ON u.user_id = ur.user_id
		SET %s = NOW()
		WHERE ur.token = ?`, column)

	result, err := r.database.ExecContext(ctx, query, token)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return errors.New("aucun utilisateur trouvé pour ce token")
	}

	return nil
}
