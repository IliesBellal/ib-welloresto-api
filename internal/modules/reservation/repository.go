package reservation

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/bookingcore"
	"welloresto-api/internal/modules/customers"
	"welloresto-api/internal/utils"
	"welloresto-api/internal/utils/dbutils"
)

// ReservationRepository définit le contrat pour l'accès aux données
type ReservationRepository interface {
	GetMerchantByQR(ctx context.Context, qr string) (*Merchant, error)
	GetOperationHoursByQR(ctx context.Context, qr string) ([]OperationHour, error)
	GetOperationRanges(ctx context.Context, merchantID string, dayOfWeek int, requestedDate string) ([]OperationRange, error)
	GetBookedCapacity(ctx context.Context, merchantID string, requestedDate string) ([]bookingcore.IntervalBooking, error)
	GetBookedCapacityExcludingBooking(ctx context.Context, merchantID string, requestedDate string, excludeBookingNumber string) ([]bookingcore.IntervalBooking, error)
	GetBookingDurationRules(ctx context.Context, merchantID string) ([]bookingcore.DurationRule, error)
	FindExistingActiveBookingWarning(ctx context.Context, merchantID, phone, startDate string) (bool, error)
	GetCustomerByPhone(ctx context.Context, phone string, merchantID string) (*CustomerData, error)
	GetRewards(ctx context.Context, customerID string) ([]Reward, error)
	CreateBooking(ctx context.Context, b *BookingRequest) (int64, error)
	GetBookingByNumber(ctx context.Context, bookingNumber string, merchantID string) (*BookingData, error)
	UpdateBooking(ctx context.Context, b *BookingData) error
	CancelBookingDB(ctx context.Context, bookingNumber string) error
	CancelBookingPublic(ctx context.Context, merchantID, bookingNumber string) error
	CreateBookingTransaction(ctx context.Context, req *BookingRequest) (string, error)
	GetBookingCustomerContact(ctx context.Context, bookingNumber, merchantID string) (name, email, phone string, err error)
}

type reservationRepository struct {
	database        *sql.DB
	customerUpdater *customers.CustomersRepository
}

// NewReservationRepository instancie le repository
func NewReservationRepository(db *sql.DB) ReservationRepository {
	return &reservationRepository{
		database:        db,
		customerUpdater: customers.NewCustomerRepository(db),
	}
}

func (r *reservationRepository) GetMerchantByQR(ctx context.Context, qr string) (*Merchant, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	query := `
		SELECT
			m.id, m.timezone, m.merchantTel, m.street_number, m.street, m.zip_code, m.city, m.handicap_access, m.logo_url, m.fullName as business_name,
			COALESCE(bs.default_booking_duration, 90), COALESCE(bs.slot_interval_minutes, 15), COALESCE(bs.auto_accept_reserve_bookings, 0),
			COALESCE(bs.reserve_maximum_party_size, 8), COALESCE(bs.reserve_minimum_party_size, 1),
			COALESCE(bs.last_booking_offset_minutes, 60), COALESCE(bs.overbooking_percent, 0), COALESCE(bs.max_booking_horizon_days, 90),
			COALESCE(bs.pending_expiration_hours, 24),
			COALESCE(bs.cancelable_by_customer, 1), COALESCE(bs.cancel_booking_limit_offset_hours, 48), COALESCE(bs.first_booking_offset_minutes, 0),
			COALESCE(bs.sms_enabled, 0),
			mp.primary_color, mp.text_color_on_primary_color
		FROM bookings_settings bs
		INNER JOIN merchant m ON bs.merchant_id = m.id
		INNER JOIN merchant_parameters mp ON mp.merchant_id = m.id
		WHERE bs.code = ?`

	var m Merchant
	var handicapAccess, autoAccept, cancelable string
	var smsEnabled int

	err := db.QueryRowContext(ctx, query, qr).Scan(
		&m.MerchantID, &m.Timezone, &m.Phone, &m.Address.StreetNumber, &m.Address.Street, &m.Address.ZipCode, &m.Address.City,
		&handicapAccess, &m.LogoURL, &m.BusinessName,
		&m.DefaultBookingDuration, &m.SlotIntervalMinutes, &autoAccept, &m.ReserveMaximumPartySize, &m.ReserveMinimumPartySize, &m.LastBookingOffsetMinutes, &m.OverbookingPercent, &m.MaxBookingHorizonDays,
		&m.PendingExpirationHours,
		&cancelable, &m.CancelBookingLimitOffsetHours, &m.FirstBookingOffsetMinutes,
		&smsEnabled,
		&m.Design.PrimaryColor, &m.Design.TextColorOnPrimaryColor,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Retourne nil si non trouvé
		}
		log.Error(err.Error())
		return nil, err
	}

	m.HandicapAccess = handicapAccess == "1"
	m.AutoAcceptReserveBookings = autoAccept == "1"
	m.CancelableByCustomer = cancelable == "1"
	m.SMSEnabled = smsEnabled == 1

	return &m, nil
}

