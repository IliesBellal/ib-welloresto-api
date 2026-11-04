package ubereats

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

// Accès base de la publication du menu Uber Eats : journal commun
// marketplace_menu_syncs (migration 201), file de réveils commune
// marketplace_sync_state (202), état envoyé par article marketplace_item_states
// (203) et tables de correspondance de l'import des commandes.

const platformUberEats = string(marketplacecatalog.UberEats)

// Statuts du journal. Uber répond de façon synchrone : pas de « processing ».
const (
	syncSuccess   = "success"
	syncFailed    = "failed"
	syncUnchanged = "unchanged"
)

// uberAPIVersion : valeur de marketplace_menu_syncs.api_version.
const uberAPIVersion = "v2"

// syncLease : durée pendant laquelle un compte pris en charge par un worker
// n'est pas repris.
const syncLease = 5 * time.Minute

// Types de réveil (marketplace_sync_state.check_kind).
const (
	checkChanged = "changed" // un statut ou le catalogue vient de changer
	checkSafety  = "safety"  // filet de sécurité
	checkRetry   = "retry"   // nouvelle tentative après un échec
	checkUber    = "uber"    // demande d'Uber Eats (store.menu_refresh_request)
)

// SyncRecord est une ligne du journal.
type SyncRecord struct {
	ID          int64
	MerchantID  string
	StoreID     string
	Origin      string
	Status      string
	Fingerprint string
	ItemCount   int
	ItemIDs     []string
	HTTPStatus  int
	Error       string
	Warnings    []string
}

