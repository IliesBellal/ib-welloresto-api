package users

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

type UsersRepository struct {
	database *sql.DB
}

func NewUserRepository(db *sql.DB) *UsersRepository {
	return &UsersRepository{database: db}
}

func (r *UsersRepository) SetUserLocation(ctx context.Context, req models.UpdateLocationRequest) error {
	db := dbutils.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `
		UPDATE user_status_view
		SET lat = ?, lng = ?
		WHERE user_id = ?
	`, req.Lat, req.Lng, req.UserID)
	if err != nil {
		return err
	}

	return nil
}

func (r *UsersRepository) GetUserByToken(ctx context.Context, token string) (*models.UserLoginRow, error) {
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

	row := r.database.QueryRowContext(ctx, query, token, token)

	data := &models.UserLoginRow{}

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

		&data.UEStoreID, &data.UEPrepTime, &data.UEDelayUntil, &data.UEDelayDuration, &data.UEClosedUntil,

		&data.UDCustomerID,
		&data.DrooLocationID,
	)

	if err == sql.ErrNoRows {
		return nil, models.ErrInvalidToken
	}
	return data, err
}

func (r *UsersRepository) GetUserLocation(ctx context.Context, merchantID, userID string) (*models.OrderUser, error) {

	query := `
        SELECT 
            usv.user_id,
            usv.first_name,
            usv.last_name,
            usv.lat,
            usv.lng,
            usv.status
        FROM user_status_view usv
        INNER JOIN users_rights ur ON ur.id = usv.user_id
        INNER JOIN merchant m ON m.id = ur.merchant_id
        WHERE usv.user_id = ?
        AND ur.merchant_id = ?
        AND ur.enabled = TRUE
        LIMIT 1;
    `

	row := r.database.QueryRowContext(ctx, query, userID, merchantID)

	var res models.OrderUser
	err := row.Scan(
		&res.UserID,
		&res.FirstName,
		&res.LastName,
		&res.Lat,
		&res.Lng,
		&res.Status,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return &res, nil
}

func (r *UsersRepository) UpdateUserSettings(ctx context.Context, userID string, req *models.UserSettingsRequest) error {

	updates := []string{}
	args := []interface{}{}

	// Build dynamic update
	if req.FirstName != nil {
		updates = append(updates, "first_name = ?")
		args = append(args, *req.FirstName)
	}
	if req.LastName != nil {
		updates = append(updates, "last_name = ?")
		args = append(args, *req.LastName)
	}
	if req.Email != nil {
		updates = append(updates, "email = ?")
		args = append(args, *req.Email)
	}
	if req.Tel != nil {
		updates = append(updates, "tel = ?")
		args = append(args, *req.Tel)
	}
	if req.Address != nil {
		updates = append(updates, "address = ?")
		args = append(args, *req.Address)
	}
	if req.StreetNumber != nil {
		updates = append(updates, "street_number = ?")
		args = append(args, *req.StreetNumber)
	}
	if req.Street != nil {
		updates = append(updates, "street = ?")
		args = append(args, *req.Street)
	}
	if req.City != nil {
		updates = append(updates, "city = ?")
		args = append(args, *req.City)
	}
	if req.Country != nil {
		updates = append(updates, "country = ?")
		args = append(args, *req.Country)
	}
	if req.ZipCode != nil {
		updates = append(updates, "zip_code = ?")
		args = append(args, *req.ZipCode)
	}
	if req.PlanningColor != nil {
		updates = append(updates, "planning_color = ?")
		args = append(args, *req.PlanningColor)
	}
	if req.ProfilePicture != nil {
		updates = append(updates, "profile_picture = ?")
		args = append(args, *req.ProfilePicture)
	}
	if req.TermsOfUseAccepted != nil {
		updates = append(updates, "terms_of_use_accepted = ?")
		args = append(args, *req.TermsOfUseAccepted)
	}
	if req.WaiterDeviceToken != nil {
		updates = append(updates, "waiter_device_token = ?")
		args = append(args, *req.WaiterDeviceToken)
	}
	if req.ReceptionDeviceToken != nil {
		updates = append(updates, "reception_device_token = ?")
		args = append(args, *req.ReceptionDeviceToken)
	}
	if req.DeliveryDeviceToken != nil {
		updates = append(updates, "delivery_device_token = ?")
		args = append(args, *req.DeliveryDeviceToken)
	}

	if len(updates) == 0 {
		return nil // nothing to update
	}

	args = append(args, userID)

	query := fmt.Sprintf(`
		UPDATE users
		SET %s
		WHERE user_id = ?
	`, strings.Join(updates, ", "))

	_, err := r.database.ExecContext(ctx, query, args...)
	return err
}

func (r *UsersRepository) UpdatePassword(ctx context.Context, userID string, merchantID string, hash string) error {
	db := dbutils.GetDB(ctx, r.database)

	// 1. Update password
	res, err := db.ExecContext(ctx, `
		UPDATE users
		SET password = ?
		WHERE user_id = ?
	`, hash, userID)
	if err != nil {
		return err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return models.ErrNotFound
	}

	// 2. Load all users_rights for this user
	rowsUR, err := db.QueryContext(ctx, `
		SELECT id
		FROM users_rights
		WHERE user_id = ?
	`, userID)
	if err != nil {
		return err
	}
	defer rowsUR.Close()

	type ur struct {
		id int64
	}

	var rights []ur
	for rowsUR.Next() {
		var r ur
		if err := rowsUR.Scan(&r.id); err != nil {
			return err
		}
		rights = append(rights, r)
	}

	if err := rowsUR.Err(); err != nil {
		return err
	}

	// 3. Reset token for each merchant
	for _, rgt := range rights {
		newToken, err := generateToken()
		if err != nil {
			return err
		}

		_, err = db.ExecContext(ctx, `
			UPDATE users_rights
			SET token = ?
			WHERE id = ?
		`, newToken, rgt.id)
		if err != nil {
			return err
		}
	}

	return nil
}

func generateToken() (string, error) {
	b := make([]byte, 64)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
