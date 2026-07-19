package users

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/models"
)

type UsersRepository struct {
	database *sql.DB
}

func NewUserRepository(db *sql.DB) *UsersRepository {
	return &UsersRepository{database: db}
}

// usersMerchantJoinCast returns the SQL fragment used to compare merchant.id
// (integer identity) against varchar merchant_id columns — merchant_id is
// carried as a string everywhere in Go (12-merchant-id-unification.md).
// Same pattern as auth.authMerchantJoinCast.
func usersMerchantJoinCast() string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "CAST(m.id AS TEXT)"
	}
	return "CAST(m.id AS CHAR)"
}

func (r *UsersRepository) SetUserLocation(ctx context.Context, req models.UpdateLocationRequest) error {
	db := dbx.GetDB(ctx, r.database)

	// users.lat/lng are text columns and users.heading is an integer NOT NULL:
	// MySQL coerced the float64 params implicitly, Postgres (pgx) refuses to
	// bind float64 on text / NULL on NOT NULL int — format and round in Go.
	lat := strconv.FormatFloat(req.Lat, 'f', -1, 64)
	lng := strconv.FormatFloat(req.Lng, 'f', -1, 64)
	heading := 0
	if req.Heading != nil {
		heading = int(math.Round(*req.Heading))
	}

	_, err := db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE users
		SET lat = ?, lng = ?, heading = ?, last_position_at = %s
		WHERE user_id = ?
	`, dbx.UTCNow()), lat, lng, heading, req.UserID)
	if err != nil {
		return err
	}

	return nil
}

// GetActiveDeliverySessionForUser returns the id and current stop (order_id) of the
// caller's active delivery session, if any. sessionID is "" if there is none.
func (r *UsersRepository) GetActiveDeliverySessionForUser(ctx context.Context, merchantID, userID string) (sessionID string, currentOrderID string, err error) {
	db := dbx.GetDB(ctx, r.database)

	var id string
	var orderID sql.NullString
	err = db.QueryRowContext(ctx, `
		SELECT id, current_order_id FROM delivery_session
		WHERE user_id = ? AND merchant_id = ? AND status = 'active'
		ORDER BY start_date DESC LIMIT 1
	`, userID, merchantID).Scan(&id, &orderID)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}

	return id, orderID.String, nil
}

// InsertDeliveryPosition records a raw position sample for the driver's active session.
func (r *UsersRepository) InsertDeliveryPosition(ctx context.Context, userID, sessionID string, lat, lng float64, heading, accuracy, speed *float64) error {
	db := dbx.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO delivery_position (user_id, delivery_session_id, lat, lng, heading, accuracy, speed, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, %s)
	`, dbx.UTCNow()), userID, sessionID, lat, lng, heading, accuracy, speed)
	if err != nil {
		return err
	}

	return nil
}

// GetDeliveryStopDestination returns the per-stop status and delivery coordinates for
// the given session/order, resolving the temporary-vs-permanent address switch. ok is
// false when no destination coordinates are available (status is still returned).
func (r *UsersRepository) GetDeliveryStopDestination(ctx context.Context, sessionID, orderID string) (status string, lat, lng float64, ok bool, err error) {
	db := dbx.GetDB(ctx, r.database)

	var useTemporary bool
	var customerLat, customerLng sql.NullFloat64
	var customerTemporaryLat, customerTemporaryLng sql.NullString

	err = db.QueryRowContext(ctx, `
		SELECT dso.status, o.use_customer_temporary_address,
		       c.customer_lat, c.customer_lng,
		       c.customer_temporary_lat, c.customer_temporary_lng
		FROM delivery_session_order dso
		JOIN orders o ON o.order_id = dso.order_id
		LEFT JOIN customer c ON c.customer_id = o.customer_id
		WHERE dso.delivery_session_id = ? AND dso.order_id = ?
	`, sessionID, orderID).Scan(&status, &useTemporary, &customerLat, &customerLng, &customerTemporaryLat, &customerTemporaryLng)
	if err == sql.ErrNoRows {
		return "", 0, 0, false, nil
	}
	if err != nil {
		return "", 0, 0, false, err
	}

	if useTemporary {
		if !customerTemporaryLat.Valid || !customerTemporaryLng.Valid {
			return status, 0, 0, false, nil
		}
		parsedLat, errLat := strconv.ParseFloat(strings.TrimSpace(customerTemporaryLat.String), 64)
		parsedLng, errLng := strconv.ParseFloat(strings.TrimSpace(customerTemporaryLng.String), 64)
		if errLat != nil || errLng != nil {
			return status, 0, 0, false, nil
		}
		return status, parsedLat, parsedLng, true, nil
	}

	if !customerLat.Valid || !customerLng.Valid {
		return status, 0, 0, false, nil
	}

	return status, customerLat.Float64, customerLng.Float64, true, nil
}

