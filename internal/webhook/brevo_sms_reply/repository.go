package brevo_sms_reply

import (
	"context"
	"database/sql"
	"fmt"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/modules/bookingcore"
)

// ActiveBooking est la réservation active retrouvée par téléphone.
type ActiveBooking struct {
	BookingID  string
	MerchantID string
	Status     string
}

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// FindActiveBookingByPhone retourne la réservation pending|confirmed la plus
// pertinente pour ce numéro : le prochain créneau à venir en priorité, sinon la
// plus récente. Renvoie (nil, nil) si aucune. Recherche cross-merchant car le
// SMS entrant n'est pas scopé (expéditeur Brevo partagé).
func (r *Repository) FindActiveBookingByPhone(ctx context.Context, phone string) (*ActiveBooking, error) {
	db := dbx.GetDB(ctx, r.db)
	row := db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT b.booking_id, b.merchant_id, b.status
		FROM bookings b
		INNER JOIN customer c ON c.customer_id = b.customer_id
		WHERE c.customer_tel = ?
		  AND b.status IN ('pending', 'confirmed', 'PENDING_APPROVAL', 'ACCEPTED')
		ORDER BY
			CASE WHEN b.booking_date_from >= %[1]s THEN 0 ELSE 1 END,
			CASE WHEN b.booking_date_from >= %[1]s THEN b.booking_date_from END ASC,
			b.booking_date_from DESC
		LIMIT 1
	`, dbx.UTCNow()), phone)

	var b ActiveBooking
	if err := row.Scan(&b.BookingID, &b.MerchantID, &b.Status); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	b.Status = bookingcore.NormalizeLegacyStatus(b.Status)
	return &b, nil
}

// Reconfirm repositionne la réservation en confirmed (reconfirmation client).
func (r *Repository) Reconfirm(ctx context.Context, merchantID, bookingID string) error {
	db := dbx.GetDB(ctx, r.db)
	_, err := db.ExecContext(ctx, `
		UPDATE bookings SET status = ?
		WHERE booking_id = ? AND merchant_id = ?
	`, bookingcore.StatusConfirmed, bookingID, merchantID)
	return err
}

// CancelByCustomer annule la réservation avec cancelled_by = CUSTOMER.
func (r *Repository) CancelByCustomer(ctx context.Context, merchantID, bookingID string) error {
	db := dbx.GetDB(ctx, r.db)
	_, err := db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE bookings
		SET status = ?, cancelled_by = ?, deletion_date = %s
		WHERE booking_id = ? AND merchant_id = ?
	`, dbx.UTCNow()), bookingcore.StatusCancelled, bookingcore.ResolveCancellationActor("customer", ""), bookingID, merchantID)
	return err
}
