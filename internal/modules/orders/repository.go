package orders

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

type OrdersRepository struct {
	database      *sql.DB
	ordersFetcher *OrdersFetcher
}

func NewOrdersRepository(db *sql.DB, ordersF *OrdersFetcher) *OrdersRepository {
	return &OrdersRepository{
		database:      db,
		ordersFetcher: ordersF}
}

// ==================================================================================
// PUBLIC METHODS
// ==================================================================================

// GetPendingOrderIDs : Récupère uniquement les IDs des commandes répondant aux critères (Requête légère)
func (r *OrdersRepository) GetPendingOrderIDs(ctx context.Context, merchantID, app string) ([]string, error) {
	// 1. Construction de la clause WHERE complexe
	criteria := " AND ((o.state IN ('OPEN') AND o.brand_status NOT IN('ONLINE_PAYMENT_PENDING'))) "

	// Ajout filtre spécifique à l'application
	if app == "1" || app == "WR_DELIVERY" {
		criteria += " AND o.order_type = 'DELIVERY' AND o.fulfillment_type = 'DELIVERY_BY_RESTAURANT' "
	} else if app == "2" || app == "WR_WAITER" {
		criteria += " AND o.order_type NOT IN ('DELIVERY','TAKE_AWAY') "
	}

	// 2. Requête pour récupérer UNIQUEMENT les IDs
	qIDs := `SELECT DISTINCT o.order_id
             FROM orders o
             LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id
             LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id AND ds.status IN ('1','PENDING')
             WHERE o.merchant_id = ? ` + criteria

	rows, err := r.database.QueryContext(ctx, qIDs, merchantID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pending order ids: %w", err)
	}
	defer rows.Close()

	var orderIDs []string
	for rows.Next() {
		var oid string
		if err := rows.Scan(&oid); err != nil {
			return nil, err
		}
		orderIDs = append(orderIDs, oid)
	}

	return orderIDs, nil
}

// GetOrdersByIDs : Appelle le constructeur lourd (FetchAndBuild) uniquement pour les IDs fournis
func (r *OrdersRepository) GetOrdersByIDs(ctx context.Context, merchantID string, orderIDs []string) ([]models.Order, error) {
	if len(orderIDs) == 0 {
		return []models.Order{}, nil
	}

	// ========================================================================
	// ÉTAPE : Construction du filtre OPTIMISÉ (IN)
	// ========================================================================
	idsStr := ""
	for i, oid := range orderIDs {
		if i > 0 {
			idsStr += ","
		}
		idsStr += fmt.Sprintf("'%s'", oid)
	}

	// Le filtre qui rend les sous-requêtes de FetchAndBuildOrders instantanées
	filterOptimized := fmt.Sprintf(" AND o.order_id IN (%s) ", idsStr)

	orders, err := r.ordersFetcher.FetchAndBuildOrders(ctx, merchantID, filterOptimized, "", "")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch and build orders by ids: %w", err)
	}

	return orders, nil
}

func (r *OrdersRepository) GetOrder(ctx context.Context, merchantID string, orderID string) (*models.PendingOrdersResponse, error) {
	// Filtre strict sur l'MerchantID
	filter := fmt.Sprintf(" AND o.order_id = '%s' ", orderID)

	orders, err := r.ordersFetcher.FetchAndBuildOrders(ctx, merchantID, filter, "", "")
	if err != nil {
		return nil, err
	}
	if len(orders) == 0 {
		return nil, sql.ErrNoRows
	}

	return &models.PendingOrdersResponse{Orders: orders}, nil
}

func (r *OrdersRepository) GetOrders(ctx context.Context, merchantID string, req *models.OrderRequest) ([]models.Order, error) {
	// Filtre strict sur l'MerchantID
	ids, err := r.GetOrdersBasic(ctx, merchantID, req)
	if err != nil {
		return nil, err
	}

	if len(ids) == 0 {
		return []models.Order{}, nil
	}

	// build IN (...)
	in := ""
	for i, id := range ids {
		if i > 0 {
			in += ","
		}
		in += fmt.Sprintf("'%s'", id)
	}

	filter := fmt.Sprintf(" AND o.order_id IN (%s) ", in)

	orders, err := r.ordersFetcher.FetchAndBuildOrders(ctx, merchantID, filter, "", "")
	if err != nil {
		return nil, err
	}
	if len(orders) == 0 {
		return nil, sql.ErrNoRows
	}

	return orders, nil
}

