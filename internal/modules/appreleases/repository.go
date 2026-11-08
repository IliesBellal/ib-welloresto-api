package appreleases

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"welloresto-api/internal/models"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// LatestPublic renvoie la plus haute version de appID ouverte à tous :
//   - publiée (release_date passée), comme pour les mises à jour des appareils ;
//   - sans restriction dans app_version_merchant. Une version en pilote,
//     réservée à quelques établissements, n'est pas proposée au public : la
//     page affiche alors la version ouverte précédente.
//
// models.ErrNotFound si aucune version ne remplit ces conditions.
func (r *Repository) LatestPublic(ctx context.Context, appID string) (versionCode int, releaseDate time.Time, downloadURL string, err error) {
	err = r.db.QueryRowContext(ctx, `
		SELECT v.version_code, v.release_date, v.download_url
		FROM app_version v
		WHERE v.app_id = $1
		  AND v.release_date < now()
		  AND v.download_url <> ''
		  AND NOT EXISTS (
		    SELECT 1 FROM app_version_merchant m
		    WHERE m.app_id = v.app_id AND m.version_code = v.version_code
		  )
		ORDER BY v.version_code DESC
		LIMIT 1
	`, appID).Scan(&versionCode, &releaseDate, &downloadURL)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, time.Time{}, "", models.ErrNotFound
	}
	return versionCode, releaseDate, downloadURL, err
}
