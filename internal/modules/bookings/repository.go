package bookings

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/bookingcore"
	"welloresto-api/internal/modules/customers"
	"welloresto-api/internal/utils"
	"welloresto-api/internal/utils/dbutils"

	"go.uber.org/zap"
)

type BookingsRepository struct {
	database        *sql.DB
	log             *zap.Logger
	builder         *BookingFetcher
	customerUpdater *customers.CustomersRepository
}

func NewBookingsRepository(db *sql.DB, log *zap.Logger) *BookingsRepository {
	return &BookingsRepository{
		database:        db,
		log:             log,
		builder:         NewBookingFetcher(db, log),
		customerUpdater: customers.NewCustomerRepository(db),
	}
}

func (r *BookingsRepository) GetBookings(ctx context.Context, req *BookingObjectRequest) ([]Booking, error) {
	return r.builder.FetchAndBuildBookings(ctx, req)
}

func (r *BookingsRepository) GetBookingByID(ctx context.Context, merchantID, bookingID string) (*Booking, error) {
	req := &BookingObjectRequest{
		MerchantID: merchantID,
		BookingID:  &bookingID,
	}
	list, err := r.builder.FetchAndBuildBookings(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, sql.ErrNoRows
	}
	return &list[0], nil
}

func (r *BookingsRepository) GetBookingSettings(ctx context.Context, merchantID string) (*BookingSettings, error) {
	db := dbutils.GetDB(ctx, r.database)

	settings := &BookingSettings{}
	err := db.QueryRowContext(ctx, `
		SELECT
			COALESCE(bs.enabled, 1),
			COALESCE(bs.code, ''),
			COALESCE(bs.auto_accept_reserve_bookings, 0),
			COALESCE(bs.slot_interval_minutes, 15),
			COALESCE(bs.default_booking_duration, 90),
			COALESCE(bs.reserve_maximum_party_size, 8),
			COALESCE(bs.reserve_minimum_party_size, 1),
			COALESCE(bs.last_booking_offset_minutes, 60),
			COALESCE(bs.min_booking_notice_minutes, 60),
			COALESCE(bs.max_booking_horizon_days, 90),
			COALESCE(bs.overbooking_percent, 0),
			COALESCE(bs.cancelable_by_customer, 1),
			COALESCE(bs.cancel_booking_limit_offset_hours, 48),
			COALESCE(bs.pending_expiration_hours, 24),
			COALESCE(bs.sms_enabled, 0),
			COALESCE(bs.waitlist_enabled, 0),
			COALESCE(bs.waitlist_max_size, 0),
			COALESCE(bs.waitlist_slot_expiry_minutes, 15)
		FROM merchant m
		LEFT JOIN bookings_settings bs ON bs.merchant_id = m.id
		WHERE m.id = ?
		LIMIT 1
	`, merchantID).Scan(
		&settings.Enabled,
		&settings.Code,
		&settings.AutoAcceptReserveBookings,
		&settings.SlotIntervalMinutes,
		&settings.DefaultBookingDuration,
		&settings.ReserveMaximumPartySize,
		&settings.ReserveMinimumPartySize,
		&settings.LastBookingOffsetMinutes,
		&settings.MinBookingNoticeMinutes,
		&settings.MaxBookingHorizonDays,
		&settings.OverbookingPercent,
		&settings.CancelableByCustomer,
		&settings.CancelBookingLimitOffsetHours,
		&settings.PendingExpirationHours,
		&settings.SMSEnabled,
		&settings.WaitlistEnabled,
		&settings.WaitlistMaxSize,
		&settings.WaitlistSlotExpiryMinutes,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, models.ErrNotFound
		}
		return nil, err
	}

	rules, err := r.ListBookingDurationRules(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	settings.DurationRules = rules

	physicalCapacity, err := r.GetPhysicalCapacity(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	settings.PhysicalCapacity = physicalCapacity

	maxBookingCapacity, err := r.GetMaxBookingCapacity(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	settings.CapacityWarning = maxBookingCapacity > physicalCapacity

	return settings, nil
}

func (r *BookingsRepository) GetPhysicalCapacity(ctx context.Context, merchantID string) (int, error) {
	db := dbutils.GetDB(ctx, r.database)

	var physicalCapacity int
	err := db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(COALESCE(seats, 0)), 0)
		FROM locations
		WHERE merchant_id = ?
		  AND enabled = 1
	`, merchantID).Scan(&physicalCapacity)
	if err != nil {
		return 0, err
	}

	return physicalCapacity, nil
}

func (r *BookingsRepository) GetMaxBookingCapacity(ctx context.Context, merchantID string) (int, error) {
	db := dbutils.GetDB(ctx, r.database)

	var maxBookingCapacity int
	err := db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(COALESCE(booking_capacity, 0)), 0)
		FROM hours_of_operation
		WHERE merchant_id = ?
		  AND enabled = 1
	`, merchantID).Scan(&maxBookingCapacity)
	if err != nil {
		return 0, err
	}

	return maxBookingCapacity, nil
}

func (r *BookingsRepository) ListBookingDurationRules(ctx context.Context, merchantID string) ([]BookingDurationRule, error) {
	db := dbutils.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx, `
		SELECT rule_id, min_party_size, max_party_size, duration_minutes, enabled
		FROM booking_duration_rules
		WHERE merchant_id = ? AND enabled = 1
		ORDER BY min_party_size ASC, max_party_size ASC
	`, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rules := make([]BookingDurationRule, 0)
	for rows.Next() {
		var rule BookingDurationRule
		if err := rows.Scan(&rule.RuleID, &rule.MinPartySize, &rule.MaxPartySize, &rule.DurationMinutes, &rule.Enabled); err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return rules, nil
}

func (r *BookingsRepository) UpsertBookingSettings(ctx context.Context, merchantID string, req *PutBookingSettingsRequest) error {
	db := dbutils.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `
		INSERT INTO bookings_settings (
			merchant_id,
			enabled,
			code,
			auto_accept_reserve_bookings,
			slot_interval_minutes,
			default_booking_duration,
			reserve_maximum_party_size,
			reserve_minimum_party_size,
			last_booking_offset_minutes,
			min_booking_notice_minutes,
			max_booking_horizon_days,
			overbooking_percent,
			cancelable_by_customer,
			cancel_booking_limit_offset_hours,
			pending_expiration_hours,
			sms_enabled,
			waitlist_enabled,
			waitlist_max_size,
			waitlist_slot_expiry_minutes
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			enabled = VALUES(enabled),
			code = VALUES(code),
			auto_accept_reserve_bookings = VALUES(auto_accept_reserve_bookings),
			slot_interval_minutes = VALUES(slot_interval_minutes),
			default_booking_duration = VALUES(default_booking_duration),
			reserve_maximum_party_size = VALUES(reserve_maximum_party_size),
			reserve_minimum_party_size = VALUES(reserve_minimum_party_size),
			last_booking_offset_minutes = VALUES(last_booking_offset_minutes),
			min_booking_notice_minutes = VALUES(min_booking_notice_minutes),
			max_booking_horizon_days = VALUES(max_booking_horizon_days),
			overbooking_percent = VALUES(overbooking_percent),
			cancelable_by_customer = VALUES(cancelable_by_customer),
			cancel_booking_limit_offset_hours = VALUES(cancel_booking_limit_offset_hours),
			pending_expiration_hours = VALUES(pending_expiration_hours),
			sms_enabled = VALUES(sms_enabled),
			waitlist_enabled = VALUES(waitlist_enabled),
			waitlist_max_size = VALUES(waitlist_max_size),
			waitlist_slot_expiry_minutes = VALUES(waitlist_slot_expiry_minutes)
	`,
		merchantID,
		req.Enabled,
		req.Code,
		req.AutoAcceptReserveBookings,
		req.SlotIntervalMinutes,
		req.DefaultBookingDuration,
		req.ReserveMaximumPartySize,
		req.ReserveMinimumPartySize,
		req.LastBookingOffsetMinutes,
		req.MinBookingNoticeMinutes,
		req.MaxBookingHorizonDays,
		req.OverbookingPercent,
		req.CancelableByCustomer,
		req.CancelBookingLimitOffsetHours,
		req.PendingExpirationHours,
		req.SMSEnabled,
		req.WaitlistEnabled,
		req.WaitlistMaxSize,
		req.WaitlistSlotExpiryMinutes,
	)

	return err
}

