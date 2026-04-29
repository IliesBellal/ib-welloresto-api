package ubereats

import (
	"context"
	"database/sql"
	"fmt"
	"time"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/utils/dbutils"
)

type UberRepository struct {
	database *sql.DB
}

func NewUberEatsRepository(db *sql.DB) *UberRepository {
	return &UberRepository{database: db}
}

// GetMerchantIDFromStoreID récupère le merchant_id du store
func (r *UberRepository) GetMerchantIDFromStoreID(ctx context.Context, storeID string) (*string, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	query := `
		SELECT iue.merchant_id
		FROM integration_uber_eats iue
		INNER JOIN merchant m on m.id = iue.merchant_id
		WHERE iue.store_id = ?`

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
	db := dbutils.GetDB(ctx, r.database)

	query := `
       SELECT iue.merchant_id, iue.store_id, m.timezone, 
              iue.estimated_preparation_time, iue.last_estimated_preparation_time,
			  iue.auto_accept_orders
       FROM integration_uber_eats iue
       INNER JOIN merchant m ON m.id = iue.merchant_id
       WHERE iue.merchant_id = ?`

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
	db := dbutils.GetDB(ctx, r.database)

	query := `SELECT access_token, expires_at FROM external_tokens WHERE token_type = ?`
	var token UberToken

	err := db.QueryRowContext(ctx, query, tokenType).Scan(&token.AccessToken, &token.ExpiresAt)

	return &token, err
}

// SaveNewToken met à jour le token
func (r *UberRepository) SaveNewToken(ctx context.Context, tokenType, accessToken string, expiresIn int) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `
       INSERT INTO external_tokens (token_type, access_token, expires_at)
       VALUES (?, ?, DATE_ADD(UTC_TIMESTAMP, INTERVAL ? SECOND))
       ON DUPLICATE KEY UPDATE expires_at = DATE_ADD(UTC_TIMESTAMP, INTERVAL ? SECOND), access_token = ?`

	_, err := db.ExecContext(ctx, query, tokenType, accessToken, expiresIn, expiresIn, accessToken)
	return err
}

// EnableIntegration insère (ou met à jour) l'intégration Uber Eats pour le marchand
func (r *UberRepository) EnableIntegration(ctx context.Context, merchantID, storeID, accessToken, refreshToken string) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `
		INSERT INTO integration_uber_eats (merchant_id, store_id, access_token, refresh_token, is_active, updated_at)
		VALUES (?, ?, ?, ?, 1, NOW())
		ON DUPLICATE KEY UPDATE 
			store_id = VALUES(store_id),
			access_token = VALUES(access_token),
			refresh_token = VALUES(refresh_token),
			is_active = 1,
			updated_at = NOW()`

	_, err := db.ExecContext(ctx, query, merchantID, storeID, accessToken, refreshToken)
	return err
}

// DisableIntegration désactive l'intégration
func (r *UberRepository) DisableIntegration(ctx context.Context, merchantID string) error {
	db := dbutils.GetDB(ctx, r.database)

	// Tu peux choisir de supprimer la ligne, ou juste de la marquer inactive (souvent mieux pour les logs)
	query := `UPDATE integration_uber_eats SET is_active = 0, access_token = NULL, refresh_token = NULL WHERE merchant_id = ?`

	_, err := db.ExecContext(ctx, query, merchantID)
	return err
}

