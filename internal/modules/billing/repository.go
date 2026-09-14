package billing

import (
	"context"
	"database/sql"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/helpers"
)

type Repository struct {
	database *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db}
}

// GetBillingCustomer returns merchantID's platform_billing_customers row, or
// (nil, nil) if it has none yet — the lazy-creation trigger (B2a-2).
func (r *Repository) GetBillingCustomer(ctx context.Context, merchantID string) (*BillingCustomer, error) {
	db := dbx.GetDB(ctx, r.database)
	var c BillingCustomer
	err := db.QueryRowContext(ctx, `
		SELECT id, merchant_id, stripe_customer_id, is_primary_for_merchant, created_at
		FROM platform_billing_customers WHERE merchant_id = ?
	`, merchantID).Scan(&c.ID, &c.MerchantID, &c.StripeCustomerID, &c.IsPrimaryForMerchant, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// UpsertBillingCustomer creates or replaces merchantID's
// platform_billing_customers row — used for the first-ever lazy creation
// (B2a-2), for attach-to (point at another merchant's stripe_customer_id,
// is_primary_for_merchant=false) and for detach (point at a freshly created
// one, is_primary_for_merchant=true). One row per merchant_id by
// construction (UNIQUE index, migration 140) — ON CONFLICT keeps this a
// single statement rather than a check-then-insert-or-update race.
func (r *Repository) UpsertBillingCustomer(ctx context.Context, merchantID, stripeCustomerID string, isPrimary bool) (BillingCustomer, error) {
	db := dbx.GetDB(ctx, r.database)
	id := helpers.GeneratePrefixedID(helpers.PlatformBillingCustomerIDPrefix)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO platform_billing_customers (id, merchant_id, stripe_customer_id, is_primary_for_merchant)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (merchant_id) DO UPDATE SET
			stripe_customer_id = EXCLUDED.stripe_customer_id,
			is_primary_for_merchant = EXCLUDED.is_primary_for_merchant
	`, id, merchantID, stripeCustomerID, isPrimary); err != nil {
		return BillingCustomer{}, err
	}
	c, err := r.GetBillingCustomer(ctx, merchantID)
	if err != nil {
		return BillingCustomer{}, err
	}
	return *c, nil
}

// ActivationStatus is GetActivationStatus's result — the bandeau's entire
// data need (B2b-3 §7.6, extended by B2c-1 §7.3 below).
type ActivationStatus struct {
	ActivationState    string `json:"activation_state"`
	SubscriptionStatus string `json:"subscription_status"`
	// TrialEndsAt — B2c-1 : set only when this merchant is LIVE via an
	// active, unexpired trial override (subscriptions.Service.GetNearestActiveTrial).
	// The back-office renders its OWN distinct "free trial ending" banner
	// off this field, never the SETUP one — ActivationState is already
	// "LIVE" for a merchant on a trial, so the existing SETUP-banner check
	// (activation_state == 'SETUP') already can't fire for them by
	// construction; this field is what lets a NEW banner exist at all.
	TrialEndsAt *time.Time `json:"trial_ends_at,omitempty"`
}

// GetActivationStatus reads merchant.activation_state/subscriptions.status
// fresh from Postgres — deliberately NOT sourced from the authenticated
// user object, which auth.Service caches in Redis for
// models.UserCacheTTL (60 minutes, see internal/modules/auth/service.go). A
// bandeau that's supposed to disappear "exactement au passage en LIVE"
// cannot tolerate a stale read of any duration, so this is its own
// always-fresh query rather than a field added to that cached blob.
func (r *Repository) GetActivationStatus(ctx context.Context, merchantID string) (ActivationStatus, error) {
	db := dbx.GetDB(ctx, r.database)
	var st ActivationStatus
	var subStatus sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT m.activation_state, s.status
		FROM merchant m
		LEFT JOIN subscriptions s ON s.merchant_id = m.id::text
		WHERE m.id::text = ?
	`, merchantID).Scan(&st.ActivationState, &subStatus)
	if subStatus.Valid {
		st.SubscriptionStatus = subStatus.String
	}
	return st, err
}

// GetStripeSubscriptionID reads subscriptions.stripe_subscription_id for
// merchantID — empty string if none yet (B2c-0: whether to create or
// update the real recurring Stripe Subscription).
func (r *Repository) GetStripeSubscriptionID(ctx context.Context, merchantID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)
	var id sql.NullString
	err := db.QueryRowContext(ctx, `SELECT stripe_subscription_id FROM subscriptions WHERE merchant_id = ?`, merchantID).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id.String, err
}

