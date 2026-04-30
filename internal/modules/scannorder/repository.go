package scannorder

import (
	"context"
	"database/sql"
	"fmt"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

type Repository struct {
	database *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db}
}

func (r *Repository) GetMerchantByQR(ctx context.Context, qr string) (*models.MerchantRow, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
    SELECT m.id, m.fullName, m.address, m.lat, m.lng, m.timezone,
           mp.currency, mp.primary_color, mp.text_color_on_primary_color,
           mp.delivery_fees, mp.delivery_fees_limit, mp.preparation_time_mode, mp.preparation_time, mp.delivery_distance_limit,
           
           qr.menu_only, qr.user_id, qr.last_waiter_call, qr.creation_date,
           
           o.order_id, l.location_id, l.location_name, snos.variable_fees, snos.fixed_fees, sa.account_id,
           
           snos.take_away_enabled, snos.take_away_available, 
           snos.delivery_enabled, snos.delivery_available,
		   snos.in_enabled, snos.in_available, mp.enable_advance_orders
    
    FROM   qrcodes qr
          INNER JOIN merchant m on m.id = qr.merchant_id
          LEFT JOIN stripe_accounts sa on sa.merchant_id = m.id
          INNER JOIN scannorder_settings snos on snos.merchant_id = m.id
          INNER JOIN merchant_parameters mp on mp.merchant_id = m.id
          LEFT JOIN bookings_settings bs on bs.merchant_id = m.id
          LEFT JOIN locations l on l.location_id = qr.location_id
          LEFT JOIN (SELECT o.order_id, ol.location_id FROM orders o INNER JOIN order_location ol on ol.order_id = o.order_id WHERE o.state = 'OPEN') o on o.location_id = l.location_id
    WHERE qr.code = ?`

	row := models.MerchantRow{}
	err := db.QueryRowContext(ctx, query, qr).Scan(
		&row.MerchantID, &row.FullName, &row.Address, &row.Lat, &row.Lng, &row.Timezone,
		&row.Currency, &row.PrimaryColor, &row.TextColor,
		&row.DeliveryFees, &row.DeliveryFeesLimit, &row.PrepTimeMode, &row.PrepTime, &row.DeliveryDistanceLimit,
		&row.MenuOnly, &row.UserID, &row.LastWaiterCall, &row.CreationDate,
		&row.OrderID, &row.LocationID, &row.LocationName, &row.VariableFees, &row.FixedFees, &row.AccountID,
		&row.TakeawayEnabled, &row.TakeawayAvailable,
		&row.DeliveryEnabled, &row.DeliveryAvailable,
		&row.InEnabled, &row.InAvailable, &row.EnableAdvanceOrders,
	)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *Repository) GetAvailableSlots(ctx context.Context, merchantID string, prepMinutes int) (map[string][]TimeSlot, error) {
	db := dbutils.GetDB(ctx, r.database)

	// On convertit les minutes en format TIME (HH:MM:SS) pour MySQL
	prepDelay := fmt.Sprintf("%02d:%02d:00", prepMinutes/60, prepMinutes%60)

	query := `
        WITH RECURSIVE time_slots AS (
            SELECT MAKETIME(0, 30, 0) AS time_slot
            UNION ALL
            SELECT ADDTIME(time_slot, '00:30:00')
            FROM time_slots
            WHERE time_slot < MAKETIME(23, 30, 0)
        )
        SELECT 
            DATE_FORMAT(DATE_ADD(CURDATE(), INTERVAL days_to_add DAY), '%Y-%m-%d') AS open_date,
            TIME_FORMAT(time_slots.time_slot, '%H:%i') as slot_time
        FROM (
            SELECT 0 AS days_to_add UNION ALL SELECT 1 UNION ALL SELECT 2
        ) AS days
        JOIN time_slots
        JOIN hours_of_operation AS hoo
            ON hoo.merchant_id = ?
            AND hoo.enabled = 1
            AND (DAYOFWEEK(CURDATE() + INTERVAL days_to_add DAY) - 1) BETWEEN hoo.day_of_week_from AND hoo.day_of_week_to
            AND time_slots.time_slot > hoo.hour_from
            AND time_slots.time_slot <= hoo.hour_to
        LEFT JOIN merchant_parameters AS mp ON mp.merchant_id = hoo.merchant_id
        WHERE (hoo.valid_from IS NULL OR hoo.valid_from <= CURDATE() + INTERVAL days_to_add DAY)
          AND (hoo.valid_to IS NULL OR hoo.valid_to >= CURDATE() + INTERVAL days_to_add DAY)
          AND days_to_add < COALESCE(mp.advance_order_days, 3)
          AND (
                DATE_ADD(CURDATE(), INTERVAL days_to_add DAY) > CURDATE() OR 
                (DATE_ADD(CURDATE(), INTERVAL days_to_add DAY) = CURDATE() AND 
                 -- ICI : On utilise le délai dynamique au lieu de 30 min fixe
                 ADDTIME(CURTIME(), ?) <= time_slots.time_slot)
          )
        ORDER BY open_date, slot_time;
    `

	rows, err := db.QueryContext(ctx, query, merchantID, prepDelay)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	slots := make(map[string][]TimeSlot)
	for rows.Next() {
		var date, timeStr string
		if err := rows.Scan(&date, &timeStr); err != nil {
			return nil, err
		}

		// Faire une jointure SQL pour vérifier si ce créneau est "full"
		isAvailable := true

		newSlot := TimeSlot{
			Time:      timeStr,
			Available: isAvailable,
		}

		slots[date] = append(slots[date], newSlot)
	}

	return slots, nil
}

func (r *Repository) GetMerchantIDAndTZFromQR(ctx context.Context, qr string) (string, string, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
	SELECT qr.merchant_id, m.timezone
	FROM qrcodes qr
	INNER JOIN merchant m ON m.id = qr.merchant_id
	WHERE qr.code = ?`

	var merchantID string
	var tz string
	err := db.QueryRowContext(ctx, query, qr).Scan(&merchantID, &tz)
	if err != nil {
		return "", "", err
	}
	return merchantID, tz, nil
}

