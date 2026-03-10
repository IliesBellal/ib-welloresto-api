package allergens

import (
	"context"
	"database/sql"
	"welloresto-api/internal/models"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// ListAllergens returns all 14 system allergens (no merchant scope – they are global).
func (r *Repository) ListAllergens(ctx context.Context) ([]models.AllergenEntry, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, code, COALESCE(icon, '') FROM allergens ORDER BY id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.AllergenEntry
	for rows.Next() {
		var a models.AllergenEntry
		if err := rows.Scan(&a.ID, &a.Name, &a.Code, &a.Icon); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}
