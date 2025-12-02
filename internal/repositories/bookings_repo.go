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
	db      *sql.DB
	log     *zap.Logger
	builder *BookingFetcher
}

func NewBookingsRepository(db *sql.DB, log *zap.Logger) *BookingsRepository {
	return &BookingsRepository{
		db:      db,
		log:     log,
		builder: NewBookingFetcher(db, log),
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

func (r *BookingsRepository) CreateBooking(ctx context.Context, req *models.BookingObjectRequest, customerID string) (string, error) {

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
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
	start, err := time.Parse("2006-01-02 15:04:05", req.Booking.StartDate)
	if err != nil {
		return rollback(err)
	}

	end, err := time.Parse("2006-01-02 15:04:05", req.Booking.EndDate)
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
