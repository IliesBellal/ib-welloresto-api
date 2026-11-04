package deliveroo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/modules/marketplacecatalog"
	"welloresto-api/internal/utils/dbutils"
)

// Accès base de la publication des menus (journal marketplace_menu_syncs,
// tables de correspondance de l'import des commandes). Voir
// docs/deliveroo-menu-sync.md.

const platformDeliveroo = string(marketplacecatalog.Deliveroo)

// Statuts du journal marketplace_menu_syncs.
const (
	syncSending    = "sending"
	syncProcessing = "processing"
	syncSuccess    = "success"
	syncFailed     = "failed"
	syncUnchanged  = "unchanged"
)

// SyncRecord est une ligne du journal.
type SyncRecord struct {
	ID          int64
	MerchantID  string
	LocationID  string
	BrandID     string
	MenuID      string
	Origin      string
	APIVersion  string
	Status      string
	Fingerprint string
	ItemCount   int
	ItemIDs     []string
	JobID       string
	CreatedAt   time.Time
}

// LoadProductExtras lit les colonnes produit propres aux plateformes, que le
// chargement commun du menu ne lit pas (voir marketplacecatalog.ProductExtras).
// Échoue explicitement si la migration 200 (contains_alcohol) n'est pas jouée.
func (r *DeliverooRepository) LoadProductExtras(ctx context.Context, merchantID string) (map[string]marketplacecatalog.ProductExtras, error) {
	db := dbx.GetDB(ctx, r.database)
	rows, err := db.QueryContext(ctx, `
		SELECT CAST(product_id AS TEXT), price_deliveroo, contains_alcohol
		FROM products
		WHERE merchant_id = ? AND enabled = TRUE`, merchantID)
	if err != nil {
		return nil, fmt.Errorf("deliveroo menu: lecture prix Deliveroo / alcool (migration 200 jouée ?): %w", err)
	}
	defer rows.Close()

	out := map[string]marketplacecatalog.ProductExtras{}
	for rows.Next() {
		var id string
		var price sql.NullInt64
		var alcohol bool
		if err := rows.Scan(&id, &price, &alcohol); err != nil {
			return nil, err
		}
		ex := marketplacecatalog.ProductExtras{ContainsAlcohol: alcohol}
		if price.Valid {
			v := price.Int64
			ex.PlatformPriceCents = &v
		}
		out[id] = ex
	}
	return out, rows.Err()
}

// GetBannerURL : bannière ScanNOrder de l'établissement ("" si aucune).
func (r *DeliverooRepository) GetBannerURL(ctx context.Context, merchantID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)
	var u sql.NullString
	err := db.QueryRowContext(ctx, `SELECT banner_url FROM scannorder_settings WHERE merchant_id = ? LIMIT 1`, merchantID).Scan(&u)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return u.String, nil
}

// GetMerchantName : nom affiché de l'établissement ("" si inconnu).
func (r *DeliverooRepository) GetMerchantName(ctx context.Context, merchantID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)
	var name sql.NullString
	err := db.QueryRowContext(ctx, `SELECT fullname FROM merchant WHERE id::text = ?`, merchantID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return name.String, nil
}

// LastSuccessfulFingerprint : empreinte du dernier menu publié avec succès sur
// ce compte ("" si aucun).
func (r *DeliverooRepository) LastSuccessfulFingerprint(ctx context.Context, merchantID, locationID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)
	var fp sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT fingerprint FROM marketplace_menu_syncs
		WHERE platform = ? AND merchant_id = ? AND location_id = ? AND status = ?
		ORDER BY id DESC LIMIT 1`, platformDeliveroo, merchantID, locationID, syncSuccess).Scan(&fp)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return fp.String, nil
}

// LastSentAt : date du dernier envoi à Deliveroo pour ce compte (zéro si aucun).
func (r *DeliverooRepository) LastSentAt(ctx context.Context, merchantID, locationID string) (time.Time, error) {
	db := dbx.GetDB(ctx, r.database)
	var t sql.NullTime
	err := db.QueryRowContext(ctx, `
		SELECT max(sent_at) FROM marketplace_menu_syncs
		WHERE platform = ? AND merchant_id = ? AND location_id = ?`, platformDeliveroo, merchantID, locationID).Scan(&t)
	if err != nil {
		return time.Time{}, err
	}
	return t.Time, nil
}

// InsertSync crée la ligne de journal d'une tentative de publication.
func (r *DeliverooRepository) InsertSync(ctx context.Context, rec SyncRecord) (int64, error) {
	db := dbx.GetDB(ctx, r.database)
	ids, err := json.Marshal(rec.ItemIDs)
	if err != nil {
		return 0, err
	}
	return db.InsertReturningID(ctx, `
		INSERT INTO marketplace_menu_syncs
			(platform, merchant_id, location_id, brand_id, menu_id, origin, api_version, status, fingerprint, item_count, item_ids, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, CASE WHEN ? THEN now() END)`,
		"id",
		platformDeliveroo, rec.MerchantID, rec.LocationID, rec.BrandID, rec.MenuID, rec.Origin, rec.APIVersion,
		rec.Status, rec.Fingerprint, rec.ItemCount, string(ids), rec.Status == syncUnchanged)
}

// MarkSyncSent enregistre la réponse de Deliveroo à l'envoi. Ne touche qu'une
// ligne encore en 'sending' : un webhook arrivé entre-temps a le dernier mot.
func (r *DeliverooRepository) MarkSyncSent(ctx context.Context, id int64, status string, httpStatus int, jobID string, warnings []string, errText string) error {
	db := dbx.GetDB(ctx, r.database)
	w, err := jsonOrNull(warnings)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		UPDATE marketplace_menu_syncs
		SET status = ?, http_status = ?, job_id = NULLIF(?, ''), warnings = ?::jsonb, error = NULLIF(?, ''),
		    sent_at = now(),
		    finished_at = CASE WHEN ? IN ('success', 'failed') THEN now() ELSE finished_at END
		WHERE id = ? AND status = 'sending'`,
		status, nullableInt(httpStatus), jobID, w, errText, status, id)
	return err
}

