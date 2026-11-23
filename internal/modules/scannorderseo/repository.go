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
	SELECT COALESCE(snos.seo_title, ''), COALESCE(snos.seo_description, ''), COALESCE(snos.seo_cuisine_type, ''),
	       COALESCE(snos.seo_links, ''), snos.seo_updated_at,
	       COALESCE(snos.google_place_id, ''), COALESCE(snos.google_maps_url, ''), COALESCE(snos.google_review_url, ''),
	       snos.review_request_enabled,
	       (SELECT COUNT(*) FROM review_requests rr
	         WHERE rr.merchant_id = snos.merchant_id AND rr.status = 'sent'
	           AND rr.sent_at >= now() - interval '30 days')
	FROM scannorder_settings snos
	WHERE snos.merchant_id = ?`, merchantID).Scan(
		&row.Title, &row.Description, &row.Cuisines, &row.Links, &updatedAt,
		&row.GooglePlaceID, &row.GoogleMapsURL, &row.GoogleReviewURL, &row.ReviewRequestEnabled, &row.ReviewRequestsSent,
	)
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
	nullable := func(column string, v *string) {
		if v == nil {
			return
		}
		set = append(set, column+" = ?")
		if *v == "" {
			args = append(args, nil)
		} else {
			args = append(args, *v)
		}
	}
	nullable("seo_links", u.Links)
	nullable("google_place_id", u.GooglePlaceID)
	nullable("google_maps_url", u.GoogleMapsURL)
	nullable("google_review_url", u.GoogleReviewURL)
	if u.ReviewRequestEnabled != nil {
		set = append(set, "review_request_enabled = ?")
		args = append(args, *u.ReviewRequestEnabled)
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

// GetPlaceQuery lit le nom, l'adresse et la position de l'établissement, pour
// chercher sa fiche Google.
func (r *Repository) GetPlaceQuery(ctx context.Context, merchantID string) (*placeQuery, error) {
	db := dbx.GetDB(ctx, r.database)

	var q placeQuery
	var lat, lng sql.NullFloat64
	err := db.QueryRowContext(ctx, `
	SELECT fullName, COALESCE(address, ''), COALESCE(zip_code, ''), COALESCE(city, ''), lat, lng
	FROM merchant WHERE id = ?`, merchantID).Scan(&q.Name, &q.Address, &q.ZipCode, &q.City, &lat, &lng)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	q.Lat, q.Lng = lat.Float64, lng.Float64
	return &q, nil
}
