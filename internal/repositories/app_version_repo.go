package repositories

import (
	"context"
	"database/sql"
)

type AppVersionRepository struct {
	db *sql.DB
}

func NewAppVersionRepository(db *sql.DB) *AppVersionRepository {
	return &AppVersionRepository{db: db}
}

func (r *AppVersionRepository) CheckAppVersion(ctx context.Context, currentVersion int, app, merchantID string) (map[string]interface{}, error) {

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	// Step 1: get highest version > currentVersion
	q1 := `
SELECT id, version_code, download_url
FROM app_version
WHERE app_id = ?
  AND version_code > ?
  AND release_date < UTC_TIMESTAMP()
ORDER BY version_code DESC
LIMIT 1;
`
	row := tx.QueryRowContext(ctx, q1, app, currentVersion)

	var versionID int
	var versionCode int
	var downloadURL string

	err = row.Scan(&versionID, &versionCode, &downloadURL)
	if err == sql.ErrNoRows {
		tx.Commit()
		return map[string]interface{}{"status": "no_update"}, nil
	}
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	// Step 2: check if version is restricted
	q2 := `
SELECT 1 FROM app_version_merchant
WHERE version_code = ?
LIMIT 1;
`
	var restricted int
	err = tx.QueryRowContext(ctx, q2, versionCode).Scan(&restricted)
	if err == sql.ErrNoRows {
		// Not restricted → update available
		tx.Commit()
		return map[string]interface{}{
			"status":       "update_available",
			"download_url": downloadURL,
		}, nil
	}
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	// Step 3: restricted → check if merchant allowed
	q3 := `
SELECT 1 FROM app_version_merchant
WHERE version_code = ?
  AND merchant_id = ?
LIMIT 1;
`

	var allowed int
	err = tx.QueryRowContext(ctx, q3, versionCode, merchantID).Scan(&allowed)
	if err == sql.ErrNoRows {
		tx.Commit()
		return map[string]interface{}{"status": "no_update"}, nil
	}
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	// Allowed → update available
	tx.Commit()
	return map[string]interface{}{
		"status":       "update_available",
		"download_url": downloadURL,
	}, nil
}
