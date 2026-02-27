package ubereats

import (
	"database/sql"
	"fmt"
	"time"
)

type UberRepository struct {
	db *sql.DB
}

func NewUberEatsRepository(db *sql.DB) *UberRepository {
	return &UberRepository{db: db}
}

// GetMerchantIDFromStoreID récupère le merchant_id du store
func (r *UberRepository) GetMerchantIDFromStoreID(tx *sql.Tx, storeID string) (*string, error) {
	query := `
		SELECT iue.merchant_id
		FROM integration_uber_eats iue
		INNER JOIN merchant m on m.id = iue.merchant_id
		WHERE iue.store_id = ?`

	var MerchantID string
	// Gestion des NULLs potentiels avec sql.NullInt64 si nécessaire, ici simplifié
	row := tx.QueryRow(query, storeID)
	err := row.Scan(&MerchantID)
	if err != nil {
		return nil, err
	}
	return &MerchantID, nil
}

// GetStoreData récupère les infos du magasin
func (r *UberRepository) GetStoreData(tx *sql.Tx, merchantID string) (*Store, error) {
	query := `
       SELECT iue.merchant_id, iue.store_id, m.timezone, 
              iue.estimated_preparation_time, iue.last_estimated_preparation_time
       FROM integration_uber_eats iue
       INNER JOIN merchant m ON m.id = iue.merchant_id
       WHERE iue.merchant_id = ?`

	var store Store

	row := tx.QueryRow(query, merchantID)

	err := row.Scan(
		&store.MerchantID,
		&store.StoreID,
		&store.Timezone,
		&store.EstimatedPreparationTime,
		&store.LastEstimatedPreparationTime,
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
func (r *UberRepository) GetCurrentToken(tx *sql.Tx, tokenType string) (*UberToken, error) {
	query := `SELECT access_token, expires_at FROM external_tokens WHERE token_type = ?`
	var token UberToken
	// Note: MySQL datetime -> Go time.Time nécessite parseTime=true dans le driver DSN
	err := tx.QueryRow(query, tokenType).Scan(&token.AccessToken, &token.ExpiresAt)
	return &token, err
}

// SaveNewToken met à jour le token
func (r *UberRepository) SaveNewToken(tx *sql.Tx, tokenType, accessToken string, expiresIn int) error {
	query := `
		INSERT INTO external_tokens (token_type, access_token, expires_at)
		VALUES (?, ?, DATE_ADD(UTC_TIMESTAMP, INTERVAL ? SECOND))
		ON DUPLICATE KEY UPDATE expires_at = DATE_ADD(UTC_TIMESTAMP, INTERVAL ? SECOND), access_token = ?`
	_, err := tx.Exec(query, tokenType, accessToken, expiresIn, expiresIn, accessToken)
	return err
}

// GetOrderMetadata récupère les IDs pour la requête
func (r *UberRepository) GetOrderMetadata(tx *sql.Tx, localOrderID string) (*UberOrderMetadata, error) {
	query := `
		SELECT o.order_id, o.brand_order_id, o.creation_date
		FROM orders o
		INNER JOIN integration_uber_eats iue on iue.merchant_id = o.merchant_id
		WHERE o.order_id = ?`

	var meta UberOrderMetadata
	err := tx.QueryRow(query, localOrderID).Scan(&meta.OrderID, &meta.BrandOrderID, &meta.CreationDate)
	if err != nil {
		return nil, fmt.Errorf("cannot retrieve uber order id: %v", err)
	}
	return &meta, nil
}

// UpdateOrderAccepted met à jour le statut local
func (r *UberRepository) UpdateOrderAccepted(tx *sql.Tx, orderID string, prepTime int) error {
	query := `
		UPDATE orders
		SET brand_status = 'ACCEPTED', merchant_approval = 'ACCEPTED', 
		    last_update = UTC_TIMESTAMP, 
		    estimated_ready = DATE_ADD(UTC_TIMESTAMP, INTERVAL ? MINUTE)
		WHERE order_id = ?`
	_, err := tx.Exec(query, prepTime, orderID)
	return err
}

// CalculateAutoPrepTime appelle la procédure stockée
func (r *UberRepository) CalculateAutoPrepTime(tx *sql.Tx, merchantID, orderID string) (int, error) {
	// 1. Get product count
	var qty int
	err := tx.QueryRow("SELECT sum(quantity) FROM orderitems WHERE order_id = ?", orderID).Scan(&qty)
	if err != nil {
		return 0, err
	}

	// 2. Call Procedure
	// Note: Pour récupérer le résultat d'un CALL et d'un SELECT interne, c'est parfois tricky en Go selon le driver.
	// Une approche simplifiée si la procédure fait un SELECT à la fin :
	rows, err := tx.Query("CALL GET_AVERAGE_DISTRIBUTION_TIME(?, ?)", merchantID, qty)
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
func (r *UberRepository) SetOrderStatusDenied(tx *sql.Tx, orderID string) error {
	query := `UPDATE orders SET brand_status = 'DENIED', last_update = UTC_TIMESTAMP WHERE order_id = ?`
	_, err := tx.Exec(query, orderID)
	return err
}

// SetOrderStatusCanceled met à jour le statut en CANCELED
func (r *UberRepository) SetOrderStatusCanceled(tx *sql.Tx, orderID string) error {
	query := `UPDATE orders SET brand_status = 'CANCELED', status = '-1', last_update = UTC_TIMESTAMP WHERE order_id = ?`
	_, err := tx.Exec(query, orderID)
	return err
}

// SetOrderStatusReady met à jour orders et orderitems pour le statut READY
func (r *UberRepository) SetOrderStatusReady(tx *sql.Tx, orderID string) error {
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

	if _, err := tx.Exec(queryOrder, orderID); err != nil {
		return err
	}
	if _, err := tx.Exec(queryItems, orderID); err != nil {
		return err
	}
	return nil
}

// SyncOrderState met à jour la commande lors du finishOrderIfDoesNotExist (succès API)
func (r *UberRepository) SyncOrderState(tx *sql.Tx, uberOrderID, status, state, approval string, reasonID sql.NullInt64) error {
	// Mise à jour principale Orders
	query := `
		UPDATE orders
		SET brand_status = ?, state = ?, merchant_approval = ?, deletion_reason_id = ?,
		    delivered_on = CASE WHEN ? = 'COMPLETED' THEN UTC_TIMESTAMP ELSE delivered_on END,
		    last_update = UTC_TIMESTAMP
		WHERE brand_order_id = ?`

	_, err := tx.Exec(query, status, state, approval, reasonID, status, uberOrderID)
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
		_, err := tx.Exec(queryItems, uberOrderID)
		if err != nil {
			return err
		}
	}
	return nil
}

// HandleOrderNotFound gère le cas 404 de l'API Uber (finishOrderIfDoesNotExist)
func (r *UberRepository) HandleOrderNotFound(tx *sql.Tx, uberOrderID string) error {
	query := `
		UPDATE orders
		SET brand_status = CASE WHEN brand_status = 'READY_FOR_HANDOFF' THEN 'DELIVERED' ELSE 'CANCELED' END,
		    state = 'CLOSED',
		    last_update = UTC_TIMESTAMP
		WHERE brand_order_id = ?`
	_, err := tx.Exec(query, uberOrderID)
	return err
}

// UpdateBusyModeData met à jour les données de "Busy Mode"
func (r *UberRepository) UpdateBusyModeData(tx *sql.Tx, storeID string, delayUntil time.Time, duration int) error {
	query := `
		UPDATE integration_uber_eats
		SET delay_until = ?, delay_duration = ?
		WHERE store_id = ?`
	_, err := tx.Exec(query, delayUntil, duration, storeID)
	return err
}

// UpdatePreparationTime met à jour le temps de préparation estimé
func (r *UberRepository) UpdatePreparationTime(tx *sql.Tx, merchantID string, timeVal int, isAuto bool) error {
	// Logique PHP :
	// estimated_preparation_time = case when :auto = 'TRUE' then estimated_preparation_time else :time end
	// last_estimated_preparation_time = :time

	query := `
		UPDATE integration_uber_eats
		SET estimated_preparation_time = CASE WHEN ? = true THEN estimated_preparation_time ELSE ? END,
		    last_estimated_preparation_time = ?
		WHERE merchant_id = ?`

	_, err := tx.Exec(query, isAuto, timeVal, timeVal, merchantID)
	return err
}

// UpdateStoreClosure met à jour la date de fermeture temporaire
func (r *UberRepository) UpdateStoreClosure(tx *sql.Tx, storeID string, closedUntil time.Time) error {
	query := `UPDATE integration_uber_eats SET closed_until = ? WHERE store_id = ?`
	_, err := tx.Exec(query, closedUntil, storeID)
	return err
}

// GetStoreInfoForMenu récupère ID, BasicAuth et Timezone (utilisé pour toggle item)
func (r *UberRepository) GetStoreInfoForMenu(tx *sql.Tx, merchantID string) (*Store, error) {
	// Similaire à GetStoreData mais s'assure que le token n'est pas null
	query := `
		SELECT iue.merchant_id, iue.store_id, m.timezone, iue.bearer_token
		FROM integration_uber_eats iue
		INNER JOIN merchant m on m.id = iue.merchant_id
		WHERE iue.merchant_id = ? AND iue.bearer_token IS NOT NULL`

	var store Store
	// On ignore les champs non demandés dans le Scan
	err := tx.QueryRow(query, merchantID).Scan(&store.MerchantID, &store.StoreID, &store.Timezone, &store.BearerToken)
	return &store, err
}
