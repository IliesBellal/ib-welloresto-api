package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"
)

type DeliverooRepository struct {
	db         *sql.DB
	log        *zap.Logger
	httpClient *http.Client
	basicAuth  string
}

func NewDeliverooRepo(db *sql.DB, log *zap.Logger) *DeliverooRepository {
	return &DeliverooRepository{db: db, log: log, httpClient: &http.Client{Timeout: 15 * time.Second}, basicAuth: os.Getenv("DELIVEROO_BASE64_BASIC_AUTH")}
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
	const q = `SELECT brand_order_id FROM orders WHERE order_id = ? LIMIT 1`

	var brandOrder sql.NullString
	if err := r.db.QueryRowContext(ctx, q, orderID).Scan(&brandOrder); err != nil {
		return "", err
	}
	if !brandOrder.Valid {
		return "", sql.ErrNoRows
	}
	return brandOrder.String, nil
}

func (r *DeliverooRepository) MarkDeliverooDeliveryStarted(ctx context.Context, brandOrderID string) (string, error) {
	// 1) Retrieve order_id and merchant_id
	var orderID string
	var merchantID int
	row := r.db.QueryRowContext(ctx, `
		SELECT order_id, merchant_id
		FROM orders
		WHERE brand_order_id = ?
		LIMIT 1
	`, brandOrderID)
	if err := row.Scan(&orderID, &merchantID); err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("order not found: %s", brandOrderID)
		}
		return "", err
	}

	// 2) Update orders
	_, err := r.db.ExecContext(ctx, `
		UPDATE orders
		SET brand_status = 'DELIVERING', status = '1', isDistributed = '1', dateDeparture = UTC_TIMESTAMP()
		WHERE order_id = ?
	`, orderID)
	if err != nil {
		return "", err
	}

	// 3) Update orderitems
	_, err = r.db.ExecContext(ctx, `
		UPDATE orderitems
		SET distributed_quantity = quantity,
		    ready_for_distribution_quantity = quantity,
		    isDistributed = 1,
		    distributed_on = UTC_TIMESTAMP()
		WHERE order_id = ?
	`, orderID)
	if err != nil {
		return "", err
	}

	return orderID, nil
}

func (r *DeliverooRepository) UpdateAcceptedStatus(ctx context.Context, brandOrderID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	const updateQ = `
		UPDATE orders
		SET brand_status =
			CASE
				WHEN DATE_ADD(UTC_TIMESTAMP(), INTERVAL 30 MINUTE) < estimated_ready THEN 'scheduled'
				ELSE 'accepted'
			END
		WHERE brand_order_id = ?
	`

	if _, err := tx.ExecContext(ctx, updateQ, brandOrderID); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (r *DeliverooRepository) GetBrandOrderIDAndMerchant(ctx context.Context, orderID int) (string, int, error) {
	var brandOrderID string
	var merchantID int

	err := r.db.QueryRowContext(ctx, `
        SELECT brand_order_id, merchant_id
        FROM orders
        WHERE order_id = ? LIMIT 1`,
		orderID,
	).Scan(&brandOrderID, &merchantID)

	return brandOrderID, merchantID, err
}

func (r *DeliverooRepository) UpdateReadyForHandoffLocal(ctx context.Context, orderID string) error {
	const q = `
		UPDATE orders
		SET brand_status='READY_FOR_COLLECTION',
		    last_update = UTC_TIMESTAMP()
		WHERE order_id = ?
	`
	_, err := r.db.ExecContext(ctx, q, orderID)
	return err
}

func (r *DeliverooRepository) MarkOrderCanceledLocal(ctx context.Context, orderID string) error {
	const q = `
		UPDATE orders
		SET brand_status='CANCELED',
		    status='-1',
		    last_update=UTC_TIMESTAMP()
		WHERE order_id = ?
	`
	_, err := r.db.ExecContext(ctx, q, orderID)
	return err
}
