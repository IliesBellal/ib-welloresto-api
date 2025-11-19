package repositories

import (
	"context"
	"database/sql"
	"welloresto-api/internal/models"
)

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Login(ctx context.Context, username, encryptedPwd, plainPwd, token string) (*models.UserLoginRow, error) {
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

WHERE 
    (
        (UPPER(u.name)=UPPER(?) AND u.password IN (?, ?))
        OR (ur.token = ?)
    )
LIMIT 1;
`

	row := r.db.QueryRowContext(ctx, query,
		username, encryptedPwd, plainPwd,
		token,
	)

	data := &models.UserLoginRow{}

	err := row.Scan(
		&data.UserID, &data.Name, &data.FirstName, &data.LastName, &data.Email, &data.Tel,
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

		&data.UEStoreID, &data.UEPrepTime, &data.UEDelayUntil, &data.UEDelayDuration, &data.UEClosedUntil,

		&data.UDCustomerID,
		&data.DrooLocationID,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	return data, err
}

func (r *UserRepository) GetMerchants(ctx context.Context, userID string) ([]models.MerchantRow, error) {
	query := `
SELECT 
    m.id,
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

	var list []models.MerchantRow
	for rows.Next() {
		var m models.MerchantRow
		rows.Scan(&m.ID, &m.FullName, &m.Lat, &m.Lng, &m.Address, &m.City, &m.Country, &m.ZipCode)
		list = append(list, m)
	}
	return list, nil
}

func (r *UserRepository) GetUserByToken(ctx context.Context, token string) (*models.UserLoginRow, error) {
	if token == "" {
		return nil, nil
	}

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

	data := &models.UserLoginRow{}

	err := row.Scan(
		&data.UserID, &data.Name, &data.FirstName, &data.LastName, &data.Email, &data.Tel,
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

		&data.UEStoreID, &data.UEPrepTime, &data.UEDelayUntil, &data.UEDelayDuration, &data.UEClosedUntil,

		&data.UDCustomerID,
		&data.DrooLocationID,
	)

	if err == sql.ErrNoRows {
		return nil, err
	}
	return data, err
}