// LoadProductExtras lit les colonnes produit propres aux plateformes que le
// chargement commun du menu ne lit pas. Échoue explicitement si la migration 200
// (contains_alcohol) n'est pas jouée.
func (r *UberRepository) LoadProductExtras(ctx context.Context, merchantID string) (map[string]marketplacecatalog.ProductExtras, error) {
	db := dbx.GetDB(ctx, r.database)
	rows, err := db.QueryContext(ctx, `
		SELECT CAST(product_id AS TEXT), price_uber_eats, contains_alcohol
		FROM products
		WHERE merchant_id = ? AND enabled = TRUE`, merchantID)
	if err != nil {
		return nil, fmt.Errorf("ubereats menu: lecture prix Uber Eats / alcool (migration 200 jouée ?): %w", err)
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

// GetMerchantName : nom affiché de l'établissement ("" si inconnu).
func (r *UberRepository) GetMerchantName(ctx context.Context, merchantID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)
	var name sql.NullString
	err := db.QueryRowContext(ctx, `SELECT fullname FROM merchant WHERE id::text = ?`, merchantID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return name.String, err
}

// LastSuccessfulFingerprint : empreinte du dernier menu publié avec succès ("" si aucun).
func (r *UberRepository) LastSuccessfulFingerprint(ctx context.Context, merchantID, storeID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)
	var fp sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT fingerprint FROM marketplace_menu_syncs
		WHERE platform = ? AND merchant_id = ? AND location_id = ? AND status = ?
		ORDER BY id DESC LIMIT 1`, platformUberEats, merchantID, storeID, syncSuccess).Scan(&fp)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return fp.String, err
}

// HasPublished : ce compte a-t-il déjà reçu un menu de WelloResto ?
func (r *UberRepository) HasPublished(ctx context.Context, merchantID, storeID string) (bool, error) {
	fp, err := r.LastSuccessfulFingerprint(ctx, merchantID, storeID)
	return fp != "", err
}

// InsertSync crée la ligne de journal d'une tentative.
func (r *UberRepository) InsertSync(ctx context.Context, rec SyncRecord) (int64, error) {
	db := dbx.GetDB(ctx, r.database)
	ids, err := json.Marshal(rec.ItemIDs)
	if err != nil {
		return 0, err
	}
	return db.InsertReturningID(ctx, `
		INSERT INTO marketplace_menu_syncs
			(platform, merchant_id, location_id, menu_id, origin, api_version, status, fingerprint, item_count, item_ids, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, CASE WHEN ? THEN now() END)`,
		"id",
		platformUberEats, rec.MerchantID, rec.StoreID, PublishedMenuID, rec.Origin, uberAPIVersion,
		rec.Status, rec.Fingerprint, rec.ItemCount, string(ids), rec.Status == syncUnchanged)
}

// FinishSync enregistre la réponse d'Uber Eats (succès ou échec) : l'envoi est
// synchrone, la ligne est close tout de suite. sent_at n'est posé que si la
// requête a réellement atteint Uber (httpStatus non nul).
func (r *UberRepository) FinishSync(ctx context.Context, id int64, status string, httpStatus int, warnings []string, errText string) error {
	db := dbx.GetDB(ctx, r.database)
	var w any
	if len(warnings) > 0 {
		raw, err := json.Marshal(warnings)
		if err != nil {
			return err
		}
		w = string(raw)
	}
	var hs any
	if httpStatus != 0 {
		hs = httpStatus
	}
	_, err := db.ExecContext(ctx, `
		UPDATE marketplace_menu_syncs
		SET status = ?, http_status = ?, warnings = ?::jsonb, error = NULLIF(?, ''),
		    sent_at = CASE WHEN ? THEN now() ELSE sent_at END, finished_at = now()
		WHERE id = ?`,
		status, hs, w, errText, httpStatus != 0, id)
	return err
}

// LastPublishedItemIDs : produits du dernier menu publié avec succès ; vide si
// rien n'a encore été publié.
func (r *UberRepository) LastPublishedItemIDs(ctx context.Context, merchantID, storeID string) ([]string, error) {
	db := dbx.GetDB(ctx, r.database)
	var raw []byte
	err := db.QueryRowContext(ctx, `
		SELECT item_ids FROM marketplace_menu_syncs
		WHERE platform = ? AND merchant_id = ? AND location_id = ? AND status = ? AND item_ids IS NOT NULL
		ORDER BY id DESC LIMIT 1`, platformUberEats, merchantID, storeID, syncSuccess).Scan(&raw)
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

// UpsertPublishedMappings fait correspondre chaque article publié à son
// produit / son option / son groupe, pour que l'import des commandes retrouve
// le bon produit au lieu d'en créer un nouveau. Le menu publié fait foi : une
// ancienne correspondance du même article vers un autre produit est réécrite.
//
//   - produits : item_id = identifiant produit ;
//   - options  : item_id = « opt_<id> » (clé unique merchant_id + item_id) ;
//   - groupes  : modifier_group_id = « mg_<id> » (clé unique merchant_id + groupe).
func (r *UberRepository) UpsertPublishedMappings(ctx context.Context, merchantID, storeID string, b *BuiltMenu) error {
	return dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, r.database)

		for productID := range b.ProductNames {
			res, err := db.ExecContext(txCtx, `
				UPDATE integration_uber_eats_products_mapping
				SET product_id = ?, store_id = ?, enabled = true, deletion_date = NULL
				WHERE merchant_id = ? AND item_id = ?`,
				productID, storeID, merchantID, productID)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				if _, err := db.ExecContext(txCtx, `
					INSERT INTO integration_uber_eats_products_mapping (item_id, product_id, merchant_id, store_id, enabled)
					VALUES (?, ?, ?, ?, true)`, productID, productID, merchantID, storeID); err != nil {
					return err
				}
			}
		}

		for groupItemID, attributeID := range b.GroupItems {
			if _, err := db.ExecContext(txCtx, `
				INSERT INTO integration_uber_eats_attributes_mapping (merchant_id, configurable_attribute_id, modifier_group_id, store_id, enabled)
				VALUES (?, ?, ?, ?, true)
				ON CONFLICT (merchant_id, modifier_group_id)
				DO UPDATE SET configurable_attribute_id = EXCLUDED.configurable_attribute_id, store_id = EXCLUDED.store_id, enabled = true`,
				merchantID, attributeID, groupItemID, storeID); err != nil {
				return fmt.Errorf("groupes d'options (migration 197 jouée ?): %w", err)
			}
		}

		for itemID, optionID := range b.OptionItems {
			if _, err := db.ExecContext(txCtx, `
				INSERT INTO integration_uber_eats_options_mapping (merchant_id, configurable_attribute_option_id, item_id, store_id, enabled)
				VALUES (?, ?, ?, ?, true)
				ON CONFLICT (merchant_id, item_id)
				DO UPDATE SET configurable_attribute_option_id = EXCLUDED.configurable_attribute_option_id, store_id = EXCLUDED.store_id, enabled = true`,
				merchantID, optionID, itemID, storeID); err != nil {
				return err
			}
		}
		return nil
	})
}

// MarkAccountSynced met à jour le résumé affiché de l'intégration.
func (r *UberRepository) MarkAccountSynced(ctx context.Context, merchantID, storeID string, items int) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
		UPDATE integration_uber_eats SET last_sync = now(), synced_items = ?
		WHERE merchant_id = ? AND store_id = ?`, items, merchantID, storeID)
	return err
}

