package bookings

import (
	"context"
	"database/sql"
	"fmt"
	"time"
	"welloresto-api/internal/models"
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

func (r *BookingsRepository) SetBookingState(ctx context.Context, bookingID string, state string) error {
	db := dbutils.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `
        UPDATE bookings
        SET status = ?
        WHERE booking_id = ?
    `, state, bookingID)

	if err != nil {
		return err
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

	// -------------------------------------------------------
	// 4) Compute occupation for each slot
	// -------------------------------------------------------
	occupation := r.computeOccupation(bookings, params.SlotIntervalMinutes)

	// -------------------------------------------------------
	// 5) Generate availability slots
	// -------------------------------------------------------
	slots := r.buildAvailabilitySlots(
		params,
		requestedDate,
		timeRanges,
		occupation,
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
        SELECT m.id, m.timezone, bs.default_booking_duration, bs.slot_interval_minutes,
               bs.auto_accept_reserve_bookings, bs.reserve_maximum_party_size,
               bs.last_booking_offset_minutes, bs.cancelable_by_customer,
               bs.cancel_booking_limit_offset_hours, m.logo_url, m.fullName
        FROM bookings_settings bs
        INNER JOIN merchant m ON bs.merchant_id = m.id
        WHERE m.id = ?
    `, merchantID)

	params := MerchantBookingParams{}
	err := row.Scan(
		&params.MerchantID,
		&params.Timezone,
		&params.DefaultBookingDuration,
		&params.SlotIntervalMinutes,
		&params.AutoAccept,
		&params.ReserveMaximumPartySize,
		&params.LastBookingOffsetMinutes,
		&params.CancelableByCustomer,
		&params.CancelBookingLimitOffsetHr,
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
	// 0 = dimanche, 1 = lundi, ..., 6 = samedi (0-6 standard)

	rows, err := db.QueryContext(ctx, `
        SELECT id, hour_from, hour_to, booking_capacity
        FROM hours_of_operation
        WHERE merchant_id = ?
          AND enabled = 1
          AND day_of_week_from = ?
          AND (valid_from IS NULL OR valid_from <= ?)
          AND (valid_to IS NULL OR valid_to >= ?)
    `,
		merchantID, dayOfWeek, requestedDate, requestedDate,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := []TimeRange{}
	for rows.Next() {
		var tr TimeRange
		if err := rows.Scan(&tr.ID, &tr.HourFrom, &tr.HourTo, &tr.BookingCapacity); err != nil {
			return nil, 0, err
		}
		list = append(list, tr)
	}

	return list, dayOfWeek, nil
}

func (r *BookingsRepository) loadExistingBookings(ctx context.Context, merchantID, requestedDate string) ([]ExistingBooking, error) {
	db := dbutils.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx, `
        SELECT party_size, booking_date_from, booking_date_to
        FROM bookings
        WHERE CAST(booking_date_from AS DATE) = ?
          AND status IN ('ACCEPTED','ORDER_OPEN')
          AND merchant_id = ?
    `, requestedDate, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []ExistingBooking{}

	for rows.Next() {
		var b ExistingBooking
		if err := rows.Scan(&b.PartySize, &b.StartDate, &b.EndDate); err != nil {
			return nil, err
		}
		list = append(list, b)
	}

	return list, nil
}

func (r *BookingsRepository) computeOccupation(bookings []ExistingBooking, interval int) map[string]int {

	occ := make(map[string]int)

	for _, b := range bookings {

		start, _ := time.Parse("2006-01-02 15:04:05", b.StartDate)
		end := start

		if b.EndDate != nil {
			end, _ = time.Parse("2006-01-02 15:04:05", *b.EndDate)
		} else {
			end = start.Add(90 * time.Minute) // fallback comme PHP
		}

		cur := start
		for cur.Before(end) {
			key := cur.Format("15:04:05")
			occ[key] += b.PartySize
			cur = cur.Add(time.Duration(interval) * time.Minute)
		}
	}

	return occ
}

func (r *BookingsRepository) buildAvailabilitySlots(params *MerchantBookingParams, requestedDate string, timeRanges []TimeRange, occupation map[string]int) []BookingSlot {

	slots := []BookingSlot{}

	now := time.Now().In(time.FixedZone("UTC", 0))
	nowStr := now.Format("2006-01-02")
	nowTime := now.Format("15:04:05")

	for _, tr := range timeRanges {

		start, _ := time.Parse("2006-01-02 15:04:05", requestedDate+" "+tr.HourFrom)
		endOfService, _ := time.Parse("2006-01-02 15:04:05", requestedDate+" "+tr.HourTo)
		last := endOfService.Add(-time.Duration(params.LastBookingOffsetMinutes) * time.Minute)

		for !start.After(last) {

			available := true
			maxOcc := 0

			// Règle : créneau déjà passé
			if requestedDate == nowStr && start.Format("15:04:05") < nowTime {
				available = false
			}

			// Calcul fenêtre nouvelle réservation
			newStart := start
			newEnd := start.Add(time.Duration(params.DefaultBookingDuration) * time.Minute)

			if newEnd.After(endOfService) {
				available = false
			}

			cur := newStart
			for cur.Before(newEnd) {
				occ := occupation[cur.Format("15:04:05")]
				if occ > maxOcc {
					maxOcc = occ
				}
				cur = cur.Add(time.Duration(params.SlotIntervalMinutes) * time.Minute)
			}

			remaining := tr.BookingCapacity - maxOcc

			slot := BookingSlot{
				HourOfOperationID:      tr.ID,
				DateFrom:               start.Format("2006-01-02 15:04:05"),
				DateTo:                 start.Add(time.Duration(params.SlotIntervalMinutes) * time.Minute).Format("2006-01-02 15:04:05"),
				Available:              available,
				Capacity:               tr.BookingCapacity,
				RemainingCapacity:      remaining,
				DebugCapacity:          tr.BookingCapacity,
				DebugMaxBookedInWindow: maxOcc,
				DebugRemainingCapacity: remaining,
			}

			slots = append(slots, slot)
			start = start.Add(time.Duration(params.SlotIntervalMinutes) * time.Minute)
		}
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
