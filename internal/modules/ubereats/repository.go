package ubereats

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/modules/distributiontime"
	"welloresto-api/internal/database/dbx"
)

type UberRepository struct {
	database *sql.DB
}

func NewUberEatsRepository(db *sql.DB) *UberRepository {
	return &UberRepository{database: db}
}

// ueMerchantJoinCast retourne le fragment de jointure merchant.id (integer)
// vs iue.merchant_id (varchar) selon le dialecte — MySQL coerçait, Postgres
// exige le cast (CHAR nu = char(1) en PG, d'où TEXT).
func ueMerchantJoinCast() string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "CAST(m.id AS TEXT)"
	}
	return "CAST(m.id AS CHAR)"
}

// GetMerchantIDFromStoreID récupère le merchant_id du store
func (r *UberRepository) GetMerchantIDFromStoreID(ctx context.Context, storeID string) (*string, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	query := fmt.Sprintf(`
		SELECT iue.merchant_id
		FROM integration_uber_eats iue
		INNER JOIN merchant m on %s = iue.merchant_id
		WHERE iue.store_id = ?`, ueMerchantJoinCast())

	var MerchantID string
	// Gestion des NULLs potentiels avec sql.NullInt64 si nécessaire, ici simplifié
	row := db.QueryRowContext(ctx, query, storeID)
	err := row.Scan(&MerchantID)
	if err != nil {
		log.Error("store id : " + storeID + " not found :" + err.Error())
		return nil, err
	}
	return &MerchantID, nil
}

