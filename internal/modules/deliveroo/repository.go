package deliveroo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/utils/dbutils"
)

type DeliverooRepository struct {
	database   *sql.DB
	httpClient *http.Client
	basicAuth  string
}

func NewDeliverooRepo(db *sql.DB) *DeliverooRepository {
	return &DeliverooRepository{database: db, httpClient: &http.Client{Timeout: 15 * time.Second}, basicAuth: os.Getenv("DELIVEROO_BASE64_BASIC_AUTH")}
}

func (r *DeliverooRepository) GetBearerToken(ctx context.Context) (string, error) {
	// For simplicity call token endpoint each time (or store in external_tokens table).
	// Implement caching or DB storage as needed.
	url := "https://auth.developers.deliveroo.com/oauth2/token"
	req, _ := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader("grant_type=client_credentials"))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Basic "+r.basicAuth)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var p struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return "", err
	}
	return p.AccessToken, nil
}

func (r *DeliverooRepository) GetBrandOrderID(ctx context.Context, orderID string) (string, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	const q = `SELECT brand_order_id FROM orders WHERE order_id = ? LIMIT 1`

	var brandOrder sql.NullString
	if err := db.QueryRowContext(ctx, q, orderID).Scan(&brandOrder); err != nil {
		log.Error("GetBrandOrderID: failed to fetch brand order ID: " + err.Error())
		return "", err
	}
	if !brandOrder.Valid {
		return "", sql.ErrNoRows
	}
	return brandOrder.String, nil
}

func (r *DeliverooRepository) MarkDeliverooDeliveryStarted(ctx context.Context, brandOrderID string) (string, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// 1) Retrieve order_id and merchant_id
	var orderID string
	var merchantID string
	row := db.QueryRowContext(ctx, `
		SELECT order_id, merchant_id
		FROM orders
		WHERE brand_order_id = ?
		LIMIT 1
	`, brandOrderID)
	if err := row.Scan(&orderID, &merchantID); err != nil {
		if err == sql.ErrNoRows {
			log.Error("MarkDeliverooDeliveryStarted: order not found: " + brandOrderID)
			return "", fmt.Errorf("order not found: %s", brandOrderID)
		}
		log.Error("MarkDeliverooDeliveryStarted: failed to scan order: " + err.Error())
		return "", err
	}

	// 2) Update orders
	_, err := db.ExecContext(ctx, `
		UPDATE orders
		SET brand_status = 'DELIVERING', status = '1', isDistributed = '1', dateDeparture = UTC_TIMESTAMP()
		WHERE order_id = ?
	`, orderID)
	if err != nil {
		log.Error("MarkDeliverooDeliveryStarted: failed to update order status: " + err.Error())
		return "", err
	}

	// 3) Update orderitems
	_, err = db.ExecContext(ctx, `
		UPDATE orderitems
		SET distributed_quantity = quantity,
		    ready_for_distribution_quantity = quantity,
		    isDistributed = 1,
		    distributed_on = UTC_TIMESTAMP()
		WHERE order_id = ?
	`, orderID)
	if err != nil {
		log.Error("MarkDeliverooDeliveryStarted: failed to update order items: " + err.Error())
		return "", err
	}

	return orderID, nil
}

func (r *DeliverooRepository) UpdateAcceptedStatus(ctx context.Context, brandOrderID string) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	const updateQ = `
		UPDATE orders
		SET brand_status =
			CASE
				WHEN DATE_ADD(UTC_TIMESTAMP(), INTERVAL 30 MINUTE) < estimated_ready THEN 'scheduled'
				ELSE 'accepted'
			END
		WHERE brand_order_id = ?
	`

	if _, err := db.ExecContext(ctx, updateQ, brandOrderID); err != nil {
		log.Error("UpdateAcceptedStatus: failed to update order status: " + err.Error())
		return err
	}

	return nil
}

func (r *DeliverooRepository) GetBrandOrderIDAndMerchant(ctx context.Context, orderID int) (string, int, error) {
	db := dbutils.GetDB(ctx, r.database)

	var brandOrderID string
	var merchantID int

	err := db.QueryRowContext(ctx, `
        SELECT brand_order_id, merchant_id
        FROM orders
        WHERE order_id = ? LIMIT 1`,
		orderID,
	).Scan(&brandOrderID, &merchantID)

	return brandOrderID, merchantID, err
}

func (r *DeliverooRepository) UpdateReadyForHandoffLocal(ctx context.Context, orderID string) error {
	db := dbutils.GetDB(ctx, r.database)

	const q = `
		UPDATE orders
		SET brand_status='READY_FOR_COLLECTION',
		    last_update = UTC_TIMESTAMP()
		WHERE order_id = ?
	`
	_, err := db.ExecContext(ctx, q, orderID)
	return err
}

func (r *DeliverooRepository) MarkOrderCanceledLocal(ctx context.Context, orderID string) error {
	db := dbutils.GetDB(ctx, r.database)

	const q = `
		UPDATE orders
		SET brand_status='CANCELED',
		    status='-1',
		    last_update=UTC_TIMESTAMP()
		WHERE order_id = ?
	`
	_, err := db.ExecContext(ctx, q, orderID)
	return err
}

// GetBrandIDByMerchant récupère le brand_id Deliveroo associé à un merchant
func (r *DeliverooRepository) GetBrandIDByMerchant(ctx context.Context, merchantID string) (string, error) {
	db := dbutils.GetDB(ctx, r.database)

	const q = `SELECT brand_id FROM integration_deliveroo id WHERE id.merchant_id = ? LIMIT 1`

	var brandID sql.NullString
	if err := db.QueryRowContext(ctx, q, merchantID).Scan(&brandID); err != nil {
		return "", err
	}
	if !brandID.Valid || brandID.String == "" {
		return "", fmt.Errorf("deliveroo: brand_id not configured for merchant %s", merchantID)
	}
	return brandID.String, nil
}

// UpdateMerchantBrandID met à jour le brand_id Deliveroo pour un restaurant donné
func (r *DeliverooRepository) UpdateMerchantBrandID(ctx context.Context, merchantID string, brandID string) error {
	db := dbutils.GetDB(ctx, r.database)

	const q = `UPDATE integration_deliveroo SET brand_id = ? WHERE merchant_id = ?`

	_, err := db.ExecContext(ctx, q, brandID, merchantID)
	return err
}

// GetSiteIDByMerchant récupère le site_id (unique par site) stocké en base
func (r *DeliverooRepository) GetSiteIDByMerchant(ctx context.Context, merchantID string) (string, error) {
	db := dbutils.GetDB(ctx, r.database)

	const q = `SELECT location_id FROM integration_deliveroo WHERE merchant_id = ? LIMIT 1`

	var siteID sql.NullString
	if err := db.QueryRowContext(ctx, q, merchantID).Scan(&siteID); err != nil {
		return "", err
	}
	if !siteID.Valid {
		return "", fmt.Errorf("deliveroo_site_id not set for merchant %s", merchantID)
	}
	return siteID.String, nil
}