func (r *Repository) GetMerchantIDAndTZFromMerchantID(ctx context.Context, merchantID string) (string, string, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
	SELECT m.id, m.timezone
	FROM merchant m
	WHERE m.id = ?`

	var tz string
	err := db.QueryRowContext(ctx, query, merchantID).Scan(&merchantID, &tz)
	if err != nil {
		return "", "", err
	}
	return merchantID, tz, nil
}

func (r *Repository) GetLoyaltyPrograms(ctx context.Context, merchantID, orderType string) ([]LoyaltyProgram, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
	SELECT id, name, description
	FROM customer_loyalty_programs
	WHERE merchant_id = ?
	AND target_order_type LIKE ?
	AND enabled = true`

	rows, err := db.QueryContext(ctx, query, merchantID, "%"+orderType+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []LoyaltyProgram
	for rows.Next() {
		var lp LoyaltyProgram
		rows.Scan(&lp.ID, &lp.Name, &lp.Description)
		result = append(result, lp)
	}
	return result, nil
}

func (r *Repository) GetDiscounts(ctx context.Context, merchantID string, orderType string, dow int) ([]Discount, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
	SELECT
		d.discount_id,
		d.discount_order_type,
		d.discount_code,
		d.discount_desc,
		d.discount_name,
		d.discount_value,
		d.discount_unit,
		d.min_order_value,
		d.min_order_unit,
		d.max_discount_value,
		d.max_discount_unit,
		d.discounted_quantity,
		d.is_cumulative,
		d.available
	FROM discounts d
	LEFT JOIN discounts_schedules ds ON ds.discount_id = d.discount_id
	WHERE d.merchant_id = ?
	AND d.discount_order_type LIKE ?
	AND (d.valid_from < UTC_TIMESTAMP()
		AND (d.valid_to > UTC_TIMESTAMP() OR d.valid_to IS NULL))
	AND (
		(ds.available_from < UTC_TIMESTAMP()
		 AND ds.available_to > UTC_TIMESTAMP()
		 AND ds.day_of_week = ?)
		OR NOT d.is_time_limited
	)
	AND d.available = true
	AND d.enabled = true
	`

	rows, err := db.QueryContext(ctx, query, merchantID, "%"+orderType+"%", dow)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	discounts := []Discount{}

	for rows.Next() {
		var d Discount
		var isCumulative int
		var available int

		err := rows.Scan(
			&d.DiscountID,
			&d.DiscountOrderType,
			&d.DiscountCode,
			&d.DiscountDesc,
			&d.DiscountName,
			&d.DiscountValue,
			&d.DiscountUnit,
			&d.MinOrderValue,
			&d.MinOrderUnit,
			&d.MaxDiscountValue,
			&d.MaxDiscountUnit,
			&d.DiscountedQuantity,
			&isCumulative,
			&available,
		)
		if err != nil {
			return nil, err
		}

		d.IsCumulative = isCumulative == 1
		d.Available = available == 1

		discounts = append(discounts, d)
	}

	return discounts, nil
}

func (r *Repository) GetMerchantOpenStatus(ctx context.Context, merchantID string, dow int, currentTime string) (*MerchantOpenStatus, error) {
	db := dbutils.GetDB(ctx, r.database)

	status := &MerchantOpenStatus{}

	// 1️⃣ Global open status
	query1 := `
	SELECT 1
	FROM merchant_parameters mp
	INNER JOIN scannorder_settings snos ON snos.merchant_id = mp.merchant_id
	WHERE mp.is_open = true
	AND snos.activated = true
	AND (snos.closed_until IS NULL OR snos.closed_until <= UTC_TIMESTAMP())
	AND mp.merchant_id = ?
	LIMIT 1`

	var tmp int
	if err := db.QueryRowContext(ctx, query1, merchantID).Scan(&tmp); err == nil {
		status.OpenStatus = true
	}

	// 2️⃣ Opening hours
	query2 := `
	SELECT 1
	FROM hours_of_operation hoo
	INNER JOIN scannorder_settings snos ON snos.merchant_id = hoo.merchant_id AND snos.activated = true
	INNER JOIN merchant_parameters mp ON mp.merchant_id = snos.merchant_id
	WHERE hoo.merchant_id = ?
	AND day_of_week_from <= ?
	AND day_of_week_to >= ?
	AND hour_from < ?
	AND hour_to > ?
	LIMIT 1`

	if err := db.QueryRowContext(ctx, query2, merchantID, dow, dow, currentTime, currentTime).Scan(&tmp); err == nil {
		status.OpenHours = true
	}

	// 3️⃣ Timezone
	var timezone string
	if err := db.QueryRowContext(ctx,
		"SELECT timezone FROM merchant WHERE id = ?",
		merchantID,
	).Scan(&timezone); err != nil {
		return nil, err
	}

	loc, _ := time.LoadLocation(timezone)
	now := time.Now().In(loc).Format("2006-01-02 15:04:05")

	// 4️⃣ Stored procedure
	if _, err := db.ExecContext(ctx,
		`CALL GET_POS_STATUS(
			?, ?, 
			@p_is_open, 
			@p_last_start, 
			@p_last_end, 
			@p_current_start, 
			@p_current_end, 
			@p_next_start, 
			@p_next_end
		)`,
		merchantID,
		now,
	); err != nil {
		return nil, err
	}

	// 5️⃣ Read output vars (SAME CONN)
	var isOpen sql.NullInt64
	var nextStart sql.NullString

	if err := db.QueryRowContext(ctx,
		"SELECT @p_is_open, @p_next_start",
	).Scan(&isOpen, &nextStart); err != nil {
		return nil, err
	}

	status.OpenHours = isOpen.Valid && isOpen.Int64 == 1
	if nextStart.Valid {
		status.NextStart = nextStart.String
	}

	return status, nil
}

func (r *Repository) GetUnavailableProducts(ctx context.Context, merchantID string, dow int, currentTime string) (map[int64]string, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
	SELECT DISTINCT p.product_id, p.name
	FROM availabilities a
	INNER JOIN availabilities_products ap ON ap.availability_id = a.availability_id
	INNER JOIN availabilities_schedules asch ON asch.availability_id = a.availability_id
	INNER JOIN products p ON ap.product_id = p.product_id
	WHERE a.merchant_id = ?
	AND ((asch.day_of_week = ? AND asch.available_from > ?) OR asch.available_to < ?)
	AND asch.enabled = true
	AND a.enabled = true
	AND a.available = true
	AND ap.enabled = true`

	rows, err := db.QueryContext(ctx, query, merchantID, dow, currentTime, currentTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64]string)
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		result[id] = name
	}
	return result, nil
}