// GetStoreData récupère les infos du magasin
func (r *UberRepository) GetStoreData(ctx context.Context, merchantID string) (*Store, error) {
	db := dbx.GetDB(ctx, r.database)

	query := fmt.Sprintf(`
       SELECT iue.merchant_id, iue.store_id, m.timezone,
              iue.estimated_preparation_time, iue.last_estimated_preparation_time,
			  iue.auto_accept_orders
       FROM integration_uber_eats iue
       INNER JOIN merchant m ON %s = iue.merchant_id
       WHERE iue.merchant_id = ?`, ueMerchantJoinCast())

	var store Store

	row := db.QueryRowContext(ctx, query, merchantID)

	err := row.Scan(
		&store.MerchantID,
		&store.StoreID,
		&store.Timezone,
		&store.EstimatedPreparationTime,
		&store.LastEstimatedPreparationTime,
		&store.AutoAcceptOrders,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return &store, nil
}

// GetCurrentToken récupère le token actuel
func (r *UberRepository) GetCurrentToken(ctx context.Context, tokenType string) (*UberToken, error) {
	db := dbx.GetDB(ctx, r.database)

	query := `SELECT access_token, expires_at FROM external_tokens WHERE token_type = ?`
	var token UberToken

	err := db.QueryRowContext(ctx, query, tokenType).Scan(&token.AccessToken, &token.ExpiresAt)

	return &token, err
}

// SaveNewToken met à jour le token
func (r *UberRepository) SaveNewToken(ctx context.Context, tokenType, accessToken string, expiresIn int) error {
	db := dbx.GetDB(ctx, r.database)

	// Upsert par dialecte (PK external_tokens.token_type) ; l'intervalle est
	// parametre : DATE_ADD(..., INTERVAL ? SECOND) n'a pas d'equivalent PG
	// direct -> `now() + (? * interval '1 second')` (pattern Tier 1 upsell).
	query := `
       INSERT INTO external_tokens (token_type, access_token, expires_at)
       VALUES (?, ?, DATE_ADD(UTC_TIMESTAMP, INTERVAL ? SECOND))
       ON DUPLICATE KEY UPDATE expires_at = DATE_ADD(UTC_TIMESTAMP, INTERVAL ? SECOND), access_token = ?`
	if dbx.ActiveDialect() == dbx.Postgres {
		query = `
       INSERT INTO external_tokens (token_type, access_token, expires_at)
       VALUES (?, ?, now() + (? * interval '1 second'))
       ON CONFLICT (token_type) DO UPDATE SET expires_at = now() + (? * interval '1 second'), access_token = ?`
	}

	_, err := db.ExecContext(ctx, query, tokenType, accessToken, expiresIn, expiresIn, accessToken)
	return err
}

// EnableIntegration insère (ou met à jour) l'intégration Uber Eats pour le marchand
func (r *UberRepository) EnableIntegration(ctx context.Context, merchantID, storeID, accessToken, refreshToken string) error {
	db := dbx.GetDB(ctx, r.database)

	// NOTE bug preexistant identique aux deux dialectes (documente, non
	// corrige) : access_token / is_active / updated_at n'existent ni dans le
	// DDL MySQL source ni dans la cible — cette requete echoue a l'execution
	// dans les deux dialectes (chemin OAuth Uber jamais fonctionnel).
	query := `
		INSERT INTO integration_uber_eats (merchant_id, store_id, access_token, refresh_token, is_active, updated_at)
		VALUES (?, ?, ?, ?, 1, NOW())
		ON DUPLICATE KEY UPDATE
			store_id = VALUES(store_id),
			access_token = VALUES(access_token),
			refresh_token = VALUES(refresh_token),
			is_active = 1,
			updated_at = NOW()`
	if dbx.ActiveDialect() == dbx.Postgres {
		query = `
		INSERT INTO integration_uber_eats (merchant_id, store_id, access_token, refresh_token, is_active, updated_at)
		VALUES (?, ?, ?, ?, 1, now())
		ON CONFLICT (merchant_id) DO UPDATE SET
			store_id = EXCLUDED.store_id,
			access_token = EXCLUDED.access_token,
			refresh_token = EXCLUDED.refresh_token,
			is_active = 1,
			updated_at = now()`
	}

	_, err := db.ExecContext(ctx, query, merchantID, storeID, accessToken, refreshToken)
	return err
}

// DisableIntegration désactive l'intégration
func (r *UberRepository) DisableIntegration(ctx context.Context, merchantID string) error {
	db := dbx.GetDB(ctx, r.database)

	// Tu peux choisir de supprimer la ligne, ou juste de la marquer inactive (souvent mieux pour les logs)
	query := `UPDATE integration_uber_eats SET is_active = 0, access_token = NULL, refresh_token = NULL WHERE merchant_id = ?`

	_, err := db.ExecContext(ctx, query, merchantID)
	return err
}

// GetOrderMetadata récupère les IDs pour la requête
func (r *UberRepository) GetOrderMetadata(ctx context.Context, localOrderID string) (*UberOrderMetadata, error) {
	db := dbx.GetDB(ctx, r.database)

	query := `
       SELECT o.brand_order_id, o.creation_date
       FROM orders o
       INNER JOIN integration_uber_eats iue on iue.merchant_id = o.merchant_id
       WHERE o.order_id = ?`

	var meta UberOrderMetadata

	err := db.QueryRowContext(ctx, query, localOrderID).Scan(&meta.BrandOrderID, &meta.CreationDate)

	if err != nil {
		return nil, fmt.Errorf("cannot retrieve uber order id: %v", err)
	}
	return &meta, nil
}

// UpdateOrderAccepted met à jour le statut local
func (r *UberRepository) UpdateOrderAccepted(ctx context.Context, orderID string, prepTime int) error {
	db := dbx.GetDB(ctx, r.database)

	estimatedReady := "DATE_ADD(UTC_TIMESTAMP, INTERVAL ? MINUTE)"
	if dbx.ActiveDialect() == dbx.Postgres {
		estimatedReady = "now() + (? * interval '1 minute')"
	}
	query := fmt.Sprintf(`
		UPDATE orders
		SET brand_status = 'ACCEPTED', merchant_approval = 'ACCEPTED',
		    last_update = %s,
		    estimated_ready = %s
		WHERE order_id = ?`, dbx.UTCNow(), estimatedReady)
	_, err := db.ExecContext(ctx, query, prepTime, orderID)
	return err
}

// CalculateAutoPrepTime calcule le temps de préparation automatique
func (r *UberRepository) CalculateAutoPrepTime(ctx context.Context, merchantID, orderID string) (int, error) {
	db := dbx.GetDB(ctx, r.database)

	// 1. Get product count
	var qty int
	err := db.QueryRowContext(ctx, "SELECT sum(quantity) FROM orderitems WHERE order_id = ?", orderID).Scan(&qty)
	if err != nil {
		return 0, err
	}

	// 2. Temps de distribution estimé (ex-procédure GET_AVERAGE_DISTRIBUTION_TIME)
	estTime, _, err := distributiontime.EstimatedSeconds(ctx, r.database, merchantID, qty)
	if err != nil {
		return 0, err
	}

	// Logique PHP: intval($data['estimated_distribution_time'])/60*0.7
	return int((float64(estTime) / 60) * 0.7), nil
}

// SetOrderStatusDenied met à jour le statut en DENIED
func (r *UberRepository) SetOrderStatusDenied(ctx context.Context, orderID string) error {
	db := dbx.GetDB(ctx, r.database)

	query := fmt.Sprintf(`UPDATE orders SET brand_status = 'DENIED', last_update = %s WHERE order_id = ?`, dbx.UTCNow())
	_, err := db.ExecContext(ctx, query, orderID)
	return err
}

// SetOrderStatusCanceled met à jour le statut en CANCELED
func (r *UberRepository) SetOrderStatusCanceled(ctx context.Context, orderID string) error {
	db := dbx.GetDB(ctx, r.database)

	query := fmt.Sprintf(`UPDATE orders SET brand_status = 'CANCELED', status = '-1', last_update = %s WHERE order_id = ?`, dbx.UTCNow())
	_, err := db.ExecContext(ctx, query, orderID)
	return err
}

// SetOrderStatusReady met à jour orders et orderitems pour le statut READY
func (r *UberRepository) SetOrderStatusReady(ctx context.Context, orderID string) error {
	db := dbx.GetDB(ctx, r.database)

	queryOrder := fmt.Sprintf(`
		UPDATE orders
		SET brand_status = 'READY_FOR_HANDOFF', status = '2', isDistributed = '1',
		    last_update = %[1]s, delivered_on = %[1]s
		WHERE order_id = ?`, dbx.UTCNow())

	queryItems := fmt.Sprintf(`
		UPDATE orderitems
		SET distributed_quantity = quantity,
		    ready_for_distribution_quantity = quantity,
		    isDistributed = '1',
		    distributed_on = %s
		WHERE order_id = ?`, dbx.UTCNow())

	if _, err := db.ExecContext(ctx, queryOrder, orderID); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, queryItems, orderID); err != nil {
		return err
	}
	return nil
}

