package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

type UberStore struct {
	MerchantID               int64      `json:"merchant_id"`
	StoreID                  string     `json:"store_id"`
	Timezone                 string     `json:"timezone"`
	RefreshToken             string     `json:"refresh_token"`
	EstimatedPreparationTime string     `json:"estimated_preparation_time"`
	LastEstimatedPreparation float64    `json:"last_estimated_preparation_time"`
	DelayUntil               *time.Time `json:"delay_until"`
	DelayDuration            int        `json:"delay_duration"`
	BearerToken              string     `json:"bearer_token"`
}

type UberEatsRepository struct {
	db         *sql.DB
	log        *zap.Logger
	httpClient *http.Client
}

func NewUberEatsRepository(db *sql.DB, log *zap.Logger) *UberEatsRepository {
	return &UberEatsRepository{
		db:         db,
		log:        log,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// GetStore - mirrors PHP getStore (reads integration_uber_eats join merchant + gets bearer token)
func (r *UberEatsRepository) GetStore(ctx context.Context, merchantID string) (*UberStore, error) {
	const q = `
		SELECT iue.merchant_id, iue.store_id, m.timezone, iue.refresh_token,
			   iue.estimated_preparation_time, iue.last_estimated_preparation_time,
			   iue.delay_until, iue.delay_duration
		FROM integration_uber_eats iue
		INNER JOIN merchant m on m.id = iue.merchant_id
		WHERE iue.merchant_id = ?;
	`
	row := r.db.QueryRowContext(ctx, q, merchantID)

	var s UberStore
	var estimated sql.NullString
	var lastEst sql.NullFloat64
	var delayUntil sql.NullTime
	var delayDuration sql.NullInt64
	if err := row.Scan(&s.MerchantID, &s.StoreID, &s.Timezone, &s.RefreshToken, &estimated, &lastEst, &delayUntil, &delayDuration); err != nil {
		return nil, fmt.Errorf("uber get store scan: %w", err)
	}
	if estimated.Valid {
		s.EstimatedPreparationTime = estimated.String
	}
	if lastEst.Valid {
		s.LastEstimatedPreparation = lastEst.Float64
	}
	if delayUntil.Valid {
		t := delayUntil.Time
		s.DelayUntil = &t
	}
	if delayDuration.Valid {
		s.DelayDuration = int(delayDuration.Int64)
	}

	// attach bearer token by calling GetUberToken (which may refresh)
	token, err := r.GetUberBearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("get uber bearer token: %w", err)
	}
	s.BearerToken = token
	return &s, nil
}

// GetUberBearerToken: reads external_tokens table; refreshes if expired (mirrors PHP getUberToken/getNewUberToken)
func (r *UberEatsRepository) GetUberBearerToken(ctx context.Context) (string, error) {
	const q = `
		SELECT et.access_token, et.expires_at
		FROM external_tokens et
		WHERE et.token_type = 'uber_eats_bearer_token'
		LIMIT 1;
	`
	var accessToken sql.NullString
	var expiresAt sql.NullTime
	err := r.db.QueryRowContext(ctx, q).Scan(&accessToken, &expiresAt)
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}

	needRefresh := false
	if !accessToken.Valid || !expiresAt.Valid {
		needRefresh = true
	} else {
		// refresh if expires within 5 days
		expires := expiresAt.Time
		if time.Now().UTC().Add(5 * 24 * time.Hour).After(expires) {
			needRefresh = true
		}
	}

	if !needRefresh {
		return accessToken.String, nil
	}

	// call get new token (HTTP POST to Uber auth)
	newTok, expiresIn, err := r.getNewUberTokenHTTP(ctx)
	if err != nil {
		return "", err
	}

	// upsert into external_tokens
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	upQ := `
		INSERT INTO external_tokens (token_type, access_token, expires_at)
		VALUES ('uber_eats_bearer_token', ?, DATE_ADD(UTC_TIMESTAMP(), INTERVAL ? SECOND))
		ON DUPLICATE KEY UPDATE access_token = VALUES(access_token), expires_at = VALUES(expires_at)
	`
	if _, err := tx.ExecContext(ctx, upQ, newTok, expiresIn); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return newTok, nil
}

func (r *UberEatsRepository) getNewUberTokenHTTP(ctx context.Context) (string, int64, error) {
	// Replace with config values or store them in env
	url := "https://auth.uber.com/oauth/v2/token"
	data := "client_secret=QDCJMawOJ21lA3BPo0xXUMpwOl7Ve_dhf5VoMCYh&client_id=MWfak4vWlF2zofxaLV7NTGK18rKxdI8t&grant_type=client_credentials&scope=eats.order%20eats.report%20eats.store%20eats.store.orders.cancel%20eats.store.orders.read%20eats.store.status.read%20eats.store.status.write"

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(data))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	var payload struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", 0, err
	}
	return payload.AccessToken, payload.ExpiresIn, nil
}

