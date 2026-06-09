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
	COALESCE(s.planning_enabled, p.planning_enabled, p.hr_management, FALSE) AS planning_enabled,
	COALESCE(s.haccp_enabled, p.haccp_enabled, TRUE) AS haccp_enabled,
	COALESCE(s.stock_enabled, p.stock_enabled, CASE WHEN p.stock_management > 0 THEN TRUE ELSE FALSE END) AS stock_enabled,
	COALESCE(s.scannorder_enabled, p.scannorder_enabled, p.scannorder_ready, FALSE) AS scannorder_enabled,
	COALESCE(s.bookings_enabled, p.bookings_enabled, TRUE) AS bookings_enabled,

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
		&data.Enabled, &data.ProfilePicture,
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
		&data.PlanningEnabled, &data.HACCPEnabled, &data.StockEnabled, &data.ScanNOrderEnabled, &data.BookingsEnabled,

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

func (r *UsersRepository) UpdatePassword(ctx context.Context, userID string, merchantID string, hash string) (string, error) {
	db := dbutils.GetDB(ctx, r.database)
	newUserToken, err := generateToken()
	if err != nil {
		return "", err
	}

	newMerchantToken, err := generateToken()
	if err != nil {
		return "", err
	}

	// 1. Update password
	res, err := db.ExecContext(ctx, `
		UPDATE users
		SET password = ?, token = ?
		WHERE user_id = ?
	`, hash, newUserToken, userID)
	if err != nil {
		return "", err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return "", err
	}
	if rows != 1 {
		return "", models.ErrNotFound
	}

	// 2. Rotate token only for the connected merchant.
	rightsRes, err := db.ExecContext(ctx, `
		UPDATE users_rights
		SET token = ?
		WHERE user_id = ? AND merchant_id = ?
	`, newMerchantToken, userID, merchantID)
	if err != nil {
		return "", err
	}

	rightsRows, err := rightsRes.RowsAffected()
	if err != nil {
		return "", err
	}
	if rightsRows != 1 {
		return "", models.ErrNotFound
	}

	return newMerchantToken, nil
}