// MarkStopArrived transitions a stop from en_route to arrived (geofence trigger).
// Returns true if a row was updated (idempotent: a no-op returns false).
func (r *UsersRepository) MarkStopArrived(ctx context.Context, sessionID, orderID string) (bool, error) {
	db := dbx.GetDB(ctx, r.database)

	res, err := db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE delivery_session_order SET status='arrived', arrived_at=%s
		WHERE delivery_session_id = ? AND order_id = ? AND status = 'en_route'
	`, dbx.UTCNow()), sessionID, orderID)
	if err != nil {
		return false, err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}

	return affected > 0, nil
}

func (r *UsersRepository) GetUserByToken(ctx context.Context, token string) (*models.UserLoginRow, error) {
	if token == "" {
		return nil, nil
	}

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
	COALESCE(s.kiosks_enabled, p.kiosks_enabled, TRUE) AS kiosks_enabled,

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
INNER JOIN merchant m ON %[1]s = ur.merchant_id
LEFT JOIN merchant_parameters mp ON mp.merchant_id = %[1]s
LEFT JOIN subscriptions s ON s.merchant_id = %[1]s
LEFT JOIN packages p ON p.id = s.package_id
LEFT JOIN scannorder_settings sset ON sset.merchant_id = %[1]s
LEFT JOIN integration_uber_eats iue ON iue.merchant_id = %[1]s AND iue.bearer_token IS NOT NULL
LEFT JOIN integration_uber_direct iud ON iud.merchant_id = %[1]s AND iud.bearer_token IS NOT NULL
LEFT JOIN integration_deliveroo ind ON ind.merchant_id = %[1]s

WHERE ur.token = ? OR u.token = ?
LIMIT 1;
`, usersMerchantJoinCast())

	row := dbx.GetDB(ctx, r.database).QueryRowContext(ctx, query, token, token)

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
		&data.KiosksEnabled,

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

	// NOTE: the join `ur.id = usv.user_id` (users_rights integer PK vs users
	// varchar user_id) is inherited as-is: MySQL coerced the varchar to a
	// number silently, Postgres needs an explicit cast. Both dialects only
	// match when user_id is a plain numeric string — preserved verbatim (no
	// behaviour fix) per the Tier 1/2 precedent on preexisting anomalies.
	idCast := "CAST(ur.id AS CHAR)"
	if dbx.ActiveDialect() == dbx.Postgres {
		idCast = "CAST(ur.id AS TEXT)"
	}
	query := fmt.Sprintf(`
        SELECT
            usv.user_id,
            usv.first_name,
            usv.last_name,
            usv.lat,
            usv.lng,
            usv.status
        FROM user_status_view usv
        INNER JOIN users_rights ur ON %s = usv.user_id
        INNER JOIN merchant m ON %s = ur.merchant_id
        WHERE usv.user_id = ?
        AND ur.merchant_id = ?
        AND ur.enabled = TRUE
        LIMIT 1;
    `, idCast, usersMerchantJoinCast())

	row := dbx.GetDB(ctx, r.database).QueryRowContext(ctx, query, userID, merchantID)

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
	db := dbx.GetDB(ctx, r.database)
	newUserToken, err := generateToken()
	if err != nil {
		return "", err
	}

	newMerchantToken, err := generateToken()
	if err != nil {
		return "", err
	}

	// users.token is varchar(30): MySQL (non-strict) silently truncated the
	// 128-char generated token, Postgres rejects it — truncate Go-side to keep
	// the historical stored value identical in both dialects. The rotated value
	// only serves to invalidate the previous user token.
	if len(newUserToken) > 30 {
		newUserToken = newUserToken[:30]
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
	db := dbx.GetDB(ctx, r.database)

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
	db := dbx.GetDB(ctx, r.database)

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
	// users.lat/lng are text columns — format Go-side (pgx refuses float64 on text).
	if req.Lat != nil {
		updates = append(updates, "lat = ?")
		args = append(args, strconv.FormatFloat(*req.Lat, 'f', -1, 64))
	}
	if req.Lng != nil {
		updates = append(updates, "lng = ?")
		args = append(args, strconv.FormatFloat(*req.Lng, 'f', -1, 64))
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
	db := dbx.GetDB(ctx, r.database)

	var avatar sql.NullString
	err := db.QueryRowContext(ctx, `SELECT profile_picture FROM users WHERE user_id = ?`, userID).Scan(&avatar)
	if err != nil {
		return "", err
	}
	return nullableString(avatar), nil
}

func (r *UsersRepository) GetMerchantCountryCode(ctx context.Context, merchantID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	var country sql.NullString
	err := db.QueryRowContext(ctx, `SELECT country FROM merchant WHERE id = ? LIMIT 1`, merchantID).Scan(&country)
	if err != nil {
		return "", err
	}

	return nullableString(country), nil
}

func (r *UsersRepository) UpdateUserAvatar(ctx context.Context, userID, avatarURL string) error {
	db := dbx.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `UPDATE users SET profile_picture = ? WHERE user_id = ?`, avatarURL, userID)
	return err
}

func (r *UsersRepository) GetOutOfStockComponents(ctx context.Context, merchantID string, maxNames int) (int, []string, error) {
	db := dbx.GetDB(ctx, r.database)

	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM components
		WHERE merchant_id = ?
		  AND enabled = TRUE
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
		  AND enabled = TRUE
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
	db := dbx.GetDB(ctx, r.database)

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