// GetBookingCustomerContact retourne les coordonnées client (nom, email,
// téléphone) associées à une réservation, indépendamment de ce que le corps
// de la requête publique contient — utilisé pour notifier modification et
// annulation même quand le client n'a pas renvoyé ses coordonnées.
func (r *reservationRepository) GetBookingCustomerContact(ctx context.Context, bookingNumber, merchantID string) (name, email, phone string, err error) {
	db := dbutils.GetDB(ctx, r.database)

	var n, e, p sql.NullString
	err = db.QueryRowContext(ctx, `
		SELECT c.customer_name, c.customer_email, c.customer_tel
		FROM bookings b
		INNER JOIN customer c ON c.customer_id = b.customer_id
		WHERE b.booking_number = ? AND b.merchant_id = ?
		LIMIT 1
	`, bookingNumber, merchantID).Scan(&n, &e, &p)
	if err != nil {
		return "", "", "", err
	}
	return n.String, e.String, p.String, nil
}

func (r *reservationRepository) GetOperationHoursByQR(ctx context.Context, qr string) ([]OperationHour, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	query := `
		SELECT 
			hoo.day_of_week_from, 
			hoo.hour_from, 
			hoo.hour_to
		FROM hours_of_operation hoo
		INNER JOIN bookings_settings bs ON bs.merchant_id = hoo.merchant_id
		WHERE bs.code = ?
		  AND bs.enabled = 1
		  AND hoo.enabled = 1
		  AND (hoo.valid_from IS NULL OR hoo.valid_from <= UTC_TIMESTAMP)
		  AND (hoo.valid_to IS NULL OR hoo.valid_to >= UTC_TIMESTAMP)
		ORDER BY hoo.day_of_week_from, hoo.hour_from;`

	rows, err := db.QueryContext(ctx, query, qr)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	defer rows.Close()

	var hours []OperationHour
	for rows.Next() {
		var h OperationHour
		if err := rows.Scan(&h.DayOfWeek, &h.HourFrom, &h.HourTo); err != nil {
			log.Error(err.Error())
			return nil, err
		}
		hours = append(hours, h)
	}

	return hours, rows.Err()
}

func (r *reservationRepository) GetOperationRanges(ctx context.Context, merchantID string, dayOfWeek int, requestedDate string) ([]OperationRange, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	query := `
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
		  AND (valid_to IS NULL OR valid_to >= ?)`

	rows, err := db.QueryContext(ctx, query, merchantID, dayOfWeek, dayOfWeek, dayOfWeek, requestedDate, requestedDate)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	defer rows.Close()

	var ranges []OperationRange
	for rows.Next() {
		var o OperationRange
		err := rows.Scan(&o.ID, &o.HourFrom, &o.HourTo, &o.BookingCapacity, &o.FirstBookingTime, &o.LastBookingTime)
		if err != nil {
			log.Error(err.Error())
			return nil, err
		}
		ranges = append(ranges, o)
	}
	return ranges, nil
}

func (r *reservationRepository) GetBookedCapacity(ctx context.Context, merchantID string, requestedDate string) ([]bookingcore.IntervalBooking, error) {
	return r.getBookedCapacity(ctx, merchantID, requestedDate, "")
}

func (r *reservationRepository) GetBookedCapacityExcludingBooking(ctx context.Context, merchantID string, requestedDate string, excludeBookingNumber string) ([]bookingcore.IntervalBooking, error) {
	return r.getBookedCapacity(ctx, merchantID, requestedDate, excludeBookingNumber)
}

func (r *reservationRepository) getBookedCapacity(ctx context.Context, merchantID string, requestedDate string, excludeBookingNumber string) ([]bookingcore.IntervalBooking, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)
	dayStart := requestedDate + " 00:00:00"
	dayEndTime, _ := time.Parse("2006-01-02 15:04:05", dayStart)
	dayEnd := dayEndTime.Add(24 * time.Hour).Format("2006-01-02 15:04:05")

	query := `
		SELECT party_size, booking_date_from, booking_date_to, booking_duration, status
		FROM bookings
		WHERE booking_date_from < ?
		  AND COALESCE(booking_date_to, booking_date_from + INTERVAL COALESCE(booking_duration, 90) MINUTE) > ?
		  AND merchant_id = ?
		  AND (? = '' OR booking_number <> ?)
	`

	rows, err := db.QueryContext(ctx, query, dayEnd, dayStart, merchantID, excludeBookingNumber, excludeBookingNumber)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	defer rows.Close()

	bookings := []bookingcore.IntervalBooking{}
	for rows.Next() {
		var booking bookingcore.IntervalBooking
		var status string
		if err := rows.Scan(&booking.PartySize, &booking.StartDate, &booking.EndDate, &booking.DurationMinutes, &status); err != nil {
			log.Error(err.Error())
			return nil, err
		}
		if !bookingcore.IsActiveForConflict(status) {
			continue
		}
		bookings = append(bookings, booking)
	}
	return bookings, nil
}

