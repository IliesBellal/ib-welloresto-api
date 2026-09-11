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
		SELECT id, merchant_id, task_key, status, completed_at, created_at, updated_at
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
		if err := rows.Scan(&t.ID, &t.MerchantID, &t.TaskKey, &t.Status, &t.CompletedAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}