func (r *BookingsRepository) CreateBookingDurationRule(ctx context.Context, merchantID string, req CreateDurationRuleRequest) (*BookingDurationRule, error) {
	db := dbutils.GetDB(ctx, r.database)

	ruleID := helpers.GeneratePrefixedID("bdr")
	_, err := db.ExecContext(ctx, `
		INSERT INTO booking_duration_rules (rule_id, merchant_id, min_party_size, max_party_size, duration_minutes, enabled)
		VALUES (?, ?, ?, ?, ?, 1)
	`, ruleID, merchantID, req.MinPartySize, req.MaxPartySize, req.DurationMinutes)
	if err != nil {
		return nil, err
	}

	return r.GetBookingDurationRuleByID(ctx, merchantID, ruleID)
}

func (r *BookingsRepository) GetBookingDurationRuleByID(ctx context.Context, merchantID, ruleID string) (*BookingDurationRule, error) {
	db := dbutils.GetDB(ctx, r.database)

	var rule BookingDurationRule
	err := db.QueryRowContext(ctx, `
		SELECT rule_id, min_party_size, max_party_size, duration_minutes, enabled
		FROM booking_duration_rules
		WHERE merchant_id = ? AND rule_id = ?
		LIMIT 1
	`, merchantID, ruleID).Scan(&rule.RuleID, &rule.MinPartySize, &rule.MaxPartySize, &rule.DurationMinutes, &rule.Enabled)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, models.ErrNotFound
		}
		return nil, err
	}

	return &rule, nil
}

func (r *BookingsRepository) UpdateBookingDurationRule(ctx context.Context, merchantID, ruleID string, req PatchDurationRuleRequest) (*BookingDurationRule, error) {
	db := dbutils.GetDB(ctx, r.database)

	rule, err := r.GetBookingDurationRuleByID(ctx, merchantID, ruleID)
	if err != nil {
		return nil, err
	}

	if req.MinPartySize != nil {
		rule.MinPartySize = *req.MinPartySize
	}
	if req.MaxPartySize != nil {
		rule.MaxPartySize = *req.MaxPartySize
	}
	if req.DurationMinutes != nil {
		rule.DurationMinutes = *req.DurationMinutes
	}

	_, err = db.ExecContext(ctx, `
		UPDATE booking_duration_rules
		SET min_party_size = ?, max_party_size = ?, duration_minutes = ?
		WHERE merchant_id = ? AND rule_id = ?
	`, rule.MinPartySize, rule.MaxPartySize, rule.DurationMinutes, merchantID, ruleID)
	if err != nil {
		return nil, err
	}

	return r.GetBookingDurationRuleByID(ctx, merchantID, ruleID)
}

func (r *BookingsRepository) DeleteBookingDurationRule(ctx context.Context, merchantID, ruleID string) error {
	db := dbutils.GetDB(ctx, r.database)

	res, err := db.ExecContext(ctx, `
		DELETE FROM booking_duration_rules
		WHERE merchant_id = ? AND rule_id = ?
	`, merchantID, ruleID)
	if err != nil {
		return err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return models.ErrNotFound
	}

	return nil
}

