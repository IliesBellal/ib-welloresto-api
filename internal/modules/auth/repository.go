package auth

import (
	"context"
	"database/sql"
	"welloresto-api/internal/helpers"
)

type AuthRepository struct {
	db *sql.DB
}

func NewAuthRepository(db *sql.DB) AuthRepository {
	return AuthRepository{db: db}
}

func (r *AuthRepository) GetUserByToken(ctx context.Context, token string) (*UserLoginRow, error) {
	if token == "" {
		return nil, nil
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
    u.reception_device_token,
    u.waiter_device_token,
    u.delivery_device_token,

    ur.token AS rights_token,
    ur.access_wrreception,
    ur.access_wrdelivery,
    ur.access_wrwaiter,
    ur.print_merchant_cash_report,
    ur.open_cash_drawer,
    ur.merchant_id,

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
INNER JOIN users_rights ur ON ur.id = u.access_id
INNER JOIN merchant m ON m.id = ur.merchant_id
LEFT JOIN merchant_parameters mp ON mp.merchant_id = m.id
LEFT JOIN subscriptions s ON s.merchant_id = m.id
LEFT JOIN packages p ON p.id = s.package_id
LEFT JOIN scannorder_settings sset ON sset.merchant_id = m.id
LEFT JOIN integration_uber_eats iue ON iue.merchant_id = m.id AND iue.bearer_token IS NOT NULL
LEFT JOIN integration_uber_direct iud ON iud.merchant_id = m.id AND iud.bearer_token IS NOT NULL
LEFT JOIN integration_deliveroo ind ON ind.merchant_id = m.id

WHERE ur.token = ? OR u.token = ?
LIMIT 1;
`

	row := r.db.QueryRowContext(ctx, query, token, token)

	data := &UserLoginRow{}

	var ueDelayUntil sql.NullTime
	var ueClosedUntil sql.NullTime

	err := row.Scan(
		&data.UserID, &data.Password, &data.Name, &data.FirstName, &data.LastName, &data.Email, &data.Tel,
		&data.Enabled, &data.PinCode, &data.ProfilePicture,
		&data.ReceptionDeviceToken, &data.WaiterDeviceToken, &data.DeliveryDeviceToken,

		&data.RightsToken, &data.AccessReception, &data.AccessDelivery, &data.AccessWaiter,
		&data.PrintMerchantCashReport, &data.OpenCashDrawer, &data.MerchantID,

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
		return nil, err
	}
	return data, err
}

func (r *AuthRepository) Login(ctx context.Context, username, encryptedPwd, plainPwd, token string) (*UserLoginRow, error) {
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
    u.reception_device_token,
    u.waiter_device_token,
    u.delivery_device_token,
    u.terms_of_use_accepted,

    ur.token AS rights_token,
    ur.access_wrreception,
    ur.access_wrdelivery,
    ur.access_wrwaiter,
    ur.print_merchant_cash_report,
    ur.open_cash_drawer,
    ur.merchant_id,
    ur.admin,

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
INNER JOIN users_rights ur ON ur.id = u.access_id
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
        (UPPER(u.name)=UPPER(?) AND u.password IN (?, ?))
        OR (UPPER(u.email)=UPPER(?) AND u.password IN (?, ?))
        OR (ur.token = ?)
    )
LIMIT 1;
`

	row := r.db.QueryRowContext(ctx, query,
		username, encryptedPwd, plainPwd,
		username, encryptedPwd, plainPwd,
		token,
	)

	data := &UserLoginRow{}

	var ueDelayUntil sql.NullTime
	var ueClosedUntil sql.NullTime

	err := row.Scan(
		&data.UserID, &data.Name, &data.FirstName, &data.LastName, &data.Email,
		&data.Tel, &data.Enabled, &data.PinCode, &data.ProfilePicture, &data.ReceptionDeviceToken,
		&data.WaiterDeviceToken, &data.DeliveryDeviceToken, &data.TermsOfUseAccepted,

		&data.RightsToken, &data.AccessReception, &data.AccessDelivery, &data.AccessWaiter,
		&data.PrintMerchantCashReport, &data.OpenCashDrawer, &data.MerchantID, &data.Admin,

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

	data.UEDelayUntil = helpers.NullTimeToNullUnixInt(ueDelayUntil)
	data.UEClosedUntil = helpers.NullTimeToNullUnixInt(ueClosedUntil)

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
    m.zip_code
FROM merchant m
INNER JOIN users_rights ur ON ur.merchant_id = m.id
WHERE ur.user_id IS NOT NULL AND ur.user_id = ?
`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var list []MerchantRow
	for rows.Next() {
		var m MerchantRow
		rows.Scan(&m.ID, &m.FullName, &m.BusinessName, &m.Lat, &m.Lng, &m.Address, &m.City, &m.Country, &m.ZipCode)
		list = append(list, m)
	}
	return list, nil
}

func (r *AuthRepository) CheckAppVersion(ctx context.Context, currentVersion int, app, merchantID string) (map[string]interface{}, error) {

	tx, err := r.db.BeginTx(ctx, nil)
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

	tx, err := r.db.BeginTx(ctx, nil)
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