// --- état envoyé par article (marketplace_item_states) ---------------------

// ReplaceItemStates remplace l'état mémorisé du compte par celui du menu qui
// vient d'être envoyé (suspensions du document comprises).
func (r *UberRepository) ReplaceItemStates(ctx context.Context, merchantID, storeID string, states map[string]marketplacecatalog.Availability) error {
	return dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, r.database)
		if _, err := db.ExecContext(txCtx, `
			DELETE FROM marketplace_item_states WHERE platform = ? AND merchant_id = ? AND location_id = ?`,
			platformUberEats, merchantID, storeID); err != nil {
			return err
		}
		for id, st := range states {
			if _, err := db.ExecContext(txCtx, `
				INSERT INTO marketplace_item_states (platform, merchant_id, location_id, item_id, state)
				VALUES (?, ?, ?, ?, ?)`, platformUberEats, merchantID, storeID, id, string(st)); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetItemState mémorise l'état qu'on vient d'envoyer pour un article.
func (r *UberRepository) SetItemState(ctx context.Context, merchantID, storeID, itemID string, st marketplacecatalog.Availability) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
		INSERT INTO marketplace_item_states (platform, merchant_id, location_id, item_id, state)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (platform, merchant_id, location_id, item_id)
		DO UPDATE SET state = EXCLUDED.state, updated_at = now()`,
		platformUberEats, merchantID, storeID, itemID, string(st))
	return err
}

// GetItemStates renvoie l'état envoyé de chaque article du compte.
func (r *UberRepository) GetItemStates(ctx context.Context, merchantID, storeID string) (map[string]marketplacecatalog.Availability, error) {
	db := dbx.GetDB(ctx, r.database)
	rows, err := db.QueryContext(ctx, `
		SELECT item_id, state FROM marketplace_item_states
		WHERE platform = ? AND merchant_id = ? AND location_id = ?`, platformUberEats, merchantID, storeID)
	if err != nil {
		return nil, fmt.Errorf("état envoyé illisible (migration 203 jouée ?): %w", err)
	}
	defer rows.Close()
	out := map[string]marketplacecatalog.Availability{}
	for rows.Next() {
		var id, st string
		if err := rows.Scan(&id, &st); err != nil {
			return nil, err
		}
		out[id] = marketplacecatalog.Availability(st)
	}
	return out, rows.Err()
}

// --- file de réveils (marketplace_sync_state) ------------------------------

// PublishedAccount : un compte Uber Eats déjà publié, donc suivi par la file.
type PublishedAccount struct {
	MerchantID string
	StoreID    string
}

// SyncState est une ligne de la file de réveils.
type SyncState struct {
	PublishedAccount
	CheckKind      string
	MenuDirtySince *time.Time
}

// EnsureSyncState inscrit un compte publié dans la file (premier réveil dans
// 2 heures : le filet de sécurité). Sans effet s'il y est déjà.
func (r *UberRepository) EnsureSyncState(ctx context.Context, merchantID, storeID string) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
		INSERT INTO marketplace_sync_state (platform, merchant_id, location_id, next_check_at, check_kind)
		VALUES (?, ?, ?, now() + interval '2 hours', ?)
		ON CONFLICT (platform, merchant_id, location_id) DO NOTHING`,
		platformUberEats, merchantID, storeID, checkSafety)
	return err
}