func (r *BookingsRepository) ListBookingHours(ctx context.Context, merchantID string) ([]models.POSHoursOfOperation, error) {
	db := dbutils.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx, `
		SELECT
			hoo.id,
			hoo.day_of_week_from,
			hoo.day_of_week_to,
			TIME_FORMAT(hoo.hour_from, '%H:%i:%s') AS hour_from,
			TIME_FORMAT(hoo.hour_to, '%H:%i:%s') AS hour_to,
			hoo.booking_capacity,
			CASE WHEN hoo.first_booking_time IS NULL THEN NULL ELSE TIME_FORMAT(hoo.first_booking_time, '%H:%i:%s') END AS first_booking_time,
			CASE WHEN hoo.last_booking_time IS NULL THEN NULL ELSE TIME_FORMAT(hoo.last_booking_time, '%H:%i:%s') END AS last_booking_time,
			CASE WHEN hoo.valid_from IS NULL THEN NULL ELSE DATE_FORMAT(hoo.valid_from, '%Y-%m-%d %H:%i:%s') END AS valid_from,
			CASE WHEN hoo.valid_to IS NULL THEN NULL ELSE DATE_FORMAT(hoo.valid_to, '%Y-%m-%d %H:%i:%s') END AS valid_to,
			hoo.enabled
		FROM hours_of_operation hoo
		WHERE hoo.merchant_id = ?
		  AND hoo.enabled = 1
		ORDER BY hoo.day_of_week_from, hoo.day_of_week_to, hoo.hour_from, hoo.id
	`, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]models.POSHoursOfOperation, 0)
	for rows.Next() {
		var item models.POSHoursOfOperation
		var bookingCapacity sql.NullInt64
		var firstBookingTime sql.NullString
		var lastBookingTime sql.NullString
		var validFrom sql.NullString
		var validTo sql.NullString
		var enabledInt int

		err := rows.Scan(
			&item.ID,
			&item.DayOfWeekFrom,
			&item.DayOfWeekTo,
			&item.HourFrom,
			&item.HourTo,
			&bookingCapacity,
			&firstBookingTime,
			&lastBookingTime,
			&validFrom,
			&validTo,
			&enabledInt,
		)
		if err != nil {
			return nil, err
		}

		if bookingCapacity.Valid {
			v := int(bookingCapacity.Int64)
			item.BookingCapacity = &v
		}
		if firstBookingTime.Valid {
			v := firstBookingTime.String
			item.FirstBookingTime = &v
		}
		if lastBookingTime.Valid {
			v := lastBookingTime.String
			item.LastBookingTime = &v
		}
		if validFrom.Valid {
			v := validFrom.String
			item.ValidFrom = &v
		}
		if validTo.Valid {
			v := validTo.String
			item.ValidTo = &v
		}
		item.Enabled = enabledInt == 1

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (r *BookingsRepository) ReplaceBookingHours(ctx context.Context, merchantID string, hours []models.POSHoursOfOperationPatch) error {
	return dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		db := dbutils.GetDB(txCtx, r.database)

		if _, err := db.ExecContext(txCtx, `
			UPDATE hours_of_operation
			SET enabled = 0
			WHERE merchant_id = ?
		`, merchantID); err != nil {
			return err
		}

		for _, item := range hours {
			id := ""
			if item.ID != nil {
				id = strings.TrimSpace(*item.ID)
			}

			enabled := true
			if item.Enabled != nil {
				enabled = *item.Enabled
			}

			var firstBooking interface{}
			if item.FirstBookingTime != nil && strings.TrimSpace(*item.FirstBookingTime) != "" {
				firstBooking = strings.TrimSpace(*item.FirstBookingTime)
			}

			var lastBooking interface{}
			if item.LastBookingTime != nil && strings.TrimSpace(*item.LastBookingTime) != "" {
				lastBooking = strings.TrimSpace(*item.LastBookingTime)
			}

			var validFrom interface{}
			if item.ValidFrom != nil && strings.TrimSpace(*item.ValidFrom) != "" {
				validFrom = strings.TrimSpace(*item.ValidFrom)
			}

			var validTo interface{}
			if item.ValidTo != nil && strings.TrimSpace(*item.ValidTo) != "" {
				validTo = strings.TrimSpace(*item.ValidTo)
			}

			if _, err := db.ExecContext(txCtx, `
				INSERT INTO hours_of_operation (
					id,
					merchant_id,
					day_of_week_from,
					day_of_week_to,
					hour_from,
					hour_to,
					booking_capacity,
					first_booking_time,
					last_booking_time,
					valid_from,
					valid_to,
					enabled
				)
				VALUES (
					COALESCE(NULLIF(?, ''), UUID()),
					?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
				)
				ON DUPLICATE KEY UPDATE
					merchant_id = VALUES(merchant_id),
					day_of_week_from = VALUES(day_of_week_from),
					day_of_week_to = VALUES(day_of_week_to),
					hour_from = VALUES(hour_from),
					hour_to = VALUES(hour_to),
					booking_capacity = VALUES(booking_capacity),
					first_booking_time = VALUES(first_booking_time),
					last_booking_time = VALUES(last_booking_time),
					valid_from = VALUES(valid_from),
					valid_to = VALUES(valid_to),
					enabled = VALUES(enabled)
			`,
				id,
				merchantID,
				item.DayOfWeekFrom,
				item.DayOfWeekTo,
				strings.TrimSpace(item.HourFrom),
				strings.TrimSpace(item.HourTo),
				item.BookingCapacity,
				firstBooking,
				lastBooking,
				validFrom,
				validTo,
				enabled,
			); err != nil {
				return err
			}
		}

		return nil
	})
}