// SetStripeSubscriptionID writes subscriptions.stripe_subscription_id —
// called exactly once, right after the Stripe Subscription is first
// created (B2c-0). Never overwritten afterwards (updates go through
// SyncSubscriptionItems on the same subscription id).
func (r *Repository) SetStripeSubscriptionID(ctx context.Context, merchantID, stripeSubscriptionID string) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `UPDATE subscriptions SET stripe_subscription_id = ? WHERE merchant_id = ?`, stripeSubscriptionID, merchantID)
	return err
}

// GetMerchantOwnerContact resolves the name/email to put on a newly created
// Stripe Customer (B2a-2's "nom, email du propriétaire"). "Propriétaire" has
// no dedicated flag in this schema (same proxy already used and documented
// for the multi-merchant discount, see subscriptions.Repository.hasMultiMerchantOwner)
// — the earliest-enabled admin user on the merchant, falling back to the
// merchant's own listed contact info if it has no admin user at all.
func (r *Repository) GetMerchantOwnerContact(ctx context.Context, merchantID string) (name, email string, err error) {
	db := dbx.GetDB(ctx, r.database)
	err = db.QueryRowContext(ctx, `
		SELECT u.name, u.email
		FROM users_rights ur
		JOIN users u ON u.user_id = ur.user_id
		WHERE ur.merchant_id = ? AND ur.admin = TRUE AND ur.enabled = TRUE
		ORDER BY u.created_at ASC
		LIMIT 1
	`, merchantID).Scan(&name, &email)
	if err == sql.ErrNoRows {
		err = db.QueryRowContext(ctx, `SELECT fullname, email FROM merchant WHERE id::text = ?`, merchantID).Scan(&name, &email)
	}
	return name, email, err
}

// CreateMandate inserts a sepa_mandates row — see
// migrations/todo/141_sepa_mandates.up.sql.
func (r *Repository) CreateMandate(ctx context.Context, merchantID, stripePaymentMethodID, status string, last4 *string, acceptedAt *time.Time) (SepaMandate, error) {
	db := dbx.GetDB(ctx, r.database)
	id := helpers.GeneratePrefixedID(helpers.SepaMandateIDPrefix)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO sepa_mandates (id, merchant_id, stripe_payment_method_id, status, last4_iban_masked, accepted_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, merchantID, stripePaymentMethodID, status, last4, acceptedAt); err != nil {
		return SepaMandate{}, err
	}
	var m SepaMandate
	err := db.QueryRowContext(ctx, `
		SELECT id, merchant_id, stripe_payment_method_id, status, last4_iban_masked, accepted_at, created_at
		FROM sepa_mandates WHERE id = ?
	`, id).Scan(&m.ID, &m.MerchantID, &m.StripePaymentMethodID, &m.Status, &m.Last4IBANMasked, &m.AcceptedAt, &m.CreatedAt)
	return m, err
}

// ActivateSubscriptionAndMerchant is B2a-3's "surtout" clause : on
// setup_intent.succeeded, subscriptions.status='active' AND
// merchant.activation_state='LIVE'/went_live_at=now() — together, since a
// merchant only ever goes LIVE because its subscription became active
// (decision N4b: never gated on anything else, see the WHERE clause below
// which contains no other condition). went_live_at is set only the first
// time (WHERE went_live_at IS NULL) — a merchant cannot "go live" twice, a
// replayed webhook must not overwrite the original timestamp.
func (r *Repository) ActivateSubscriptionAndMerchant(ctx context.Context, merchantID string) error {
	db := dbx.GetDB(ctx, r.database)
	if _, err := db.ExecContext(ctx, `UPDATE subscriptions SET status = 'active' WHERE merchant_id = ?`, merchantID); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `
		UPDATE merchant SET activation_state = 'LIVE', went_live_at = `+dbx.UTCNow()+`
		WHERE id::text = ? AND went_live_at IS NULL
	`, merchantID)
	return err
}
