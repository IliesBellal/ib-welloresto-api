package onboarding

import (
	"context"
	"database/sql"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/helpers"
)

type Repository struct {
	database *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db}
}

// CreateDefaultTasks inserts the five fixed onboarding tasks for a
// newly-signed-up merchant, all starting StatusPending. Meant to run inside
// the same transaction as the merchant's creation (ambient via ctx, same
// pattern as presets.Service.ApplyPreset).
func (r *Repository) CreateDefaultTasks(ctx context.Context, merchantID string) error {
	db := dbx.GetDB(ctx, r.database)

	for _, key := range TaskKeys {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO onboarding_tasks (id, merchant_id, task_key, status)
			VALUES (?, ?, ?, ?)
		`, helpers.GeneratePrefixedID(helpers.OnboardingTaskIDPrefix), merchantID, key, StatusPending); err != nil {
			return err
		}
	}
	return nil
}

// ListByMerchant returns a merchant's onboarding tasks, ordered to match
// TaskKeys — GET /v1/merchants/{id}/onboarding.
func (r *Repository) ListByMerchant(ctx context.Context, merchantID string) ([]Task, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx, `
		SELECT id, merchant_id, task_key, status, completed_at, skip_reason, skipped_at, created_at, updated_at
		FROM onboarding_tasks
		WHERE merchant_id = ?
		ORDER BY created_at ASC
	`, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := make([]Task, 0, len(TaskKeys))
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.MerchantID, &t.TaskKey, &t.Status, &t.CompletedAt, &t.SkipReason, &t.SkippedAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// SetTaskDoneIfNotDone marks taskKey 'done' for merchantID, unless it
// already is — a silent no-op (0 rows affected, nil error) otherwise, which
// is what lets RecomputeOnboarding call this from every relevant write point
// without worrying about calling it twice for the same event. A business
// event is a one-way door: this never reverts 'done' back to 'pending'.
func (r *Repository) SetTaskDoneIfNotDone(ctx context.Context, merchantID, taskKey string) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
		UPDATE onboarding_tasks
		SET status = ?, completed_at = `+dbx.UTCNow()+`, updated_at = `+dbx.UTCNow()+`
		WHERE merchant_id = ? AND task_key = ? AND status <> ?
	`, StatusDone, merchantID, taskKey, StatusDone)
	return err
}

// SetTaskSkipped marks taskKey 'skipped' with a mandatory reason — POST
// /v1/merchants/{id}/onboarding/{code}/skip. Refuses (0 rows affected) a task
// already 'done': skipping something already accomplished makes no sense,
// and the caller (Service.SkipTask) treats that as ErrOnboardingTaskAlreadyDone.
func (r *Repository) SetTaskSkipped(ctx context.Context, merchantID, taskKey, reason string) (bool, error) {
	db := dbx.GetDB(ctx, r.database)
	res, err := db.ExecContext(ctx, `
		UPDATE onboarding_tasks
		SET status = ?, skip_reason = ?, skipped_at = `+dbx.UTCNow()+`, updated_at = `+dbx.UTCNow()+`
		WHERE merchant_id = ? AND task_key = ? AND status <> ?
	`, StatusSkipped, reason, merchantID, taskKey, StatusDone)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// --- Business-event condition checks used by RecomputeOnboarding ---
// Each returns whether merchantID has already produced the event that
// completes the corresponding task. "payment" has no query here — LOT B
// (SEPA mandate) owns that condition; see Service.MarkPaymentMandateAccepted
// for the hook this chantier leaves in place instead.

// MenuConditionMet reports whether merchantID has at least one active
// product priced and with all three VAT rates set — the minimum needed to
// actually sell something.
func (r *Repository) MenuConditionMet(ctx context.Context, merchantID string) (bool, error) {
	db := dbx.GetDB(ctx, r.database)
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM products
			WHERE merchant_id = ? AND enabled = TRUE AND price > 0
			  AND tva_in_id <> 0 AND tva_delivery_id <> 0 AND tva_take_away_id <> 0
		)
	`, merchantID).Scan(&exists)
	return exists, err
}

// DeviceConditionMet reports whether merchantID has paired at least one
// kiosk (POST /kiosk/enroll having succeeded once is enough — no further
// status distinction).
func (r *Repository) DeviceConditionMet(ctx context.Context, merchantID string) (bool, error) {
	db := dbx.GetDB(ctx, r.database)
	var exists bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM kiosks WHERE merchant_id = ?)`, merchantID).Scan(&exists)
	return exists, err
}

// TeamConditionMet reports whether merchantID has a second active
// users_rights row — the owner created at signup is the first; "team" is
// done once at least one more staff member is active.
func (r *Repository) TeamConditionMet(ctx context.Context, merchantID string) (bool, error) {
	db := dbx.GetDB(ctx, r.database)
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users_rights WHERE merchant_id = ? AND enabled = TRUE`, merchantID).Scan(&count)
	return count >= 2, err
}

// LogoConditionMet reports whether merchant.logo_url has been set.
func (r *Repository) LogoConditionMet(ctx context.Context, merchantID string) (bool, error) {
	db := dbx.GetDB(ctx, r.database)
	castExpr := "CAST(id AS CHAR)"
	if dbx.ActiveDialect() == dbx.Postgres {
		castExpr = "CAST(id AS TEXT)"
	}
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM merchant WHERE `+castExpr+` = ? AND logo_url IS NOT NULL AND logo_url <> '')
	`, merchantID).Scan(&exists)
	return exists, err
}
