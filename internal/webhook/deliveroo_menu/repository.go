package deliveroo_menu

import (
	"context"
	"database/sql"
	"fmt"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// GetBrandIDByMerchant récupère le brand_id Deliveroo associé à un merchant
func (r *Repository) GetBrandIDBySiteID(ctx context.Context, locationID string) (string, error) {
	const q = `SELECT brand_id FROM integration_deliveroo id WHERE id.location_id = ? LIMIT 1`

	var brandID sql.NullString
	if err := r.db.QueryRowContext(ctx, q, locationID).Scan(&brandID); err != nil {
		return "", err
	}
	if !brandID.Valid || brandID.String == "" {
		return "", fmt.Errorf("deliveroo: brand_id not configured for locaiton_id %s", locationID)
	}
	return brandID.String, nil
}