func (r *UberEatsRepository) GetBrandOrderID(ctx context.Context, orderID string) (string, error) {
	var brandOrderID string
	err := r.db.QueryRowContext(ctx, `
        SELECT brand_order_id
        FROM orders
        WHERE order_id = ? LIMIT 1`,
		orderID,
	).Scan(&brandOrderID)

	return brandOrderID, err
}

func (r *UberEatsRepository) GetBearerToken(ctx context.Context, merchantID string) (string, error) {
	var token string
	err := r.db.QueryRowContext(ctx, `
        SELECT bearer_token
        FROM integration_uber_eats
        WHERE merchant_id = ? LIMIT 1`,
		merchantID,
	).Scan(&token)

	return token, err
}

func (r *UberEatsRepository) UpdateReadyForHandoffLocal(ctx context.Context, orderID string) error {
	_, err := r.db.ExecContext(ctx, `
        UPDATE orders
        SET brand_status = 'READY_FOR_HANDOFF',
            isDistributed = '1',
            last_update = UTC_TIMESTAMP,
            delivered_on = UTC_TIMESTAMP
        WHERE order_id = ?`,
		orderID,
	)
	if err != nil {
		return err
	}

	_, err = r.db.ExecContext(ctx, `
        UPDATE orderitems
        SET distributed_quantity = quantity,
            ready_for_distribution_quantity = quantity,
            isDistributed = '1',
            distributed_on = UTC_TIMESTAMP
        WHERE order_id = ?`,
		orderID,
	)

	return err
}

func (r *UberEatsRepository) GetBrandOrderForCancel(ctx context.Context, orderID string) (string, error) {
	var brandOrder string
	err := r.db.QueryRowContext(ctx, `
        SELECT brand_order_id
        FROM orders
        WHERE order_id = ?`,
		orderID,
	).Scan(&brandOrder)
	return brandOrder, err
}

func (r *UberEatsRepository) GetOrderInfo(ctx context.Context, orderID string) (brandOrderID string, creationTime sql.NullTime, err error) {
	const q = `SELECT brand_order_id, creation_date FROM orders WHERE order_id = ? LIMIT 1`
	var brand sql.NullString
	if err := r.db.QueryRowContext(ctx, q, orderID).Scan(&brand, &creationTime); err != nil {
		return "", creationTime, err
	}
	if !brand.Valid {
		return "", creationTime, fmt.Errorf("missing brand_order_id")
	}
	return brand.String, creationTime, nil
}

func (r *UberEatsRepository) CountOrderItems(ctx context.Context, orderID string) (int, error) {
	const q = `SELECT SUM(quantity) FROM orderitems WHERE order_id = ?`
	var n sql.NullInt64
	if err := r.db.QueryRowContext(ctx, q, orderID).Scan(&n); err != nil {
		return 0, err
	}
	if !n.Valid {
		return 0, nil
	}
	return int(n.Int64), nil
}

func (r *UberEatsRepository) GetAverageDistributionTime(ctx context.Context, merchantID string, count int) (int, error) {
	rows, err := r.db.QueryContext(ctx, "CALL GET_AVERAGE_DISTRIBUTION_TIME(?, ?)", merchantID, count)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var estimated sql.NullInt64
		if err := rows.Scan(&estimated); err == nil && estimated.Valid {
			return int(estimated.Int64), nil
		}
	}
	return 0, nil
}

func (r *UberEatsRepository) UpdateOrderAccepted(ctx context.Context, orderID string, estMinutes int) error {
	const q = `
	UPDATE orders 
	SET brand_status='ACCEPTED', merchant_approval='ACCEPTED', 
	    last_update=UTC_TIMESTAMP(), 
	    estimated_ready=DATE_ADD(UTC_TIMESTAMP(), INTERVAL ? MINUTE)
	WHERE order_id = ?
	`
	_, err := r.db.ExecContext(ctx, q, estMinutes, orderID)
	return err
}

func (r *UberEatsRepository) MarkOrderCanceledLocal(ctx context.Context, orderID string) error {
	const q = `
		UPDATE orders
		SET brand_status = 'CANCELED',
		    status = '-1',
		    last_update = UTC_TIMESTAMP()
		WHERE order_id = ?
	`
	_, err := r.db.ExecContext(ctx, q, orderID)
	return err
}

func (r *UberEatsRepository) SetOrderBrandDenied(ctx context.Context, orderID string) error {
	const q = `
		UPDATE orders
		SET brand_status='DENIED',
		    merchant_approval='DENIED',
		    last_update=UTC_TIMESTAMP()
		WHERE order_id = ?
	`
	_, err := r.db.ExecContext(ctx, q, orderID)
	return err
}