func (r *BookingsRepository) ListBookingsBackOffice(ctx context.Context, merchantID string, filters BookingListFilters) ([]BookingListItem, int, error) {
	db := dbutils.GetDB(ctx, r.database)

	sortBy := "b.booking_date_from"
	switch strings.ToLower(strings.TrimSpace(filters.SortBy)) {
	case "booking_date_from":
		sortBy = "b.booking_date_from"
	case "party_size":
		sortBy = "b.party_size"
	case "status":
		sortBy = "b.status"
	case "customer_name":
		sortBy = "c.customer_name"
	}

	sortDir := "DESC"
	if strings.EqualFold(strings.TrimSpace(filters.SortDir), "asc") {
		sortDir = "ASC"
	}

	where := []string{"b.merchant_id = ?"}
	args := []interface{}{merchantID}

	if len(filters.Statuses) > 0 {
		placeholders := make([]string, 0, len(filters.Statuses))
		for _, status := range filters.Statuses {
			status = strings.TrimSpace(status)
			if status == "" {
				continue
			}
			placeholders = append(placeholders, "?")
			args = append(args, status)
		}
		if len(placeholders) > 0 {
			where = append(where, "b.status IN ("+strings.Join(placeholders, ",")+")")
		}
	}

	if filters.DateFrom != nil && strings.TrimSpace(*filters.DateFrom) != "" {
		where = append(where, "b.booking_date_from >= ?")
		args = append(args, strings.TrimSpace(*filters.DateFrom))
	}
	if filters.DateTo != nil && strings.TrimSpace(*filters.DateTo) != "" {
		where = append(where, "b.booking_date_from <= ?")
		args = append(args, strings.TrimSpace(*filters.DateTo))
	}
	if filters.PartySize != nil {
		where = append(where, "b.party_size = ?")
		args = append(args, *filters.PartySize)
	}
	if filters.Source != nil && strings.TrimSpace(*filters.Source) != "" {
		where = append(where, "b.source = ?")
		args = append(args, strings.TrimSpace(*filters.Source))
	}
	if filters.Search != nil && strings.TrimSpace(*filters.Search) != "" {
		search := "%" + strings.TrimSpace(*filters.Search) + "%"
		where = append(where, "(c.customer_name LIKE ? OR c.customer_tel LIKE ?)")
		args = append(args, search, search)
	}

	whereSQL := strings.Join(where, " AND ")

	var totalItems int
	countQuery := `
		SELECT COUNT(*)
		FROM bookings b
		INNER JOIN customer c ON c.customer_id = b.customer_id
		WHERE ` + whereSQL
	err := db.QueryRowContext(ctx, countQuery, args...).Scan(&totalItems)
	if err != nil {
		return nil, 0, err
	}

	offset := (filters.Page - 1) * filters.Limit
	dataQuery := `
		SELECT
			b.booking_id,
			b.booking_number,
			b.status,
			COALESCE(b.source, 'staff') AS source,
			DATE_FORMAT(b.booking_date_from, '%Y-%m-%d %H:%i:%s') AS booking_date_from,
			b.party_size,
			COALESCE(c.customer_name, '') AS customer_name,
			COALESCE(c.customer_tel, '') AS customer_tel,
			GROUP_CONCAT(DISTINCT l.location_name ORDER BY l.location_name SEPARATOR '||') AS assigned_tables
		FROM bookings b
		INNER JOIN customer c ON c.customer_id = b.customer_id
		LEFT JOIN booked_location bl ON bl.booking_id = b.booking_id
		LEFT JOIN locations l ON l.location_id = bl.location_id
		WHERE ` + whereSQL + `
		GROUP BY b.booking_id, b.booking_number, b.status, b.source, b.booking_date_from, b.party_size, c.customer_name, c.customer_tel
		ORDER BY ` + sortBy + ` ` + sortDir + `
		LIMIT ? OFFSET ?`

	dataArgs := append([]interface{}{}, args...)
	dataArgs = append(dataArgs, filters.Limit, offset)

	rows, err := db.QueryContext(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]BookingListItem, 0)
	for rows.Next() {
		var item BookingListItem
		var assignedTables sql.NullString
		if err := rows.Scan(
			&item.BookingID,
			&item.BookingNumber,
			&item.Status,
			&item.Source,
			&item.BookingDateFrom,
			&item.PartySize,
			&item.CustomerName,
			&item.CustomerTel,
			&assignedTables,
		); err != nil {
			return nil, 0, err
		}

		if assignedTables.Valid && strings.TrimSpace(assignedTables.String) != "" {
			item.AssignedTables = strings.Split(assignedTables.String, "||")
		} else {
			item.AssignedTables = []string{}
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return items, totalItems, nil
}

func (r *BookingsRepository) CreateBooking(ctx context.Context, req *BookingObjectRequest) (string, error) {
	db := dbutils.GetDB(ctx, r.database)

	var normalizedCustomerID *string
	if req.Customer.CustomerID != nil {
		trimmedCustomerID := strings.TrimSpace(*req.Customer.CustomerID)
		if trimmedCustomerID != "" {
			normalizedCustomerID = &trimmedCustomerID
		}
	}

	// 1️⃣ Construire un modèle Customer
	customer := &models.Customer{
		CustomerID:    normalizedCustomerID,
		MerchantID:    req.MerchantID,
		CustomerName:  req.Customer.CustomerName,
		CustomerTel:   req.Customer.CustomerTel,
		CustomerEmail: req.Customer.CustomerEmail,
		// Tous les autres champs sont optionnels → nil
	}
	if normalizedCustomerID == nil {
		brand := models.BrandWelloResto
		customer.CustomerBrand = &brand
	}

	// 2️⃣ Update or Create
	customerID, err := r.customerUpdater.UpdateOrCreateCustomer(ctx, customer)
	if err != nil {
		return "", fmt.Errorf("failed to update/create customer: %w", err)
	}

	// injecter l’MerchantID dans la requête
	req.Customer.CustomerID = customerID

	// 3️⃣ Check dates
	if req.Booking.StartDate == "" || req.Booking.EndDate == "" {
		return "", fmt.Errorf("start_date or end_date is empty")
	}
	/*
		rollback := func(err error) (string, error) {
			tx.Rollback()
			return "", err
		}
	*/
	//---------------------------------------------------------
	// 1. Generate unique booking number
	//---------------------------------------------------------
	var exists string
	var bookingNumber string

	for {
		bookingNumber = strings.ToUpper(utils.GenerateRandomString(6))

		err = db.QueryRowContext(ctx,
			`SELECT booking_id FROM bookings WHERE booking_number = ?`,
			bookingNumber,
		).Scan(&exists)

		if err == sql.ErrNoRows {
			break
		}
		if err != nil {
			return "", err
		}
	}

	//---------------------------------------------------------
	// 2. Compute duration
	//---------------------------------------------------------
	start, err := time.Parse("2006-01-02 15:04:05", req.Booking.StartDate)
	if err != nil {
		return "", err
	}

	end, err := time.Parse("2006-01-02 15:04:05", req.Booking.EndDate)
	if err != nil {
		return "", err
	}

	diff := end.Sub(start)
	if diff < 0 {
		return "", fmt.Errorf("start date is after end date")
	}

	duration := int(diff.Minutes())

	//---------------------------------------------------------
	// 3. Insert booking
	//---------------------------------------------------------
	res, err := db.ExecContext(ctx, `
        INSERT INTO bookings (
            booking_number, status, merchant_id, party_size,
            customer_id, comment, creation_date,
            booking_date_from, booking_date_to,
            booking_duration, created_by
        ) VALUES (?, ?, ?, ?, ?, ?, UTC_TIMESTAMP, ?, ?, ?, ?)
    `,
		bookingNumber,
		req.Booking.Status,
		req.MerchantID,
		req.Booking.PartySize,
		customerID,
		req.Booking.Comment,
		req.Booking.StartDate,
		req.Booking.EndDate,
		duration,
		req.CreatedBy,
	)
	if err != nil {
		return "", err
	}

	bookingID, _ := res.LastInsertId()

	//---------------------------------------------------------
	// 4. Insert locations
	//---------------------------------------------------------
	for _, loc := range req.Booking.Locations {
		_, err := db.ExecContext(ctx, `
            INSERT INTO booked_location(booking_id, location_id)
            VALUES (?, ?)
        `,
			bookingID,
			loc.LocationID,
		)
		if err != nil {
			return "", err
		}
	}

	//---------------------------------------------------------
	// Commit
	//---------------------------------------------------------
	/*
		if err := tx.Commit(); err != nil {
			return rollback(err)
		}
	*/

	return fmt.Sprintf("%d", bookingID), nil
}

// FindConflictingBookings retourne les affectations de tables en collision avec
// le créneau [dateFrom, dateTo) pour les tables demandées (chevauchement strict :
// deux créneaux dos à dos ne sont pas en conflit). Le FOR UPDATE verrouille les
// lignes candidates le temps de la transaction appelante — stratégie de verrou
// SQL seul (addendum, décision 7.5) ; avec le pool à 1 connexion les écritures
// d'une instance sont déjà sérialisées, le verrou couvre le multi-instances.
// excludeBookingID vide = pas d'exclusion (création) ; renseigné = réattribution.
// Statuts legacy actifs uniquement — bascule vers le vocabulaire normalisé en
// Phase 1 (T-08). Les booking_date_to NULL du flux public legacy retombent sur
// booking_duration (défaut 90 min), même convention que le calcul d'occupation.
func (r *BookingsRepository) FindConflictingBookings(ctx context.Context, merchantID string, locationIDs []string, dateFrom, dateTo, excludeBookingID string) ([]BookingLocationConflict, error) {
	if len(locationIDs) == 0 {
		return nil, nil
	}

	db := dbutils.GetDB(ctx, r.database)

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(locationIDs)), ",")

	if excludeBookingID == "" {
		excludeBookingID = "0" // booking_id est un AUTO_INCREMENT : 0 n'existe jamais
	}

	args := make([]interface{}, 0, len(locationIDs)+4)
	for _, id := range locationIDs {
		args = append(args, id)
	}
	args = append(args, merchantID, dateTo, dateFrom, excludeBookingID)

	query := fmt.Sprintf(`
        SELECT b.booking_id, bl.location_id
        FROM booked_location bl
        INNER JOIN bookings b ON b.booking_id = bl.booking_id
        WHERE bl.location_id IN (%s)
          AND b.merchant_id = ?
          AND b.status IN ('PENDING_APPROVAL','ACCEPTED','ORDER_OPEN')
          AND b.booking_date_from < ?
          AND COALESCE(b.booking_date_to, b.booking_date_from + INTERVAL COALESCE(b.booking_duration, 90) MINUTE) > ?
          AND b.booking_id <> ?
        FOR UPDATE`, placeholders)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	conflicts := []BookingLocationConflict{}
	for rows.Next() {
		var c BookingLocationConflict
		if err := rows.Scan(&c.BookingID, &c.LocationID); err != nil {
			return nil, err
		}
		conflicts = append(conflicts, c)
	}

	return conflicts, rows.Err()
}

