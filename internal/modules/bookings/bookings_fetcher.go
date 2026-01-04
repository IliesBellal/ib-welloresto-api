package bookings

import (
	"context"
	"database/sql"
	"welloresto-api/internal/helpers"

	"go.uber.org/zap"
)

type BookingFetcher struct {
	db  *sql.DB
	log *zap.Logger
}

func NewBookingFetcher(db *sql.DB, log *zap.Logger) *BookingFetcher {
	return &BookingFetcher{db: db, log: log}
}

func (f *BookingFetcher) FetchAndBuildBookings(
	ctx context.Context,
	req *BookingObjectRequest,
) ([]Booking, error) {

	f.log.Info("FetchAndBuildBookings START")

	tx, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	//-----------------------------------------------------
	// MAIN BOOKING QUERY
	//-----------------------------------------------------
	rows, err := tx.QueryContext(ctx, `
        SELECT
            b.booking_id, b.order_id, b.booking_number, b.status, b.party_size,
            c.customer_id, b.sequence_number,
            c.customer_name, c.customer_tel, c.customer_email, b.comment,
            b.booking_date_from, b.booking_date_to, b.creation_date,
            c.customer_nb_orders, c.customer_nb_bookings, bs.code,
            m.fullName AS business_name, m.address, m.timezone,
            bs.default_booking_duration, m.logo_url,
            CASE
                WHEN u.user_id IS NOT NULL THEN u.name
                ELSE b.created_by
            END AS created_by
        FROM bookings b
        INNER JOIN merchant m ON b.merchant_id = m.id
        INNER JOIN bookings_settings bs ON bs.merchant_id = b.merchant_id
        INNER JOIN customer c ON c.customer_id = b.customer_id
        LEFT JOIN users u ON u.user_id = b.created_by
        WHERE b.merchant_id = ?
          AND (? IS NULL OR b.booking_id = ?)
          AND (? IS NULL OR b.booking_number = ?)
          AND (
                (? IS NULL OR ? IS NULL)
                OR b.booking_date_from BETWEEN ? AND ?
              )
        ORDER BY b.booking_date_from
    `,
		req.MerchantID,
		req.BookingID, req.BookingID,
		req.BookingNumber, req.BookingNumber,
		req.BookingDateFrom, req.BookingDateTo,
		req.BookingDateFrom, req.BookingDateTo,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type RawBooking struct {
		Booking Booking
		Code    string
	}

	var rawBookings []RawBooking

	for rows.Next() {
		var r RawBooking
		var date_from, date_to, creation_date sql.NullTime

		err := rows.Scan(
			&r.Booking.BookingID,
			new(interface{}), // order_id unused
			&r.Booking.BookingNumber,
			&r.Booking.Status,
			&r.Booking.PartySize,
			&r.Booking.Customer.CustomerID,
			&r.Booking.SequenceNumber,
			&r.Booking.Customer.CustomerName,
			&r.Booking.Customer.CustomerTel,
			&r.Booking.Customer.CustomerEmail,
			&r.Booking.Comment,

			&date_from,
			&date_to,
			&creation_date,

			&r.Booking.Customer.CustomerNbOrders,
			&r.Booking.Customer.CustomerNbBookings,
			&r.Code,
			&r.Booking.Merchant.BusinessName,
			&r.Booking.Merchant.Address.Address,
			&r.Booking.Merchant.Timezone,
			&r.Booking.Merchant.DefaultBookingDuration,
			&r.Booking.Merchant.LogoURL,
			&r.Booking.CreatedBy,
		)

		r.Booking.BookingDateFrom = helpers.NullTimePtr(date_from).UTC().Unix()
		r.Booking.BookingDateTo = helpers.NullTimePtr(date_to).UTC().Unix()
		r.Booking.CreationDate = helpers.NullTimePtr(creation_date).UTC().Unix()
		if err != nil {
			return nil, err
		}

		// build access link (like PHP)
		r.Booking.AccessLink = "https://reserve.welloresto.fr/restaurant/" + r.Code + "/" + r.Booking.BookingNumber

		rawBookings = append(rawBookings, r)
	}

	//-----------------------------------------------------
	// LOCATIONS QUERY
	//-----------------------------------------------------
	locRows, err := tx.QueryContext(ctx, `
        SELECT
            b.booking_id,
            l.location_id,
            l.location_name,
            l.location_desc
        FROM bookings b
        INNER JOIN booked_location bl ON bl.booking_id = b.booking_id
        INNER JOIN locations l ON l.location_id = bl.location_id
        WHERE b.merchant_id = ?
          AND (? IS NULL OR b.booking_id = ?)
          AND (? IS NULL OR b.booking_number = ?)
          AND (
                (? IS NULL OR ? IS NULL)
                OR b.booking_date_from BETWEEN ? AND ?
              )
    `,
		req.MerchantID,
		req.BookingID, req.BookingID,
		req.BookingNumber, req.BookingNumber,
		req.BookingDateFrom, req.BookingDateTo,
		req.BookingDateFrom, req.BookingDateTo,
	)
	if err != nil {
		return nil, err
	}
	defer locRows.Close()

	// group by booking_id
	locationsByBooking := make(map[string][]BookingLocation)

	for locRows.Next() {
		var l BookingLocation

		err := locRows.Scan(
			&l.BookingID,
			&l.LocationID,
			&l.LocationName,
			&l.LocationDesc,
		)
		if err != nil {
			return nil, err
		}

		locationsByBooking[l.BookingID] = append(locationsByBooking[l.BookingID], l)
	}

	//-----------------------------------------------------
	// FINAL BUILD
	//-----------------------------------------------------

	bookings := make([]Booking, 0, len(rawBookings))
	for _, r := range rawBookings {
		r.Booking.Locations = locationsByBooking[r.Booking.BookingID]
		bookings = append(bookings, r.Booking)
	}

	tx.Commit()

	f.log.Info("FetchAndBuildBookings END",
		zap.Int("count", len(bookings)),
	)

	return bookings, nil
}
