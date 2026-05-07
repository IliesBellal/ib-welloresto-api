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

func (r *UsersRepository) GetUserProfile(ctx context.Context, userID string) (*models.UserProfileResponse, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
		SELECT first_name, last_name, email, tel, address, profile_picture, email_verified_at, tel_verified_at
		FROM users
		WHERE user_id = ?
		LIMIT 1
	`

	var firstName, lastName, email, phone sql.NullString
	var address, avatar sql.NullString
	var emailVerifiedAt, phoneVerifiedAt sql.NullTime

	if err := db.QueryRowContext(ctx, query, userID).Scan(
		&firstName,
		&lastName,
		&email,
		&phone,
		&address,
		&avatar,
		&emailVerifiedAt,
		&phoneVerifiedAt,
	); err != nil {
		return nil, err
	}

	resp := &models.UserProfileResponse{
		FirstName:     nullableString(firstName),
		LastName:      nullableString(lastName),
		Email:         nullableString(email),
		Phone:         nullableString(phone),
		Address:       nullableString(address),
		Avatar:        nullableString(avatar),
		EmailVerified: emailVerifiedAt.Valid,
		PhoneVerified: phoneVerifiedAt.Valid,
	}

	return resp, nil
}

func (r *UsersRepository) UpdateUserProfile(ctx context.Context, userID string, req *models.UpdateUserProfileRequest) error {
	db := dbutils.GetDB(ctx, r.database)

	updates := []string{}
	args := []interface{}{}

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
		updates = append(updates, "email_verified_at = NULL")
	}
	if req.Phone != nil {
		updates = append(updates, "tel = ?")
		args = append(args, *req.Phone)
		updates = append(updates, "tel_verified_at = NULL")
	}
	if req.Address != nil {
		updates = append(updates, "address = ?")
		args = append(args, *req.Address)
	}
	if req.Avatar != nil {
		updates = append(updates, "profile_picture = ?")
		args = append(args, *req.Avatar)
	}

	if len(updates) == 0 {
		return nil
	}

	args = append(args, userID)

	query := fmt.Sprintf(`
		UPDATE users
		SET %s
		WHERE user_id = ?
	`, strings.Join(updates, ", "))

	_, err := db.ExecContext(ctx, query, args...)
	return err
}

func (r *UsersRepository) GetUserAvatarURL(ctx context.Context, userID string) (string, error) {
	db := dbutils.GetDB(ctx, r.database)

	var avatar sql.NullString
	err := db.QueryRowContext(ctx, `SELECT profile_picture FROM users WHERE user_id = ?`, userID).Scan(&avatar)
	if err != nil {
		return "", err
	}
	return nullableString(avatar), nil
}

func (r *UsersRepository) GetMerchantCountryCode(ctx context.Context, merchantID string) (string, error) {
	db := dbutils.GetDB(ctx, r.database)

	var country sql.NullString
	err := db.QueryRowContext(ctx, `SELECT country FROM merchant WHERE id = ? LIMIT 1`, merchantID).Scan(&country)
	if err != nil {
		return "", err
	}

	return nullableString(country), nil
}

func (r *UsersRepository) UpdateUserAvatar(ctx context.Context, userID, avatarURL string) error {
	db := dbutils.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `UPDATE users SET profile_picture = ? WHERE user_id = ?`, avatarURL, userID)
	return err
}

func nullableString(v sql.NullString) string {
	if v.Valid {
		return v.String
	}
	return ""
}

func generateToken() (string, error) {
	b := make([]byte, 64)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
