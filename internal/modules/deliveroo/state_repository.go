package deliveroo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/utils/dbutils"
)

// File de réveils par compte (table marketplace_sync_state, migration 202).
// Voir docs/deliveroo-menu-sync.md, « Réveils ».

// Types de réveil (marketplace_sync_state.check_kind).
const (
	checkChanged = "changed" // un statut ou le catalogue vient de changer
	checkOpening = "opening" // après l'ouverture prévue du site
	checkRecheck = "recheck" // second contrôle après l'ouverture
	checkSafety  = "safety"  // filet de sécurité
	checkRetry   = "retry"   // nouvelle tentative après un échec
)

// syncLease : durée pendant laquelle un compte pris en charge par un worker
// n'est pas repris par un autre (ou par le tour suivant).
const syncLease = 5 * time.Minute

// PublishedAccount : un compte Deliveroo déjà publié, donc suivi par la file.
type PublishedAccount struct {
	MerchantID string
	LocationID string
	BrandID    string
}

// SyncState est une ligne de la file de réveils.
type SyncState struct {
	PublishedAccount
	CheckKind          string
	Attempts           int
	MenuDirtySince     *time.Time
	OpeningHours       OpeningHours
	TimeZone           string
	OpeningRefreshedAt time.Time
}

// EnsureSyncState inscrit un compte publié dans la file : premier réveil dans
// une minute (lecture des horaires, alignement). Sans effet s'il y est déjà.
func (r *DeliverooRepository) EnsureSyncState(ctx context.Context, merchantID, locationID string) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
		INSERT INTO marketplace_sync_state (platform, merchant_id, location_id, next_check_at, check_kind)
		VALUES (?, ?, ?, now() + interval '1 minute', ?)
		ON CONFLICT (platform, merchant_id, location_id) DO NOTHING`,
		platformDeliveroo, merchantID, locationID, checkSafety)
	return err
}

// GetPublishedAccounts : comptes Deliveroo actifs de l'établissement déjà
// publiés (présents dans la file). Vide pour un établissement sans Deliveroo.
func (r *DeliverooRepository) GetPublishedAccounts(ctx context.Context, merchantID string) ([]PublishedAccount, error) {
	db := dbx.GetDB(ctx, r.database)
	rows, err := db.QueryContext(ctx, `
		SELECT s.merchant_id, s.location_id, i.brand_id
		FROM marketplace_sync_state s
		JOIN integration_deliveroo i ON i.merchant_id = s.merchant_id AND i.location_id = s.location_id
		WHERE s.platform = ? AND s.merchant_id = ? AND i.enabled = TRUE AND i.brand_id <> ''
		ORDER BY s.location_id`, platformDeliveroo, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PublishedAccount
	for rows.Next() {
		var a PublishedAccount
		if err := rows.Scan(&a.MerchantID, &a.LocationID, &a.BrandID); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// MarkMenuDirty note que le catalogue a changé : le menu sera recalculé et
// republié s'il a vraiment changé, après quelques minutes de calme. Ne touche
// que les comptes déjà publiés.
func (r *DeliverooRepository) MarkMenuDirty(ctx context.Context, merchantID string) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
		UPDATE marketplace_sync_state SET menu_dirty_since = COALESCE(menu_dirty_since, now())
		WHERE platform = ? AND merchant_id = ?`, platformDeliveroo, merchantID)
	return err
}

// ClearMenuDirty efface le marqueur, sauf si un changement plus récent que
// since est arrivé entre-temps.
func (r *DeliverooRepository) ClearMenuDirty(ctx context.Context, merchantID, locationID string, since time.Time) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
		UPDATE marketplace_sync_state SET menu_dirty_since = NULL
		WHERE platform = ? AND merchant_id = ? AND location_id = ? AND menu_dirty_since <= ?`,
		platformDeliveroo, merchantID, locationID, since)
	return err
}

// RequestCheckSoon avance le prochain réveil (jamais ne le recule) : filet si
// l'alignement immédiat échoue ou si le serveur redémarre.
func (r *DeliverooRepository) RequestCheckSoon(ctx context.Context, merchantID, locationID string, delay time.Duration, kind string) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
		UPDATE marketplace_sync_state
		SET next_check_at = now() + make_interval(secs => ?), check_kind = ?
		WHERE platform = ? AND merchant_id = ? AND location_id = ?
		  AND (next_check_at IS NULL OR next_check_at > now() + make_interval(secs => ?))`,
		delay.Seconds(), kind, platformDeliveroo, merchantID, locationID, delay.Seconds())
	return err
}

