package bookings

import (
	"context"
	"database/sql"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/bookingcore"
	"welloresto-api/internal/utils/dbutils"
)

// FindOrCreateCustomerByPhone retrouve un client par téléphone (normalisé FR)
// ou le crée s'il n'existe pas — même logique que le flux public de réservation.
func (r *BookingsRepository) FindOrCreateCustomerByPhone(ctx context.Context, merchantID, name, phone string) (*string, error) {
	db := dbutils.GetDB(ctx, r.database)
	normalized := helpers.NormalizePhoneNumber(phone, "FR")

	var customerID string
	err := db.QueryRowContext(ctx, `
		SELECT customer_id FROM customer
		WHERE merchant_id = ? AND customer_tel = ? AND enabled = 1
		LIMIT 1
	`, merchantID, normalized).Scan(&customerID)
	if err == nil {
		return &customerID, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	customer := &models.Customer{
		MerchantID:   merchantID,
		CustomerName: strPtr(name),
		CustomerTel:  strPtr(phone),
	}
	return r.customerUpdater.UpdateOrCreateCustomer(ctx, customer)
}

func strPtr(v string) *string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return &v
}

// waitlistColumns liste les colonnes lues dans le même ordre que scanWaitlist.
const waitlistColumns = `
	id, merchant_id, customer_id, party_size, customer_name, customer_phone, notes,
	status,
	DATE_FORMAT(notified_at, '%Y-%m-%d %H:%i:%s') AS notified_at,
	DATE_FORMAT(expires_at, '%Y-%m-%d %H:%i:%s') AS expires_at,
	DATE_FORMAT(created_at, '%Y-%m-%d %H:%i:%s') AS created_at,
	DATE_FORMAT(updated_at, '%Y-%m-%d %H:%i:%s') AS updated_at`

func scanWaitlist(scanner interface {
	Scan(dest ...interface{}) error
}) (*WaitlistEntry, error) {
	var e WaitlistEntry
	var customerID, notes, notifiedAt, expiresAt sql.NullString
	if err := scanner.Scan(
		&e.ID, &e.MerchantID, &customerID, &e.PartySize, &e.CustomerName, &e.CustomerPhone, &notes,
		&e.Status, &notifiedAt, &expiresAt, &e.CreatedAt, &e.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if customerID.Valid {
		e.CustomerID = &customerID.String
	}
	if notes.Valid {
		e.Notes = &notes.String
	}
	if notifiedAt.Valid {
		e.NotifiedAt = &notifiedAt.String
	}
	if expiresAt.Valid {
		e.ExpiresAt = &expiresAt.String
	}
	return &e, nil
}

// CountActiveWaitlist retourne le nombre d'entrées waiting|notified d'un
// marchand (utilisé pour vérifier waitlist_max_size).
func (r *BookingsRepository) CountActiveWaitlist(ctx context.Context, merchantID string) (int, error) {
	db := dbutils.GetDB(ctx, r.database)
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM booking_waitlist
		WHERE merchant_id = ? AND status IN (?, ?)
	`, merchantID, bookingcore.WaitlistWaiting, bookingcore.WaitlistNotified).Scan(&count)
	return count, err
}

// InsertWaitlistEntry insère une entrée (id/customer_id déjà résolus par le service).
func (r *BookingsRepository) InsertWaitlistEntry(ctx context.Context, e *WaitlistEntry) error {
	db := dbutils.GetDB(ctx, r.database)

	var customerID interface{}
	if e.CustomerID != nil && strings.TrimSpace(*e.CustomerID) != "" {
		customerID = *e.CustomerID
	}
	var notes interface{}
	if e.Notes != nil && strings.TrimSpace(*e.Notes) != "" {
		notes = *e.Notes
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO booking_waitlist (
			id, merchant_id, customer_id, party_size, customer_name, customer_phone, notes, status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, e.ID, e.MerchantID, customerID, e.PartySize, e.CustomerName, e.CustomerPhone, notes, bookingcore.WaitlistWaiting)
	return err
}

// ListWaitlist retourne les entrées actives (waiting|notified) ordonnées par
// ancienneté (created_at ASC).
func (r *BookingsRepository) ListWaitlist(ctx context.Context, merchantID string) ([]WaitlistEntry, error) {
	db := dbutils.GetDB(ctx, r.database)
	rows, err := db.QueryContext(ctx, `
		SELECT `+waitlistColumns+`
		FROM booking_waitlist
		WHERE merchant_id = ? AND status IN (?, ?)
		ORDER BY created_at ASC
	`, merchantID, bookingcore.WaitlistWaiting, bookingcore.WaitlistNotified)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]WaitlistEntry, 0)
	for rows.Next() {
		entry, err := scanWaitlist(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *entry)
	}
	return list, rows.Err()
}

// GetWaitlistEntry charge une entrée par id, scoping merchant.
func (r *BookingsRepository) GetWaitlistEntry(ctx context.Context, merchantID, id string) (*WaitlistEntry, error) {
	db := dbutils.GetDB(ctx, r.database)
	row := db.QueryRowContext(ctx, `
		SELECT `+waitlistColumns+`
		FROM booking_waitlist
		WHERE merchant_id = ? AND id = ?
		LIMIT 1
	`, merchantID, id)
	entry, err := scanWaitlist(row)
	if err == sql.ErrNoRows {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return entry, nil
}

// SetWaitlistStatus met à jour le statut d'une entrée (scoping merchant).
func (r *BookingsRepository) SetWaitlistStatus(ctx context.Context, merchantID, id, status string) error {
	db := dbutils.GetDB(ctx, r.database)
	res, err := db.ExecContext(ctx, `
		UPDATE booking_waitlist SET status = ?
		WHERE merchant_id = ? AND id = ?
	`, status, merchantID, id)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return models.ErrNotFound
	}
	return nil
}

// DeleteWaitlistEntry supprime définitivement une entrée (scoping merchant).
func (r *BookingsRepository) DeleteWaitlistEntry(ctx context.Context, merchantID, id string) error {
	db := dbutils.GetDB(ctx, r.database)
	res, err := db.ExecContext(ctx, `
		DELETE FROM booking_waitlist WHERE merchant_id = ? AND id = ?
	`, merchantID, id)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return models.ErrNotFound
	}
	return nil
}