func (r *UsersRepository) GetUserProfile(ctx context.Context, userID string) (*models.UserProfileResponse, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
		SELECT first_name, last_name, email, tel, address, street, city, zip_code, country, lat, lng, profile_picture, mfa_type, email_verified_at, tel_verified_at
		FROM users
		WHERE user_id = ?
		LIMIT 1
	`

	var firstName, lastName, email, phone sql.NullString
	var address, street, city, postalCode, country, avatar, mfaType sql.NullString
	var lat, lng sql.NullFloat64
	var emailVerifiedAt, phoneVerifiedAt sql.NullTime

	if err := db.QueryRowContext(ctx, query, userID).Scan(
		&firstName,
		&lastName,
		&email,
		&phone,
		&address,
		&street,
		&city,
		&postalCode,
		&country,
		&lat,
		&lng,
		&avatar,
		&mfaType,
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
		Street:        nullableString(street),
		City:          nullableString(city),
		PostalCode:    nullableString(postalCode),
		Country:       nullableString(country),
		Lat:           nullableFloat64(lat),
		Lng:           nullableFloat64(lng),
		Avatar:        nullableString(avatar),
		MFAType:       nullableString(mfaType),
		EmailVerified: emailVerifiedAt.Valid,
		PhoneVerified: phoneVerifiedAt.Valid,
	}

	return resp, nil
}

func (r *UsersRepository) UpdateUserProfile(ctx context.Context, userID string, req *models.UpdateUserProfileRequest) error {
	db := dbutils.GetDB(ctx, r.database)

	updates := []string{}
	args := []interface{}{}

	var currentEmail sql.NullString
	var currentPhone sql.NullString
	if req.Email != nil || req.Phone != nil {
		err := db.QueryRowContext(ctx, `SELECT email, tel FROM users WHERE user_id = ? LIMIT 1`, userID).Scan(&currentEmail, &currentPhone)
		if err != nil {
			return err
		}
	}

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
		if !currentEmail.Valid || currentEmail.String != *req.Email {
			updates = append(updates, "email_verified_at = NULL")
		}
	}
	if req.Phone != nil {
		updates = append(updates, "tel = ?")
		args = append(args, *req.Phone)
		if !currentPhone.Valid || currentPhone.String != *req.Phone {
			updates = append(updates, "tel_verified_at = NULL")
		}
	}
	if req.Address != nil {
		updates = append(updates, "address = ?")
		args = append(args, *req.Address)
	}
	if req.Street != nil {
		updates = append(updates, "street = ?")
		args = append(args, *req.Street)
	}
	if req.City != nil {
		updates = append(updates, "city = ?")
		args = append(args, *req.City)
	}
	if req.PostalCode != nil {
		updates = append(updates, "zip_code = ?")
		args = append(args, *req.PostalCode)
	}
	if req.Country != nil {
		updates = append(updates, "country = ?")
		args = append(args, *req.Country)
	}
	if req.Lat != nil {
		updates = append(updates, "lat = ?")
		args = append(args, *req.Lat)
	}
	if req.Lng != nil {
		updates = append(updates, "lng = ?")
		args = append(args, *req.Lng)
	}
	if req.MFAType != nil {
		updates = append(updates, "mfa_type = ?")
		args = append(args, *req.MFAType)
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

func (r *UsersRepository) GetOutOfStockComponents(ctx context.Context, merchantID string, maxNames int) (int, []string, error) {
	db := dbutils.GetDB(ctx, r.database)

	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM components
		WHERE merchant_id = ?
		  AND enabled = 1
		  AND category_id <> 'UBER_EATS_TEMP'
		  AND stock <= 0
	`, merchantID).Scan(&count); err != nil {
		return 0, nil, err
	}

	if count == 0 || maxNames <= 0 {
		return count, []string{}, nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT name
		FROM components
		WHERE merchant_id = ?
		  AND enabled = 1
		  AND category_id <> 'UBER_EATS_TEMP'
		  AND stock <= 0
		ORDER BY name ASC
		LIMIT ?
	`, merchantID, maxNames)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()

	names := make([]string, 0, maxNames)
	for rows.Next() {
		var name sql.NullString
		if err := rows.Scan(&name); err != nil {
			return 0, nil, err
		}
		if name.Valid && strings.TrimSpace(name.String) != "" {
			names = append(names, name.String)
		}
	}

	if err := rows.Err(); err != nil {
		return 0, nil, err
	}

	return count, names, nil
}

func (r *UsersRepository) GetUserVerificationStatus(ctx context.Context, userID string) (*UserVerificationStatus, error) {
	db := dbutils.GetDB(ctx, r.database)

	var email sql.NullString
	var phone sql.NullString
	var emailVerifiedAt sql.NullTime
	var phoneVerifiedAt sql.NullTime

	if err := db.QueryRowContext(ctx, `
		SELECT email, tel, email_verified_at, tel_verified_at
		FROM users
		WHERE user_id = ?
		LIMIT 1
	`, userID).Scan(&email, &phone, &emailVerifiedAt, &phoneVerifiedAt); err != nil {
		return nil, err
	}

	return &UserVerificationStatus{
		Email:         nullableString(email),
		Phone:         nullableString(phone),
		EmailVerified: emailVerifiedAt.Valid,
		PhoneVerified: phoneVerifiedAt.Valid,
	}, nil
}

func nullableString(v sql.NullString) string {
	if v.Valid {
		return v.String
	}
	return ""
}

func nullableFloat64(v sql.NullFloat64) float64 {
	if v.Valid {
		return v.Float64
	}
	return 0
}

func generateToken() (string, error) {
	b := make([]byte, 64)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
