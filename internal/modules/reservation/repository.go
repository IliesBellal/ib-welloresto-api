package reservation

import (
	"context"
	"database/sql"
	"strconv"
)

// ReservationRepository définit le contrat pour l'accès aux données
type ReservationRepository interface {
	GetMerchantByQR(ctx context.Context, qr string) (*Merchant, error)
	GetOperationHoursByQR(ctx context.Context, qr string) ([]OperationHour, error)
	GetOperationRanges(ctx context.Context, merchantID string, dayOfWeek int, requestedDate string) ([]OperationRange, error)
	GetBookedCapacity(ctx context.Context, merchantID string, requestedDate string) (map[string]int, error)
	GetCustomerByPhone(ctx context.Context, tx *sql.Tx, phone string, merchantID string) (*CustomerData, error)
	GetRewards(ctx context.Context, tx *sql.Tx, customerID string) ([]Reward, error)
	CreateBooking(ctx context.Context, tx *sql.Tx, b *BookingRequest) (int64, error)
	GetBookingByNumber(ctx context.Context, bookingNumber string, merchantID string) (*BookingData, error)
	UpdateBooking(ctx context.Context, b *BookingData) error
	CancelBookingDB(ctx context.Context, tx *sql.Tx, bookingNumber string) error
	CreateBookingTransaction(ctx context.Context, req *BookingRequest) (string, error)
}

type reservationRepository struct {
	db *sql.DB
}

// NewReservationRepository instancie le repository
func NewReservationRepository(db *sql.DB) ReservationRepository {
	return &reservationRepository{db: db}
}

func (r *reservationRepository) GetMerchantByQR(ctx context.Context, qr string) (*Merchant, error) {
	query := `
		SELECT 
			m.id, m.timezone, m.merchantTel, m.street_number, m.street, m.zip_code, m.city, m.handicap_access, m.logo_url, m.fullName as business_name, 
			bs.default_booking_duration, bs.slot_interval_minutes, bs.auto_accept_reserve_bookings, bs.reserve_maximum_party_size, bs.last_booking_offset_minutes,
			bs.cancelable_by_customer, bs.cancel_booking_limit_offset_hours, bs.first_booking_offset_minutes,
			mp.primary_color, mp.text_color_on_primary_color 
		FROM bookings_settings bs
		INNER JOIN merchant m ON bs.merchant_id = m.id
		INNER JOIN merchant_parameters mp ON mp.merchant_id = m.id
		WHERE bs.code = ?`

	var m Merchant
	var handicapAccess, autoAccept, cancelable string

	err := r.db.QueryRowContext(ctx, query, qr).Scan(
		&m.MerchantID, &m.Timezone, &m.Phone, &m.Address.StreetNumber, &m.Address.Street, &m.Address.ZipCode, &m.Address.City,
		&handicapAccess, &m.LogoURL, &m.BusinessName,
		&m.DefaultBookingDuration, &m.SlotIntervalMinutes, &autoAccept, &m.ReserveMaximumPartySize, &m.LastBookingOffsetMinutes,
		&cancelable, &m.CancelBookingLimitOffsetHours, &m.FirstBookingOffsetMinutes,
		&m.Design.PrimaryColor, &m.Design.TextColorOnPrimaryColor,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Retourne nil si non trouvé
		}
		return nil, err
	}

	m.HandicapAccess = handicapAccess == "1"
	m.AutoAcceptReserveBookings = autoAccept == "1"
	m.CancelableByCustomer = cancelable == "1"

	return &m, nil
}

