package bookings

import (
	"context"
	"database/sql"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/modules/bookingcore"
)

// FindOrCreateCustomerByPhone retrouve un client par téléphone (normalisé FR)
// ou le crée s'il n'existe pas — même logique que le flux public de réservation.
func (r *BookingsRepository) FindOrCreateCustomerByPhone(ctx context.Context, merchantID, name, phone string) (*string, error) {
	db := dbx.GetDB(ctx, r.database)
	normalized := helpers.NormalizePhoneNumber(phone, "FR")

	var customerID string
	err := db.QueryRowContext(ctx, `
		SELECT customer_id FROM customer
		WHERE merchant_id = ? AND customer_tel = ? AND enabled = TRUE
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
func waitlistColumns() string {
	return `
	id, merchant_id, customer_id, party_size, customer_name, customer_phone, notes,
	status,
	` + bkgDateTimeFmt("notified_at") + ` AS notified_at,
	` + bkgDateTimeFmt("expires_at") + ` AS expires_at,
	` + bkgDateTimeFmt("created_at") + ` AS created_at,
	` + bkgDateTimeFmt("updated_at") + ` AS updated_at`
}

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
	db := dbx.GetDB(ctx, r.database)
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM booking_waitlist
		WHERE merchant_id = ? AND status IN (?, ?)
	`, merchantID, bookingcore.WaitlistWaiting, bookingcore.WaitlistNotified).Scan(&count)
	return count, err
}

// InsertWaitlistEntry insère une entrée (id/customer_id déjà résolus par le service).
func (r *BookingsRepository) InsertWaitlistEntry(ctx context.Context, e *WaitlistEntry) error {
	db := dbx.GetDB(ctx, r.database)

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
	db := dbx.GetDB(ctx, r.database)
	rows, err := db.QueryContext(ctx, `
		SELECT `+waitlistColumns()+`
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
	db := dbx.GetDB(ctx, r.database)
	row := db.QueryRowContext(ctx, `
		SELECT `+waitlistColumns()+`
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
	db := dbx.GetDB(ctx, r.database)
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

// GetFirstWaitingEntry retourne la plus ancienne entrée en statut waiting.
// Renvoie (nil, nil) si la file est vide.
func (r *BookingsRepository) GetFirstWaitingEntry(ctx context.Context, merchantID string) (*WaitlistEntry, error) {
	db := dbx.GetDB(ctx, r.database)
	row := db.QueryRowContext(ctx, `
		SELECT `+waitlistColumns()+`
		FROM booking_waitlist
		WHERE merchant_id = ? AND status = ?
		ORDER BY created_at ASC
		LIMIT 1
	`, merchantID, bookingcore.WaitlistWaiting)
	entry, err := scanWaitlist(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return entry, nil
}

// MarkWaitlistNotified passe une entrée waiting -> notified et pose
// notified_at + expires_at (= notified_at + expiryMinutes). La garde
// status='waiting' évite une double notification concurrente.
func (r *BookingsRepository) MarkWaitlistNotified(ctx context.Context, merchantID, id string, expiryMinutes int) error {
	db := dbx.GetDB(ctx, r.database)
	res, err := db.ExecContext(ctx, `
		UPDATE booking_waitlist
		SET status = ?, notified_at = ` + dbx.UTCNow() + `, expires_at = ` + dbx.UTCNow() + ` ` + bkgPlusMinutesParam() + `
		WHERE merchant_id = ? AND id = ? AND status = ?
	`, bookingcore.WaitlistNotified, expiryMinutes, merchantID, id, bookingcore.WaitlistWaiting)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return models.ErrNotFound
	}
	return nil
}

// ListExpiredNotifiedWaitlist retourne toutes les entrées notified dont le
// délai est dépassé, tous marchands confondus (utilisé par le cron).
func (r *BookingsRepository) ListExpiredNotifiedWaitlist(ctx context.Context) ([]WaitlistEntry, error) {
	db := dbx.GetDB(ctx, r.database)
	rows, err := db.QueryContext(ctx, `
		SELECT `+waitlistColumns()+`
		FROM booking_waitlist
		WHERE status = ? AND expires_at IS NOT NULL AND expires_at < ` + dbx.UTCNow() + `
		ORDER BY merchant_id, created_at ASC
	`, bookingcore.WaitlistNotified)
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

// GetCustomerEmail retourne l'email du client (chaîne vide si absent).
func (r *BookingsRepository) GetCustomerEmail(ctx context.Context, merchantID, customerID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)
	var email sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT customer_email FROM customer
		WHERE merchant_id = ? AND customer_id = ?
		LIMIT 1
	`, merchantID, customerID).Scan(&email)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if email.Valid {
		return email.String, nil
	}
	return "", nil
}

// GetMerchantBusinessName retourne le nom commercial du marchand.
func (r *BookingsRepository) GetMerchantBusinessName(ctx context.Context, merchantID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)
	var name sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT fullName FROM merchant WHERE id = ? LIMIT 1
	`, merchantID).Scan(&name)
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	if name.Valid {
		return name.String, nil
	}
	return "", nil
}

// DeleteWaitlistEntry supprime définitivement une entrée (scoping merchant).
func (r *BookingsRepository) DeleteWaitlistEntry(ctx context.Context, merchantID, id string) error {
	db := dbx.GetDB(ctx, r.database)
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