// SyncOrderState met à jour la commande lors du finishOrderIfDoesNotExist (succès API)
func (r *UberRepository) SyncOrderState(ctx context.Context, uberOrderID, status, state, approval string, reasonID sql.NullInt64) error {
	db := dbx.GetDB(ctx, r.database)

	// Mise à jour principale Orders
	query := fmt.Sprintf(`
		UPDATE orders
		SET brand_status = ?, state = ?, merchant_approval = ?, deletion_reason_id = ?,
		    delivered_on = CASE WHEN ? = 'COMPLETED' THEN %[1]s ELSE delivered_on END,
		    last_update = %[1]s
		WHERE brand_order_id = ?`, dbx.UTCNow())

	_, err := db.ExecContext(ctx, query, status, state, approval, reasonID, status, uberOrderID)
	if err != nil {
		return err
	}

	// Si CLOSED, mise a jour orderitems. La forme MySQL multi-table modifiait
	// aussi orders.state via le `state = 'CLOSED'` non qualifie (orderitems n'a
	// pas de colonne state) — Postgres ne peut pas modifier deux tables dans un
	// UPDATE : deux requetes, meme effet.
	if state == "CLOSED" {
		if dbx.ActiveDialect() == dbx.Postgres {
			if _, err := db.ExecContext(ctx, fmt.Sprintf(`
				UPDATE orderitems
				SET distributed_on = %s,
				    isDistributed = '1'
				FROM orders o
				WHERE o.order_id = orderitems.order_id AND o.brand_order_id = ?`, dbx.UTCNow()), uberOrderID); err != nil {
				return err
			}
			if _, err := db.ExecContext(ctx, `
				UPDATE orders SET state = 'CLOSED' WHERE brand_order_id = ?`, uberOrderID); err != nil {
				return err
			}
			return nil
		}
		queryItems := `
			UPDATE orderitems
			INNER JOIN orders o on o.order_id = orderitems.order_id
			SET orderitems.distributed_on = UTC_TIMESTAMP,
			    orderitems.isDistributed = '1',
			    state = 'CLOSED'
			WHERE o.brand_order_id = ?`
		_, err := db.ExecContext(ctx, queryItems, uberOrderID)
		if err != nil {
			return err
		}
	}
	return nil
}

