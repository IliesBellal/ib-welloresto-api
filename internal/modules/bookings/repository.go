package bookings

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
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

func (r *BookingsRepository) CreateBooking(ctx context.Context, req *BookingObjectRequest) (string, error) {
	db := dbutils.GetDB(ctx, r.database)

	// 1️⃣ Construire un modèle Customer
	customer := &models.Customer{
		CustomerID:    req.Customer.CustomerID,
		MerchantID:    req.MerchantID,
		CustomerName:  req.Customer.CustomerName,
		CustomerTel:   req.Customer.CustomerTel,
		CustomerEmail: req.Customer.CustomerEmail,
		// Tous les autres champs sont optionnels → nil
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
		bookingNumber = utils.GenerateRandomString(6)

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
	bookings, err := r.loadExistingBookings(ctx, merchantID, requestedDate)
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

func (r *BookingsRepository) loadExistingBookings(ctx context.Context, merchantID, requestedDate string) ([]ExistingBooking, error) {
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
		`, dayEnd, dayStart, merchantID)
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