// GetPublishedAccounts : comptes Uber Eats actifs de l'établissement déjà
// publiés. Vide pour un établissement sans Uber Eats.
func (r *UberRepository) GetPublishedAccounts(ctx context.Context, merchantID string) ([]PublishedAccount, error) {
	db := dbx.GetDB(ctx, r.database)
	rows, err := db.QueryContext(ctx, `
		SELECT s.merchant_id, s.location_id
		FROM marketplace_sync_state s
		JOIN integration_uber_eats i ON i.merchant_id = s.merchant_id AND i.store_id = s.location_id
		WHERE s.platform = ? AND s.merchant_id = ? AND i.enabled = TRUE
		ORDER BY s.location_id`, platformUberEats, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PublishedAccount
	for rows.Next() {
		var a PublishedAccount
		if err := rows.Scan(&a.MerchantID, &a.StoreID); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// MarkMenuDirty note que le catalogue a changé : le menu sera recalculé et
// republié s'il a vraiment changé, après quelques minutes de calme.
func (r *UberRepository) MarkMenuDirty(ctx context.Context, merchantID string) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
		UPDATE marketplace_sync_state SET menu_dirty_since = COALESCE(menu_dirty_since, now())
		WHERE platform = ? AND merchant_id = ?`, platformUberEats, merchantID)
	return err
}

// ClearMenuDirty efface le marqueur, sauf si un changement plus récent que since
// est arrivé entre-temps.
func (r *UberRepository) ClearMenuDirty(ctx context.Context, merchantID, storeID string, since time.Time) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
		UPDATE marketplace_sync_state SET menu_dirty_since = NULL
		WHERE platform = ? AND merchant_id = ? AND location_id = ? AND menu_dirty_since <= ?`,
		platformUberEats, merchantID, storeID, since)
	return err
}

// RequestCheckSoon avance le prochain réveil (ne le recule jamais).
func (r *UberRepository) RequestCheckSoon(ctx context.Context, merchantID, storeID string, delay time.Duration, kind string) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
		UPDATE marketplace_sync_state
		SET next_check_at = now() + make_interval(secs => ?), check_kind = ?
		WHERE platform = ? AND merchant_id = ? AND location_id = ?
		  AND (next_check_at IS NULL OR next_check_at > now() + make_interval(secs => ?))`,
		delay.Seconds(), kind, platformUberEats, merchantID, storeID, delay.Seconds())
	return err
}

// ScheduleCheck fixe le prochain réveil d'un compte.
func (r *UberRepository) ScheduleCheck(ctx context.Context, merchantID, storeID string, at time.Time, kind, errText string) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
		UPDATE marketplace_sync_state
		SET next_check_at = ?, check_kind = ?, last_check_at = now(), last_error = NULLIF(?, '')
		WHERE platform = ? AND merchant_id = ? AND location_id = ?`,
		at, kind, errText, platformUberEats, merchantID, storeID)
	return err
}