// FindConfirmedBookingForAutoSeat cherche la resa confirmed la plus proche de
// maintenant parmi celles dont une table attribuee figure dans locationIDs,
// avec une tolerance de 30 min de part et d'autre de sa fenetre
// [booking_date_from, booking_date_to] (arrivee en avance/en retard). "" est
// retourne (sans erreur) si aucune resa ne correspond.
func (r *BookingsRepository) FindConfirmedBookingForAutoSeat(ctx context.Context, merchantID string, locationIDs []string) (string, error) {
	if len(locationIDs) == 0 {
		return "", nil
	}

	db := dbutils.GetDB(ctx, r.database)

	placeholders := make([]string, len(locationIDs))
	args := make([]interface{}, 0, len(locationIDs)+1)
	for i, id := range locationIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	args = append(args, merchantID)

	query := fmt.Sprintf(`
        SELECT b.booking_id
        FROM bookings b
        INNER JOIN booked_location bl ON bl.booking_id = b.booking_id
        WHERE bl.location_id IN (%s)
          AND b.merchant_id = ?
          AND b.status = 'confirmed'
          AND b.booking_date_from - INTERVAL 30 MINUTE <= UTC_TIMESTAMP()
          AND COALESCE(b.booking_date_to, b.booking_date_from + INTERVAL COALESCE(b.booking_duration, 90) MINUTE) + INTERVAL 30 MINUTE >= UTC_TIMESTAMP()
        GROUP BY b.booking_id, b.booking_date_from
        ORDER BY ABS(TIMESTAMPDIFF(SECOND, b.booking_date_from, UTC_TIMESTAMP())) ASC
        LIMIT 1
    `, strings.Join(placeholders, ","))

	var bookingID string
	err := db.QueryRowContext(ctx, query, args...).Scan(&bookingID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	return bookingID, nil
}

// SetBookingSeatedWithOrder transitionne une resa vers seated et pose le lien
// caisse (bookings.order_id), active par le hook auto-seat (cf.
// order_life_cycle.CreateOrder). Ce lien est ensuite exploite tel quel par
// order_life_cycle.ClearBookings (detache a l'annulation de commande, sans
// toucher au statut) et par le hook auto-complete (FindSeatedBookingByOrderID).
func (r *BookingsRepository) SetBookingSeatedWithOrder(ctx context.Context, merchantID, bookingID, orderID string) error {
	db := dbutils.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `
        UPDATE bookings
        SET status = ?, order_id = ?
        WHERE booking_id = ? AND merchant_id = ?
    `, bookingcore.StatusSeated, orderID, bookingID, merchantID)
	return err
}

// FindSeatedBookingByOrderID retrouve la resa seated liee a une commande
// (posee par SetBookingSeatedWithOrder). "" est retourne (sans erreur) si
// aucune resa n'est liee ou si elle n'est plus au statut seated.
func (r *BookingsRepository) FindSeatedBookingByOrderID(ctx context.Context, merchantID, orderID string) (string, error) {
	db := dbutils.GetDB(ctx, r.database)

	var bookingID string
	err := db.QueryRowContext(ctx, `
        SELECT booking_id
        FROM bookings
        WHERE merchant_id = ? AND order_id = ? AND status = ?
        LIMIT 1
    `, merchantID, orderID, bookingcore.StatusSeated).Scan(&bookingID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	return bookingID, nil
}

func (r *BookingsRepository) SetBookingState(ctx context.Context, merchantID, bookingID string, state string) error {
	db := dbutils.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `
        UPDATE bookings
        SET status = ?
        WHERE booking_id = ? AND merchant_id = ?
    `, state, bookingID, merchantID)

	if err != nil {
		return err
	}

	return nil
}

func (r *BookingsRepository) DenyBooking(ctx context.Context, merchantID, bookingID, userID string, req *DenyBookingRequest) error {
	db := dbutils.GetDB(ctx, r.database)

	var deletionReasonID interface{}
	if req != nil && req.DeletionReasonID != nil {
		deletionReasonID = *req.DeletionReasonID
	}

	_, err := db.ExecContext(ctx, `
		UPDATE bookings
		SET status = ?, cancelled_by = ?, deletion_reason_id = ?, deletion_date = UTC_TIMESTAMP
		WHERE booking_id = ? AND merchant_id = ?
	`, bookingcore.StatusDenied, userID, deletionReasonID, bookingID, merchantID)
	return err
}

func (r *BookingsRepository) IsValidDeletionReason(ctx context.Context, deletionReasonID string) (bool, error) {
	db := dbutils.GetDB(ctx, r.database)

	var existing string
	err := db.QueryRowContext(ctx, `
		SELECT deletion_reason_id
		FROM deletion_reasons
		WHERE deletion_reason_id = ?
		  AND enabled = 1
		  AND LOWER(deletion_reason_object) IN ('booking', 'bookings')
		LIMIT 1
	`, deletionReasonID).Scan(&existing)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

// CancelBooking annule staff une résa confirmed|seated (miroir de DenyBooking,
// qui couvre les pending — cf. addendum §7.9).
func (r *BookingsRepository) CancelBooking(ctx context.Context, merchantID, bookingID, userID string, req *CancelBookingRequest) error {
	db := dbutils.GetDB(ctx, r.database)

	var deletionReasonID interface{}
	if req != nil && req.DeletionReasonID != nil {
		deletionReasonID = *req.DeletionReasonID
	}

	_, err := db.ExecContext(ctx, `
		UPDATE bookings
		SET status = ?, cancelled_by = ?, deletion_reason_id = ?, deletion_date = UTC_TIMESTAMP
		WHERE booking_id = ? AND merchant_id = ?
	`, bookingcore.StatusCancelled, userID, deletionReasonID, bookingID, merchantID)
	return err
}

// RescheduleBooking modifie staff la date/heure (et éventuellement le nombre
// de couverts) d'une résa. Le statut n'est pas touché (aligné sur le contrat
// staff, à la différence de la modification publique qui peut repasser en
// pending — cf. reservation.UpdateReservation).
func (r *BookingsRepository) RescheduleBooking(ctx context.Context, merchantID, bookingID, dateFrom, dateTo string, partySize *int) error {
	db := dbutils.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `
		UPDATE bookings
		SET booking_date_from = ?,
		    booking_date_to = ?,
		    booking_duration = TIMESTAMPDIFF(MINUTE, ?, ?),
		    party_size = COALESCE(?, party_size),
		    sequence_number = sequence_number + 1
		WHERE booking_id = ? AND merchant_id = ?
	`, dateFrom, dateTo, dateFrom, dateTo, partySize, bookingID, merchantID)
	return err
}

