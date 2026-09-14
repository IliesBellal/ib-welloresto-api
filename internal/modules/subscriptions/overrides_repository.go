package subscriptions

import (
	"context"
	"database/sql"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

// CreateOverrideRow inserts a subscription_overrides row. Callers (see
// Service.CreateOverride) are responsible for validating kind/reason/target
// first and for applying the side-effect write (subscriptions.*_enabled,
// override_price_cents or max_kiosks) in the same transaction — this method
// only writes the audit row itself.
func (r *Repository) CreateOverrideRow(ctx context.Context, merchantID, kind, target, reason string, note *string, createdBy string, expiresAt, trialEndsAt *time.Time) (Override, error) {
	db := dbx.GetDB(ctx, r.database)
	id := helpers.GeneratePrefixedID(helpers.SubscriptionOverrideIDPrefix)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO subscription_overrides (id, merchant_id, kind, target, reason, note, created_by, expires_at, trial_ends_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, merchantID, kind, target, reason, note, createdBy, expiresAt, trialEndsAt); err != nil {
		return Override{}, err
	}
	return r.getOverride(ctx, id)
}

const overrideColumns = `id, merchant_id, kind, target, reason, note, created_by, created_at, expires_at, revoked_at, trial_ends_at, trial_reminder_7d_sent_at, trial_reminder_1d_sent_at`

func scanOverride(row interface {
	Scan(dest ...interface{}) error
}, o *Override) error {
	return row.Scan(&o.ID, &o.MerchantID, &o.Kind, &o.Target, &o.Reason, &o.Note, &o.CreatedBy, &o.CreatedAt, &o.ExpiresAt, &o.RevokedAt, &o.TrialEndsAt, &o.TrialReminder7dSentAt, &o.TrialReminder1dSentAt)
}

// ListActiveOverrides returns every override across every merchant that is
// neither revoked nor past its expiry, oldest first — GET /v1/admin/overrides
// ("toutes les actives, triées par ancienneté").
func (r *Repository) ListActiveOverrides(ctx context.Context) ([]Override, error) {
	db := dbx.GetDB(ctx, r.database)
	rows, err := db.QueryContext(ctx, `
		SELECT `+overrideColumns+`
		FROM subscription_overrides
		WHERE revoked_at IS NULL AND (expires_at IS NULL OR expires_at > `+dbx.UTCNow()+`)
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	overrides := make([]Override, 0)
	for rows.Next() {
		var o Override
		if err := scanOverride(rows, &o); err != nil {
			return nil, err
		}
		overrides = append(overrides, o)
	}
	return overrides, rows.Err()
}

// ListActiveTrialOverrides returns every non-revoked override with a
// trial_ends_at set, across every merchant — RunTrialExpiryCheck's input
// (B2c-1), re-read fresh on every cron tick.
func (r *Repository) ListActiveTrialOverrides(ctx context.Context) ([]Override, error) {
	db := dbx.GetDB(ctx, r.database)
	rows, err := db.QueryContext(ctx, `
		SELECT `+overrideColumns+`
		FROM subscription_overrides
		WHERE revoked_at IS NULL AND trial_ends_at IS NOT NULL
		ORDER BY trial_ends_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	overrides := make([]Override, 0)
	for rows.Next() {
		var o Override
		if err := scanOverride(rows, &o); err != nil {
			return nil, err
		}
		overrides = append(overrides, o)
	}
	return overrides, rows.Err()
}

// GetNearestActiveTrial returns merchantID's soonest-expiring active trial
// override, or (nil, nil) if it has none — B2c-1's bandeau data source.
func (r *Repository) GetNearestActiveTrial(ctx context.Context, merchantID string) (*Override, error) {
	db := dbx.GetDB(ctx, r.database)
	var o Override
	err := scanOverride(db.QueryRowContext(ctx, `
		SELECT `+overrideColumns+`
		FROM subscription_overrides
		WHERE merchant_id = ? AND revoked_at IS NULL AND trial_ends_at IS NOT NULL AND trial_ends_at > `+dbx.UTCNow()+`
		ORDER BY trial_ends_at ASC
		LIMIT 1
	`, merchantID), &o)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// MarkTrialReminder7dSent / MarkTrialReminder1dSent record that a reminder
// was just sent — guards against resending on the next cron tick.
func (r *Repository) MarkTrialReminder7dSent(ctx context.Context, id string) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `UPDATE subscription_overrides SET trial_reminder_7d_sent_at = `+dbx.UTCNow()+` WHERE id = ?`, id)
	return err
}

func (r *Repository) MarkTrialReminder1dSent(ctx context.Context, id string) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `UPDATE subscription_overrides SET trial_reminder_1d_sent_at = `+dbx.UTCNow()+` WHERE id = ?`, id)
	return err
}

