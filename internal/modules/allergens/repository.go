package allergens

import (
	"context"
	"database/sql"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

type Repository struct {
	database *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db}
}

// ListAllergens returns all 14 system allergens (no merchant scope – they are global).
func (r *Repository) ListAllergens(ctx context.Context) ([]models.AllergenEntry, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	rows, err := db.QueryContext(ctx,
		`SELECT allergen_id , name, code, icon, color FROM allergens ORDER BY id ASC`,
	)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	defer rows.Close()

	var result []models.AllergenEntry
	for rows.Next() {
		var a models.AllergenEntry
		if err := rows.Scan(&a.ID, &a.Name, &a.Code, &a.Icon, &a.Color); err != nil {
			log.Error(err.Error())
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}