// GetOrderMetadata récupère les IDs pour la requête
func (r *UberRepository) GetOrderMetadata(ctx context.Context, localOrderID string) (*UberOrderMetadata, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
       SELECT o.order_id, o.brand_order_id, o.creation_date
       FROM orders o
       INNER JOIN integration_uber_eats iue on iue.merchant_id = o.merchant_id
       WHERE o.order_id = ?`

	var meta UberOrderMetadata

	err := db.QueryRowContext(ctx, query, localOrderID).Scan(&meta.OrderID, &meta.BrandOrderID, &meta.CreationDate)

	if err != nil {
		return nil, fmt.Errorf("cannot retrieve uber order id: %v", err)
	}
	return &meta, nil
}

// UpdateOrderAccepted met à jour le statut local
func (r *UberRepository) UpdateOrderAccepted(ctx context.Context, orderID string, prepTime int) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `
		UPDATE orders
		SET brand_status = 'ACCEPTED', merchant_approval = 'ACCEPTED', 
		    last_update = UTC_TIMESTAMP, 
		    estimated_ready = DATE_ADD(UTC_TIMESTAMP, INTERVAL ? MINUTE)
		WHERE order_id = ?`
	_, err := db.ExecContext(ctx, query, prepTime, orderID)
	return err
}

// CalculateAutoPrepTime appelle la procédure stockée
func (r *UberRepository) CalculateAutoPrepTime(ctx context.Context, merchantID, orderID string) (int, error) {
	db := dbutils.GetDB(ctx, r.database)

	// 1. Get product count
	var qty int
	err := db.QueryRowContext(ctx, "SELECT sum(quantity) FROM orderitems WHERE order_id = ?", orderID).Scan(&qty)
	if err != nil {
		return 0, err
	}

	// 2. Call Procedure
	// Note: Pour récupérer le résultat d'un CALL et d'un SELECT interne, c'est parfois tricky en Go selon le driver.
	// Une approche simplifiée si la procédure fait un SELECT à la fin :
	rows, err := db.QueryContext(ctx, "CALL GET_AVERAGE_DISTRIBUTION_TIME(?, ?)", merchantID, qty)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var estTime float64
	if rows.Next() {
		if err := rows.Scan(&estTime); err != nil {
			return 0, err
		}
	}

	// Logique PHP: intval($data['estimated_distribution_time'])/60*0.7
	return int((estTime / 60) * 0.7), nil
}

// SetOrderStatusDenied met à jour le statut en DENIED
func (r *UberRepository) SetOrderStatusDenied(ctx context.Context, orderID string) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `UPDATE orders SET brand_status = 'DENIED', last_update = UTC_TIMESTAMP WHERE order_id = ?`
	_, err := db.ExecContext(ctx, query, orderID)
	return err
}

// SetOrderStatusCanceled met à jour le statut en CANCELED
func (r *UberRepository) SetOrderStatusCanceled(ctx context.Context, orderID string) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `UPDATE orders SET brand_status = 'CANCELED', status = '-1', last_update = UTC_TIMESTAMP WHERE order_id = ?`
	_, err := db.ExecContext(ctx, query, orderID)
	return err
}

// SetOrderStatusReady met à jour orders et orderitems pour le statut READY
func (r *UberRepository) SetOrderStatusReady(ctx context.Context, orderID string) error {
	db := dbutils.GetDB(ctx, r.database)

	queryOrder := `
		UPDATE orders
		SET brand_status = 'READY_FOR_HANDOFF', status = '2', isDistributed = '1', 
		    last_update = UTC_TIMESTAMP, delivered_on = UTC_TIMESTAMP
		WHERE order_id = ?`

	queryItems := `
		UPDATE orderitems
		SET distributed_quantity = quantity,
		    ready_for_distribution_quantity = quantity,
		    isDistributed = '1',
		    distributed_on = UTC_TIMESTAMP
		WHERE order_id = ?`

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
	db := dbutils.GetDB(ctx, r.database)

	// Mise à jour principale Orders
	query := `
		UPDATE orders
		SET brand_status = ?, state = ?, merchant_approval = ?, deletion_reason_id = ?,
		    delivered_on = CASE WHEN ? = 'COMPLETED' THEN UTC_TIMESTAMP ELSE delivered_on END,
		    last_update = UTC_TIMESTAMP
		WHERE brand_order_id = ?`

	_, err := db.ExecContext(ctx, query, status, state, approval, reasonID, status, uberOrderID)
	if err != nil {
		return err
	}

	// Si CLOSED, mise à jour orderitems
	if state == "CLOSED" {
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
	db := dbutils.GetDB(ctx, r.database)

	query := `
		UPDATE orders
		SET brand_status = CASE WHEN brand_status = 'READY_FOR_HANDOFF' THEN 'CLOSED' ELSE 'CANCELED' END,
		    state = 'CLOSED',
		    last_update = UTC_TIMESTAMP
		WHERE brand_order_id = ?`
	_, err := db.ExecContext(ctx, query, uberOrderID)
	return err
}

// UpdateBusyModeData met à jour les données de "Busy Mode"
func (r *UberRepository) UpdateBusyModeData(ctx context.Context, storeID string, delayUntil time.Time, duration int) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `
		UPDATE integration_uber_eats
		SET delay_until = ?, delay_duration = ?
		WHERE store_id = ?`
	_, err := db.ExecContext(ctx, query, delayUntil, duration, storeID)
	return err
}

// UpdatePreparationTime met à jour le temps de préparation estimé
func (r *UberRepository) UpdatePreparationTime(ctx context.Context, merchantID string, timeVal int, isAuto bool) error {
	db := dbutils.GetDB(ctx, r.database)
	// Logique PHP :
	// estimated_preparation_time = case when :auto = 'TRUE' then estimated_preparation_time else :time end
	// last_estimated_preparation_time = :time

	query := `
		UPDATE integration_uber_eats
		SET estimated_preparation_time = CASE WHEN ? = true THEN estimated_preparation_time ELSE ? END,
		    last_estimated_preparation_time = ?
		WHERE merchant_id = ?`

	_, err := db.ExecContext(ctx, query, isAuto, timeVal, timeVal, merchantID)
	return err
}

// UpdateStoreClosure met à jour la date de fermeture temporaire
func (r *UberRepository) UpdateStoreClosure(ctx context.Context, storeID string, closedUntil time.Time) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `UPDATE integration_uber_eats SET closed_until = ? WHERE store_id = ?`
	_, err := db.ExecContext(ctx, query, closedUntil, storeID)
	return err
}

// GetStoreInfoForMenu récupère ID, BasicAuth et Timezone (utilisé pour toggle item)
func (r *UberRepository) GetStoreInfoForMenu(ctx context.Context, merchantID string) (*Store, error) {
	db := dbutils.GetDB(ctx, r.database)

	// Similaire à GetStoreData mais s'assure que le token n'est pas null
	query := `
		SELECT iue.merchant_id, iue.store_id, m.timezone, iue.bearer_token
		FROM integration_uber_eats iue
		INNER JOIN merchant m on m.id = iue.merchant_id
		WHERE iue.merchant_id = ? AND iue.bearer_token IS NOT NULL`

	var store Store
	// On ignore les champs non demandés dans le Scan
	err := db.QueryRowContext(ctx, query, merchantID).Scan(&store.MerchantID, &store.StoreID, &store.Timezone, &store.BearerToken)
	return &store, err
}
