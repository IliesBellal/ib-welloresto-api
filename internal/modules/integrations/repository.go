package integrations

import (
	"context"
	"database/sql"
	"welloresto-api/internal/utils/dbutils"
)

// kpiExcludedStatuses lists brand_status values that represent failed / unpaid orders.
// These are excluded from revenue and order-count KPIs.
const kpiExcludedStatuses = `('CANCELED','DENIED','ONLINE_PAYMENT_PENDING')`

type Repository struct {
	database *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db}
}

// GetUberEatsIntegration returns the Uber Eats integration row plus live KPIs.
// Returns (nil, nil) when the merchant has no Uber Eats integration configured.
func (r *Repository) GetUberEatsIntegration(ctx context.Context, merchantID string) (*UberEatsIntegration, error) {
	db := dbutils.GetDB(ctx, r.database)

	const q = `
		SELECT
			iue.enabled,
			iue.commission_rate,
			iue.auto_accept_orders,
			iue.last_sync,
			(
				SELECT COUNT(*)
				FROM   integration_uber_eats_products_mapping
				WHERE  merchant_id = ?
			) AS synced_items,
			(
				SELECT COALESCE(SUM(price), 0)
				FROM   orders
				WHERE  merchant_id = ?
				  AND  brand       = 'UBER_EATS'
				  AND  isPaid      = 1
				  AND  brand_status NOT IN ` + kpiExcludedStatuses + `
			) AS revenue,
			(
				SELECT COUNT(*)
				FROM   orders
				WHERE  merchant_id = ?
				  AND  brand       = 'UBER_EATS'
				  AND  isPaid      = 1
				  AND  brand_status NOT IN ` + kpiExcludedStatuses + `
			) AS orders_count
		FROM   integration_uber_eats iue
		WHERE  iue.merchant_id = ?
		LIMIT  1`

	var (
		enabled        bool
		commissionRate int
		autoAccept     bool
		lastSync       sql.NullTime
		syncedItems    int
		revenue        int
		ordersCount    int
	)

	err := db.QueryRowContext(ctx, q,
		merchantID, // synced_items subquery
		merchantID, // revenue subquery
		merchantID, // orders_count subquery
		merchantID, // WHERE clause
	).Scan(
		&enabled, &commissionRate, &autoAccept, &lastSync,
		&syncedItems, &revenue, &ordersCount,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	avgBasket := 0
	if ordersCount > 0 {
		avgBasket = revenue / ordersCount
	}

	result := &UberEatsIntegration{
		Platform:         "uber_eats",
		Active:           enabled,
		CommissionRate:   commissionRate,
		AutoAcceptOrders: autoAccept,
		SyncedItems:      syncedItems,
		KPIs: IntegrationKPIs{
			Revenue:   revenue,
			Orders:    ordersCount,
			AvgBasket: avgBasket,
		},
	}
	if lastSync.Valid {
		t := lastSync.Time
		result.LastSync = &t
	}

	return result, nil
}

// GetDeliverooIntegration returns the Deliveroo integration row plus live KPIs.
// Returns (nil, nil) when the merchant has no Deliveroo integration configured.
func (r *Repository) GetDeliverooIntegration(ctx context.Context, merchantID string) (*DeliverooIntegration, error) {
	db := dbutils.GetDB(ctx, r.database)

	const q = `
		SELECT
			ind.enabled,
			ind.commission_rate,
			ind.auto_accept_orders,
			ind.last_sync,
			(
				SELECT COUNT(*)
				FROM   integration_deliveroo_products_mapping
				WHERE  merchant_id = ?
			) AS synced_items,
			(
				SELECT COALESCE(SUM(price), 0)
				FROM   orders
				WHERE  merchant_id = ?
				  AND  brand       = 'DELIVEROO'
				  AND  isPaid      = 1
				  AND  brand_status NOT IN ` + kpiExcludedStatuses + `
			) AS revenue,
			(
				SELECT COUNT(*)
				FROM   orders
				WHERE  merchant_id = ?
				  AND  brand       = 'DELIVEROO'
				  AND  isPaid      = 1
				  AND  brand_status NOT IN ` + kpiExcludedStatuses + `
			) AS orders_count
		FROM   integration_deliveroo ind
		WHERE  ind.merchant_id = ?
		LIMIT  1`

	var (
		active         bool
		commissionRate int
		autoAccept     bool
		lastSync       sql.NullTime
		syncedItems    int
		revenue        int
		ordersCount    int
	)

	err := db.QueryRowContext(ctx, q,
		merchantID, // synced_items subquery
		merchantID, // revenue subquery
		merchantID, // orders_count subquery
		merchantID, // WHERE clause
	).Scan(
		&active, &commissionRate, &autoAccept, &lastSync,
		&syncedItems, &revenue, &ordersCount,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	avgBasket := 0
	if ordersCount > 0 {
		avgBasket = revenue / ordersCount
	}

	result := &DeliverooIntegration{
		Platform:         "deliveroo",
		Active:           active,
		CommissionRate:   commissionRate,
		AutoAcceptOrders: autoAccept,
		SyncedItems:      syncedItems,
		KPIs: IntegrationKPIs{
			Revenue:   revenue,
			Orders:    ordersCount,
			AvgBasket: avgBasket,
		},
	}
	if lastSync.Valid {
		t := lastSync.Time
		result.LastSync = &t
	}

	return result, nil
}

// GetScanNOrderIntegration returns the ScanNOrder integration row plus live KPIs.
// Returns (nil, nil) when the merchant has no ScanNOrder settings row.
func (r *Repository) GetScanNOrderIntegration(ctx context.Context, merchantID string) (*ScanNOrderIntegration, error) {
	db := dbutils.GetDB(ctx, r.database)

	const q = `
		SELECT
			snos.activated,
			snos.commission_rate,
			snos.last_sync,
			snos.synced_items,
			snos.logo_url,
			snos.banner_url,
			mp.primary_color,
			snos.header_title,
			snos.header_text,
			snos.cgv_link,
			snos.return_policy_link,
			snos.legal_notices_link,
			snos.take_away_enabled,
			snos.takeaway_auto_accept,
			snos.delivery_enabled,
			snos.delivery_auto_accept,
			COALESCE(mp.delivery_distance_limit, 0),
			(
				SELECT COALESCE(SUM(price), 0)
				FROM   orders
				WHERE  merchant_id = ?
				  AND  brand       = 'WELLO_RESTO'
				  AND  created_by  = 'SCANNORDER'
				  AND  isPaid      = 1
				  AND  brand_status NOT IN ` + kpiExcludedStatuses + `
			) AS revenue,
			(
				SELECT COUNT(*)
				FROM   orders
				WHERE  merchant_id = ?
				  AND  brand       = 'WELLO_RESTO'
				  AND  created_by  = 'SCANNORDER'
				  AND  isPaid      = 1
				  AND  brand_status NOT IN ` + kpiExcludedStatuses + `
			) AS orders_count
		FROM   scannorder_settings snos
		INNER JOIN merchant_parameters mp ON mp.merchant_id = snos.merchant_id
		WHERE  snos.merchant_id = ?
		LIMIT  1`

	var (
		activated             bool
		commissionRate        int
		lastSync              sql.NullTime
		syncedItems           int
		logoURL               sql.NullString
		bannerURL             sql.NullString
		primaryColor          string
		headerTitle           sql.NullString
		headerText            sql.NullString
		cgvLink               sql.NullString
		returnPolicyLink      sql.NullString
		legalNoticesLink      sql.NullString
		takeawayEnabled       bool
		takeawayAutoAccept    bool
		deliveryEnabled       bool
		deliveryAutoAccept    bool
		deliveryDistanceLimit int
		revenue               int
		ordersCount           int
	)

	err := db.QueryRowContext(ctx, q,
		merchantID, // revenue subquery
		merchantID, // orders_count subquery
		merchantID, // WHERE clause
	).Scan(
		&activated, &commissionRate, &lastSync, &syncedItems,
		&logoURL, &bannerURL, &primaryColor,
		&headerTitle, &headerText,
		&cgvLink, &returnPolicyLink, &legalNoticesLink,
		&takeawayEnabled, &takeawayAutoAccept,
		&deliveryEnabled, &deliveryAutoAccept,
		&deliveryDistanceLimit,
		&revenue, &ordersCount,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	avgBasket := 0
	if ordersCount > 0 {
		avgBasket = revenue / ordersCount
	}

	result := &ScanNOrderIntegration{
		Platform:              "scannorder",
		Active:                activated,
		CommissionRate:        commissionRate,
		AutoAcceptOrders:      takeawayAutoAccept || deliveryAutoAccept,
		SyncedItems:           syncedItems,
		PrimaryColor:          primaryColor,
		TakeawayEnabled:       takeawayEnabled,
		TakeawayAutoAccept:    takeawayAutoAccept,
		DeliveryEnabled:       deliveryEnabled,
		DeliveryAutoAccept:    deliveryAutoAccept,
		DeliveryDistanceLimit: deliveryDistanceLimit,
		KPIs: IntegrationKPIs{
			Revenue:   revenue,
			Orders:    ordersCount,
			AvgBasket: avgBasket,
		},
	}
	if lastSync.Valid {
		t := lastSync.Time
		result.LastSync = &t
	}
	if logoURL.Valid {
		result.LogoURL = &logoURL.String
	}
	if bannerURL.Valid {
		result.BannerURL = &bannerURL.String
	}
	if headerTitle.Valid {
		result.HeaderTitle = &headerTitle.String
	}
	if headerText.Valid {
		result.HeaderText = &headerText.String
	}
	if cgvLink.Valid {
		result.CGVLink = &cgvLink.String
	}
	if returnPolicyLink.Valid {
		result.ReturnPolicyLink = &returnPolicyLink.String
	}
	if legalNoticesLink.Valid {
		result.LegalNoticesLink = &legalNoticesLink.String
	}

	return result, nil
}

// GetScanNOrderCurrentImageURL returns the current logo_url or banner_url for a merchant.
// column must be "logo_url" or "banner_url".
func (r *Repository) GetScanNOrderCurrentImageURL(ctx context.Context, merchantID, column string) (string, error) {
	db := dbutils.GetDB(ctx, r.database)

	q := `SELECT COALESCE(` + column + `, '') FROM scannorder_settings WHERE merchant_id = ? LIMIT 1`

	var url string
	if err := db.QueryRowContext(ctx, q, merchantID).Scan(&url); err != nil {
		return "", err
	}
	return url, nil
}

// UpdateScanNOrderImageURL persists a new logo_url or banner_url for a merchant.
// column must be "logo_url" or "banner_url".
func (r *Repository) UpdateScanNOrderImageURL(ctx context.Context, merchantID, column, publicURL string) error {
	db := dbutils.GetDB(ctx, r.database)

	q := `UPDATE scannorder_settings SET ` + column + ` = ? WHERE merchant_id = ?`

	_, err := db.ExecContext(ctx, q, publicURL, merchantID)
	return err
}

// UpdateUberEatsSettings updates editable settings for the Uber Eats integration.
func (r *Repository) UpdateUberEatsSettings(ctx context.Context, merchantID string, commissionRate int, autoAccept bool) error {
	db := dbutils.GetDB(ctx, r.database)

	const q = `
		UPDATE integration_uber_eats
		SET    commission_rate    = ?,
		       auto_accept_orders = ?
		WHERE  merchant_id = ?`

	_, err := db.ExecContext(ctx, q, commissionRate, autoAccept, merchantID)
	return err
}

// DisableUberEats sets is_active = 0 for the Uber Eats integration.
func (r *Repository) DisableUberEats(ctx context.Context, merchantID string) error {
	db := dbutils.GetDB(ctx, r.database)

	const q = `UPDATE integration_uber_eats SET is_active = 0 WHERE merchant_id = ?`

	_, err := db.ExecContext(ctx, q, merchantID)
	return err
}

// UpdateDeliverooSettings updates editable settings for the Deliveroo integration.
func (r *Repository) UpdateDeliverooSettings(ctx context.Context, merchantID string, commissionRate int, autoAccept bool) error {
	db := dbutils.GetDB(ctx, r.database)

	const q = `
		UPDATE integration_deliveroo
		SET    commission_rate    = ?,
		       auto_accept_orders = ?
		WHERE  merchant_id = ?`

	_, err := db.ExecContext(ctx, q, commissionRate, autoAccept, merchantID)
	return err
}

// DisableDeliveroo sets enabled = 0 for the Deliveroo integration.
func (r *Repository) DisableDeliveroo(ctx context.Context, merchantID string) error {
	db := dbutils.GetDB(ctx, r.database)

	const q = `UPDATE integration_deliveroo SET enabled = 0 WHERE merchant_id = ?`

	_, err := db.ExecContext(ctx, q, merchantID)
	return err
}

// UpdateScanNOrderSettings updates editable settings for the ScanNOrder integration.
func (r *Repository) UpdateScanNOrderSettings(ctx context.Context, merchantID string, commissionRate int, autoAccept bool) error {
	db := dbutils.GetDB(ctx, r.database)

	const q = `
		UPDATE scannorder_settings
		SET    commission_rate      = ?,
		       takeaway_auto_accept = ?,
		       delivery_auto_accept = ?
		WHERE  merchant_id = ?`

	_, err := db.ExecContext(ctx, q, commissionRate, autoAccept, autoAccept, merchantID)
	return err
}

// ─── Stripe Connect ───────────────────────────────────────────────────────────

// GetStripeAccountID returns the Stripe connected account ID for a merchant.
// Returns ("", sql.ErrNoRows) when no stripe_accounts row exists.
func (r *Repository) GetStripeAccountID(ctx context.Context, merchantID string) (string, error) {
	db := dbutils.GetDB(ctx, r.database)

	var accountID string
	err := db.QueryRowContext(ctx,
		`SELECT account_id FROM stripe_accounts WHERE merchant_id = ? LIMIT 1`,
		merchantID,
	).Scan(&accountID)
	return accountID, err
}

// UpdateStripeVerificationStatus caches the Stripe verification status in stripe_accounts.
// Called by the webhook handler when an account.updated event arrives.
func (r *Repository) UpdateStripeVerificationStatus(ctx context.Context, accountID, status string) error {
	db := dbutils.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx,
		`UPDATE stripe_accounts SET verification_status = ? WHERE account_id = ?`,
		status, accountID,
	)
	return err
}
