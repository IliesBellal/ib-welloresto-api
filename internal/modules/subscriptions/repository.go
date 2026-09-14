// Package subscriptions owns subscription_items (LOT B B1b) — what a
// merchant is actually BILLED for. Kept deliberately separate from
// subscriptions.*_enabled, which stays what a merchant has ACCESS to
// (pos.POSRepository / auth / kiosk repositories, unchanged by this
// chantier) — docs/WelloResto-Parcours-Client-v2.docx §7.1 treats these as
// two axes that can diverge on purpose (commercial dérogation, chantier
// B1d), not as a single source of truth to unify.
package subscriptions

import (
	"context"
	"database/sql"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

// BillingInfo is the slice of subscriptions (LOT B B1b's new columns)
// ComputeSubscriptionAmount needs — not the whole row, since the rest
// (package_id, *_enabled) belongs to the ACCESS side of §7.1, untouched by
// this chantier. CurrentPeriodEnd is read only by preview.go's B1e prorata
// estimate — nil until a merchant's first real billing cycle starts (LOT B2).
type BillingInfo struct {
	OverridePriceCents *int
	BillingCycle       string
	CurrentPeriodEnd   *time.Time
}

type Repository struct {
	database *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db}
}

// AddItem creates a new billed line for merchantID. Validates kind/code
// against the closed sets from the brief before writing — subscription_items
// has no DB-level CHECK constraint (same convention as pricing_catalog.kind),
// so this is the only guard. Does not check for an existing active item on
// the same code: the partial unique index (merchant_id, code) WHERE
// removed_at IS NULL does that at the database level, and callers should
// remove (RemoveItem) the previous line — a plan change, not two concurrent
// prices for the same code — before adding a replacement.
func (r *Repository) AddItem(ctx context.Context, merchantID, code, kind string, quantity, unitPriceCents int) (Item, error) {
	if !ValidKinds[kind] {
		return Item{}, models.ErrInvalidSubscriptionItemKind
	}
	if !ValidCodes[code] {
		return Item{}, models.ErrInvalidSubscriptionItemCode
	}

	db := dbx.GetDB(ctx, r.database)
	id := helpers.GeneratePrefixedID(helpers.SubscriptionItemIDPrefix)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO subscription_items (id, merchant_id, code, kind, quantity, unit_price_cents)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, merchantID, code, kind, quantity, unitPriceCents); err != nil {
		return Item{}, err
	}

	return r.get(ctx, id)
}

// RemoveItem soft-removes item id — sets removed_at, never deletes the row
// (kept for billing history, see the column's doc comment in the
// migration). A no-op (nil error) if id doesn't exist or is already removed.
func (r *Repository) RemoveItem(ctx context.Context, id string) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
		UPDATE subscription_items
		SET removed_at = `+dbx.UTCNow()+`, updated_at = `+dbx.UTCNow()+`
		WHERE id = ? AND removed_at IS NULL
	`, id)
	return err
}

// ListActive returns merchantID's currently-billed lines (removed_at IS
// NULL) — the input to ComputeSubscriptionAmount (chantier B1c).
func (r *Repository) ListActive(ctx context.Context, merchantID string) ([]Item, error) {
	db := dbx.GetDB(ctx, r.database)
	rows, err := db.QueryContext(ctx, `
		SELECT id, merchant_id, code, kind, quantity, unit_price_cents, created_at, updated_at, removed_at
		FROM subscription_items
		WHERE merchant_id = ? AND removed_at IS NULL
		ORDER BY created_at ASC
	`, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Item, 0)
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.MerchantID, &it.Code, &it.Kind, &it.Quantity, &it.UnitPriceCents, &it.CreatedAt, &it.UpdatedAt, &it.RemovedAt); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// GetBillingInfo reads merchantID's subscriptions row for the columns
// ComputeSubscriptionAmount needs. Returns models.ErrSubscriptionNotFound if
// merchantID has no subscriptions row at all (every merchant gets one at
// creation via pos.POSRepository.InsertSubscription — this only fires for a
// data inconsistency).
func (r *Repository) GetBillingInfo(ctx context.Context, merchantID string) (BillingInfo, error) {
	db := dbx.GetDB(ctx, r.database)
	var info BillingInfo
	err := db.QueryRowContext(ctx, `
		SELECT override_price_cents, billing_cycle, current_period_end FROM subscriptions WHERE merchant_id = ?
	`, merchantID).Scan(&info.OverridePriceCents, &info.BillingCycle, &info.CurrentPeriodEnd)
	if err == sql.ErrNoRows {
		return BillingInfo{}, models.ErrSubscriptionNotFound
	}
	return info, err
}

// activeEmployeeCount counts merchantID's active employee records
// (internal/modules/planning/employees) — the input to the
// planning_employee metered quantity (chantier B1c, rule 3). A direct
// cross-module read rather than a dependency on the employees package,
// same posture as onboarding.Repository's reads of products/kiosks/merchant.
func (r *Repository) activeEmployeeCount(ctx context.Context, merchantID string) (int, error) {
	db := dbx.GetDB(ctx, r.database)
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM employees WHERE merchant_id = ? AND enabled = TRUE AND active = TRUE
	`, merchantID).Scan(&count)
	return count, err
}