func (r *reservationRepository) GetBookingDurationRules(ctx context.Context, merchantID string) ([]bookingcore.DurationRule, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	rows, err := db.QueryContext(ctx, `
		SELECT min_party_size, max_party_size, duration_minutes, enabled
		FROM booking_duration_rules
		WHERE merchant_id = ?
		ORDER BY min_party_size, max_party_size
	`, merchantID)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	defer rows.Close()

	rules := []bookingcore.DurationRule{}
	for rows.Next() {
		var rule bookingcore.DurationRule
		if err := rows.Scan(&rule.MinPartySize, &rule.MaxPartySize, &rule.DurationMinutes, &rule.Enabled); err != nil {
			log.Error(err.Error())
			return nil, err
		}
		rules = append(rules, rule)
	}

	return rules, rows.Err()
}

func (r *reservationRepository) FindExistingActiveBookingWarning(ctx context.Context, merchantID, phone, startDate string) (bool, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	row := db.QueryRowContext(ctx, `
		SELECT b.status
		FROM bookings b
		INNER JOIN customer c ON c.customer_id = b.customer_id
		WHERE b.merchant_id = ?
		  AND c.customer_tel = ?
		  AND b.booking_date_from = ?
		ORDER BY b.booking_id DESC
		LIMIT 1
	`, merchantID, phone, startDate)

	var status string
	if err := row.Scan(&status); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		log.Error(err.Error())
		return false, err
	}

	normalized := bookingcore.NormalizeLegacyStatus(status)
	return normalized == bookingcore.StatusPending || normalized == bookingcore.StatusConfirmed, nil
}

func (r *reservationRepository) GetCustomerByPhone(ctx context.Context, phone string, merchantID string) (*CustomerData, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	query := `
		SELECT c.customer_id
		FROM customer c 
		INNER JOIN merchant_parameters mp on mp.merchant_id = c.merchant_id
		WHERE c.customer_tel = ? AND c.enabled = 1 AND c.merchant_id = ?`

	var c CustomerData
	err := db.QueryRowContext(ctx, query, phone, merchantID).Scan(&c.CustomerID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		log.Error(err.Error())
		return nil, err
	}
	return &c, nil
}

func (r *reservationRepository) GetRewards(ctx context.Context, customerID string) ([]Reward, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	query := `
		SELECT cr.reward_id, cr.loyalty_program_id, cr.creation_date, cr.reward_type, cr.reward_value
		FROM customer_rewards cr
		WHERE cr.customer_id = ? AND cr.usage_date IS NULL`

	rows, err := db.QueryContext(ctx, query, customerID)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	defer rows.Close()

	var rewards []Reward
	for rows.Next() {
		var rw Reward
		rows.Scan(&rw.RewardID, &rw.LoyaltyProgramID, &rw.CreationDate, &rw.RewardType, &rw.RewardValue)
		rewards = append(rewards, rw)
	}
	return rewards, nil
}

// Simulation de l'insertion (à adapter selon ta table 'bookings')
func (r *reservationRepository) CreateBooking(ctx context.Context, b *BookingRequest) (int64, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	query := `INSERT INTO bookings (merchant_id, customer_id, booking_date_from, booking_date_to, party_size, status, created_by) 
	          VALUES (?, ?, ?, ?, ?, ?, ?)`
	res, err := db.ExecContext(ctx, query,
		b.MerchantID, b.Customer.CustomerID, b.Booking.StartDate, b.Booking.EndDate,
		b.Booking.PartySize, b.Booking.Status, b.CreatedBy)
	if err != nil {
		log.Error(err.Error())
		return 0, err
	}
	return res.LastInsertId()
}

