package dunning

import (
	"context"
	"database/sql"
	"time"

	"welloresto-api/internal/database/dbx"
)

type Repository struct {
	database *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db}
}

// GetState returns merchantID's current dunning row, or (nil, nil) if it has
// none — "no row" is the sole source of truth for "no cascade in progress"
// (see the table's doc comment).
func (r *Repository) GetState(ctx context.Context, merchantID string) (*State, error) {
	db := dbx.GetDB(ctx, r.database)
	var s State
	err := db.QueryRowContext(ctx, `
		SELECT merchant_id, first_failed_at, second_failed_at, suspension_deadline, reminder_count, last_reminder_sent_at, final_notice_sent_at
		FROM subscription_dunning WHERE merchant_id = ?
	`, merchantID).Scan(&s.MerchantID, &s.FirstFailedAt, &s.SecondFailedAt, &s.SuspensionDeadline, &s.ReminderCount, &s.LastReminderSentAt, &s.FinalNoticeSentAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// CreateFirstFailure records the 1st invoice.payment_failed — a no-op if a
// row already exists (ON CONFLICT DO NOTHING), so a replayed webhook never
// resets first_failed_at.
func (r *Repository) CreateFirstFailure(ctx context.Context, merchantID string) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
		INSERT INTO subscription_dunning (merchant_id, first_failed_at)
		VALUES (?, `+dbx.UTCNow()+`)
		ON CONFLICT (merchant_id) DO NOTHING
	`, merchantID)
	return err
}

// RecordSecondFailure sets second_failed_at/suspension_deadline — a no-op if
// already set, so a 3rd+ failure doesn't push the deadline back out.
func (r *Repository) RecordSecondFailure(ctx context.Context, merchantID string, deadline time.Time) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
		UPDATE subscription_dunning
		SET second_failed_at = `+dbx.UTCNow()+`, suspension_deadline = ?, updated_at = `+dbx.UTCNow()+`
		WHERE merchant_id = ? AND second_failed_at IS NULL
	`, deadline, merchantID)
	return err
}

// IncrementReminder records that a weekly reminder was just sent.
func (r *Repository) IncrementReminder(ctx context.Context, merchantID string) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
		UPDATE subscription_dunning
		SET reminder_count = reminder_count + 1, last_reminder_sent_at = `+dbx.UTCNow()+`, updated_at = `+dbx.UTCNow()+`
		WHERE merchant_id = ?
	`, merchantID)
	return err
}

// MarkFinalNoticeSent records the once-only 48h-before-suspension notice.
func (r *Repository) MarkFinalNoticeSent(ctx context.Context, merchantID string) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
		UPDATE subscription_dunning
		SET final_notice_sent_at = `+dbx.UTCNow()+`, updated_at = `+dbx.UTCNow()+`
		WHERE merchant_id = ?
	`, merchantID)
	return err
}

// ClearDunning removes merchantID's row entirely — called on invoice.paid
// ("toutes les relances programmées annulées"): with no future sends ever
// queued (see the table's doc comment), "cancel" just means the cron will no
// longer find a row to act on.
func (r *Repository) ClearDunning(ctx context.Context, merchantID string) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `DELETE FROM subscription_dunning WHERE merchant_id = ?`, merchantID)
	return err
}

// SetSubscriptionStatus writes subscriptions.status for merchantID.
func (r *Repository) SetSubscriptionStatus(ctx context.Context, merchantID, status string) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `UPDATE subscriptions SET status = ? WHERE merchant_id = ?`, status, merchantID)
	return err
}

// ListPastDue returns every dunning row whose subscription is currently
// past_due — the cron's input (RunCascade), re-read fresh on every run.
func (r *Repository) ListPastDue(ctx context.Context) ([]State, error) {
	db := dbx.GetDB(ctx, r.database)
	rows, err := db.QueryContext(ctx, `
		SELECT d.merchant_id, d.first_failed_at, d.second_failed_at, d.suspension_deadline, d.reminder_count, d.last_reminder_sent_at, d.final_notice_sent_at
		FROM subscription_dunning d
		JOIN subscriptions s ON s.merchant_id = d.merchant_id
		WHERE s.status = 'past_due'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	states := make([]State, 0)
	for rows.Next() {
		var s State
		if err := rows.Scan(&s.MerchantID, &s.FirstFailedAt, &s.SecondFailedAt, &s.SuspensionDeadline, &s.ReminderCount, &s.LastReminderSentAt, &s.FinalNoticeSentAt); err != nil {
			return nil, err
		}
		states = append(states, s)
	}
	return states, rows.Err()
}

// OwnerContact is who every dunning communication is sent to — resolved the
// same way as B2a-2's Customer contact (earliest-enabled admin), but
// including tel for SMS, which billing.Repository.GetMerchantOwnerContact
// doesn't need.
type OwnerContact struct {
	Name  string
	Email string
	Tel   string
}

func (r *Repository) GetMerchantOwnerContact(ctx context.Context, merchantID string) (OwnerContact, error) {
	db := dbx.GetDB(ctx, r.database)
	var c OwnerContact
	err := db.QueryRowContext(ctx, `
		SELECT u.name, u.email, u.tel
		FROM users_rights ur
		JOIN users u ON u.user_id = ur.user_id
		WHERE ur.merchant_id = ? AND ur.admin = TRUE AND ur.enabled = TRUE
		ORDER BY u.created_at ASC
		LIMIT 1
	`, merchantID).Scan(&c.Name, &c.Email, &c.Tel)
	if err == sql.ErrNoRows {
		err = db.QueryRowContext(ctx, `SELECT fullname, email, merchanttel FROM merchant WHERE id::text = ?`, merchantID).Scan(&c.Name, &c.Email, &c.Tel)
	}
	return c, err
}

// GetStripeCustomerID resolves merchantID's platform_billing_customers row
// (LOT B B2a) — a direct cross-module read (same posture as every other
// direct read already in this codebase, e.g.
// subscriptions.Repository.activeEmployeeCount), not an import of the
// billing package.
func (r *Repository) GetStripeCustomerID(ctx context.Context, merchantID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)
	var id string
	err := db.QueryRowContext(ctx, `SELECT stripe_customer_id FROM platform_billing_customers WHERE merchant_id = ?`, merchantID).Scan(&id)
	return id, err
}

// GetMerchantTimezone reads merchant.timezone — needed to evaluate "pendant
// les services" in the merchant's own local time, not the server's.
func (r *Repository) GetMerchantTimezone(ctx context.Context, merchantID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)
	var tz string
	err := db.QueryRowContext(ctx, `SELECT timezone FROM merchant WHERE id::text = ?`, merchantID).Scan(&tz)
	return tz, err
}
