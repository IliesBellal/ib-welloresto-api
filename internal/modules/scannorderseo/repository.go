package scannorderseo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/models"
)

type Repository struct {
	database *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db}
}

// GetSettings lit les champs de référencement d'un merchant. ErrNotFound s'il
// n'a pas de ligne scannorder_settings (ScanNOrder jamais initialisé).
func (r *Repository) GetSettings(ctx context.Context, merchantID string) (*settingsRow, error) {
	db := dbx.GetDB(ctx, r.database)

	var row settingsRow
	var updatedAt sql.NullTime
	err := db.QueryRowContext(ctx, `
	SELECT COALESCE(seo_title, ''), COALESCE(seo_description, ''), COALESCE(seo_cuisine_type, ''),
	       COALESCE(seo_links, ''), seo_updated_at
	FROM scannorder_settings
	WHERE merchant_id = ?`, merchantID).Scan(&row.Title, &row.Description, &row.Cuisines, &row.Links, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if updatedAt.Valid {
		t := updatedAt.Time.UTC()
		row.UpdatedAt = &t
	}
	return &row, nil
}

// UpdateSettings écrit les champs fournis et date la modification
// (seo_updated_at : lastmod du sitemap, signalement IndexNow). Les colonnes
// seo_title, seo_description et seo_cuisine_type sont NOT NULL : effacer
// écrit une chaîne vide ; seo_links passe à NULL.
func (r *Repository) UpdateSettings(ctx context.Context, merchantID string, u update, now time.Time) error {
	db := dbx.GetDB(ctx, r.database)

	set := []string{}
	args := []interface{}{}
	if u.Title != nil {
		set = append(set, "seo_title = ?")
		args = append(args, *u.Title)
	}
	if u.Description != nil {
		set = append(set, "seo_description = ?")
		args = append(args, *u.Description)
	}
	if u.Cuisines != nil {
		set = append(set, "seo_cuisine_type = ?")
		args = append(args, *u.Cuisines)
	}
	if u.Links != nil {
		set = append(set, "seo_links = ?")
		if *u.Links == "" {
			args = append(args, nil)
		} else {
			args = append(args, *u.Links)
		}
	}
	if len(set) == 0 {
		return nil
	}
	set = append(set, "seo_updated_at = ?")
	args = append(args, now.UTC(), merchantID)

	res, err := db.ExecContext(ctx, fmt.Sprintf(`UPDATE scannorder_settings SET %s WHERE merchant_id = ?`, strings.Join(set, ", ")), args...)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return models.ErrNotFound
	}
	return nil
}

// GetMerchantIdentity lit le nom et la ville de l'établissement, donnés à
// l'IA avec la carte.
func (r *Repository) GetMerchantIdentity(ctx context.Context, merchantID string) (name, city string, err error) {
	db := dbx.GetDB(ctx, r.database)

	err = db.QueryRowContext(ctx, `SELECT fullName, city FROM merchant WHERE id = ?`, merchantID).Scan(&name, &city)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", models.ErrNotFound
	}
	return name, city, err
}