// HasSepaMandate reports whether merchantID has an ACTIVE SEPA mandate on
// file (sepa_mandates.status, LOT B B2a-3) — RunTrialExpiryCheck's condition
// for "the recurring subscription has taken over, let the trial lapse
// quietly" vs. "no mandate showed up, revert to SETUP". LOT B F2 : filters
// explicitly on status, not just row existence — today every row this
// codebase ever writes is 'active' (billing.MandateStatusActive; not
// imported here, billing already imports subscriptions, a reverse import
// would cycle — see docs/decisions.md, B2c-0), so a bare EXISTS happened to
// be correct so far, but only by accident: nothing re-checks the real
// mandate state at expiry, and a future revoked/cancelled mandate row would
// have silently let the trial "lapse quietly" instead of correctly
// reverting to SETUP.
func (r *Repository) HasSepaMandate(ctx context.Context, merchantID string) (bool, error) {
	db := dbx.GetDB(ctx, r.database)
	var exists bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sepa_mandates WHERE merchant_id = ? AND status = 'active')`, merchantID).Scan(&exists)
	return exists, err
}

// RevertTrialActivation — B2c-1's "il retombe en SETUP, pas en SUSPENDED"
// for a kind='price' trial that expired with no mandate on file. Only
// touches rows currently in the trial-granted state (activation_state=LIVE,
// subscriptions.status='active') — a merchant who reached LIVE some other
// way in the meantime (a real mandate that HasSepaMandate's caller already
// ruled out, or a platform-staff override) is not clobbered.
func (r *Repository) RevertTrialActivation(ctx context.Context, merchantID string) error {
	db := dbx.GetDB(ctx, r.database)
	if _, err := db.ExecContext(ctx, `
		UPDATE merchant SET activation_state = 'SETUP' WHERE id::text = ? AND activation_state = 'LIVE'
	`, merchantID); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `
		UPDATE subscriptions SET status = 'setup' WHERE merchant_id = ?
	`, merchantID)
	return err
}

// GrantTrialActivation — B2c-1's counterpart at creation: a kind='price'
// override with a trial grants LIVE access immediately (that's the whole
// point of "accès gratuit") exactly like a real mandate would (B2a-3), so
// went_live_at is set the same way — never overwritten if already set
// (WHERE went_live_at IS NULL, same guard as
// billing.Repository.ActivateSubscriptionAndMerchant).
func (r *Repository) GrantTrialActivation(ctx context.Context, merchantID string) error {
	db := dbx.GetDB(ctx, r.database)
	if _, err := db.ExecContext(ctx, `
		UPDATE merchant SET activation_state = 'LIVE', went_live_at = `+dbx.UTCNow()+`
		WHERE id::text = ? AND went_live_at IS NULL
	`, merchantID); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `UPDATE subscriptions SET status = 'active' WHERE merchant_id = ?`, merchantID)
	return err
}

// RevokeOverride sets revoked_at on override id. Returns
// models.ErrOverrideNotFound if id doesn't exist or is already revoked —
// DELETE /v1/admin/overrides/{id} is not a silent no-op the way
// RemoveItem is, since a caller revoking a specific id needs to know
// whether their call actually did anything.
func (r *Repository) RevokeOverride(ctx context.Context, id string) error {
	db := dbx.GetDB(ctx, r.database)
	res, err := db.ExecContext(ctx, `
		UPDATE subscription_overrides
		SET revoked_at = `+dbx.UTCNow()+`
		WHERE id = ? AND revoked_at IS NULL
	`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return models.ErrOverrideNotFound
	}
	return nil
}

func (r *Repository) getOverride(ctx context.Context, id string) (Override, error) {
	db := dbx.GetDB(ctx, r.database)
	var o Override
	err := scanOverride(db.QueryRowContext(ctx, `
		SELECT `+overrideColumns+`
		FROM subscription_overrides
		WHERE id = ?
	`, id), &o)
	return o, err
}

// --- Override side-effect writes -------------------------------------------
//
// Each of these applies exactly the effect one Override.Kind grants — see
// migrations/todo/139_subscription_overrides.up.sql's table comment for the
// Kind -> column mapping this mirrors.

// moduleOverrideColumns maps a subscription_items-style module code to the
// subscriptions.*_enabled column an OverrideKindModule override flips. Only
// codes with a real column are present — "marketplaces" has none yet
// (ErrOverrideTargetUnsupported), and "kiosk"/"sms" are additionally
// rejected upstream by the P3 guard before this map is even consulted, so
// their presence/absence here does not matter for them.
var moduleOverrideColumns = map[string]string{
	CodeReservation: "bookings_enabled",
	CodeHACCP:       "haccp_enabled",
	CodePlanning:    "planning_enabled",
	CodeDelivery:    "delivery_enabled",
	CodeKiosk:       "kiosks_enabled",
}

// SetModuleEnabled flips subscriptions.<column> to TRUE for merchantID.
// column must be one of moduleOverrideColumns' values (a fixed, code-chosen
// literal — never derived directly from request input) since it is
// interpolated into the query as an identifier, not a bound parameter.
func (r *Repository) SetModuleEnabled(ctx context.Context, merchantID, column string) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `UPDATE subscriptions SET `+column+` = TRUE WHERE merchant_id = ?`, merchantID)
	return err
}

// SetOverridePriceCents writes subscriptions.override_price_cents for
// merchantID — OverrideKindPrice's effect (§7.3).
func (r *Repository) SetOverridePriceCents(ctx context.Context, merchantID string, cents int) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `UPDATE subscriptions SET override_price_cents = ? WHERE merchant_id = ?`, cents, merchantID)
	return err
}

// SetMaxKiosks writes subscriptions.max_kiosks for merchantID —
// OverrideKindKioskQuota's effect. Pre-existing column/quota mechanism
// (kiosk.Repository.GetMerchantMaxKiosks), not introduced by this chantier.
func (r *Repository) SetMaxKiosks(ctx context.Context, merchantID string, quota int) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `UPDATE subscriptions SET max_kiosks = ? WHERE merchant_id = ?`, quota, merchantID)
	return err
}
