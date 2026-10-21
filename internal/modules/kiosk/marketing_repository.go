package kiosk

import (
	"context"
	"database/sql"
	"fmt"

	"welloresto-api/internal/database/dbx"
)

// ---- Ruban marketing (kiosk_marketing_media, migration 190) ----
//
// Toutes les requêtes filtrent par merchant_id : un identifiant de média
// appartenant à un autre merchant se comporte comme un identifiant inconnu.

const marketingMediaColumns = `id, merchant_id, kind, url, duration_seconds, sort_order, created_at, updated_at`

func scanMarketingMedia(scan func(dest ...any) error) (MarketingMediaRow, error) {
	row := MarketingMediaRow{}
	err := scan(
		&row.ID, &row.MerchantID, &row.Kind, &row.URL, &row.DurationSeconds,
		&row.SortOrder, &row.CreatedAt, &row.UpdatedAt,
	)
	return row, err
}

// ListMarketingMedia retourne la rotation du merchant, dans l'ordre de lecture.
func (r *Repository) ListMarketingMedia(ctx context.Context, merchantID string) ([]MarketingMediaRow, error) {
	db := dbx.GetDB(ctx, r.database)

	query := `SELECT ` + marketingMediaColumns + `
	FROM kiosk_marketing_media
	WHERE merchant_id = ?
	ORDER BY sort_order ASC, created_at ASC, id ASC`

	rows, err := db.QueryContext(ctx, query, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []MarketingMediaRow{}
	for rows.Next() {
		item, err := scanMarketingMedia(rows.Scan)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetMarketingMedia retourne un média précis. Retourne (nil, nil) s'il n'existe
// pas ou n'appartient pas à ce merchant.
func (r *Repository) GetMarketingMedia(ctx context.Context, merchantID, mediaID string) (*MarketingMediaRow, error) {
	db := dbx.GetDB(ctx, r.database)

	query := `SELECT ` + marketingMediaColumns + `
	FROM kiosk_marketing_media
	WHERE id = ? AND merchant_id = ?`

	item, err := scanMarketingMedia(db.QueryRowContext(ctx, query, mediaID, merchantID).Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// CountMarketingMedia retourne la taille de la rotation du merchant.
func (r *Repository) CountMarketingMedia(ctx context.Context, merchantID string) (int, error) {
	db := dbx.GetDB(ctx, r.database)

	var count int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM kiosk_marketing_media WHERE merchant_id = ?`, merchantID).Scan(&count)
	return count, err
}

// NextMarketingSortOrder retourne le rang à attribuer à un nouveau média.
func (r *Repository) NextMarketingSortOrder(ctx context.Context, merchantID string) (int, error) {
	db := dbx.GetDB(ctx, r.database)

	var next sql.NullInt64
	err := db.QueryRowContext(ctx,
		`SELECT MAX(sort_order) + 1 FROM kiosk_marketing_media WHERE merchant_id = ?`, merchantID).Scan(&next)
	if err != nil {
		return 0, err
	}
	if !next.Valid {
		return 0, nil
	}
	return int(next.Int64), nil
}

func (r *Repository) CreateMarketingMedia(ctx context.Context, item MarketingMediaRow) error {
	db := dbx.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `
	INSERT INTO kiosk_marketing_media (id, merchant_id, kind, url, duration_seconds, sort_order)
	VALUES (?, ?, ?, ?, ?, ?)`,
		item.ID, item.MerchantID, item.Kind, item.URL, item.DurationSeconds, item.SortOrder)
	return err
}

// ReorderMarketingMedia réécrit le sort_order de la rotation : l'index dans
// orderedIDs devient le rang. À appeler dans une transaction — un
// réordonnancement partiel laisserait deux médias sur le même rang.
func (r *Repository) ReorderMarketingMedia(ctx context.Context, merchantID string, orderedIDs []string) error {
	db := dbx.GetDB(ctx, r.database)

	query := fmt.Sprintf(`UPDATE kiosk_marketing_media SET sort_order = ?, updated_at = %s WHERE id = ? AND merchant_id = ?`, dbx.UTCNow())
	for i, id := range orderedIDs {
		if _, err := db.ExecContext(ctx, query, i, id, merchantID); err != nil {
			return err
		}
	}
	return nil
}

// UpdateMarketingMediaDuration fixe la durée PROPRE d'un média, ou la retire
// (nil) pour qu'il suive à nouveau la durée par défaut. Retourne false si le
// média n'existe pas.
func (r *Repository) UpdateMarketingMediaDuration(ctx context.Context, merchantID, mediaID string, seconds *int) (bool, error) {
	db := dbx.GetDB(ctx, r.database)

	query := fmt.Sprintf(`UPDATE kiosk_marketing_media SET duration_seconds = ?, updated_at = %s WHERE id = ? AND merchant_id = ?`, dbx.UTCNow())
	res, err := db.ExecContext(ctx, query, seconds, mediaID, merchantID)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// ClearMarketingMediaDurations retire toutes les durées propres du merchant :
// chaque média suit alors la durée par défaut. Retourne le nombre de médias
// touchés.
func (r *Repository) ClearMarketingMediaDurations(ctx context.Context, merchantID string) (int64, error) {
	db := dbx.GetDB(ctx, r.database)

	query := fmt.Sprintf(`UPDATE kiosk_marketing_media SET duration_seconds = NULL, updated_at = %s WHERE merchant_id = ? AND duration_seconds IS NOT NULL`, dbx.UTCNow())
	res, err := db.ExecContext(ctx, query, merchantID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteMarketingMedia supprime un média. Retourne false s'il n'existe pas.
func (r *Repository) DeleteMarketingMedia(ctx context.Context, merchantID, mediaID string) (bool, error) {
	db := dbx.GetDB(ctx, r.database)

	res, err := db.ExecContext(ctx,
		`DELETE FROM kiosk_marketing_media WHERE id = ? AND merchant_id = ?`, mediaID, merchantID)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}