// FindPendingSync : dernière publication en attente de résultat pour ce site
// et ce menu (nil si aucune).
func (r *DeliverooRepository) FindPendingSync(ctx context.Context, locationID, menuID string) (*SyncRecord, error) {
	db := dbx.GetDB(ctx, r.database)
	var rec SyncRecord
	var brand, fp, job sql.NullString
	var ids []byte
	err := db.QueryRowContext(ctx, `
		SELECT id, merchant_id, location_id, brand_id, menu_id, origin, api_version, status, fingerprint, item_count, item_ids, job_id, created_at
		FROM marketplace_menu_syncs
		WHERE platform = ? AND location_id = ? AND menu_id = ? AND status IN ('sending', 'processing')
		ORDER BY id DESC LIMIT 1`, platformDeliveroo, locationID, menuID).
		Scan(&rec.ID, &rec.MerchantID, &rec.LocationID, &brand, &rec.MenuID, &rec.Origin, &rec.APIVersion, &rec.Status, &fp, &rec.ItemCount, &ids, &job, &rec.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rec.BrandID, rec.Fingerprint, rec.JobID = brand.String, fp.String, job.String
	if len(ids) > 0 {
		if err := json.Unmarshal(ids, &rec.ItemIDs); err != nil {
			return nil, fmt.Errorf("marketplace_menu_syncs.item_ids illisible: %w", err)
		}
	}
	return &rec, nil
}

// FinishSync enregistre le résultat final (webhook ou job).
func (r *DeliverooRepository) FinishSync(ctx context.Context, id int64, status string, httpStatus int, uploadErrors any, errText string) error {
	db := dbx.GetDB(ctx, r.database)
	ue, err := jsonOrNull(uploadErrors)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		UPDATE marketplace_menu_syncs
		SET status = ?, http_status = COALESCE(?, http_status), upload_errors = ?::jsonb,
		    error = COALESCE(NULLIF(?, ''), error), finished_at = now()
		WHERE id = ?`,
		status, nullableInt(httpStatus), ue, errText, id)
	return err
}

// SetAvailabilityResult enregistre l'alignement des disponibilités qui suit
// une publication réussie.
func (r *DeliverooRepository) SetAvailabilityResult(ctx context.Context, id int64, errText string) error {
	db := dbx.GetDB(ctx, r.database)
	status := syncSuccess
	if errText != "" {
		status = syncFailed
	}
	_, err := db.ExecContext(ctx, `
		UPDATE marketplace_menu_syncs
		SET availability_status = ?, availability_error = NULLIF(?, ''), availability_applied_at = now()
		WHERE id = ?`, status, errText, id)
	return err
}

// MarkAccountSynced met à jour le résumé affiché de l'intégration.
func (r *DeliverooRepository) MarkAccountSynced(ctx context.Context, merchantID, locationID string, items int) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
		UPDATE integration_deliveroo SET last_sync = now(), synced_items = ?
		WHERE merchant_id = ? AND location_id = ?`, items, merchantID, locationID)
	return err
}

// UpsertPublishedMappings fait correspondre chaque article publié à son
// produit / son option, pour que l'import des commandes (pos_item_id) retrouve
// le bon produit au lieu d'en créer un nouveau.
//
//   - produits : item_id = identifiant produit. Toute autre correspondance
//     active du même item_id vers un autre produit est désactivée (le menu
//     publié fait foi) ;
//   - options : item_id = "opt-<id>", clé unique (merchant_id, item_id).
func (r *DeliverooRepository) UpsertPublishedMappings(ctx context.Context, merchantID, locationID string, productNames map[string]string, optionItems map[string]string) error {
	return dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, r.database)
		for productID, name := range productNames {
			if _, err := db.ExecContext(txCtx, `
				UPDATE integration_deliveroo_products_mapping
				SET enabled = false, deletion_date = now()
				WHERE merchant_id = ? AND item_id = ? AND product_id <> ? AND enabled = true`,
				merchantID, productID, productID); err != nil {
				return err
			}
			if _, err := db.ExecContext(txCtx, `
				INSERT INTO integration_deliveroo_products_mapping (item_id, item_name, product_id, merchant_id, location_id, enabled)
				VALUES (?, ?, ?, ?, ?, true)
				ON CONFLICT (item_id, product_id, merchant_id)
				DO UPDATE SET item_name = EXCLUDED.item_name, location_id = EXCLUDED.location_id, enabled = true, deletion_date = NULL`,
				productID, name, productID, merchantID, locationID); err != nil {
				return err
			}
		}
		for itemID, optionID := range optionItems {
			if _, err := db.ExecContext(txCtx, `
				INSERT INTO integration_deliveroo_options_mapping (merchant_id, configurable_attribute_option_id, item_id, location_id, enabled)
				VALUES (?, ?, ?, ?, true)
				ON CONFLICT (merchant_id, item_id)
				DO UPDATE SET configurable_attribute_option_id = EXCLUDED.configurable_attribute_option_id,
				              location_id = EXCLUDED.location_id, enabled = true`,
				merchantID, optionID, itemID, locationID); err != nil {
				return err
			}
		}
		return nil
	})
}

func jsonOrNull(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	if s, ok := v.([]string); ok && len(s) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if string(b) == "null" {
		return nil, nil
	}
	return string(b), nil
}

func nullableInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}