// ScheduleCheck fixe le prochain réveil d'un compte.
func (r *DeliverooRepository) ScheduleCheck(ctx context.Context, merchantID, locationID string, at time.Time, kind string, attempts int, errText string) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
		UPDATE marketplace_sync_state
		SET next_check_at = ?, check_kind = ?, check_attempts = ?, last_check_at = now(), last_error = NULLIF(?, '')
		WHERE platform = ? AND merchant_id = ? AND location_id = ?`,
		at, kind, attempts, errText, platformDeliveroo, merchantID, locationID)
	return err
}

// SaveOpening enregistre les horaires et le fuseau lus chez Deliveroo.
func (r *DeliverooRepository) SaveOpening(ctx context.Context, merchantID, locationID string, hours OpeningHours, tz string) error {
	db := dbx.GetDB(ctx, r.database)
	raw, err := json.Marshal(hours)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		UPDATE marketplace_sync_state
		SET opening_hours = ?::jsonb, time_zone = NULLIF(?, ''), opening_refreshed_at = now()
		WHERE platform = ? AND merchant_id = ? AND location_id = ?`,
		string(raw), tz, platformDeliveroo, merchantID, locationID)
	return err
}

// ClaimDue prend en charge les comptes à réveiller : réveil échu, ou menu
// modifié depuis plus de menuQuiet. Un bail de syncLease évite qu'un autre
// tour (ou une autre instance) les reprenne pendant leur traitement.
func (r *DeliverooRepository) ClaimDue(ctx context.Context, menuQuiet time.Duration, limit int) ([]SyncState, error) {
	var out []SyncState
	err := dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, r.database)
		rows, err := db.QueryContext(txCtx, `
			SELECT s.merchant_id, s.location_id, COALESCE(i.brand_id, ''), COALESCE(s.check_kind, ''), s.check_attempts,
			       s.menu_dirty_since, s.opening_hours, COALESCE(s.time_zone, ''), s.opening_refreshed_at
			FROM marketplace_sync_state s
			LEFT JOIN integration_deliveroo i ON i.merchant_id = s.merchant_id AND i.location_id = s.location_id
			WHERE s.platform = ?
			  AND (s.next_check_at <= now() OR s.menu_dirty_since <= now() - make_interval(secs => ?))
			ORDER BY LEAST(COALESCE(s.next_check_at, 'infinity'), COALESCE(s.menu_dirty_since, 'infinity'))
			LIMIT ?
			FOR UPDATE OF s SKIP LOCKED`, platformDeliveroo, menuQuiet.Seconds(), limit)
		if err != nil {
			return err
		}
		for rows.Next() {
			var st SyncState
			var dirty, refreshed sql.NullTime
			var hours []byte
			if err := rows.Scan(&st.MerchantID, &st.LocationID, &st.BrandID, &st.CheckKind, &st.Attempts, &dirty, &hours, &st.TimeZone, &refreshed); err != nil {
				rows.Close()
				return err
			}
			if dirty.Valid {
				t := dirty.Time
				st.MenuDirtySince = &t
			}
			if refreshed.Valid {
				st.OpeningRefreshedAt = refreshed.Time
			}
			if len(hours) > 0 {
				if err := json.Unmarshal(hours, &st.OpeningHours); err != nil {
					st.OpeningHours = nil // horaires illisibles : relus au prochain tour
					st.OpeningRefreshedAt = time.Time{}
				}
			}
			out = append(out, st)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()

		for _, st := range out {
			if _, err := db.ExecContext(txCtx, `
				UPDATE marketplace_sync_state SET next_check_at = now() + make_interval(secs => ?)
				WHERE platform = ? AND merchant_id = ? AND location_id = ?`,
				syncLease.Seconds(), platformDeliveroo, st.MerchantID, st.LocationID); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

// GetSyncState relit la ligne d'un compte (horaires, fuseau, marqueur de menu).
func (r *DeliverooRepository) GetSyncState(ctx context.Context, merchantID, locationID string) (SyncState, error) {
	db := dbx.GetDB(ctx, r.database)
	st := SyncState{PublishedAccount: PublishedAccount{MerchantID: merchantID, LocationID: locationID}}
	var dirty, refreshed sql.NullTime
	var hours []byte
	err := db.QueryRowContext(ctx, `
		SELECT COALESCE(check_kind, ''), check_attempts, menu_dirty_since, opening_hours, COALESCE(time_zone, ''), opening_refreshed_at
		FROM marketplace_sync_state WHERE platform = ? AND merchant_id = ? AND location_id = ?`,
		platformDeliveroo, merchantID, locationID).Scan(&st.CheckKind, &st.Attempts, &dirty, &hours, &st.TimeZone, &refreshed)
	if err != nil {
		return st, err
	}
	if dirty.Valid {
		t := dirty.Time
		st.MenuDirtySince = &t
	}
	if refreshed.Valid {
		st.OpeningRefreshedAt = refreshed.Time
	}
	if len(hours) > 0 {
		_ = json.Unmarshal(hours, &st.OpeningHours)
	}
	return st, nil
}

// DropSyncState retire un compte de la file (intégration désactivée).
func (r *DeliverooRepository) DropSyncState(ctx context.Context, merchantID, locationID string) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
		UPDATE marketplace_sync_state SET next_check_at = NULL, menu_dirty_since = NULL
		WHERE platform = ? AND merchant_id = ? AND location_id = ?`, platformDeliveroo, merchantID, locationID)
	return err
}

// GetMerchantTimezone : fuseau de l'établissement, repli quand Deliveroo n'en
// donne pas.
func (r *DeliverooRepository) GetMerchantTimezone(ctx context.Context, merchantID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)
	var tz sql.NullString
	err := db.QueryRowContext(ctx, `SELECT timezone FROM merchant WHERE id::text = ?`, merchantID).Scan(&tz)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return tz.String, err
}

// LastPublishedItemIDs : articles produits du dernier menu en ligne (publication
// réussie ou non confirmée) ; vide si rien n'a encore été publié.
func (r *DeliverooRepository) LastPublishedItemIDs(ctx context.Context, merchantID, locationID string) ([]string, error) {
	db := dbx.GetDB(ctx, r.database)
	var raw []byte
	err := db.QueryRowContext(ctx, `
		SELECT item_ids FROM marketplace_menu_syncs
		WHERE platform = ? AND merchant_id = ? AND location_id = ? AND status IN ('success', 'unconfirmed') AND item_ids IS NOT NULL
		ORDER BY id DESC LIMIT 1`, platformDeliveroo, merchantID, locationID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil, fmt.Errorf("marketplace_menu_syncs.item_ids illisible: %w", err)
	}
	return ids, nil
}

// ListStaleProcessing : publications en attente de résultat depuis plus de
// olderThan (CreatedAt porte la date d'envoi).
func (r *DeliverooRepository) ListStaleProcessing(ctx context.Context, olderThan time.Duration, limit int) ([]SyncRecord, error) {
	db := dbx.GetDB(ctx, r.database)
	rows, err := db.QueryContext(ctx, `
		SELECT id, merchant_id, location_id, COALESCE(brand_id, ''), menu_id, origin, api_version, COALESCE(job_id, ''), COALESCE(item_count, 0), sent_at
		FROM marketplace_menu_syncs
		WHERE platform = ? AND status = 'processing' AND sent_at < now() - make_interval(secs => ?)
		ORDER BY id LIMIT ?`, platformDeliveroo, olderThan.Seconds(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncRecord
	for rows.Next() {
		var rec SyncRecord
		if err := rows.Scan(&rec.ID, &rec.MerchantID, &rec.LocationID, &rec.BrandID, &rec.MenuID, &rec.Origin, &rec.APIVersion, &rec.JobID, &rec.ItemCount, &rec.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}