// CheckCapacityForWindow revalide la disponibilité d'une fenêtre [dateFrom,
// dateTo) explicite pour party_size couverts, en excluant excludeBookingID de
// l'occupation (§6.1/§6.2 du cadrage technico-fonctionnel, moteur unifié
// bookingcore). Retourne false si aucune plage d'ouverture ne couvre la
// fenêtre, ou si la capacité (avec overbooking) est dépassée à un instant de
// la fenêtre.
func (r *BookingsRepository) CheckCapacityForWindow(ctx context.Context, merchantID, dateFrom, dateTo string, partySize int, excludeBookingID string) (bool, error) {
	from, err := time.Parse("2006-01-02 15:04:05", dateFrom)
	if err != nil {
		return false, fmt.Errorf("invalid dateFrom: %w", err)
	}
	to, err := time.Parse("2006-01-02 15:04:05", dateTo)
	if err != nil {
		return false, fmt.Errorf("invalid dateTo: %w", err)
	}
	if !to.After(from) {
		return false, nil
	}

	requestedDate := from.Format("2006-01-02")

	params, err := r.loadMerchantBookingParams(ctx, merchantID)
	if err != nil {
		return false, err
	}

	timeRanges, _, err := r.loadHoursOfOperation(ctx, merchantID, requestedDate)
	if err != nil {
		return false, err
	}

	existingBookings, err := r.loadExistingBookings(ctx, merchantID, requestedDate, excludeBookingID)
	if err != nil {
		return false, err
	}

	rules, err := r.loadBookingDurationRules(ctx, merchantID)
	if err != nil {
		return false, err
	}

	occupation := r.computeOccupation(existingBookings, params, rules)
	capacityMultiplier := 100 + params.OverbookingPercent

	// Une plage doit couvrir toute la fenêtre demandée (bornes de service,
	// first/last booking time) pour que la fenêtre soit jouable.
	var coveringRange *TimeRange
	for i := range timeRanges {
		tr := &timeRanges[i]

		rangeStart, err := time.ParseInLocation("2006-01-02 15:04:05", requestedDate+" "+tr.HourFrom, from.Location())
		if err != nil {
			continue
		}
		rangeEnd, err := time.ParseInLocation("2006-01-02 15:04:05", requestedDate+" "+tr.HourTo, from.Location())
		if err != nil {
			continue
		}

		if tr.FirstBookingTime != nil && *tr.FirstBookingTime != "" {
			if fb, err := time.ParseInLocation("2006-01-02 15:04:05", requestedDate+" "+*tr.FirstBookingTime, from.Location()); err == nil && fb.After(rangeStart) {
				rangeStart = fb
			}
		}
		if tr.LastBookingTime != nil && *tr.LastBookingTime != "" {
			if lb, err := time.ParseInLocation("2006-01-02 15:04:05", requestedDate+" "+*tr.LastBookingTime, from.Location()); err == nil && lb.Before(rangeEnd) {
				rangeEnd = lb
			}
		}

		if !from.Before(rangeStart) && !to.After(rangeEnd) {
			coveringRange = tr
			break
		}
	}

	if coveringRange == nil {
		return false, nil
	}

	capacity := (coveringRange.BookingCapacity * capacityMultiplier) / 100
	interval := params.SlotIntervalMinutes
	if interval <= 0 {
		interval = 15
	}

	for cur := from; cur.Before(to); cur = cur.Add(time.Duration(interval) * time.Minute) {
		occ := occupation[cur.Format("15:04:05")]
		if occ+partySize > capacity {
			return false, nil
		}
	}

	return true, nil
}

func (r *BookingsRepository) ReplaceBookingLocations(ctx context.Context, merchantID, bookingID string, locationIDs []string) error {
	db := dbutils.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `
		DELETE bl
		FROM booked_location bl
		INNER JOIN bookings b ON b.booking_id = bl.booking_id
		WHERE bl.booking_id = ? AND b.merchant_id = ?
	`, bookingID, merchantID)
	if err != nil {
		return err
	}

	for _, locationID := range locationIDs {
		if strings.TrimSpace(locationID) == "" {
			continue
		}
		_, err := db.ExecContext(ctx, `
			INSERT INTO booked_location(booking_id, location_id)
			VALUES (?, ?)
		`, bookingID, locationID)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *BookingsRepository) GetBookingAvailability(ctx context.Context, merchantID, requestedDate string) (*BookingAvailabilityResponse, error) {
	// -------------------------------------------------------
	// 1) Merchant + booking_settings
	// -------------------------------------------------------
	params, err := r.loadMerchantBookingParams(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	// -------------------------------------------------------
	// 2) Hours of operation
	// -------------------------------------------------------
	timeRanges, dayOfWeek, err := r.loadHoursOfOperation(ctx, merchantID, requestedDate)
	if err != nil {
		return nil, err
	}

	// -------------------------------------------------------
	// 3) Existing bookings (start + end)
	// -------------------------------------------------------
	bookings, err := r.loadExistingBookings(ctx, merchantID, requestedDate, "")
	if err != nil {
		return nil, err
	}

	durationRules, err := r.loadBookingDurationRules(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	// -------------------------------------------------------
	// 4) Compute occupation for each slot
	// -------------------------------------------------------
	occupation := r.computeOccupation(bookings, params, durationRules)

	// -------------------------------------------------------
	// 5) Generate availability slots
	// -------------------------------------------------------
	slots := r.buildAvailabilitySlots(
		params,
		requestedDate,
		timeRanges,
		occupation,
		durationRules,
	)

	// -------------------------------------------------------
	// 6) Add locations
	// -------------------------------------------------------
	locations, err := r.loadMerchantLocations(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	// -------------------------------------------------------
	// 7) Response
	// -------------------------------------------------------
	resp := &BookingAvailabilityResponse{
		Merchant:   params,
		Locations:  locations,
		TimeRanges: timeRanges,
		Slots:      slots,
		Occupation: occupation,
		Date:       requestedDate,
		DayOfWeek:  dayOfWeek,
		Status:     "1",
	}

	return resp, nil
}

// ListPendingBookingsToExpire retourne les réservations pending sur le point
// d'être expirées (même prédicat que ExpirePendingBookings), avec les
// données de contact nécessaires à l'envoi d'une notification d'annulation
// avant la bascule de statut.
func (r *BookingsRepository) ListPendingBookingsToExpire(ctx context.Context) ([]BookingContact, error) {
	db := dbutils.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx, `
		SELECT
			b.booking_id, b.merchant_id, b.booking_number, b.party_size,
			DATE_FORMAT(b.booking_date_from, '%Y-%m-%d %H:%i:%s'),
			m.fullName, COALESCE(bs.code, ''), m.timezone, COALESCE(bs.sms_enabled, 0),
			COALESCE(c.customer_name, ''), COALESCE(c.customer_email, ''), COALESCE(c.customer_tel, '')
		FROM bookings b
		INNER JOIN merchant m ON m.id = b.merchant_id
		LEFT JOIN bookings_settings bs ON bs.merchant_id = b.merchant_id
		LEFT JOIN customer c ON c.customer_id = b.customer_id
		WHERE b.status = ?
		  AND COALESCE(
				b.booking_date_to,
				b.booking_date_from + INTERVAL COALESCE(b.booking_duration, 90) MINUTE
		  ) < UTC_TIMESTAMP() - INTERVAL COALESCE(bs.pending_expiration_hours, 24) HOUR
	`, bookingcore.StatusPending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]BookingContact, 0)
	for rows.Next() {
		var c BookingContact
		var smsEnabled int
		if err := rows.Scan(
			&c.BookingID, &c.MerchantID, &c.BookingNumber, &c.PartySize, &c.StartDate,
			&c.MerchantName, &c.MerchantSlug, &c.Timezone, &smsEnabled,
			&c.CustomerName, &c.CustomerEmail, &c.CustomerPhone,
		); err != nil {
			return nil, err
		}
		c.SMSEnabled = smsEnabled == 1
		list = append(list, c)
	}
	return list, rows.Err()
}

func (r *BookingsRepository) ExpirePendingBookings(ctx context.Context) (int64, error) {
	db := dbutils.GetDB(ctx, r.database)

	result, err := db.ExecContext(ctx, `
		UPDATE bookings b
		LEFT JOIN bookings_settings bs ON bs.merchant_id = b.merchant_id
		SET b.status = ?,
		    b.cancelled_by = ?,
		    b.deletion_reason_id = ?,
		    b.deletion_date = UTC_TIMESTAMP
		WHERE b.status = ?
		  AND COALESCE(
				b.booking_date_to,
				b.booking_date_from + INTERVAL COALESCE(b.booking_duration, 90) MINUTE
		  ) < UTC_TIMESTAMP() - INTERVAL COALESCE(bs.pending_expiration_hours, 24) HOUR
	`, bookingcore.StatusCancelled, bookingcore.ResolveCancellationActor("system", ""), "booking_pending_expired", bookingcore.StatusPending)
	if err != nil {
		return 0, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	return rowsAffected, nil
}

// ListBookingsForReminder retourne les réservations confirmed dont le
// créneau tombe dans les prochaines withinHours heures et qui n'ont pas
// encore reçu de rappel (reminder_sent_at IS NULL).
func (r *BookingsRepository) ListBookingsForReminder(ctx context.Context, withinHours int) ([]BookingContact, error) {
	db := dbutils.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx, `
		SELECT
			b.booking_id, b.merchant_id, b.booking_number, b.party_size,
			DATE_FORMAT(b.booking_date_from, '%Y-%m-%d %H:%i:%s'),
			m.fullName, COALESCE(bs.code, ''), m.timezone, COALESCE(bs.sms_enabled, 0),
			COALESCE(c.customer_name, ''), COALESCE(c.customer_email, ''), COALESCE(c.customer_tel, '')
		FROM bookings b
		INNER JOIN merchant m ON m.id = b.merchant_id
		LEFT JOIN bookings_settings bs ON bs.merchant_id = b.merchant_id
		LEFT JOIN customer c ON c.customer_id = b.customer_id
		WHERE b.status = ?
		  AND b.reminder_sent_at IS NULL
		  AND b.booking_date_from >= UTC_TIMESTAMP()
		  AND b.booking_date_from < UTC_TIMESTAMP() + INTERVAL ? HOUR
	`, bookingcore.StatusConfirmed, withinHours)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]BookingContact, 0)
	for rows.Next() {
		var c BookingContact
		var smsEnabled int
		if err := rows.Scan(
			&c.BookingID, &c.MerchantID, &c.BookingNumber, &c.PartySize, &c.StartDate,
			&c.MerchantName, &c.MerchantSlug, &c.Timezone, &smsEnabled,
			&c.CustomerName, &c.CustomerEmail, &c.CustomerPhone,
		); err != nil {
			return nil, err
		}
		c.SMSEnabled = smsEnabled == 1
		list = append(list, c)
	}
	return list, rows.Err()
}

