package integrations

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"
	"welloresto-api/internal/database/dbx"
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
// storeID vide résout sur le compte "principal" du marchand (ORDER BY
// store_id, comportement historique identique pour un marchand mono-compte) ;
// storeID renseigné cible ce compte précis, y compris pour les KPIs (revenue/
// orders_count/synced_items sont alors scopés à ce seul compte).
func (r *Repository) GetUberEatsIntegration(ctx context.Context, merchantID, storeID string) (*UberEatsIntegration, error) {
	db := dbx.GetDB(ctx, r.database)

	storeFilter := ""
	orderBy := "ORDER BY iue.store_id LIMIT 1"
	if storeID != "" {
		storeFilter = " AND store_id = ?"
		orderBy = "AND iue.store_id = ? LIMIT 1"
	}

	q := `
		SELECT
			iue.enabled,
			iue.commission_rate,
			iue.auto_accept_orders,
			COALESCE(iue.estimated_preparation_time, '0') AS preparation_time_minutes,
			iue.closed_until,
			iue.last_sync,
			iue.store_id,
			(
				SELECT COUNT(*)
				FROM   integration_uber_eats_products_mapping
				WHERE  merchant_id = ?` + storeFilter + `
			) AS synced_items,
			(
				SELECT COALESCE(SUM(price), 0)
				FROM   orders
				WHERE  merchant_id = ?
				  AND  brand       = 'UBER_EATS'
				  AND  isPaid      = true
				  AND  brand_status NOT IN ` + kpiExcludedStatuses + storeFilter + `
			) AS revenue,
			(
				SELECT COUNT(*)
				FROM   orders
				WHERE  merchant_id = ?
				  AND  brand       = 'UBER_EATS'
				  AND  isPaid      = true
				  AND  brand_status NOT IN ` + kpiExcludedStatuses + storeFilter + `
			) AS orders_count
		FROM   integration_uber_eats iue
		WHERE  iue.merchant_id = ?
		` + orderBy

	args := []interface{}{merchantID}
	if storeID != "" {
		args = append(args, storeID)
	}
	args = append(args, merchantID)
	if storeID != "" {
		args = append(args, storeID)
	}
	args = append(args, merchantID)
	if storeID != "" {
		args = append(args, storeID)
	}
	args = append(args, merchantID)
	if storeID != "" {
		args = append(args, storeID)
	}

	var (
		enabled                bool
		commissionRate         int
		autoAccept             bool
		preparationTimeMinutes int
		closedUntil            sql.NullTime
		lastSync               sql.NullTime
		resolvedStoreID        string
		syncedItems            int
		revenue                int
		ordersCount            int
	)

	err := db.QueryRowContext(ctx, q, args...).Scan(
		&enabled, &commissionRate, &autoAccept, &preparationTimeMinutes, &closedUntil, &lastSync, &resolvedStoreID,
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
		Platform:               "uber_eats",
		StoreID:                resolvedStoreID,
		Active:                 enabled,
		CommissionRate:         commissionRate,
		AutoAcceptOrders:       autoAccept,
		PreparationTimeMinutes: preparationTimeMinutes,
		SyncedItems:            syncedItems,
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
	if closedUntil.Valid {
		t := closedUntil.Time
		result.ClosedUntil = &t
	}

	return result, nil
}

// ListUberEatsAccounts liste tous les comptes Uber Eats d'un marchand pour le
// sélecteur de compte (back-office / POS).
func (r *Repository) ListUberEatsAccounts(ctx context.Context, merchantID string) ([]AccountSummary, error) {
	db := dbx.GetDB(ctx, r.database)

	const q = `
		SELECT store_id, enabled
		FROM   integration_uber_eats
		WHERE  merchant_id = ?
		ORDER BY store_id`

	rows, err := db.QueryContext(ctx, q, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []AccountSummary
	for rows.Next() {
		var acc AccountSummary
		if err := rows.Scan(&acc.AccountID, &acc.Enabled); err != nil {
			return nil, err
		}
		accounts = append(accounts, acc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range accounts {
		accounts[i].IsPrimary = i == 0
	}
	return accounts, nil
}

// GetDeliverooIntegration returns the Deliveroo integration row plus live KPIs.
// Returns (nil, nil) when the merchant has no Deliveroo integration configured.
// locationID vide résout sur le compte "principal" du marchand (ORDER BY
// location_id, comportement historique identique pour un marchand
// mono-compte) ; locationID renseigné cible ce compte précis, y compris pour
// les KPIs (revenue/orders_count/synced_items sont alors scopés à ce seul
// compte).
func (r *Repository) GetDeliverooIntegration(ctx context.Context, merchantID, locationID string) (*DeliverooIntegration, error) {
	db := dbx.GetDB(ctx, r.database)

	locationFilter := ""
	orderBy := "ORDER BY ind.location_id LIMIT 1"
	if locationID != "" {
		locationFilter = " AND location_id = ?"
		orderBy = "AND ind.location_id = ? LIMIT 1"
	}

	q := `
		SELECT
			ind.enabled,
			ind.commission_rate,
			ind.auto_accept_orders,
			COALESCE(ind.preparation_time_minutes, 0) AS preparation_time_minutes,
			ind.last_sync,
			ind.location_id,
			(
				SELECT COUNT(*)
				FROM   integration_deliveroo_products_mapping
				WHERE  merchant_id = ?` + locationFilter + `
			) AS synced_items,
			(
				SELECT COALESCE(SUM(price), 0)
				FROM   orders
				WHERE  merchant_id = ?
				  AND  brand       = 'DELIVEROO'
				  AND  isPaid      = true
				  AND  brand_status NOT IN ` + kpiExcludedStatuses + locationFilter + `
			) AS revenue,
			(
				SELECT COUNT(*)
				FROM   orders
				WHERE  merchant_id = ?
				  AND  brand       = 'DELIVEROO'
				  AND  isPaid      = true
				  AND  brand_status NOT IN ` + kpiExcludedStatuses + locationFilter + `
			) AS orders_count
		FROM   integration_deliveroo ind
		WHERE  ind.merchant_id = ?
		` + orderBy

	args := []interface{}{merchantID}
	if locationID != "" {
		args = append(args, locationID)
	}
	args = append(args, merchantID)
	if locationID != "" {
		args = append(args, locationID)
	}
	args = append(args, merchantID)
	if locationID != "" {
		args = append(args, locationID)
	}
	args = append(args, merchantID)
	if locationID != "" {
		args = append(args, locationID)
	}

	var (
		active                 bool
		commissionRate         int
		autoAccept             bool
		preparationTimeMinutes int
		lastSync               sql.NullTime
		resolvedLocationID     string
		syncedItems            int
		revenue                int
		ordersCount            int
	)

	err := db.QueryRowContext(ctx, q, args...).Scan(
		&active, &commissionRate, &autoAccept, &preparationTimeMinutes, &lastSync, &resolvedLocationID,
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
		Platform:               "deliveroo",
		LocationID:             resolvedLocationID,
		Active:                 active,
		CommissionRate:         commissionRate,
		AutoAcceptOrders:       autoAccept,
		PreparationTimeMinutes: preparationTimeMinutes,
		SyncedItems:            syncedItems,
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

// ListDeliverooAccounts liste tous les comptes Deliveroo d'un marchand pour le
// sélecteur de compte (back-office / POS).
func (r *Repository) ListDeliverooAccounts(ctx context.Context, merchantID string) ([]AccountSummary, error) {
	db := dbx.GetDB(ctx, r.database)

	const q = `
		SELECT location_id, enabled
		FROM   integration_deliveroo
		WHERE  merchant_id = ?
		ORDER BY location_id`

	rows, err := db.QueryContext(ctx, q, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []AccountSummary
	for rows.Next() {
		var acc AccountSummary
		if err := rows.Scan(&acc.AccountID, &acc.Enabled); err != nil {
			return nil, err
		}
		accounts = append(accounts, acc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range accounts {
		accounts[i].IsPrimary = i == 0
	}
	return accounts, nil
}

// GetScanNOrderIntegration returns the ScanNOrder integration row plus live KPIs.
// Returns (nil, nil) when the merchant has no ScanNOrder settings row.
func (r *Repository) GetScanNOrderIntegration(ctx context.Context, merchantID string) (*ScanNOrderIntegration, error) {
	db := dbx.GetDB(ctx, r.database)

	const q = `
		SELECT
			snos.activated,
			snos.commission_rate,
			snos.closed_until,
			snos.extra_prep_minutes,
			snos.extra_prep_until,
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
				  AND  isPaid      = true
				  AND  brand_status NOT IN ` + kpiExcludedStatuses + `
			) AS revenue,
			(
				SELECT COUNT(*)
				FROM   orders
				WHERE  merchant_id = ?
				  AND  brand       = 'WELLO_RESTO'
				  AND  created_by  = 'SCANNORDER'
				  AND  isPaid      = true
				  AND  brand_status NOT IN ` + kpiExcludedStatuses + `
			) AS orders_count,
			(
				-- QR principal du merchant (ni table ni serveur) = {slug} des
				-- routes ScanNOrder, cf. kiosk.getMerchantSlug
				SELECT code
				FROM   qrcodes
				WHERE  merchant_id = ?
				  AND  location_id IS NULL
				  AND  user_id     IS NULL
				  AND  deleted     = false
				LIMIT  1
			) AS slug
		FROM   scannorder_settings snos
		INNER JOIN merchant_parameters mp ON mp.merchant_id = snos.merchant_id
		WHERE  snos.merchant_id = ?
		LIMIT  1`

	var (
		activated             bool
		commissionRate        int
		closedUntil           sql.NullTime
		extraPrepMinutes      sql.NullInt64
		extraPrepUntil        sql.NullTime
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
		slug                  sql.NullString
	)

	err := db.QueryRowContext(ctx, q,
		merchantID, // revenue subquery
		merchantID, // orders_count subquery
		merchantID, // slug subquery
		merchantID, // WHERE clause
	).Scan(
		&activated, &commissionRate, &closedUntil, &extraPrepMinutes, &extraPrepUntil, &lastSync, &syncedItems,
		&logoURL, &bannerURL, &primaryColor,
		&headerTitle, &headerText,
		&cgvLink, &returnPolicyLink, &legalNoticesLink,
		&takeawayEnabled, &takeawayAutoAccept,
		&deliveryEnabled, &deliveryAutoAccept,
		&deliveryDistanceLimit,
		&revenue, &ordersCount, &slug,
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
		slug:                  slug.String, // "" si le merchant n'a pas de QR principal
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
	if closedUntil.Valid {
		t := closedUntil.Time
		result.ClosedUntil = &t
	}
	if extraPrepMinutes.Valid {
		m := int(extraPrepMinutes.Int64)
		result.ExtraPrepMinutes = &m
	}
	if extraPrepUntil.Valid {
		t := extraPrepUntil.Time
		result.ExtraPrepUntil = &t
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
	db := dbx.GetDB(ctx, r.database)

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
	db := dbx.GetDB(ctx, r.database)

	q := `UPDATE scannorder_settings SET ` + column + ` = ? WHERE merchant_id = ?`

	_, err := db.ExecContext(ctx, q, publicURL, merchantID)
	return err
}

// UpdateUberEatsSettings updates editable settings for the Uber Eats
// integration. storeID vide cible tous les comptes du marchand (comportement
// historique, correct pour un marchand mono-compte) ; storeID renseigné ne
// cible que ce compte précis.
func (r *Repository) UpdateUberEatsSettings(ctx context.Context, merchantID, storeID string, commissionRate *int, autoAccept *bool, preparationTimeMinutes *int) error {
	db := dbx.GetDB(ctx, r.database)

	setClauses := []string{}
	args := []interface{}{}

	if commissionRate != nil {
		setClauses = append(setClauses, "commission_rate = ?")
		args = append(args, *commissionRate)
	}
	if autoAccept != nil {
		setClauses = append(setClauses, "auto_accept_orders = ?")
		args = append(args, *autoAccept)
	}
	if preparationTimeMinutes != nil {
		// estimated_preparation_time / last_estimated_preparation_time are varchar
		// columns; MySQL implicitly casts an int parameter to text, pgx does not.
		prepStr := strconv.Itoa(*preparationTimeMinutes)
		setClauses = append(setClauses, "estimated_preparation_time = ?")
		args = append(args, prepStr)
		setClauses = append(setClauses, "last_estimated_preparation_time = ?")
		args = append(args, prepStr)
	}

	if len(setClauses) == 0 {
		return nil
	}

	q := "UPDATE integration_uber_eats SET " + strings.Join(setClauses, ", ") + " WHERE merchant_id = ?"
	args = append(args, merchantID)
	if storeID != "" {
		q += " AND store_id = ?"
		args = append(args, storeID)
	}

	_, err := db.ExecContext(ctx, q, args...)
	return err
}

// DisableUberEats disables the Uber Eats integration. storeID vide désactive
// tous les comptes du marchand (comportement historique) ; storeID renseigné
// ne désactive que ce compte précis.
func (r *Repository) DisableUberEats(ctx context.Context, merchantID, storeID string) error {
	db := dbx.GetDB(ctx, r.database)

	q := `UPDATE integration_uber_eats SET enabled = false WHERE merchant_id = ?`
	args := []interface{}{merchantID}
	if storeID != "" {
		q += ` AND store_id = ?`
		args = append(args, storeID)
	}

	_, err := db.ExecContext(ctx, q, args...)
	return err
}

// UpdateDeliverooSettings updates editable settings for the Deliveroo
// integration. locationID vide cible tous les comptes du marchand
// (comportement historique, correct pour un marchand mono-compte) ;
// locationID renseigné ne cible que ce compte précis.
func (r *Repository) UpdateDeliverooSettings(ctx context.Context, merchantID, locationID string, commissionRate *int, autoAccept *bool, preparationTimeMinutes *int) error {
	db := dbx.GetDB(ctx, r.database)

	setClauses := []string{}
	args := []interface{}{}

	if commissionRate != nil {
		setClauses = append(setClauses, "commission_rate = ?")
		args = append(args, *commissionRate)
	}
	if autoAccept != nil {
		setClauses = append(setClauses, "auto_accept_orders = ?")
		args = append(args, *autoAccept)
	}
	if preparationTimeMinutes != nil {
		setClauses = append(setClauses, "preparation_time_minutes = ?")
		args = append(args, *preparationTimeMinutes)
	}

	if len(setClauses) == 0 {
		return nil
	}

	q := "UPDATE integration_deliveroo SET " + strings.Join(setClauses, ", ") + " WHERE merchant_id = ?"
	args = append(args, merchantID)
	if locationID != "" {
		q += " AND location_id = ?"
		args = append(args, locationID)
	}

	_, err := db.ExecContext(ctx, q, args...)
	return err
}

// DisableDeliveroo sets enabled = false for the Deliveroo integration.
// locationID vide désactive tous les comptes du marchand (comportement
// historique) ; locationID renseigné ne désactive que ce compte précis.
func (r *Repository) DisableDeliveroo(ctx context.Context, merchantID, locationID string) error {
	db := dbx.GetDB(ctx, r.database)

	q := `UPDATE integration_deliveroo SET enabled = false WHERE merchant_id = ?`
	args := []interface{}{merchantID}
	if locationID != "" {
		q += ` AND location_id = ?`
		args = append(args, locationID)
	}

	_, err := db.ExecContext(ctx, q, args...)
	return err
}

// SetScanNOrderClosedUntil sets the temporary closure timestamp for ScanNOrder.
func (r *Repository) SetScanNOrderClosedUntil(ctx context.Context, merchantID string, closedUntil time.Time) error {
	db := dbx.GetDB(ctx, r.database)

	const q = `UPDATE scannorder_settings SET closed_until = ? WHERE merchant_id = ?`

	_, err := db.ExecContext(ctx, q, closedUntil.UTC(), merchantID)
	return err
}

// SetScanNOrderExtraPrep sets the temporary extra preparation time for ScanNOrder.
//
// Les deux colonnes sont écrites ensemble : un supplément sans échéance ne
// s'effacerait jamais, une échéance sans supplément ne veut rien dire.
func (r *Repository) SetScanNOrderExtraPrep(ctx context.Context, merchantID string, minutes int, until time.Time) error {
	db := dbx.GetDB(ctx, r.database)

	const q = `UPDATE scannorder_settings SET extra_prep_minutes = ?, extra_prep_until = ? WHERE merchant_id = ?`

	_, err := db.ExecContext(ctx, q, minutes, until.UTC(), merchantID)
	return err
}

// UpdateScanNOrderSettings updates editable settings for the ScanNOrder integration.
func (r *Repository) UpdateScanNOrderSettings(ctx context.Context, merchantID string, req *UpdateScanNOrderRequest) error {
	db := dbx.GetDB(ctx, r.database)

	// --- 1. Update scannorder_settings ---
	setClauses := []string{}
	args := []interface{}{}

	if req.Active != nil {
		setClauses = append(setClauses, "activated = ?")
		args = append(args, *req.Active)
	}
	if req.HeaderTitle != nil {
		setClauses = append(setClauses, "header_title = ?")
		args = append(args, *req.HeaderTitle)
	}
	if req.HeaderText != nil {
		setClauses = append(setClauses, "header_text = ?")
		args = append(args, *req.HeaderText)
	}
	if req.CGVLink != nil {
		setClauses = append(setClauses, "cgv_link = ?")
		args = append(args, *req.CGVLink)
	}
	if req.ReturnPolicyLink != nil {
		setClauses = append(setClauses, "return_policy_link = ?")
		args = append(args, *req.ReturnPolicyLink)
	}
	if req.LegalNoticesLink != nil {
		setClauses = append(setClauses, "legal_notices_link = ?")
		args = append(args, *req.LegalNoticesLink)
	}
	if req.TakeawayEnabled != nil {
		setClauses = append(setClauses, "take_away_enabled = ?")
		args = append(args, *req.TakeawayEnabled)
	}
	if req.TakeawayAutoAccept != nil {
		setClauses = append(setClauses, "takeaway_auto_accept = ?")
		args = append(args, *req.TakeawayAutoAccept)
	}
	if req.DeliveryEnabled != nil {
		setClauses = append(setClauses, "delivery_enabled = ?")
		args = append(args, *req.DeliveryEnabled)
	}
	if req.DeliveryAutoAccept != nil {
		setClauses = append(setClauses, "delivery_auto_accept = ?")
		args = append(args, *req.DeliveryAutoAccept)
	}

	if len(setClauses) > 0 {
		q := "UPDATE scannorder_settings SET " + strings.Join(setClauses, ", ") + " WHERE merchant_id = ?"
		args = append(args, merchantID)
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			return err
		}
	}

	// --- 2. Update merchant_parameters (primary_color, delivery_distance_limit) ---
	mpSetClauses := []string{}
	mpArgs := []interface{}{}
	if req.PrimaryColor != nil {
		mpSetClauses = append(mpSetClauses, "primary_color = ?")
		mpArgs = append(mpArgs, *req.PrimaryColor)
	}
	if req.DeliveryDistanceLimit != nil {
		mpSetClauses = append(mpSetClauses, "delivery_distance_limit = ?")
		mpArgs = append(mpArgs, *req.DeliveryDistanceLimit)
	}
	if len(mpSetClauses) > 0 {
		q := "UPDATE merchant_parameters SET " + strings.Join(mpSetClauses, ", ") + " WHERE merchant_id = ?"
		mpArgs = append(mpArgs, merchantID)
		if _, err := db.ExecContext(ctx, q, mpArgs...); err != nil {
			return err
		}
	}

	return nil
}

// ─── Stripe Connect ───────────────────────────────────────────────────────────

// stripeBrandingData groups the data needed to sync branding to Stripe.
type stripeBrandingData struct {
	AccountID    string
	LogoURL      sql.NullString
	PrimaryColor string
}

// GetStripeBrandingData fetches the Stripe account ID, logo URL, and primary color
// for a merchant in a single query. Returns sql.ErrNoRows when either the
// stripe_accounts or scannorder_settings row is missing.
func (r *Repository) GetStripeBrandingData(ctx context.Context, merchantID string) (*stripeBrandingData, error) {
	db := dbx.GetDB(ctx, r.database)

	const q = `
		SELECT sa.account_id,
		       snos.logo_url,
		       COALESCE(mp.primary_color, '')
		FROM   stripe_accounts sa
		INNER  JOIN scannorder_settings snos ON snos.merchant_id = sa.merchant_id
		INNER  JOIN merchant_parameters  mp  ON mp.merchant_id  = sa.merchant_id
		WHERE  sa.merchant_id = ?
		LIMIT  1`

	var d stripeBrandingData
	err := db.QueryRowContext(ctx, q, merchantID).Scan(
		&d.AccountID,
		&d.LogoURL,
		&d.PrimaryColor,
	)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// GetStripeAccountID returns the Stripe connected account ID for a merchant.
// Returns ("", sql.ErrNoRows) when no stripe_accounts row exists.
func (r *Repository) GetStripeAccountID(ctx context.Context, merchantID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	var accountID string
	err := db.QueryRowContext(ctx,
		`SELECT account_id FROM stripe_accounts WHERE merchant_id = ? LIMIT 1`,
		merchantID,
	).Scan(&accountID)
	return accountID, err
}

// UpsertStripeAccountID stores the Stripe connected account ID for a merchant.
// It updates an existing stripe_accounts row first; if none exists, it inserts one.
func (r *Repository) UpsertStripeAccountID(ctx context.Context, merchantID, accountID string) error {
	db := dbx.GetDB(ctx, r.database)

	res, err := db.ExecContext(ctx,
		`UPDATE stripe_accounts SET account_id = ? WHERE merchant_id = ?`,
		accountID, merchantID,
	)
	if err != nil {
		return err
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected > 0 {
		return nil
	}

	_, err = db.ExecContext(ctx,
		`INSERT INTO stripe_accounts (merchant_id, account_id) VALUES (?, ?)`,
		merchantID, accountID,
	)
	return err
}

// UpdateStripeVerificationStatus caches the Stripe verification status in stripe_accounts.
// Called by the webhook handler when an account.updated event arrives.
func (r *Repository) UpdateStripeVerificationStatus(ctx context.Context, accountID, status string) error {
	db := dbx.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx,
		`UPDATE stripe_accounts SET verification_status = ? WHERE account_id = ?`,
		status, accountID,
	)
	return err
}
