package translation

import (
	"context"
	"database/sql"
	"time"

	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

// Language represents a platform-level language from available_languages.
type Language struct {
	Code    string `json:"code"    db:"code"`
	Name    string `json:"name"    db:"name"`
	Enabled bool   `json:"enabled" db:"enabled"`
}

// MerchantLanguage represents a language configured for a specific merchant,
// joined with available_languages to include the human-readable name.
type MerchantLanguage struct {
	MerchantID string    `json:"merchant_id" db:"merchant_id"`
	LangCode   string    `json:"lang_code"   db:"lang_code"`
	Name       string    `json:"name"        db:"name"`
	Enabled    bool      `json:"enabled"     db:"enabled"`
	CreatedAt  time.Time `json:"created_at"  db:"created_at"`
}

// Repository handles read access to available_languages and
// merchant_translation_languages.
type Repository struct {
	database *sql.DB
}

// NewRepository creates a new read-only translation repository.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db}
}

// ListAvailableLanguages returns all globally enabled languages,
// ordered by code ASC.
func (r *Repository) ListAvailableLanguages(ctx context.Context) ([]Language, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	rows, err := db.QueryContext(ctx, `
		SELECT code, name, enabled
		FROM available_languages
		WHERE enabled = true
		ORDER BY code ASC
	`)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	defer rows.Close()

	result := make([]Language, 0)
	for rows.Next() {
		var l Language
		if err := rows.Scan(&l.Code, &l.Name, &l.Enabled); err != nil {
			log.Error(err.Error())
			return nil, err
		}
		result = append(result, l)
	}

	return result, rows.Err()
}

// ListMerchantLanguages returns all languages configured for a merchant
// (including disabled ones), joined with available_languages for the name,
// ordered by lang_code ASC.
func (r *Repository) ListMerchantLanguages(ctx context.Context, merchantID string) ([]MerchantLanguage, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	rows, err := db.QueryContext(ctx, `
		SELECT mtl.merchant_id, mtl.lang_code, al.name, mtl.enabled, mtl.created_at
		FROM merchant_translation_languages mtl
		INNER JOIN available_languages al ON al.code = mtl.lang_code
		WHERE mtl.merchant_id = ?
		ORDER BY mtl.lang_code ASC
	`, merchantID)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	defer rows.Close()

	result := make([]MerchantLanguage, 0)
	for rows.Next() {
		var ml MerchantLanguage
		if err := rows.Scan(
			&ml.MerchantID,
			&ml.LangCode,
			&ml.Name,
			&ml.Enabled,
			&ml.CreatedAt,
		); err != nil {
			log.Error(err.Error())
			return nil, err
		}
		result = append(result, ml)
	}

	return result, rows.Err()
}

// IsLanguageEnabledForMerchant returns true only when both conditions hold in
// a single SQL query:
//   - the language exists in available_languages with enabled = true
//   - the language is configured for this merchant in
//     merchant_translation_languages with enabled = true
//
// Returns (false, nil) — not an error — when the language does not exist at all.
func (r *Repository) IsLanguageEnabledForMerchant(ctx context.Context, merchantID string, langCode string) (bool, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM available_languages al
		INNER JOIN merchant_translation_languages mtl
			ON mtl.lang_code = al.code
			AND mtl.merchant_id = ?
		WHERE al.code = ?
		  AND al.enabled  = true
		  AND mtl.enabled = true
	`, merchantID, langCode).Scan(&count)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		log.Error(err.Error())
		return false, err
	}

	return count > 0, nil
}

// ActivateLanguageForMerchant ensures the row exists for (merchant_id, lang_code).
// Activation is represented by row presence in merchant_translation_languages.
func (r *Repository) ActivateLanguageForMerchant(ctx context.Context, merchantID string, langCode string) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	_, err := db.ExecContext(ctx, `
		INSERT INTO merchant_translation_languages (merchant_id, lang_code, enabled)
		VALUES (?, ?, true)
		ON DUPLICATE KEY UPDATE enabled = VALUES(enabled)
	`, merchantID, langCode)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	return nil
}

// ActivateLanguageForMerchantWithLimit activates a language only if the merchant
// has not exceeded maxLanguages active rows.
// It verifies the limit both before and after activation inside a transaction.
func (r *Repository) ActivateLanguageForMerchantWithLimit(ctx context.Context, merchantID string, langCode string, maxLanguages int) error {
	log := logger.FromContext(ctx)

	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		log.Error(err.Error())
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var alreadyEnabled int
	err = tx.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM merchant_translation_languages
		WHERE merchant_id = ? AND lang_code = ? AND enabled = true
	`, merchantID, langCode).Scan(&alreadyEnabled)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	if alreadyEnabled > 0 {
		if err := tx.Commit(); err != nil {
			log.Error(err.Error())
			return err
		}
		return nil
	}

	var beforeCount int
	err = tx.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM merchant_translation_languages
		WHERE merchant_id = ? AND enabled = true
	`, merchantID).Scan(&beforeCount)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	if beforeCount >= maxLanguages {
		return models.ErrTranslationLanguagesLimitReached
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO merchant_translation_languages (merchant_id, lang_code, enabled)
		VALUES (?, ?, true)
		ON DUPLICATE KEY UPDATE enabled = VALUES(enabled)
	`, merchantID, langCode)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	var afterCount int
	err = tx.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM merchant_translation_languages
		WHERE merchant_id = ? AND enabled = true
	`, merchantID).Scan(&afterCount)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	if afterCount > maxLanguages {
		return models.ErrTranslationLanguagesLimitReached
	}

	if err := tx.Commit(); err != nil {
		log.Error(err.Error())
		return err
	}

	return nil
}

// DeactivateLanguageForMerchant removes the language row for a merchant.
// Deactivation is represented by row deletion.
func (r *Repository) DeactivateLanguageForMerchant(ctx context.Context, merchantID string, langCode string) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	_, err := db.ExecContext(ctx, `
		DELETE FROM merchant_translation_languages
		WHERE merchant_id = ? AND lang_code = ?
	`, merchantID, langCode)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	return nil
}