// MarkReminderSent pose reminder_sent_at pour éviter un double envoi.
func (r *BookingsRepository) MarkReminderSent(ctx context.Context, bookingID string) error {
	db := dbutils.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
		UPDATE bookings SET reminder_sent_at = UTC_TIMESTAMP() WHERE booking_id = ?
	`, bookingID)
	return err
}

func (r *BookingsRepository) loadMerchantBookingParams(ctx context.Context, merchantID string) (*MerchantBookingParams, error) {
	db := dbutils.GetDB(ctx, r.database)

	row := db.QueryRowContext(ctx, `
	 SELECT m.id, m.timezone,
		 COALESCE(bs.default_booking_duration, 90),
		 COALESCE(bs.slot_interval_minutes, 15),
		 COALESCE(bs.auto_accept_reserve_bookings, 0),
		 COALESCE(bs.reserve_maximum_party_size, 8),
		 COALESCE(bs.reserve_minimum_party_size, 1),
		 COALESCE(bs.first_booking_offset_minutes, 0),
		 COALESCE(bs.last_booking_offset_minutes, 60),
		 COALESCE(bs.cancelable_by_customer, 1),
		 COALESCE(bs.cancel_booking_limit_offset_hours, 48),
		 COALESCE(bs.enabled, 1),
		 COALESCE(bs.overbooking_percent, 0),
		 COALESCE(bs.max_booking_horizon_days, 90),
		 COALESCE(bs.pending_expiration_hours, 24),
		 m.logo_url, m.fullName
	 FROM merchant m
	 LEFT JOIN bookings_settings bs ON bs.merchant_id = m.id
        WHERE m.id = ?
    `, merchantID)

	params := MerchantBookingParams{}
	err := row.Scan(
		&params.MerchantID,
		&params.Timezone,
		&params.DefaultBookingDuration,
		&params.SlotIntervalMinutes,
		&params.AutoAcceptReserveBookings,
		&params.ReserveMaximumPartySize,
		&params.ReserveMinimumPartySize,
		&params.FirstBookingOffsetMinutes,
		&params.LastBookingOffsetMinutes,
		&params.CancelableByCustomer,
		&params.CancelBookingLimitOffsetHours,
		&params.Enabled,
		&params.OverbookingPercent,
		&params.MaxBookingHorizonDays,
		&params.PendingExpirationHours,
		&params.LogoURL,
		&params.BusinessName,
	)

	if err != nil {
		return nil, err
	}

	return &params, nil
}

func (r *BookingsRepository) loadHoursOfOperation(ctx context.Context, merchantID, requestedDate string) ([]TimeRange, int, error) {
	db := dbutils.GetDB(ctx, r.database)

	dateObj, _ := time.Parse("2006-01-02", requestedDate)
	dayOfWeek := int(dateObj.Weekday())
	if dayOfWeek == 0 {
		dayOfWeek = 7
	}
	// 1 = lundi, ..., 7 = dimanche (1-7 standard)

	rows, err := db.QueryContext(ctx, `
				SELECT id, hour_from, hour_to, booking_capacity, first_booking_time, last_booking_time
        FROM hours_of_operation
        WHERE merchant_id = ?
          AND enabled = 1
					AND (
								(day_of_week_from <= day_of_week_to AND ? BETWEEN day_of_week_from AND day_of_week_to)
								OR
								(day_of_week_from > day_of_week_to AND (? >= day_of_week_from OR ? <= day_of_week_to))
							)
          AND (valid_from IS NULL OR valid_from <= ?)
          AND (valid_to IS NULL OR valid_to >= ?)
    `,
		merchantID, dayOfWeek, dayOfWeek, dayOfWeek, requestedDate, requestedDate,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := []TimeRange{}
	for rows.Next() {
		var tr TimeRange
		if err := rows.Scan(&tr.ID, &tr.HourFrom, &tr.HourTo, &tr.BookingCapacity, &tr.FirstBookingTime, &tr.LastBookingTime); err != nil {
			return nil, 0, err
		}
		list = append(list, tr)
	}

	return list, dayOfWeek, nil
}

// loadExistingBookings charge les résas actives du jour demandé. excludeBookingID,
// si non vide, retire cette résa du calcul d'occupation (revalidation d'un
// créneau en excluant la résa que l'on est en train de modifier).
func (r *BookingsRepository) loadExistingBookings(ctx context.Context, merchantID, requestedDate, excludeBookingID string) ([]ExistingBooking, error) {
	db := dbutils.GetDB(ctx, r.database)
	dayStart := requestedDate + " 00:00:00"
	dayEndTime, _ := time.Parse("2006-01-02 15:04:05", dayStart)
	dayEnd := dayEndTime.Add(24 * time.Hour).Format("2006-01-02 15:04:05")

	rows, err := db.QueryContext(ctx, `
				SELECT party_size, booking_date_from, booking_date_to, booking_duration, status
        FROM bookings
				WHERE booking_date_from < ?
					AND COALESCE(booking_date_to, booking_date_from + INTERVAL COALESCE(booking_duration, 90) MINUTE) > ?
          AND merchant_id = ?
          AND (? = '' OR booking_id <> ?)
		`, dayEnd, dayStart, merchantID, excludeBookingID, excludeBookingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []ExistingBooking{}

	for rows.Next() {
		var b ExistingBooking
		if err := rows.Scan(&b.PartySize, &b.StartDate, &b.EndDate, &b.DurationMinutes, &b.Status); err != nil {
			return nil, err
		}
		if !bookingcore.IsActiveForConflict(b.Status) {
			continue
		}
		list = append(list, b)
	}

	return list, nil
}

func (r *BookingsRepository) loadBookingDurationRules(ctx context.Context, merchantID string) ([]bookingcore.DurationRule, error) {
	db := dbutils.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx, `
        SELECT min_party_size, max_party_size, duration_minutes, enabled
        FROM booking_duration_rules
        WHERE merchant_id = ?
        ORDER BY min_party_size, max_party_size
    `, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rules := []bookingcore.DurationRule{}
	for rows.Next() {
		var rule bookingcore.DurationRule
		if err := rows.Scan(&rule.MinPartySize, &rule.MaxPartySize, &rule.DurationMinutes, &rule.Enabled); err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}

	return rules, rows.Err()
}

func (r *BookingsRepository) computeOccupation(bookings []ExistingBooking, params *MerchantBookingParams, rules []bookingcore.DurationRule) map[string]int {
	input := make([]bookingcore.IntervalBooking, 0, len(bookings))
	for _, b := range bookings {
		input = append(input, bookingcore.IntervalBooking{
			PartySize:       b.PartySize,
			StartDate:       b.StartDate,
			EndDate:         b.EndDate,
			DurationMinutes: b.DurationMinutes,
		})
	}

	return bookingcore.BuildOccupationByInterval(input, params.SlotIntervalMinutes, bookingcore.BookingSettings{
		DefaultBookingDuration:        params.DefaultBookingDuration,
		AutoAcceptReserveBookings:     params.AutoAcceptReserveBookings,
		ReserveMaximumPartySize:       params.ReserveMaximumPartySize,
		ReserveMinimumPartySize:       params.ReserveMinimumPartySize,
		FirstBookingOffsetMinutes:     params.FirstBookingOffsetMinutes,
		LastBookingOffsetMinutes:      params.LastBookingOffsetMinutes,
		CancelBookingLimitOffsetHours: params.CancelBookingLimitOffsetHours,
		SlotIntervalMinutes:           params.SlotIntervalMinutes,
		CancelableByCustomer:          params.CancelableByCustomer,
		Enabled:                       params.Enabled,
		OverbookingPercent:            params.OverbookingPercent,
		MaxBookingHorizonDays:         params.MaxBookingHorizonDays,
		PendingExpirationHours:        params.PendingExpirationHours,
	}, rules)
}

func (r *BookingsRepository) buildAvailabilitySlots(params *MerchantBookingParams, requestedDate string, timeRanges []TimeRange, occupation map[string]int, rules []bookingcore.DurationRule) []BookingSlot {
	ranges := make([]bookingcore.SlotRange, 0, len(timeRanges))
	for _, tr := range timeRanges {
		ranges = append(ranges, bookingcore.SlotRange{
			ID:               tr.ID,
			HourFrom:         tr.HourFrom,
			HourTo:           tr.HourTo,
			BookingCapacity:  tr.BookingCapacity,
			FirstBookingTime: tr.FirstBookingTime,
			LastBookingTime:  tr.LastBookingTime,
		})
	}

	computed := bookingcore.ComputeSlots(
		bookingcore.SlotParams{
			RequestedDate: requestedDate,
			PartySize:     params.ReserveMinimumPartySize,
			BookingSettings: bookingcore.BookingSettings{
				DefaultBookingDuration:        params.DefaultBookingDuration,
				AutoAcceptReserveBookings:     params.AutoAcceptReserveBookings,
				ReserveMaximumPartySize:       params.ReserveMaximumPartySize,
				ReserveMinimumPartySize:       params.ReserveMinimumPartySize,
				FirstBookingOffsetMinutes:     params.FirstBookingOffsetMinutes,
				LastBookingOffsetMinutes:      params.LastBookingOffsetMinutes,
				CancelBookingLimitOffsetHours: params.CancelBookingLimitOffsetHours,
				SlotIntervalMinutes:           params.SlotIntervalMinutes,
				CancelableByCustomer:          params.CancelableByCustomer,
				Enabled:                       params.Enabled,
				OverbookingPercent:            params.OverbookingPercent,
				MaxBookingHorizonDays:         params.MaxBookingHorizonDays,
				PendingExpirationHours:        params.PendingExpirationHours,
			},
			DurationRules: rules,
		},
		ranges,
		occupation,
		time.Now().In(time.FixedZone(params.Timezone, 0)),
	)

	slots := make([]BookingSlot, 0, len(computed))
	for _, slot := range computed {
		slots = append(slots, BookingSlot{
			HourOfOperationID:      slot.HourOfOperationID,
			DateFrom:               slot.DateFrom,
			DateTo:                 slot.DateTo,
			Available:              slot.Available,
			Capacity:               slot.Capacity,
			RemainingCapacity:      slot.RemainingCapacity,
			DebugCapacity:          slot.DebugCapacity,
			DebugMaxBookedInWindow: slot.DebugMaxBookedInWindow,
			DebugRemainingCapacity: slot.DebugRemainingCapacity,
		})
	}

	return slots
}

func (r *BookingsRepository) loadMerchantLocations(ctx context.Context, merchantID string) ([]Location, error) {
	db := dbutils.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx, `
        SELECT location_id, location_name, location_desc
        FROM locations
        WHERE merchant_id = ?
    `, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []Location{}

	for rows.Next() {
		var loc Location
		if err := rows.Scan(&loc.LocationID, &loc.LocationName, &loc.LocationDesc); err != nil {
			return nil, err
		}
		list = append(list, loc)
	}

	return list, nil
}