// activeCashDeskCount counts merchantID's active cash_desks — the input to
// the extra_pos metered quantity (chantier B1c, rule 3: "postes actifs moins
// 1"). Every merchant gets exactly one cash desk ("Caisse principale") at
// creation (pos.POSRepository.InitMerchantSatellites) — extra_pos bills
// every one beyond that first, included one.
func (r *Repository) activeCashDeskCount(ctx context.Context, merchantID string) (int, error) {
	db := dbx.GetDB(ctx, r.database)
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cash_desks WHERE merchant_id = ? AND enabled = TRUE
	`, merchantID).Scan(&count)
	return count, err
}

// hasMultiMerchantOwner reports whether merchantID has an admin
// (users_rights.admin = TRUE — the "propriétaire" role, see
// pos.POSRepository.InsertSubscription's caller for how it's granted at
// creation) who also administers at least one other merchant. There is no
// single dedicated "owner" flag distinct from "admin" in this schema — this
// is the closest available proxy for the brief's "l'utilisateur
// propriétaire" (chantier B1c, rule 5), documented as an assumption in
// docs/decisions.md.
func (r *Repository) hasMultiMerchantOwner(ctx context.Context, merchantID string) (bool, error) {
	db := dbx.GetDB(ctx, r.database)
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM users_rights ur1
			WHERE ur1.merchant_id = ? AND ur1.admin = TRUE AND ur1.enabled = TRUE
			AND EXISTS (
				SELECT 1 FROM users_rights ur2
				WHERE ur2.user_id = ur1.user_id AND ur2.admin = TRUE AND ur2.enabled = TRUE
				AND ur2.merchant_id <> ur1.merchant_id
			)
		)
	`, merchantID).Scan(&exists)
	return exists, err
}

// OwnerContact — B2c-1's trial-reminder recipient, the same resolution
// convention already duplicated in billing.Repository/dunning.Repository
// (earliest-enabled admin, falling back to the merchant's own contact info)
// — each module reads this directly rather than importing another for it.
type OwnerContact struct {
	Name  string
	Email string
}

func (r *Repository) GetMerchantOwnerContact(ctx context.Context, merchantID string) (OwnerContact, error) {
	db := dbx.GetDB(ctx, r.database)
	var c OwnerContact
	err := db.QueryRowContext(ctx, `
		SELECT u.name, u.email
		FROM users_rights ur
		JOIN users u ON u.user_id = ur.user_id
		WHERE ur.merchant_id = ? AND ur.admin = TRUE AND ur.enabled = TRUE
		ORDER BY u.created_at ASC
		LIMIT 1
	`, merchantID).Scan(&c.Name, &c.Email)
	if err == sql.ErrNoRows {
		err = db.QueryRowContext(ctx, `SELECT fullname, email FROM merchant WHERE id::text = ?`, merchantID).Scan(&c.Name, &c.Email)
	}
	return c, err
}

func (r *Repository) get(ctx context.Context, id string) (Item, error) {
	db := dbx.GetDB(ctx, r.database)
	var it Item
	err := db.QueryRowContext(ctx, `
		SELECT id, merchant_id, code, kind, quantity, unit_price_cents, created_at, updated_at, removed_at
		FROM subscription_items
		WHERE id = ?
	`, id).Scan(&it.ID, &it.MerchantID, &it.Code, &it.Kind, &it.Quantity, &it.UnitPriceCents, &it.CreatedAt, &it.UpdatedAt, &it.RemovedAt)
	return it, err
}