// HandleOrderNotFound gère le cas 404 de l'API Uber (finishOrderIfDoesNotExist)
func (r *UberRepository) HandleOrderNotFound(ctx context.Context, uberOrderID string) error {
	db := dbx.GetDB(ctx, r.database)

	query := fmt.Sprintf(`
		UPDATE orders
		SET brand_status = CASE WHEN brand_status = 'READY_FOR_HANDOFF' THEN 'CLOSED' ELSE 'CANCELED' END,
		    state = 'CLOSED',
		    last_update = %s
		WHERE brand_order_id = ?`, dbx.UTCNow())
	_, err := db.ExecContext(ctx, query, uberOrderID)
	return err
}

// UpdateBusyModeData met à jour les données de "Busy Mode"
func (r *UberRepository) UpdateBusyModeData(ctx context.Context, storeID string, delayUntil time.Time, duration int) error {
	db := dbx.GetDB(ctx, r.database)

	query := `
		UPDATE integration_uber_eats
		SET delay_until = ?, delay_duration = ?
		WHERE store_id = ?`
	_, err := db.ExecContext(ctx, query, delayUntil, duration, storeID)
	return err
}

// UpdatePreparationTime met à jour le temps de préparation estimé
func (r *UberRepository) UpdatePreparationTime(ctx context.Context, merchantID string, timeVal int, isAuto bool) error {
	db := dbx.GetDB(ctx, r.database)
	// Logique PHP :
	// estimated_preparation_time = case when :auto = 'TRUE' then estimated_preparation_time else :time end
	// last_estimated_preparation_time = :time

	// estimated_preparation_time / last_estimated_preparation_time sont des
	// colonnes varchar : pgx refuse d'y lier un int Go (MySQL coercait) —
	// meme correctif que le module integrations (Tier 2).
	timeStr := strconv.Itoa(timeVal)
	query := `
		UPDATE integration_uber_eats
		SET estimated_preparation_time = CASE WHEN ? = true THEN estimated_preparation_time ELSE ? END,
		    last_estimated_preparation_time = ?
		WHERE merchant_id = ?`

	_, err := db.ExecContext(ctx, query, isAuto, timeStr, timeStr, merchantID)
	return err
}

// UpdateStoreClosure met à jour la date de fermeture temporaire
func (r *UberRepository) UpdateStoreClosure(ctx context.Context, storeID string, closedUntil time.Time) error {
	db := dbx.GetDB(ctx, r.database)

	query := `UPDATE integration_uber_eats SET closed_until = ? WHERE store_id = ?`
	_, err := db.ExecContext(ctx, query, closedUntil, storeID)
	return err
}

// GetStoreInfoForMenu récupère ID, BasicAuth et Timezone (utilisé pour toggle item)
func (r *UberRepository) GetStoreInfoForMenu(ctx context.Context, merchantID string) (*Store, error) {
	db := dbx.GetDB(ctx, r.database)

	// Similaire à GetStoreData mais s'assure que le token n'est pas null
	query := fmt.Sprintf(`
		SELECT iue.merchant_id, iue.store_id, m.timezone, iue.bearer_token
		FROM integration_uber_eats iue
		INNER JOIN merchant m on %s = iue.merchant_id
		WHERE iue.merchant_id = ? AND iue.bearer_token IS NOT NULL`, ueMerchantJoinCast())

	var store Store
	// On ignore les champs non demandés dans le Scan
	err := db.QueryRowContext(ctx, query, merchantID).Scan(&store.MerchantID, &store.StoreID, &store.Timezone, &store.BearerToken)
	return &store, err
}
