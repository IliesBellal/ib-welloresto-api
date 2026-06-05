package revenueforecast

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/utils/dbutils"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Upsert(ctx context.Context, merchantID string, date time.Time, amountCents int64) error {
	db := dbutils.GetDB(ctx, r.db)
	query := `
		INSERT INTO planning_revenue_forecasts (id, merchant_id, forecast_date, amount_ht_cents)
		VALUES (?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE amount_ht_cents = VALUES(amount_ht_cents)
	`
	_, err := db.ExecContext(
		ctx,
		query,
		helpers.GeneratePrefixedID(helpers.PlanningRevenueForecastIDPrefix),
		merchantID,
		date.Format("2006-01-02"),
		amountCents,
	)
	if err != nil {
		return fmt.Errorf("upsert planning revenue forecast: %w", err)
	}

	return nil
}

func (r *Repository) DeleteByDate(ctx context.Context, merchantID string, date time.Time) error {
	db := dbutils.GetDB(ctx, r.db)
	query := `
		DELETE FROM planning_revenue_forecasts
		WHERE merchant_id = ? AND forecast_date = ?
	`
	_, err := db.ExecContext(ctx, query, merchantID, date.Format("2006-01-02"))
	if err != nil {
		return fmt.Errorf("delete planning revenue forecast: %w", err)
	}

	return nil
}