func (r *OrdersRepository) GetOrdersBasic(ctx context.Context, merchantID string, req *models.OrderRequest) ([]string, error) {

	where := " WHERE o.merchant_id = ? "
	args := []interface{}{merchantID}

	// Filtre order_id
	if req.OrderID != nil {
		where += " AND o.order_id = ? "
		args = append(args, *req.OrderID)
	}

	// Filtre customer_id
	if req.Customer != nil && req.Customer.CustomerID != nil {
		where += " AND o.customer_id = ? "
		args = append(args, req.Customer.CustomerID)
	}

	query := `
        SELECT o.order_id
        FROM orders o
        ` + where + `
        ORDER BY o.creation_date DESC
        LIMIT 10
    `

	rows, err := r.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}

	return out, nil
}

func (r *OrdersRepository) GetHistory(ctx context.Context, merchantID string, req models.OrderHistoryRequest) ([]models.Order, int, int, int, error) {

	// =========================
	// 1️⃣ BUILD WHERE + ARGS
	// =========================
	fromClause := " FROM orders o LEFT JOIN customer c ON o.customer_id = c.customer_id "
	where := " WHERE o.merchant_id = ? AND o.state = 'CLOSED' "
	args := []interface{}{merchantID}

	if req.DateFrom != nil && req.DateTo != nil {
		where += " AND o.creation_date BETWEEN ? AND ? "
		args = append(args, *req.DateFrom, *req.DateTo)
	}

	if req.CustomerID != nil {
		customerID := strings.TrimSpace(*req.CustomerID)
		if customerID != "" {
			where += " AND o.customer_id = ? "
			args = append(args, customerID)
		}
	}

	if req.Search != nil {
		searchTerm := strings.TrimSpace(*req.Search)
		if searchTerm != "" {
			likeTerm := "%" + searchTerm + "%"
			where += `
				AND (
					LOWER(COALESCE(c.customer_name, '')) LIKE LOWER(?)
					OR LOWER(COALESCE(c.customer_first_name, '')) LIKE LOWER(?)
					OR LOWER(COALESCE(c.customer_last_name, '')) LIKE LOWER(?)
					OR LOWER(COALESCE(c.customer_id, '')) LIKE LOWER(?)
					OR LOWER(COALESCE(c.customer_code, '')) LIKE LOWER(?)
					OR LOWER(COALESCE(o.brand_order_num, '')) LIKE LOWER(?)
					OR LOWER(COALESCE(o.order_id, '')) LIKE LOWER(?)
				)
			`
			args = append(args, likeTerm, likeTerm, likeTerm, likeTerm, likeTerm, likeTerm, likeTerm)
		}
	}

	if len(req.Channel) > 0 {
		placeholders := make([]string, len(req.Channel))
		for i, v := range req.Channel {
			placeholders[i] = "?"
			args = append(args, v)
		}
		where += fmt.Sprintf(" AND o.brand IN (%s) ", strings.Join(placeholders, ","))
	}

	if len(req.OrderType) > 0 {
		placeholders := make([]string, len(req.OrderType))
		for i, v := range req.OrderType {
			placeholders[i] = "?"
			args = append(args, v)
		}
		where += fmt.Sprintf(" AND o.order_type IN (%s) ", strings.Join(placeholders, ","))
	}

	if len(req.Status) > 0 {
		placeholders := make([]string, len(req.Status))
		for i, v := range req.Status {
			placeholders[i] = "?"
			args = append(args, v)
		}
		where += fmt.Sprintf(" AND o.brand_status IN (%s) ", strings.Join(placeholders, ","))
	}

	// =========================
	// 2️⃣ PAGINATION (IDS ONLY)
	// =========================
	limit := 50
	if req.Limit != nil && *req.Limit > 0 {
		limit = *req.Limit
	}

	page := 1
	if req.Page != nil && *req.Page > 0 {
		page = *req.Page
	}

	countQuery := `
		SELECT COUNT(*)
	` + fromClause + where

	var totalItems int
	if err := r.database.QueryRowContext(ctx, countQuery, args...).Scan(&totalItems); err != nil {
		return nil, 0, page, limit, err
	}

	offset := (page - 1) * limit

	// =========================
	// 3️⃣ FETCH ORDER IDS
	// =========================
	query := `
		SELECT o.order_id
	` + fromClause + where + `
		ORDER BY o.creation_date DESC
		LIMIT ? OFFSET ?
	`

	args = append(args, limit, offset)

	rows, err := r.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, page, limit, err
	}
	defer rows.Close()

	var orderIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, 0, page, limit, err
		}
		orderIDs = append(orderIDs, id)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, page, limit, err
	}

	if len(orderIDs) == 0 {
		return []models.Order{}, totalItems, page, limit, nil
	}

	// =========================
	// 4️⃣ BUILD IN (...) FILTER
	// =========================
	var inParts []string
	for _, id := range orderIDs {
		inParts = append(inParts, fmt.Sprintf("'%s'", id))
	}

	whereFilter := fmt.Sprintf(
		" AND o.order_id IN (%s) ",
		strings.Join(inParts, ","),
	)

	orderBy := " ORDER BY o.creation_date DESC "

	// =========================
	// 5️⃣ FETCH FULL ORDERS
	// =========================
	orders, err := r.ordersFetcher.FetchAndBuildOrders(
		ctx,
		merchantID,
		whereFilter,
		orderBy,
		"",
	)
	if err != nil {
		return nil, 0, page, limit, err
	}

	return orders, totalItems, page, limit, nil
}