func (r *Repository) GetDeliverySessionByOrderID(ctx context.Context, orderID string) (deliverySessionID *string, err error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
	SELECT dso.order_id, dso.delivery_session_id
	FROM delivery_session ds
	INNER JOIN delivery_session_order dso ON ds.id = dso.delivery_session_id
	INNER JOIN orders o ON o.order_id = dso.order_id
	WHERE o.order_id = ?`

	row := db.QueryRowContext(ctx, query, orderID)

	var dsID string

	err = row.Scan(&orderID, &dsID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &dsID, nil
}

func (r *Repository) GetMerchantIDByQR(ctx context.Context, qr string) (*string, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `SELECT merchant_id FROM qrcodes WHERE code = ?`
	var merchantID string

	err := db.QueryRowContext(ctx, query, qr).Scan(&merchantID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &merchantID, nil
}

func (r *Repository) GetCustomerFromQR(ctx context.Context, qrCode string) (*models.CustomerRequest, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
        SELECT b.customer_id
        FROM qrcodes qr
        INNER JOIN booked_location bc ON bc.location_id = qr.location_id
        INNER JOIN bookings b ON b.booking_id = bc.booking_id
        WHERE qr.code = $1
        AND NOW() BETWEEN b.booking_date_from AND b.booking_date_to
        LIMIT 1;
    `

	var customerID string
	err := db.QueryRowContext(ctx, query, qrCode).Scan(&customerID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &models.CustomerRequest{
		CustomerID: &customerID,
	}, nil
}

func (r *Repository) GetCustomerByPhone(ctx context.Context, customer models.CustomerRequest) (*models.CustomerRequest, error) {
	db := dbutils.GetDB(ctx, r.database)

	phone := helpers.NormalizePhoneNumber(*customer.Tel, "FR")

	query := `
        SELECT c.customer_id, mp.automatically_add_customer_rewards
        FROM customer c
        INNER JOIN merchant_parameters mp ON mp.merchant_id = c.merchant_id
        WHERE c.customer_tel = $1
        AND c.enabled = true
        AND c.merchant_id = $2
        LIMIT 1;
    `

	var customerID string
	var autoRewards bool

	err := db.QueryRowContext(ctx, query, phone, customer.MerchantID).Scan(&customerID, &autoRewards)
	if err == sql.ErrNoRows {
		return &customer, nil
	}
	if err != nil {
		return &customer, err
	}

	customer.CustomerID = &customerID

	// 🎁 Récupération rewards si activé
	if autoRewards {
		rewardsQuery := `
            SELECT cr.reward_id, cr.loyalty_program_id, cr.creation_date,
				   cr.reward_type, cr.reward_value
            FROM customer_rewards cr
            WHERE cr.customer_id = $1
            AND cr.usage_date IS NULL;
        `

		rows, err := db.QueryContext(ctx, rewardsQuery, customerID)
		if err != nil {
			return &customer, nil // comme PHP → fail silencieux
		}
		defer rows.Close()

		rewards := []models.DBReward{}
		for rows.Next() {
			var rewardID, loyaltyID, creationDate, rewardType string
			var rewardValue int

			if err := rows.Scan(&rewardID, &loyaltyID, &creationDate, &rewardType, &rewardValue); err != nil {
				continue
			}

			rewards = append(rewards, models.DBReward{
				RewardID:         rewardID,
				LoyaltyProgramID: loyaltyID,
				CreationDate:     &creationDate,
				RewardType:       rewardType,
				RewardValue:      &rewardValue,
			})
		}

		customer.AvailableRewards = rewards
	}

	return &customer, nil
}

func (s *Repository) GetBooking(ctx context.Context, qrCode string) (*models.Booking, error) {
	return nil, nil
}

// GetMerchantsByBrandSlug fetches the brand and its merchants.
// If lat and lng are non-nil, only merchants within 50 km are returned (Haversine).
func (r *Repository) GetMerchantsByBrandSlug(ctx context.Context, slug string, lat, lng *float64) (*BrandData, []BrandMerchantRow, error) {
	db := dbutils.GetDB(ctx, r.database)

	// 1. Fetch brand
	brandQuery := `
		SELECT brand_id, name, slug, logo_url, banner_url, description
		FROM brands
		WHERE slug = ?
		LIMIT 1`

	brand := &BrandData{}
	err := db.QueryRowContext(ctx, brandQuery, slug).Scan(
		&brand.BrandID, &brand.Name, &brand.Slug,
		&brand.LogoURL, &brand.BannerURL, &brand.Description,
	)
	if err == sql.ErrNoRows {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	// 2. Fetch merchants (with optional distance filter)
	var rows *sql.Rows

	if lat != nil && lng != nil {
		merchantQuery := `
			SELECT
				m.id,
				m.fullName,
				m.address,
				m.lat,
				m.lng,
				m.timezone,
				m.logo_url,
				snos.take_away_enabled,
				snos.take_away_available,
				snos.delivery_enabled,
				snos.delivery_available,
				snos.in_enabled,
				snos.in_available,
				snos.header_background,
				mp.preparation_time_mode,
				mp.preparation_time,
				qr.code as slug,
				(6371 * ACOS(
					COS(RADIANS(?)) * COS(RADIANS(m.lat)) *
					COS(RADIANS(m.lng) - RADIANS(?)) +
					SIN(RADIANS(?)) * SIN(RADIANS(m.lat))
				)) AS distance_km
			FROM brands b
			INNER JOIN merchant m ON m.brand_id = b.brand_id
          	INNER JOIN qrcodes qr ON qr.merchant_id = m.id and qr.location_id IS NULL and qr.user_id IS NULL and qr.deleted = false
			INNER JOIN scannorder_settings snos ON snos.merchant_id = m.id
			INNER JOIN merchant_parameters mp ON mp.merchant_id = m.id
			WHERE b.brand_id = ?
			HAVING distance_km < 50
			ORDER BY distance_km ASC`

		rows, err = db.QueryContext(ctx, merchantQuery, *lat, *lng, *lat, brand.BrandID)
	} else {
		merchantQuery := `
			SELECT
				m.id,
				m.fullName,
				m.address,
				m.lat,
				m.lng,
				m.timezone,
				m.logo_url,
				snos.take_away_enabled,
				snos.take_away_available,
				snos.delivery_enabled,
				snos.delivery_available,
				snos.in_enabled,
				snos.in_available,
				snos.header_background,
				mp.preparation_time_mode,
				mp.preparation_time,
				qr.code as slug,
				NULL AS distance_km
			FROM brands b
			INNER JOIN merchant m ON m.brand_id = b.brand_id
          	INNER JOIN qrcodes qr ON qr.merchant_id = m.id and qr.location_id IS NULL and qr.user_id IS NULL and qr.deleted = false
			INNER JOIN scannorder_settings snos ON snos.merchant_id = m.id
			INNER JOIN merchant_parameters mp ON mp.merchant_id = m.id
			WHERE b.brand_id = ?
			ORDER BY m.fullName ASC`

		rows, err = db.QueryContext(ctx, merchantQuery, brand.BrandID)
	}

	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var merchants []BrandMerchantRow
	for rows.Next() {
		var row BrandMerchantRow
		var distanceKm sql.NullFloat64

		if err := rows.Scan(
			&row.MerchantID,
			&row.FullName,
			&row.Address,
			&row.Lat,
			&row.Lng,
			&row.Timezone,
			&row.LogoURL,
			&row.TakeawayEnabled,
			&row.TakeawayAvailable,
			&row.DeliveryEnabled,
			&row.DeliveryAvailable,
			&row.InEnabled,
			&row.InAvailable,
			&row.BannerURL,
			&row.PrepTimeMode,
			&row.PrepTime,
			&row.Slug,
			&distanceKm,
		); err != nil {
			return nil, nil, err
		}

		if distanceKm.Valid {
			v := distanceKm.Float64
			row.DistanceKm = &v
		}

		merchants = append(merchants, row)
	}

	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	return brand, merchants, nil
}

func (r *Repository) GetUpsellProducts(ctx context.Context, merchantID string) ([]UpsellProduct, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
	SELECT 
		p.product_id,
		p.name,
		p.product_desc,
		p.price,
		p.image_url
	FROM products p
	WHERE p.merchant_id = ?
	AND p.is_popular = 1
	AND p.status in ('available','1')
	ORDER BY p.name ASC`

	rows, err := db.QueryContext(ctx, query, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []UpsellProduct
	for rows.Next() {
		var product UpsellProduct
		var description sql.NullString
		var imageURL sql.NullString

		if err := rows.Scan(
			&product.ProductID,
			&product.Name,
			&description,
			&product.Price,
			&imageURL,
		); err != nil {
			return nil, err
		}

		if description.Valid {
			product.Description = &description.String
		}
		if imageURL.Valid {
			product.ImageURL = &imageURL.String
		}

		products = append(products, product)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return products, nil
}

// GetProductPricesForSNO retrieves official product prices for SNO from database
// Returns a map of productID -> {price, price_delivery, price_take_away}
// This ensures backend is the single source of truth for pricing
func (r *Repository) GetProductPricesForSNO(ctx context.Context, merchantID string, productIDs []string) (map[string]map[string]int64, error) {
	if len(productIDs) == 0 {
		return make(map[string]map[string]int64), nil
	}

	db := dbutils.GetDB(ctx, r.database)

	// Build placeholders for IN clause
	placeholders := ""
	args := []interface{}{merchantID}
	for i, id := range productIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}

	query := fmt.Sprintf(`
		SELECT 
			p.product_id,
			p.price,
			COALESCE(p.price_delivery, p.price) as price_delivery,
			COALESCE(p.price_take_away, p.price) as price_take_away
		FROM products p
		WHERE p.merchant_id = ?
		AND p.product_id IN (%s)
	`, placeholders)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query product prices: %w", err)
	}
	defer rows.Close()

	result := make(map[string]map[string]int64)
	for rows.Next() {
		var productID string
		var price, priceDelivery, priceTakeaway int64

		if err := rows.Scan(&productID, &price, &priceDelivery, &priceTakeaway); err != nil {
			return nil, fmt.Errorf("failed to scan product price: %w", err)
		}

		result[productID] = map[string]int64{
			"price":           price,
			"price_delivery":  priceDelivery,
			"price_take_away": priceTakeaway,
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error during product price fetch: %w", err)
	}

	return result, nil
}

// GetConfigurationOptionPricesForSNO retrieves official configuration option prices from database
// Returns a map of optionID -> extra_price
// Ensures client cannot manipulate option prices
func (r *Repository) GetConfigurationOptionPricesForSNO(ctx context.Context, optionIDs []string) (map[string]int, error) {
	if len(optionIDs) == 0 {
		return make(map[string]int), nil
	}

	db := dbutils.GetDB(ctx, r.database)

	// Build placeholders for IN clause
	placeholders := ""
	args := []interface{}{}
	for i, id := range optionIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}

	query := fmt.Sprintf(`
		SELECT id, extra_price
		FROM configurable_attribute_options
		WHERE id IN (%s)
	`, placeholders)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query option prices: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var optionID string
		var extraPrice int

		if err := rows.Scan(&optionID, &extraPrice); err != nil {
			return nil, fmt.Errorf("failed to scan option price: %w", err)
		}

		result[optionID] = extraPrice
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error during option price fetch: %w", err)
	}

	return result, nil
}
