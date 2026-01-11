package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"time"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils"

	"go.uber.org/zap"
)

type BookingsRepository struct {
	db              *sql.DB
	log             *zap.Logger
	builder         *BookingFetcher
	customerUpdater *CustomersRepository
}

func NewBookingsRepository(db *sql.DB, log *zap.Logger) *BookingsRepository {
	return &BookingsRepository{
		db:              db,
		log:             log,
		builder:         NewBookingFetcher(db, log),
		customerUpdater: NewCustomerRepository(db, log),
	}
}

func (r *BookingsRepository) GetBookings(ctx context.Context, req *models.BookingObjectRequest) ([]models.Booking, error) {
	return r.builder.FetchAndBuildBookings(ctx, req)
}

func (r *BookingsRepository) GetBookingByID(ctx context.Context, merchantID, bookingID string) (*models.Booking, error) {
	req := &models.BookingObjectRequest{
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

func (r *BookingsRepository) CreateBooking(ctx context.Context, req *models.BookingObjectRequest) (string, error) {

	// 1️⃣ Construire un modèle Customer
	customer := &models.Customer{
		CustomerID:    req.Customer.CustomerID,
		MerchantID:    req.MerchantID,
		CustomerName:  req.Customer.CustomerName,
		CustomerTel:   req.Customer.CustomerTel,
		CustomerEmail: req.Customer.CustomerEmail,
		// Tous les autres champs sont optionnels → nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}

	// 2️⃣ Update or Create
	customerID, err := r.customerUpdater.UpdateOrCreateCustomer(ctx, tx, customer)
	if err != nil {
		return "", fmt.Errorf("failed to update/create customer: %w", err)
	}

	// injecter l’MerchantID dans la requête
	req.Customer.CustomerID = customerID

	// 3️⃣ Check dates
	if req.Booking.StartDate == nil || *req.Booking.StartDate == "" || req.Booking.EndDate == nil || *req.Booking.EndDate == "" {
		return "", fmt.Errorf("start_date or end_date is empty")
	}

	rollback := func(err error) (string, error) {
		tx.Rollback()
		return "", err
	}

	//---------------------------------------------------------
	// 1. Generate unique booking number
	//---------------------------------------------------------
	var exists string
	var bookingNumber string

	for {
		bookingNumber = utils.GenerateRandomString(6)

		err = tx.QueryRowContext(ctx,
			`SELECT booking_id FROM bookings WHERE booking_number = ?`,
			bookingNumber,
		).Scan(&exists)

		if err == sql.ErrNoRows {
			break
		}
		if err != nil {
			return rollback(err)
		}
	}

	//---------------------------------------------------------
	// 2. Compute duration
	//---------------------------------------------------------
	start, err := time.Parse("2006-01-02 15:04:05", *req.Booking.StartDate)
	if err != nil {
		return rollback(err)
	}

	end, err := time.Parse("2006-01-02 15:04:05", *req.Booking.EndDate)
	if err != nil {
		return rollback(err)
	}

	diff := end.Sub(start)
	if diff < 0 {
		return rollback(fmt.Errorf("start date is after end date"))
	}

	duration := int(diff.Minutes())

	//---------------------------------------------------------
	// 3. Insert booking
	//---------------------------------------------------------
	res, err := tx.ExecContext(ctx, `
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
		return rollback(err)
	}

	bookingID, _ := res.LastInsertId()

	//---------------------------------------------------------
	// 4. Insert locations
	//---------------------------------------------------------
	for _, loc := range req.Booking.Locations {
		_, err := tx.ExecContext(ctx, `
            INSERT INTO booked_location(booking_id, location_id)
            VALUES (?, ?)
        `,
			bookingID,
			loc.LocationID,
		)
		if err != nil {
			return rollback(err)
		}
	}

	//---------------------------------------------------------
	// Commit
	//---------------------------------------------------------
	if err := tx.Commit(); err != nil {
		return rollback(err)
	}

	return fmt.Sprintf("%d", bookingID), nil
}

func (r *BookingsRepository) SetBookingState(ctx context.Context, bookingID string, state string) error {

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	rollback := func(err error) error {
		tx.Rollback()
		return err
	}

	_, err = tx.ExecContext(ctx, `
        UPDATE bookings
        SET status = ?
        WHERE booking_id = ?
    `, state, bookingID)

	if err != nil {
		return rollback(err)
	}

	if err := tx.Commit(); err != nil {
		return rollback(err)
	}

	return nil
}

func (r *BookingsRepository) GetBookingAvailability(ctx context.Context, merchantID, requestedDate string) (*models.BookingAvailabilityResponse, error) {

	r.log.Info("BookingAvailability START",
		zap.String("merchant_id", merchantID),
		zap.String("date", requestedDate),
	)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	rollback := func(err error) (*models.BookingAvailabilityResponse, error) {
		tx.Rollback()
		return nil, err
	}

	// -------------------------------------------------------
	// 1) Merchant + booking_settings
	// -------------------------------------------------------
	params, err := r.loadMerchantBookingParams(ctx, tx, merchantID)
	if err != nil {
		return rollback(err)
	}

	// -------------------------------------------------------
	// 2) Hours of operation
	// -------------------------------------------------------
	timeRanges, dayOfWeek, err := r.loadHoursOfOperation(ctx, tx, merchantID, requestedDate)
	if err != nil {
		return rollback(err)
	}

	// -------------------------------------------------------
	// 3) Existing bookings (start + end)
	// -------------------------------------------------------
	bookings, err := r.loadExistingBookings(ctx, tx, merchantID, requestedDate)
	if err != nil {
		return rollback(err)
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

	tx.Commit()

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
	resp := &models.BookingAvailabilityResponse{
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

func (r *BookingsRepository) loadMerchantBookingParams(ctx context.Context, tx *sql.Tx, merchantID string) (*models.MerchantBookingParams, error) {

	row := tx.QueryRowContext(ctx, `
        SELECT m.id, m.timezone, bs.default_booking_duration, bs.slot_interval_minutes,
               bs.auto_accept_reserve_bookings, bs.reserve_maximum_party_size,
               bs.last_booking_offset_minutes, bs.cancelable_by_customer,
               bs.cancel_booking_limit_offset_hours, m.logo_url, m.fullName
        FROM bookings_settings bs
        INNER JOIN merchant m ON bs.merchant_id = m.id
        WHERE m.id = ?
    `, merchantID)

	params := models.MerchantBookingParams{}
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

func (r *BookingsRepository) loadHoursOfOperation(ctx context.Context, tx *sql.Tx, merchantID, requestedDate string) ([]models.TimeRange, int, error) {

	dateObj, _ := time.Parse("2006-01-02", requestedDate)
	dayOfWeek := int(dateObj.Weekday())
	if dayOfWeek == 0 {
		dayOfWeek = 7 // dimanche = 7
	}

	rows, err := tx.QueryContext(ctx, `
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

	list := []models.TimeRange{}
	for rows.Next() {
		var tr models.TimeRange
		if err := rows.Scan(&tr.ID, &tr.HourFrom, &tr.HourTo, &tr.BookingCapacity); err != nil {
			return nil, 0, err
		}
		list = append(list, tr)
	}

	return list, dayOfWeek, nil
}

func (r *BookingsRepository) loadExistingBookings(ctx context.Context, tx *sql.Tx, merchantID, requestedDate string) ([]models.Booking, error) {

	rows, err := tx.QueryContext(ctx, `
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

	list := []models.Booking{}

	for rows.Next() {
		var b models.Booking
		if err := rows.Scan(&b.PartySize, &b.StartDate, &b.EndDate); err != nil {
			return nil, err
		}
		list = append(list, b)
	}

	return list, nil
}

func (r *BookingsRepository) computeOccupation(bookings []models.Booking, interval int) map[string]int {

	occ := make(map[string]int)

	for _, b := range bookings {

		start, _ := time.Parse("2006-01-02 15:04:05", *b.StartDate)
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

func (r *BookingsRepository) buildAvailabilitySlots(params *models.MerchantBookingParams, requestedDate string, timeRanges []models.TimeRange, occupation map[string]int) []models.BookingSlot {

	slots := []models.BookingSlot{}

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

			slot := models.BookingSlot{
				HourOfOperationID: tr.ID,
				DateFrom:          start.Format("2006-01-02 15:04:05"),
				DateTo:            start.Add(time.Duration(params.SlotIntervalMinutes) * time.Minute).Format("2006-01-02 15:04:05"),
				Available:         available,
				Capacity:          tr.BookingCapacity,
				RemainingCapacity: remaining,
			}

			slots = append(slots, slot)
			start = start.Add(time.Duration(params.SlotIntervalMinutes) * time.Minute)
		}
	}

	return slots
}

func (r *BookingsRepository) loadMerchantLocations(ctx context.Context, merchantID string) ([]models.Location, error) {

	rows, err := r.db.QueryContext(ctx, `
        SELECT location_id, location_name, location_desc
        FROM locations
        WHERE merchant_id = ?
    `, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []models.Location{}

	for rows.Next() {
		var loc models.Location
		if err := rows.Scan(&loc.LocationID, &loc.LocationName, &loc.LocationDesc); err != nil {
			return nil, err
		}
		list = append(list, loc)
	}

	return list, nil
}