func (r *OrdersRepository) GetPaymentsForOrder(ctx context.Context, orderID string) ([]models.Payment, error) {
	q := `
		SELECT order_id, payment_id, mop, amount, payment_date, enabled
		FROM payments
		WHERE order_id = ?
		ORDER BY payment_date ASC
	`

	rows, err := r.database.QueryContext(ctx, q, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	payments := []models.Payment{}

	for rows.Next() {
		var p models.Payment
		var paymentDate sql.NullTime

		err := rows.Scan(&p.OrderID, &p.PaymentID, &p.MOP, &p.Amount, &paymentDate, &p.Enabled)
		if err != nil {
			return nil, err
		}

		if paymentDate.Valid {
			p.PaymentDate = helpers.NullTimePtr(paymentDate).UTC().Unix()
		}

		payments = append(payments, p)
	}

	return payments, nil
}

// ValidateProducts: check which products are blocked (return slice of product ids that are blocked)
func (r *OrdersRepository) ValidateProducts(ctx context.Context, tx *sql.Tx, merchantID int64, productIDs []int64) ([]int64, error) {
	if len(productIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(productIDs))
	args := make([]interface{}, 0, len(productIDs)+1)
	for i, id := range productIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	// merchant id as first arg
	args = append([]interface{}{merchantID}, args...)
	query := fmt.Sprintf(`
SELECT DISTINCT p.product_id
FROM products p
LEFT JOIN (
    SELECT DISTINCT r.product_id
    FROM requires rq
    INNER JOIN recipes r ON r.recipe_id = rq.recipe_id
    INNER JOIN components c ON rq.component_id = c.component_id AND c.status = 0 AND rq.enabled = true
) a ON a.product_id = p.product_id
WHERE p.merchant_id = ?
AND p.product_id IN (%s)
AND (CASE WHEN a.product_id IS NOT NULL THEN 0 ELSE p.status END) = 0
`, strings.Join(placeholders, ","))
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var blocked []int64
	for rows.Next() {
		var pid int64
		if err := rows.Scan(&pid); err != nil {
			return nil, err
		}
		blocked = append(blocked, pid)
	}
	return blocked, nil
}

/*
// OrderInsert is the minimal data to create an order row
type OrderInsert struct {
	CashRegisterID interface{}
	MerchantID     int64
	CustomerID     interface{}
	OrderNum       int64
	Price          float64
	TVA            float64
	HT             float64
	// other fields omitted for brevity
}
*/
// InsertOrder inserts order and returns order_id
/*
func (r *OrdersRepository) InsertOrder(ctx context.Context, tx *sql.Tx, o *OrderInsert) (int64, error) {
	res, err := tx.ExecContext(ctx, `
INSERT INTO orders (cash_register_id, merchant_id, customer_id, order_num, price, TVA, HT, creation_date, dateCall, last_update)
VALUES (?, ?, ?, ?, ?, ?, ?, UTC_TIMESTAMP, UTC_TIMESTAMP, UTC_TIMESTAMP)
`, o.CashRegisterID, o.MerchantID, o.CustomerID, o.OrderNum, o.Price, o.TVA, o.HT)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
*/
// ResetOrderItems conservé pour compatibilité ascendante (utilisé éventuellement ailleurs).
// Préférer deleteRemovedOrderItems dans UpdateOrder.
/*
func (r *OrdersRepository) ResetOrderItems(ctx context.Context, tx *sql.Tx, req *models.RequestObject) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE orderitems
		SET quantity = 0
		WHERE order_id = ?`,
		req.Order.OrderID,
	)
	return err
}*/
/*
func (r *OrdersRepository) InsertPayment(ctx context.Context, p *models.Payment) error {
	db := dbutils.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
INSERT INTO payments (merchant_id, cash_register_id, order_id, amount, mop, payment_date, user_id, operation_type)
VALUES (?, ?, ?, ?, ?, UTC_TIMESTAMP, ?, ?)
`, p.MerchantID, p.CashRegisterID, p.OrderID, p.Amount, p.MOP, p.UserID, p.OperationType)
	return err
}*/

func (r *OrdersRepository) GetMerchantPricingInfo(ctx context.Context, MerchantID string) (*models.MerchantPricingInfo, error) {
	q := `
		SELECT m.timezone, mp.currency, COALESCE(mp.delivery_fees,0) as delivery_fees,
			   COALESCE(mp.delivery_fees_limit,0) as delivery_fees_limit,
			   COALESCE(mp.minimum_cart_for_delivery_order,0) as minimum_cart_for_delivery_order
		FROM merchant m
		JOIN merchant_parameters mp ON mp.merchant_id = m.id
		WHERE m.id = ? LIMIT 1;
		`
	var cfg models.MerchantPricingInfo
	row := r.database.QueryRowContext(ctx, q, MerchantID)
	if err := row.Scan(&cfg.Timezone, &cfg.Currency, &cfg.DeliveryFees, &cfg.DeliveryFeesLimit, &cfg.MinimumCartForDeliveryOrder); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &cfg, nil
}

func (r *OrdersRepository) GetUnavailableProducts(ctx context.Context, req *models.PricingRequest) ([]models.UnavailableProductInfo, error) {
	// Si aucun produit dans la requête, on retourne un tableau vide
	if len(req.Order.Products) == 0 {
		return []models.UnavailableProductInfo{}, nil
	}

	// 1. Extraction des IDs pour la clause IN
	productIDs := make([]interface{}, 0, len(req.Order.Products))
	for _, p := range req.Order.Products {
		productIDs = append(productIDs, p.ProductID)
	}

	// Génération des placeholders (?,?,?)
	placeholders := strings.TrimRight(strings.Repeat("?,", len(productIDs)), ",")

	// 2. La Query (Identique au PHP)
	// On utilise le CASE pour déterminer le statut et HAVING pour filtrer
	query := fmt.Sprintf(`
       SELECT 
           p.product_id, 
           p.name,
           CASE
               WHEN a.product_id IS NOT NULL THEN 'out_of_stock' -- Composant manquant = Indisponible (changer par "missing_component")
               ELSE p.status                        -- Sinon statut du produit
           END as status
       FROM products p
       LEFT JOIN (
           SELECT DISTINCT r.product_id
           FROM requires rq
           INNER JOIN recipes r ON r.recipe_id = rq.recipe_id
           INNER JOIN components c ON rq.component_id = c.component_id 
               AND c.status IN ('0','out_of_stock')      -- Composant inactif/épuisé
               AND rq.enabled = TRUE -- Recette active
       ) a ON a.product_id = p.product_id
       WHERE p.merchant_id = ?
       AND p.product_id IN (%s)
       HAVING status IN ('0','out_of_stock')
    `, placeholders)

	// 3. Préparation des arguments (MerchantID + Liste des ProductIDs)
	args := make([]interface{}, 0, len(productIDs)+1)
	args = append(args, req.MerchantID)
	args = append(args, productIDs...)

	// 4. Exécution
	rows, err := r.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// 5. Mapping des résultats
	results := []models.UnavailableProductInfo{}

	for rows.Next() {
		var info models.UnavailableProductInfo
		// Scan doit correspondre à l'ordre du SELECT : product_id, name, status
		if err := rows.Scan(&info.ProductID, &info.Name, &info.Status); err != nil {
			return nil, err
		}
		results = append(results, info)
	}

	// Vérification d'erreurs post-itération
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

func (r *OrdersRepository) GetProductsForPricing(ctx context.Context, req *models.PricingRequest) ([]models.DBProduct, error) {
	if len(req.Order.Products) == 0 {
		return []models.DBProduct{}, nil
	}

	productIDs := make([]string, 0)
	for _, p := range req.Order.Products {
		productIDs = append(productIDs, p.ProductID)
	}

	placeholders := strings.TrimRight(strings.Repeat("?,", len(productIDs)), ",")

	query := fmt.Sprintf(`
		SELECT 
		    p.product_id,
		    p.name,
		    p.price,
		    p.price_take_away,
		    p.price_delivery,
		    tva_in.tva_rate AS tva_rate_in,
		    tva_delivery.tva_rate AS tva_rate_delivery,
		    tva_take_away.tva_rate AS tva_rate_take_away
		FROM products p
		INNER JOIN tva_categories tva_in ON tva_in.tva_id = p.tva_in_id
		INNER JOIN tva_categories tva_delivery ON tva_delivery.tva_id = p.tva_delivery_id
		INNER JOIN tva_categories tva_take_away ON tva_take_away.tva_id = p.tva_take_away_id
		WHERE p.merchant_id = ?
		AND p.product_id IN (%s)
	`, placeholders)

	args := []interface{}{req.MerchantID}
	for _, id := range productIDs {
		args = append(args, id)
	}

	rows, err := r.database.QueryContext(ctx, query, args...)
	if err != nil {
		logger.FromContext(ctx).Error(err.Error())
		return nil, err
	}
	defer rows.Close()

	out := []models.DBProduct{}

	for rows.Next() {
		p := models.DBProduct{}
		err := rows.Scan(
			&p.ProductID,
			&p.Name,
			&p.Price,
			&p.PriceTakeAway,
			&p.PriceDelivery,
			&p.TVARateIn,
			&p.TVARateDelivery,
			&p.TVARateTakeAway,
		)
		if err != nil {
			logger.FromContext(ctx).Error(err.Error())
			return nil, err
		}
		out = append(out, p)
	}

	return out, nil
}

func (r *OrdersRepository) GetDiscounts(ctx context.Context, req *models.PricingRequest) ([]*models.DBDiscount, error) {
	query := `
		SELECT 
			d.discount_id,
			d.discount_order_type,
			d.discount_code,
			d.discount_name,
			d.discount_desc,
			d.discount_value,
			d.discount_unit,
			d.min_order_value,
			d.min_order_unit,
			d.max_discount_value,
			d.max_discount_unit,
			d.discounted_quantity,
			d.is_cumulative,
			d.available,
			d.prefered_order
		FROM discounts d
		LEFT JOIN discounts_schedules ds ON ds.discount_id = d.discount_id
		WHERE d.merchant_id = ?
		  AND (d.valid_from < UTC_TIMESTAMP() AND (d.valid_to > UTC_TIMESTAMP() OR d.valid_to IS NULL))
		  AND ((TIME(UTC_TIMESTAMP()) BETWEEN ds.available_from AND ds.available_to AND DAYOFWEEK(UTC_TIMESTAMP()) = ds.day_of_week)
		       OR NOT d.is_time_limited)
		  AND d.available = TRUE
		ORDER BY d.prefered_order ASC
	`

	rows, err := r.database.QueryContext(ctx, query, req.MerchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.DBDiscount

	for rows.Next() {
		var d models.DBDiscount
		err := rows.Scan(
			&d.DiscountID,
			&d.DiscountOrderType,
			&d.DiscountCode,
			&d.DiscountName,
			&d.DiscountDesc,
			&d.DiscountValue,
			&d.DiscountUnit,
			&d.MinOrderValue,
			&d.MinOrderUnit,
			&d.MaxDiscountValue,
			&d.MaxDiscountUnit,
			&d.DiscountedQuantity,
			&d.IsCumulative,
			&d.Available,
			&d.PreferredOrder,
		)
		if err != nil {
			return nil, err
		}

		out = append(out, &d)
	}

	return out, nil
}

func (r *OrdersRepository) GetDiscountProducts(ctx context.Context, merchantID string) (map[string]map[string]*models.DiscountProductInfo, error) {
	query := `
		SELECT dp.discount_id, dp.product_id, dp.new_price
		FROM discounts_products dp
		INNER JOIN discounts d ON d.discount_id = dp.discount_id
		WHERE d.merchant_id = ?
	`

	rows, err := r.database.QueryContext(ctx, query, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]map[string]*models.DiscountProductInfo{}

	for rows.Next() {
		var discountID, productID string
		var newPrice sql.NullInt64

		if err := rows.Scan(&discountID, &productID, &newPrice); err != nil {
			return nil, err
		}

		if _, exists := out[discountID]; !exists {
			out[discountID] = map[string]*models.DiscountProductInfo{}
		}

		var p int
		if newPrice.Valid {
			v := newPrice.Int64
			p = int(v)
		}

		out[discountID][productID] = &models.DiscountProductInfo{
			ProductID: productID,
			NewPrice:  p,
		}
	}

	return out, nil
}

func (r *OrdersRepository) GetDiscountProductOptions(ctx context.Context, merchantID string) (map[string]map[string][]models.DiscountOptionInfo, error) {
	query := `
		SELECT dpo.option_id, dpo.product_id, dpo.discount_id, dpo.new_price, dpo.is_option_mandatory
                FROM discounts d
                INNER JOIN discounts_products dp ON dp.discount_id = d.discount_id
                INNER JOIN discounts_products_options dpo ON dpo.discount_id = d.discount_id AND dpo.product_id = dp.product_id
                LEFT JOIN discounts_schedules ds ON ds.discount_id = d.discount_id
                WHERE merchant_id = ?
                  AND (valid_from < UTC_TIMESTAMP AND (valid_to > UTC_TIMESTAMP OR valid_to IS NULL))
                  AND ((available_from < UTC_TIMESTAMP AND available_to > UTC_TIMESTAMP) OR NOT is_time_limited)
                  AND d.available IS TRUE
	`

	rows, err := r.database.QueryContext(ctx, query, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]map[string][]models.DiscountOptionInfo{}

	for rows.Next() {
		var discountID, productID, optionID string
		var newPrice sql.NullInt64
		var mandatory sql.NullBool

		if err := rows.Scan(&discountID, &productID, &optionID, &newPrice, &mandatory); err != nil {
			return nil, err
		}

		if _, exists := out[discountID]; !exists {
			out[discountID] = map[string][]models.DiscountOptionInfo{}
		}

		var np *int
		if newPrice.Valid {
			v := int(newPrice.Int64)
			np = &v
		}

		out[discountID][productID] = append(out[discountID][productID], models.DiscountOptionInfo{
			OptionID:          optionID,
			IsOptionMandatory: mandatory.Bool,
			NewPrice:          np,
		})
	}

	return out, nil
}

func (r *OrdersRepository) GetRewards(ctx context.Context, req *models.PricingRequest) ([]*models.DBReward, error) {
	if req.Order.Customer == nil || len(req.Order.Customer.AvailableRewards) == 0 {
		return []*models.DBReward{}, nil
	}

	rewardIDs := make([]string, 0)
	for _, rw := range req.Order.Customer.AvailableRewards {
		rewardIDs = append(rewardIDs, rw.RewardID)
	}

	placeholders := strings.TrimRight(strings.Repeat("?,", len(rewardIDs)), ",")

	query := fmt.Sprintf(`
		SELECT 
		    cr.reward_id,
		    cr.reward_type,
		    cr.reward_order_type,
		    cr.reward_value,
		    cr.loyalty_program_id,
		    cr.creation_date,
		    cr.is_used,
		    COALESCE(clp.min_order_value, 0) AS min_order_value,
		    clp.max_discount_value,
		    COALESCE(clp.max_rewards_per_order, 0) AS max_rewards_per_order
		FROM customer_rewards cr
		INNER JOIN customer_loyalty_programs clp ON clp.id = cr.loyalty_program_id
		WHERE cr.reward_id IN (%s)
		  AND cr.usage_date IS NULL
		  AND cr.is_used = FALSE
	`, placeholders)

	args := make([]interface{}, len(rewardIDs))
	for i, id := range rewardIDs {
		args[i] = id
	}

	rows, err := r.database.QueryContext(ctx, query, args...)
	if err != nil {
		logger.FromContext(ctx).Error(err.Error())
		return nil, err
	}
	defer rows.Close()

	outMap := map[string]*models.DBReward{}

	for rows.Next() {
		rw := &models.DBReward{}
		var maxDiscountValue sql.NullInt64
		err := rows.Scan(
			&rw.RewardID,
			&rw.RewardType,
			&rw.RewardOrderType,
			&rw.RewardValue,
			&rw.LoyaltyProgramID,
			&rw.CreationDate,
			&rw.IsUsed,
			&rw.MinOrderValue,
			&maxDiscountValue,
			&rw.MaxRewardsPerOrder,
		)
		if err != nil {
			logger.FromContext(ctx).Error(err.Error())
			return nil, err
		}

		if maxDiscountValue.Valid {
			v := int(maxDiscountValue.Int64)
			rw.MaxDiscountValue = &v
		}

		rw.ProductIDs = []string{}
		outMap[rw.RewardID] = rw
	}

	if len(outMap) == 0 {
		return []*models.DBReward{}, nil
	}

	// Load related products
	placeholders2 := strings.TrimRight(strings.Repeat("?,", len(outMap)), ",")

	query2 := fmt.Sprintf(`
		SELECT cr.reward_id, clprp.product_id
		FROM customer_rewards cr
		JOIN customer_loyalty_programs clp ON clp.id = cr.loyalty_program_id
		JOIN customer_loyalty_program_reward_products clprp ON clprp.loyalty_program_id = clp.id
		WHERE cr.reward_id IN (%s)
	`, placeholders2)

	args2 := make([]interface{}, 0, len(outMap))
	for id := range outMap {
		args2 = append(args2, id)
	}

	rows2, err := r.database.QueryContext(ctx, query2, args2...)
	if err != nil {
		logger.FromContext(ctx).Error(err.Error())
		return nil, err
	}
	defer rows2.Close()

	for rows2.Next() {
		var rewardID, productID string
		if err := rows2.Scan(&rewardID, &productID); err != nil {
			logger.FromContext(ctx).Error(err.Error())
			return nil, err
		}

		outMap[rewardID].ProductIDs = append(outMap[rewardID].ProductIDs, productID)
	}

	// map → slice
	out := make([]*models.DBReward, 0, len(outMap))
	for _, rw := range outMap {
		out = append(out, rw)
	}

	return out, nil
}

func (r *OrdersRepository) GetEstimatedDistributionTime(ctx context.Context, req *models.PricingRequest, count int) (int, error) {
	rows, err := r.database.QueryContext(ctx, "CALL GET_AVERAGE_DISTRIBUTION_TIME(?, ?)", req.MerchantID, count)
	if err != nil {
		logger.FromContext(ctx).Error(err.Error())
		return 0, err
	}
	defer rows.Close()

	var sec int
	if rows.Next() {
		if err := rows.Scan(&sec); err != nil {
			logger.FromContext(ctx).Error(err.Error())
			return 0, err
		}
	}

	return sec, nil
}

func (r *OrdersRepository) GetConfigurationOptionPrices(ctx context.Context, optionIDs []string) (map[string]int, error) {

	if len(optionIDs) == 0 {
		return map[string]int{}, nil
	}

	placeholders := strings.TrimRight(strings.Repeat("?,", len(optionIDs)), ",")
	query := fmt.Sprintf(`
        SELECT id, extra_price
        FROM configurable_attribute_options
        WHERE id IN (%s)
    `, placeholders)

	args := make([]interface{}, len(optionIDs))
	for i, id := range optionIDs {
		args[i] = id
	}

	rows, err := r.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]int{}

	for rows.Next() {
		var (
			id    string
			price int
		)
		if err := rows.Scan(&id, &price); err != nil {
			return nil, err
		}
		out[id] = price
	}

	return out, nil
}

func (r *OrdersRepository) ExistsByBrandOrderID(ctx context.Context, brand, brandOrderID string) (bool, error) {
	var exists bool
	db := dbutils.GetDB(ctx, r.database)

	// La requête SELECT 1 est très légère pour la DB
	query := `
		SELECT EXISTS(
			SELECT 1 
			FROM orders 
			WHERE brand = ? 
			  AND brand_order_id = ?
		)
	`

	err := db.QueryRowContext(ctx, query, brand, brandOrderID).Scan(&exists)
	if err != nil {
		return false, err
	}

	return exists, nil
}