func (r *reservationRepository) GetBookingByNumber(ctx context.Context, bookingNumber string, merchantID string) (*BookingData, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	query := `
		SELECT booking_id, booking_number, merchant_id, booking_date_from, booking_date_to, booking_duration, party_size, comment, status, sequence_number
		FROM bookings
		WHERE booking_number = ? AND merchant_id = ?`

	var b BookingData
	var err error

	row := db.QueryRowContext(ctx, query, bookingNumber, merchantID)
	err = row.Scan(&b.BookingID, &b.BookingNumber, &b.MerchantID, &b.StartDate, &b.EndDate, &b.DurationMinutes, &b.PartySize, &b.Comment, &b.Status, &b.SequenceNumber)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		log.Error(err.Error())
		return nil, err
	}
	return &b, nil
}

func (r *reservationRepository) UpdateBooking(ctx context.Context, b *BookingData) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `
		UPDATE bookings 
		SET booking_date_from = ?, booking_date_to = ?, status = ?, sequence_number = sequence_number + 1
		WHERE booking_id = ?`

	// On utilise db.ExecContext directement, pas besoin de transaction pour une requête unique
	_, err := db.ExecContext(ctx, query, b.StartDate, b.EndDate, b.Status, b.BookingID)
	return err
}

func (r *reservationRepository) CancelBookingDB(ctx context.Context, bookingNumber string) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `
		UPDATE bookings
		SET status = 'CANCELED', deletion_date = UTC_TIMESTAMP(), deletion_reason_id = '9'
		WHERE booking_number = ?`
	_, err := db.ExecContext(ctx, query, bookingNumber)
	return err
}

// CreateBookingTransaction gère la récupération du client et l'insertion de la réservation dans une transaction isolée.
func (r *reservationRepository) CreateBookingTransaction(ctx context.Context, req *BookingRequest) (string, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	customer := &models.Customer{
		MerchantID:    req.MerchantID,
		CustomerName:  stringPtr(req.Customer.CustomerName),
		CustomerTel:   stringPtr(req.Customer.CustomerTel),
		CustomerEmail: stringPtr(req.Customer.CustomerEmail),
	}

	customerID, err := r.customerUpdater.UpdateOrCreateCustomer(ctx, customer)
	if err != nil {
		log.Error(err.Error())
		return "", err
	}
	if customerID == nil || *customerID == "" {
		return "", fmt.Errorf("customer_upsert_failed")
	}

	bookingNumber, err := r.generateUniqueBookingNumber(ctx, req.MerchantID)
	if err != nil {
		return "", err
	}

	start, err := time.Parse("2006-01-02 15:04:05", req.Booking.StartDate)
	if err != nil {
		return "", err
	}
	end, err := time.Parse("2006-01-02 15:04:05", req.Booking.EndDate)
	if err != nil {
		return "", err
	}
	duration := int(end.Sub(start).Minutes())
	if duration < 0 {
		return "", fmt.Errorf("start date is after end date")
	}

	queryInsert := `
		INSERT INTO bookings (
			booking_number, status, source, merchant_id, party_size,
			customer_id, comment, creation_date, booking_date_from,
			booking_date_to, booking_duration, created_by
		)
		VALUES (?, ?, 'web', ?, ?, ?, ?, UTC_TIMESTAMP, ?, ?, ?, ?)`

	res, err := db.ExecContext(ctx, queryInsert,
		bookingNumber,
		bookingcore.NormalizeLegacyStatus(req.Booking.Status),
		req.MerchantID,
		req.Booking.PartySize,
		*customerID,
		req.Booking.Comment,
		req.Booking.StartDate,
		req.Booking.EndDate,
		duration,
		req.CreatedBy,
	)
	if err != nil {
		return "", err
	}

	bookingID, err := res.LastInsertId()
	if err != nil {
		return "", err
	}

	req.Booking.BookingNumber = bookingNumber
	return strconv.FormatInt(bookingID, 10), nil
}

func (r *reservationRepository) CancelBookingPublic(ctx context.Context, merchantID, bookingNumber string) error {
	db := dbutils.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `
		UPDATE bookings
		SET status = ?, cancelled_by = ?, deletion_reason_id = NULL
		WHERE booking_number = ? AND merchant_id = ?`,
		bookingcore.StatusCancelled, bookingcore.ResolveCancellationActor("customer", ""), bookingNumber, merchantID,
	)
	return err
}

func (r *reservationRepository) generateUniqueBookingNumber(ctx context.Context, merchantID string) (string, error) {
	db := dbutils.GetDB(ctx, r.database)

	for {
		bookingNumber := utils.GenerateRandomString(6)

		var exists string
		err := db.QueryRowContext(ctx,
			`SELECT booking_id FROM bookings WHERE merchant_id = ? AND booking_number = ?`,
			merchantID,
			bookingNumber,
		).Scan(&exists)

		if err == sql.ErrNoRows {
			return bookingNumber, nil
		}
		if err != nil {
			return "", err
		}
	}
}

func stringPtr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