func (r *reservationRepository) GetOperationHoursByQR(ctx context.Context, qr string) ([]OperationHour, error) {
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

	rows, err := r.db.QueryContext(ctx, query, qr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hours []OperationHour
	for rows.Next() {
		var h OperationHour
		if err := rows.Scan(&h.DayOfWeek, &h.HourFrom, &h.HourTo); err != nil {
			return nil, err
		}
		hours = append(hours, h)
	}

	return hours, rows.Err()
}

func (r *reservationRepository) GetOperationRanges(ctx context.Context, merchantID string, dayOfWeek int, requestedDate string) ([]OperationRange, error) {
	query := `
		SELECT id, hour_from, hour_to, booking_capacity, first_booking_time, last_booking_time
		FROM hours_of_operation
		WHERE merchant_id = ?
		  AND enabled = 1
		  AND day_of_week_from = ?
		  AND (valid_from IS NULL OR valid_from <= ?)
		  AND (valid_to IS NULL OR valid_to >= ?)`

	rows, err := r.db.QueryContext(ctx, query, merchantID, dayOfWeek, requestedDate, requestedDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ranges []OperationRange
	for rows.Next() {
		var o OperationRange
		err := rows.Scan(&o.ID, &o.HourFrom, &o.HourTo, &o.BookingCapacity, &o.FirstBookingTime, &o.LastBookingTime)
		if err != nil {
			return nil, err
		}
		ranges = append(ranges, o)
	}
	return ranges, nil
}

func (r *reservationRepository) GetBookedCapacity(ctx context.Context, merchantID string, requestedDate string) (map[string]int, error) {
	query := `
		SELECT TIME(booking_date_from) AS slot_time, SUM(party_size) AS total_booked
		FROM bookings
		WHERE CAST(booking_date_from as DATE) = ?
		  AND status IN ('ACCEPTED','ORDER_OPEN')
		  AND merchant_id = ?
		GROUP BY TIME(booking_date_from)`

	rows, err := r.db.QueryContext(ctx, query, requestedDate, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	bookings := make(map[string]int)
	for rows.Next() {
		var slotTime string
		var total int
		if err := rows.Scan(&slotTime, &total); err != nil {
			return nil, err
		}
		bookings[slotTime] = total
	}
	return bookings, nil
}

func (r *reservationRepository) GetCustomerByPhone(ctx context.Context, tx *sql.Tx, phone string, merchantID string) (*CustomerData, error) {
	query := `
		SELECT c.customer_id
		FROM customer c 
		INNER JOIN merchant_parameters mp on mp.merchant_id = c.merchant_id
		WHERE c.customer_tel = ? AND c.enabled = 1 AND c.merchant_id = ?`

	var c CustomerData
	err := tx.QueryRowContext(ctx, query, phone, merchantID).Scan(&c.CustomerID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (r *reservationRepository) GetRewards(ctx context.Context, tx *sql.Tx, customerID string) ([]Reward, error) {
	query := `
		SELECT cr.reward_id, cr.loyalty_program_id, cr.creation_date, p.reward_type, p.reward_value
		FROM customer_rewards cr
		INNER JOIN customer_loyalty_programs p ON cr.loyalty_program_id = p.id
		WHERE cr.customer_id = ? AND cr.usage_date IS NULL`

	rows, err := tx.QueryContext(ctx, query, customerID)
	if err != nil {
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
func (r *reservationRepository) CreateBooking(ctx context.Context, tx *sql.Tx, b *BookingRequest) (int64, error) {
	query := `INSERT INTO bookings (merchant_id, customer_id, booking_date_from, booking_date_to, party_size, status, created_by) 
	          VALUES (?, ?, ?, ?, ?, ?, ?)`
	res, err := tx.ExecContext(ctx, query,
		b.MerchantID, b.Customer.CustomerID, b.Booking.StartDate, b.Booking.EndDate,
		b.Booking.PartySize, b.Booking.Status, b.CreatedBy)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (r *reservationRepository) GetBookingByNumber(ctx context.Context, bookingNumber string, merchantID string) (*BookingData, error) {
	query := `
		SELECT booking_id, booking_number, merchant_id, booking_date_from, booking_date_to, party_size, status, sequence_number
		FROM bookings
		WHERE booking_number = ? AND merchant_id = ?`

	var b BookingData
	var err error

	row := r.db.QueryRowContext(ctx, query, bookingNumber, merchantID)
	err = row.Scan(&b.BookingID, &b.BookingNumber, &b.MerchantID, &b.StartDate, &b.EndDate, &b.PartySize, &b.Status, &b.SequenceNumber)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &b, nil
}

func (r *reservationRepository) UpdateBooking(ctx context.Context, b *BookingData) error {
	query := `
		UPDATE bookings 
		SET booking_date_from = ?, booking_date_to = ?, status = ?, sequence_number = sequence_number + 1
		WHERE booking_id = ?`

	// On utilise db.ExecContext directement, pas besoin de transaction pour une requête unique
	_, err := r.db.ExecContext(ctx, query, b.StartDate, b.EndDate, b.Status, b.BookingID)
	return err
}

func (r *reservationRepository) CancelBookingDB(ctx context.Context, tx *sql.Tx, bookingNumber string) error {
	query := `
		UPDATE bookings
		SET status = 'CANCELED', deletion_date = UTC_TIMESTAMP(), deletion_reason_id = '9'
		WHERE booking_number = ?`
	_, err := tx.ExecContext(ctx, query, bookingNumber)
	return err
}

// CreateBookingTransaction gère la récupération du client et l'insertion de la réservation dans une transaction isolée.
func (r *reservationRepository) CreateBookingTransaction(ctx context.Context, req *BookingRequest) (string, error) {
	// 1. Début de transaction
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	// Sécurité absolue : Rollback automatique si on ne Commit pas à la fin.
	defer tx.Rollback()

	// 2. Recherche du client (en utilisant la transaction courante)
	queryCustomer := `
		SELECT customer_id 
		FROM customer 
		WHERE customer_tel = ? AND enabled = 1 AND merchant_id = ?`

	var customerID string
	err = tx.QueryRowContext(ctx, queryCustomer, req.Customer.CustomerTel, req.MerchantID).Scan(&customerID)

	if err != nil && err != sql.ErrNoRows {
		// Erreur SQL autre que "non trouvé"
		return "", err
	}

	if err == nil {
		// Client trouvé
		req.Customer.CustomerID = customerID
	}

	// 3. Insertion de la réservation
	queryInsert := `
		INSERT INTO bookings (merchant_id, customer_id, booking_date_from, booking_date_to, party_size, status, created_by) 
		VALUES (?, ?, ?, ?, ?, ?, ?)`

	res, err := tx.ExecContext(ctx, queryInsert,
		req.MerchantID,
		req.Customer.CustomerID,
		req.Booking.StartDate,
		req.Booking.EndDate,
		req.Booking.PartySize,
		req.Booking.Status,
		req.CreatedBy,
	)
	if err != nil {
		return "", err
	}

	bookingID, err := res.LastInsertId()
	if err != nil {
		return "", err
	}

	// 4. On valide la transaction
	if err := tx.Commit(); err != nil {
		return "", err
	}

	return strconv.FormatInt(bookingID, 10), nil
}