// ClaimDue prend en charge les comptes à réveiller : réveil échu, ou menu modifié
// depuis plus de menuQuiet. Un bail de syncLease évite qu'un autre tour (ou une
// autre instance) les reprenne pendant leur traitement.
func (r *UberRepository) ClaimDue(ctx context.Context, menuQuiet time.Duration, limit int) ([]SyncState, error) {
	var out []SyncState
	err := dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, r.database)
		rows, err := db.QueryContext(txCtx, `
			SELECT s.merchant_id, s.location_id, COALESCE(s.check_kind, ''), s.menu_dirty_since
			FROM marketplace_sync_state s
			WHERE s.platform = ?
			  AND (s.next_check_at <= now() OR s.menu_dirty_since <= now() - make_interval(secs => ?))
			ORDER BY LEAST(COALESCE(s.next_check_at, 'infinity'), COALESCE(s.menu_dirty_since, 'infinity'))
			LIMIT ?
			FOR UPDATE OF s SKIP LOCKED`, platformUberEats, menuQuiet.Seconds(), limit)
		if err != nil {
			return err
		}
		for rows.Next() {
			var st SyncState
			var dirty sql.NullTime
			if err := rows.Scan(&st.MerchantID, &st.StoreID, &st.CheckKind, &dirty); err != nil {
				rows.Close()
				return err
			}
			if dirty.Valid {
				t := dirty.Time
				st.MenuDirtySince = &t
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
				syncLease.Seconds(), platformUberEats, st.MerchantID, st.StoreID); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

// DropSyncState retire un compte de la file (intégration désactivée).
func (r *UberRepository) DropSyncState(ctx context.Context, merchantID, storeID string) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
		UPDATE marketplace_sync_state SET next_check_at = NULL, menu_dirty_since = NULL
		WHERE platform = ? AND merchant_id = ? AND location_id = ?`, platformUberEats, merchantID, storeID)
	return err
}

// SyncSummary est une ligne du journal telle qu'affichée au restaurateur.
type SyncSummary struct {
	ID         int64           `json:"id"`
	StoreID    string          `json:"store_id"`
	Origin     string          `json:"origin"`
	Status     string          `json:"status"`
	ItemCount  int             `json:"item_count"`
	HTTPStatus *int            `json:"http_status,omitempty"`
	Error      string          `json:"error,omitempty"`
	Warnings   json.RawMessage `json:"warnings,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
}

// ListSyncs renvoie les dernières publications de l'établissement, de la plus
// récente à la plus ancienne. storeID vide = tous ses comptes Uber Eats.
func (r *UberRepository) ListSyncs(ctx context.Context, merchantID, storeID string, limit int) ([]SyncSummary, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	db := dbx.GetDB(ctx, r.database)
	rows, err := db.QueryContext(ctx, `
		SELECT id, location_id, origin, status, item_count, http_status, COALESCE(error, ''), warnings, created_at, finished_at
		FROM marketplace_menu_syncs
		WHERE platform = ? AND merchant_id = ? AND (? = '' OR location_id = ?)
		ORDER BY id DESC LIMIT ?`, platformUberEats, merchantID, storeID, storeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []SyncSummary{}
	for rows.Next() {
		var s SyncSummary
		var httpStatus sql.NullInt64
		var finished sql.NullTime
		var warnings []byte
		if err := rows.Scan(&s.ID, &s.StoreID, &s.Origin, &s.Status, &s.ItemCount, &httpStatus, &s.Error, &warnings, &s.CreatedAt, &finished); err != nil {
			return nil, err
		}
		if httpStatus.Valid {
			v := int(httpStatus.Int64)
			s.HTTPStatus = &v
		}
		if finished.Valid {
			t := finished.Time
			s.FinishedAt = &t
		}
		s.Warnings = warnings
		out = append(out, s)
	}
	return out, rows.Err()
}

// AccountEnabled : le compte Uber Eats est-il toujours connecté et actif ?
func (r *UberRepository) AccountEnabled(ctx context.Context, merchantID, storeID string) (bool, error) {
	db := dbx.GetDB(ctx, r.database)
	var enabled bool
	err := db.QueryRowContext(ctx, `
		SELECT enabled FROM integration_uber_eats WHERE merchant_id = ? AND store_id = ?`, merchantID, storeID).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return enabled, err
}
