package scannorder

import (
	"context"
	"database/sql"
	"fmt"
	"time"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/customers"
	"welloresto-api/internal/modules/openinghours"
	settingspkg "welloresto-api/internal/modules/planning/settings"
	"welloresto-api/internal/database/dbx"
)

// snoMerchantJoinCast retourne le fragment merchant.id (integer) comparable a
// une colonne merchant_id varchar selon le dialecte (MySQL coercait, Postgres
// exige le cast ; CHAR nu = char(1) en PG, d'ou TEXT).
func snoMerchantJoinCast() string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "CAST(m.id AS TEXT)"
	}
	return "CAST(m.id AS CHAR)"
}

type Repository struct {
	database     *sql.DB
	customerRepo *customers.CustomersRepository
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db, customerRepo: customers.NewCustomerRepository(db)}
}

func (r *Repository) GetMerchantByQR(ctx context.Context, qr string) (*models.MerchantRow, error) {
	db := dbx.GetDB(ctx, r.database)

	query := fmt.Sprintf(`
	SELECT m.id, m.fullName, m.address, m.lat, m.lng, m.timezone, m.merchantTel,
		   mp.currency, mp.primary_color, mp.text_color_on_primary_color,
		   snos.logo_url, snos.banner_url,
           mp.delivery_fees, mp.delivery_fees_limit, mp.minimum_cart_for_delivery_order, mp.preparation_time_mode, mp.preparation_time, mp.delivery_distance_limit,

           qr.menu_only, qr.user_id, qr.last_waiter_call, qr.creation_date,

           o.order_id, l.location_id, l.location_name, snos.variable_fees, snos.fixed_fees, sa.account_id,

           snos.take_away_enabled, snos.take_away_available,
           snos.delivery_enabled, snos.delivery_available,
		   snos.in_enabled, snos.in_available, mp.enable_advance_orders

    FROM   qrcodes qr
          INNER JOIN merchant m on %[1]s = qr.merchant_id
          LEFT JOIN stripe_accounts sa on sa.merchant_id = %[1]s
          INNER JOIN scannorder_settings snos on snos.merchant_id = %[1]s
          INNER JOIN merchant_parameters mp on mp.merchant_id = %[1]s
          LEFT JOIN bookings_settings bs on bs.merchant_id = %[1]s
          LEFT JOIN locations l on l.location_id = qr.location_id
          LEFT JOIN (SELECT o.order_id, ol.location_id FROM orders o INNER JOIN order_location ol on ol.order_id = o.order_id WHERE o.state = 'OPEN') o on o.location_id = l.location_id
    WHERE qr.code = ?`, snoMerchantJoinCast())

	row := models.MerchantRow{}
	err := db.QueryRowContext(ctx, query, qr).Scan(
		&row.MerchantID, &row.FullName, &row.Address, &row.Lat, &row.Lng, &row.Timezone, &row.Phone,
		&row.Currency, &row.PrimaryColor, &row.TextColor,
		&row.LogoURL, &row.BannerURL,
		&row.DeliveryFees, &row.DeliveryFeesLimit, &row.MinimumCartForDeliveryOrder, &row.PrepTimeMode, &row.PrepTime, &row.DeliveryDistanceLimit,
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

func (r *Repository) GetAvailableSlots(ctx context.Context, merchantID string, prepMinutes int, localDate string, localTime string) (map[string][]TimeSlot, error) {
	db := dbx.GetDB(ctx, r.database)

	// On convertit les minutes en format TIME (HH:MM:SS) pour MySQL
	prepDelay := fmt.Sprintf("%02d:%02d:00", prepMinutes/60, prepMinutes%60)

	// Requete par dialecte : MAKETIME/ADDTIME/DATE_FORMAT/TIME_FORMAT/DAYOFWEEK
	// n'ont pas d'equivalent syntaxique PG. La branche PG utilise le meme CTE
	// recursif avec time/interval natifs, EXTRACT(ISODOW) (deja en convention
	// ISO, sans le CASE MySQL), et compare heure+delai en interval (les
	// intervalles ne bouclent pas a minuit, comme ADDTIME MySQL qui peut
	// depasser 24h — un delai franchissant minuit n'ouvre aucun creneau,
	// comportement conserve).
	query := `
        WITH RECURSIVE time_slots AS (
            SELECT MAKETIME(0, 30, 0) AS time_slot
            UNION ALL
            SELECT ADDTIME(time_slot, '00:30:00')
            FROM time_slots
            WHERE time_slot < MAKETIME(23, 30, 0)
        )
		SELECT DISTINCT
			DATE_FORMAT(DATE_ADD(?, INTERVAL days_to_add DAY), '%Y-%m-%d') AS open_date,
            TIME_FORMAT(time_slots.time_slot, '%H:%i') as slot_time
        FROM (
            SELECT 0 AS days_to_add UNION ALL SELECT 1 UNION ALL SELECT 2
        ) AS days
        JOIN time_slots
        JOIN hours_of_operation AS hoo
            ON hoo.merchant_id = ?
            AND hoo.enabled = 1
			AND (CASE WHEN DAYOFWEEK(DATE_ADD(?, INTERVAL days_to_add DAY)) = 1 THEN 7 ELSE DAYOFWEEK(DATE_ADD(?, INTERVAL days_to_add DAY)) - 1 END) BETWEEN hoo.day_of_week_from AND hoo.day_of_week_to
            AND time_slots.time_slot > hoo.hour_from
            AND time_slots.time_slot <= hoo.hour_to
        LEFT JOIN merchant_parameters AS mp ON mp.merchant_id = hoo.merchant_id
		WHERE (hoo.valid_from IS NULL OR hoo.valid_from <= DATE_ADD(?, INTERVAL days_to_add DAY))
		  AND (hoo.valid_to IS NULL OR hoo.valid_to >= DATE_ADD(?, INTERVAL days_to_add DAY))
          AND days_to_add < COALESCE(mp.advance_order_days, 3)
          AND (
				DATE_ADD(?, INTERVAL days_to_add DAY) > ? OR
				(DATE_ADD(?, INTERVAL days_to_add DAY) = ? AND
				 ADDTIME(?, ?) <= time_slots.time_slot)
          )
        ORDER BY open_date, slot_time;
    `
	args := []interface{}{
		localDate,
		merchantID,
		localDate,
		localDate,
		localDate,
		localDate,
		localDate,
		localDate,
		localDate,
		localDate,
		localTime,
		prepDelay,
	}
	if dbx.ActiveDialect() == dbx.Postgres {
		query = `
        WITH RECURSIVE time_slots AS (
            SELECT time '00:30:00' AS time_slot
            UNION ALL
            SELECT time_slot + interval '30 minutes'
            FROM time_slots
            WHERE time_slot < time '23:30:00'
        )
		SELECT DISTINCT
			to_char(CAST(? AS date) + days_to_add, 'YYYY-MM-DD') AS open_date,
            to_char(time_slots.time_slot, 'HH24:MI') as slot_time
        FROM (
            SELECT 0 AS days_to_add UNION ALL SELECT 1 UNION ALL SELECT 2
        ) AS days
        CROSS JOIN time_slots
        JOIN hours_of_operation AS hoo
            ON hoo.merchant_id = ?
            AND hoo.enabled = TRUE
			AND EXTRACT(ISODOW FROM CAST(? AS date) + days_to_add) BETWEEN hoo.day_of_week_from AND hoo.day_of_week_to
            AND time_slots.time_slot > hoo.hour_from
            AND time_slots.time_slot <= hoo.hour_to
        LEFT JOIN merchant_parameters AS mp ON mp.merchant_id = hoo.merchant_id
		WHERE (hoo.valid_from IS NULL OR hoo.valid_from <= CAST(? AS date) + days_to_add)
		  AND (hoo.valid_to IS NULL OR hoo.valid_to >= CAST(? AS date) + days_to_add)
          AND days_to_add < COALESCE(mp.advance_order_days, 3)
          AND (
				CAST(? AS date) + days_to_add > CAST(? AS date) OR
				(CAST(? AS date) + days_to_add = CAST(? AS date) AND
				 (CAST(? AS interval) + CAST(? AS interval)) <= CAST(time_slots.time_slot AS interval))
          )
        ORDER BY open_date, slot_time`
		args = []interface{}{
			localDate,
			merchantID,
			localDate,
			localDate,
			localDate,
			localDate,
			localDate,
			localDate,
			localDate,
			localTime,
			prepDelay,
		}
	}

	rows, err := db.QueryContext(ctx, query, args...)
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
	db := dbx.GetDB(ctx, r.database)

	query := fmt.Sprintf(`
	SELECT qr.merchant_id, m.timezone
	FROM qrcodes qr
	INNER JOIN merchant m ON %s = qr.merchant_id
	WHERE qr.code = ?`, snoMerchantJoinCast())

	var merchantID string
	var tz string
	err := db.QueryRowContext(ctx, query, qr).Scan(&merchantID, &tz)
	if err != nil {
		return "", "", err
	}
	return merchantID, tz, nil
}

func (r *Repository) GetMerchantIDAndTZFromMerchantID(ctx context.Context, merchantID string) (string, string, error) {
	db := dbx.GetDB(ctx, r.database)

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
	db := dbx.GetDB(ctx, r.database)

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
	db := dbx.GetDB(ctx, r.database)

	query := `
	SELECT DISTINCT
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
		CASE WHEN d.is_cumulative THEN 1 ELSE 0 END,
		CASE WHEN d.available THEN 1 ELSE 0 END
	FROM discounts d
	LEFT JOIN discounts_schedules ds ON ds.discount_id = d.discount_id AND ds.enabled = true
	WHERE d.merchant_id = ?
	AND d.discount_order_type LIKE ?
	AND (d.valid_from < %[1]s
		AND (d.valid_to > %[1]s OR d.valid_to IS NULL))
	AND (
		(%[2]s
		 AND ds.day_of_week = ?)
		OR NOT d.is_time_limited
	)
	AND d.available = true
	AND d.enabled = true
	`
	// available_from/to sont des colonnes time comparees a un timestamp en
	// MySQL (coercition implicite) — traduites en comparaison d'heure du jour
	// UTC cote PG, comme orders.GetDiscountProductOptions. Les scans
	// is_cumulative/available passent par CASE 1/0 (booleens PG vs int Go).
	timeWindow := `(ds.available_from < UTC_TIMESTAMP()
		 AND ds.available_to > UTC_TIMESTAMP())`
	if dbx.ActiveDialect() == dbx.Postgres {
		timeWindow = `(ds.available_from < CAST(now() AT TIME ZONE 'UTC' AS time)
		 AND ds.available_to > CAST(now() AT TIME ZONE 'UTC' AS time))`
	}
	query = fmt.Sprintf(query, dbx.UTCNow(), timeWindow)

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

func (r *Repository) GetMerchantStatus(ctx context.Context, merchantID string, dow int, currentTime string) (*MerchantStatus, error) {
	db := dbx.GetDB(ctx, r.database)

	status := &MerchantStatus{}
	day := normalizeDayOfWeek(dow)
	holidayRepo := settingspkg.NewRepository(r.database)

	// 1️⃣ Global open status
	query1 := fmt.Sprintf(`
	SELECT 1
	FROM merchant_parameters mp
	INNER JOIN scannorder_settings snos ON snos.merchant_id = mp.merchant_id
	WHERE mp.is_open = true
	AND snos.activated = true
	AND (snos.closed_until IS NULL OR snos.closed_until <= %s)
	AND mp.merchant_id = ?
	LIMIT 1`, dbx.UTCNow())

	var tmp int
	var isMerchantEnabled bool
	if err := db.QueryRowContext(ctx, query1, merchantID).Scan(&tmp); err == nil {
		isMerchantEnabled = true
	}

	// 2️⃣ Opening hours
	query2 := `
	SELECT 1
	FROM hours_of_operation hoo
	INNER JOIN scannorder_settings snos ON snos.merchant_id = hoo.merchant_id AND snos.activated = true
	INNER JOIN merchant_parameters mp ON mp.merchant_id = snos.merchant_id
	WHERE hoo.merchant_id = ?
	AND (
		(day_of_week_from <= day_of_week_to AND ? BETWEEN day_of_week_from AND day_of_week_to)
		OR
		(day_of_week_from > day_of_week_to AND (? >= day_of_week_from OR ? <= day_of_week_to))
	)
	AND hour_from < ?
	AND hour_to > ?
	LIMIT 1`

	var isOpenNow bool
	if err := db.QueryRowContext(ctx, query2, merchantID, day, day, day, currentTime, currentTime).Scan(&tmp); err == nil {
		isOpenNow = true
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
	localNow := time.Now().In(loc)
	holidayDate := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.UTC)
	holiday, err := holidayRepo.ResolvePlanningHoliday(ctx, merchantID, holidayDate)
	if err != nil {
		return nil, err
	}
	forcedClosed := holiday.IsOpen != nil && !*holiday.IsOpen

	// Vacances : periode de fermeture exceptionnelle definie par le marchand
	// (planning_vacation_periods), meme effet que forcedClosed ci-dessus mais
	// sur une plage de dates plutot qu'un jour ferie isole (meme logique que
	// pos.GetPOSStatus).
	onVacation, err := holidayRepo.ResolveVacationOverlap(ctx, merchantID, localNow)
	if err != nil {
		return nil, err
	}
	forcedClosed = forcedClosed || onVacation

	// 4️⃣ Statut horaires : ex-procédure GET_POS_STATUS, calculée en Go
	slots, err := openinghours.FetchActiveSlots(ctx, r.database, merchantID, localNow)
	if err != nil {
		return nil, err
	}
	hoursStatus := openinghours.ComputePOSStatus(localNow, slots)

	status.IsOpen = isMerchantEnabled && isOpenNow && hoursStatus.IsOpen && !forcedClosed
	if hoursStatus.NextStart != nil && !forcedClosed {
		status.NextStart = openinghours.FormatDateTime(hoursStatus.NextStart)
	}

	openHours, err := r.GetMerchantOpenHours(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	status.OpenHours = openHours

	return status, nil
}

func (r *Repository) GetMerchantOpenHours(ctx context.Context, merchantID string) ([]OpenHoursDay, error) {
	db := dbx.GetDB(ctx, r.database)

	type openHourRow struct {
		DayFrom  int
		DayTo    int
		HourFrom string
		HourTo   string
	}

	query := fmt.Sprintf(`
	SELECT
		hoo.day_of_week_from,
		hoo.day_of_week_to,
		CAST(hoo.hour_from AS CHAR(8)),
		CAST(hoo.hour_to AS CHAR(8))
	FROM hours_of_operation hoo
	INNER JOIN scannorder_settings snos ON snos.merchant_id = hoo.merchant_id AND snos.activated = true
	INNER JOIN merchant_parameters mp ON mp.merchant_id = snos.merchant_id
	WHERE hoo.merchant_id = ?
	AND hoo.enabled = true
	AND (hoo.valid_from IS NULL OR hoo.valid_from <= %[1]s)
	AND (hoo.valid_to IS NULL OR hoo.valid_to >= %[1]s)
	ORDER BY hoo.day_of_week_from, hoo.hour_from`, dbx.UTCNow())

	rows, err := db.QueryContext(ctx, query, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dayNames := map[int]string{
		1: "Lundi",
		2: "Mardi",
		3: "Mercredi",
		4: "Jeudi",
		5: "Vendredi",
		6: "Samedi",
		7: "Dimanche",
	}

	weekly := make([]OpenHoursDay, 0, 7)
	for day := 1; day <= 7; day++ {
		weekly = append(weekly, OpenHoursDay{
			DayOfWeek: day,
			DayName:   dayNames[day],
			Hours:     []OpeningPeriod{},
		})
	}

	for rows.Next() {
		var row openHourRow
		if err := rows.Scan(&row.DayFrom, &row.DayTo, &row.HourFrom, &row.HourTo); err != nil {
			return nil, err
		}

		days := expandDayRange(normalizeDayOfWeek(row.DayFrom), normalizeDayOfWeek(row.DayTo))
		period := OpeningPeriod{
			From: formatHour(row.HourFrom),
			To:   formatHour(row.HourTo),
		}

		for _, day := range days {
			if day >= 1 && day <= len(weekly) {
				weekly[day-1].Hours = append(weekly[day-1].Hours, period)
			}
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return weekly, nil
}

func normalizeDayOfWeek(day int) int {
	if day <= 0 {
		return 7
	}
	if day > 7 {
		day = ((day - 1) % 7) + 1
	}
	return day
}

func expandDayRange(dayFrom, dayTo int) []int {
	if dayFrom <= dayTo {
		days := make([]int, 0, dayTo-dayFrom+1)
		for day := dayFrom; day <= dayTo; day++ {
			days = append(days, day)
		}
		return days
	}

	days := make([]int, 0, (8-dayFrom)+dayTo)
	for day := dayFrom; day <= 7; day++ {
		days = append(days, day)
	}
	for day := 1; day <= dayTo; day++ {
		days = append(days, day)
	}
	return days
}

func formatHour(hour string) string {
	if len(hour) >= 5 {
		return hour[:5]
	}
	return hour
}

func (r *Repository) GetUnavailableProducts(ctx context.Context, merchantID string, dow int, currentTime string) (map[int64]string, error) {
	db := dbx.GetDB(ctx, r.database)

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

// GetDeliverySessionByOrderID resolves the delivery session currently linked to an order.
// An order can have been dispatched more than once (delivery_session_order gets a new row
// each time), so this always returns the most recent non-canceled session — ORDER BY
// start_date DESC (delivery_session has no created_at column) with a status filter that
// excludes 'canceled' but keeps 'active' and 'done' (a just-finished session must stay
// visible for the post-delivery SNO display).
func (r *Repository) GetDeliverySessionByOrderID(ctx context.Context, orderID string) (deliverySessionID *string, err error) {
	db := dbx.GetDB(ctx, r.database)

	query := `
	SELECT dso.order_id, dso.delivery_session_id
	FROM delivery_session ds
	INNER JOIN delivery_session_order dso ON ds.id = dso.delivery_session_id
	INNER JOIN orders o ON o.order_id = dso.order_id
	WHERE o.order_id = ? AND ds.status != 'canceled'
	ORDER BY ds.start_date DESC
	LIMIT 1`

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
	db := dbx.GetDB(ctx, r.database)

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

// GetStripePaymentForOrder returns the intent_id and account_id of the active Stripe
// payment for a given order. Returns ("", "", nil) when no Stripe payment exists.
func (r *Repository) GetStripePaymentForOrder(ctx context.Context, orderID string) (intentID string, accountID string, err error) {
	db := dbx.GetDB(ctx, r.database)

	q := `
		SELECT sp.payment_intent_id, sa.account_id
		FROM payments p
		INNER JOIN stripe_payments sp ON sp.payment_id = p.payment_id
		INNER JOIN stripe_accounts sa ON sa.merchant_id = p.merchant_id
		WHERE p.order_id = ?
		  AND p.mop = 'STRIPE'
		  AND p.enabled = TRUE
		LIMIT 1`

	err = db.QueryRowContext(ctx, q, orderID).Scan(&intentID, &accountID)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	return intentID, accountID, err
}

func (r *Repository) GetCustomerFromQR(ctx context.Context, qrCode string) (*models.CustomerRequest, error) {
	db := dbx.GetDB(ctx, r.database)

	query := `
        SELECT b.customer_id
        FROM qrcodes qr
        INNER JOIN booked_location bc ON bc.location_id = qr.location_id
        INNER JOIN bookings b ON b.booking_id = bc.booking_id
        WHERE qr.code = ?
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
	db := dbx.GetDB(ctx, r.database)

	if customer.Tel == nil || customer.MerchantID == nil || *customer.Tel == "" || *customer.MerchantID == "" {
		return &customer, nil
	}

	existing, err := r.customerRepo.FindCustomerByPhone(ctx, *customer.Tel, *customer.MerchantID)
	if err == sql.ErrNoRows {
		return &customer, nil
	}
	if err != nil {
		return &customer, err
	}
	if existing == nil || existing.CustomerID == nil || *existing.CustomerID == "" {
		return &customer, nil
	}

	query := `
        SELECT mp.automatically_add_customer_rewards
        FROM merchant_parameters mp
        WHERE mp.merchant_id = ?
        LIMIT 1;
    `

	var autoRewards bool

	err = db.QueryRowContext(ctx, query, customer.MerchantID).Scan(&autoRewards)
	if err != nil {
		return &customer, err
	}

	customer.CustomerID = existing.CustomerID

	// 🎁 Récupération rewards si activé
	if autoRewards {
		rewardsQuery := `
            SELECT cr.reward_id, cr.loyalty_program_id, cr.creation_date,
				   cr.reward_type, cr.reward_value
            FROM customer_rewards cr
            WHERE cr.customer_id = ?
            AND cr.usage_date IS NULL;
        `

		rows, err := db.QueryContext(ctx, rewardsQuery, *existing.CustomerID)
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
	db := dbx.GetDB(ctx, r.database)

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
		// Le HAVING sans GROUP BY sur l'alias distance_km (tolerance MySQL)
		// devient une sous-requete filtree en WHERE cote PG — meme resultat.
		merchantQuery := fmt.Sprintf(`
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
          	INNER JOIN qrcodes qr ON qr.merchant_id = %[1]s and qr.location_id IS NULL and qr.user_id IS NULL and qr.deleted = false
			INNER JOIN scannorder_settings snos ON snos.merchant_id = %[1]s
			INNER JOIN merchant_parameters mp ON mp.merchant_id = %[1]s
			WHERE b.brand_id = ?
			HAVING distance_km < 50
			ORDER BY distance_km ASC`, snoMerchantJoinCast())
		if dbx.ActiveDialect() == dbx.Postgres {
			merchantQuery = fmt.Sprintf(`
			SELECT * FROM (
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
				INNER JOIN qrcodes qr ON qr.merchant_id = %[1]s and qr.location_id IS NULL and qr.user_id IS NULL and qr.deleted = false
				INNER JOIN scannorder_settings snos ON snos.merchant_id = %[1]s
				INNER JOIN merchant_parameters mp ON mp.merchant_id = %[1]s
				WHERE b.brand_id = ?
			) nearby
			WHERE distance_km < 50
			ORDER BY distance_km ASC`, snoMerchantJoinCast())
		}

		rows, err = db.QueryContext(ctx, merchantQuery, *lat, *lng, *lat, brand.BrandID)
	} else {
		merchantQuery := fmt.Sprintf(`
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
          	INNER JOIN qrcodes qr ON qr.merchant_id = %[1]s and qr.location_id IS NULL and qr.user_id IS NULL and qr.deleted = false
			INNER JOIN scannorder_settings snos ON snos.merchant_id = %[1]s
			INNER JOIN merchant_parameters mp ON mp.merchant_id = %[1]s
			WHERE b.brand_id = ?
			ORDER BY m.fullName ASC`, snoMerchantJoinCast())

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

// GetUpsellProducts retrieves the IDs of "popular" products (is_popular = 1) eligible for
// upsell. Product groups are excluded since they are not orderable on their own — only their
// sub-products are (same rule as the SNO menu, which flattens groups into their sub-products).
func (r *Repository) GetUpsellProducts(ctx context.Context, merchantID string) ([]string, error) {
	db := dbx.GetDB(ctx, r.database)

	query := `
	SELECT
		p.product_id
	FROM products p
	WHERE p.merchant_id = ?
	AND p.is_popular = TRUE
	AND p.status in ('available','1')
	AND (p.is_product_group IS NULL OR p.is_product_group != TRUE)
	ORDER BY p.name ASC`

	rows, err := db.QueryContext(ctx, query, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var productIDs []string
	for rows.Next() {
		var productID string
		if err := rows.Scan(&productID); err != nil {
			return nil, err
		}
		productIDs = append(productIDs, productID)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return productIDs, nil
}

// GetProductPricesForSNO retrieves official product prices for SNO from database
// Returns a map of productID -> {price, price_delivery, price_take_away}
// This ensures backend is the single source of truth for pricing
func (r *Repository) GetProductPricesForSNO(ctx context.Context, merchantID string, productIDs []string) (map[string]map[string]int64, error) {
	if len(productIDs) == 0 {
		return make(map[string]map[string]int64), nil
	}

	db := dbx.GetDB(ctx, r.database)

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

	db := dbx.GetDB(ctx, r.database)

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
