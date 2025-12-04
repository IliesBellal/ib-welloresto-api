package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

type OrdersRepository struct {
	db            *sql.DB
	log           *zap.Logger
	ordersFetcher *OrdersFetcher
}

func NewOrdersRepository(db *sql.DB, log *zap.Logger) *OrdersRepository {
	temp := NewOrdersFetcher(db, log)
	return &OrdersRepository{
		db:            db,
		ordersFetcher: temp,
		log:           log}
}

// ==================================================================================
// PUBLIC METHODS
// ==================================================================================

// GetPendingOrders : Récupère toutes les commandes en cours (Optimisé)
func (r *OrdersRepository) GetPendingOrders(ctx context.Context, merchantID, app string) (*models.PendingOrdersResponse, error) {
	r.log.Info("GetPendingOrders START", zap.String("merchant_id", merchantID))

	// On a besoin du repo session pour récupérer les sessions à la fin
	deliverySessionRepo := NewDeliverySessionsRepository(r.db, r.log)

	// ========================================================================
	// ÉTAPE 1 : OPTIMISATION - Récupérer les IDs d'abord
	// ========================================================================

	// 1.a. On construit la clause WHERE complexe ici
	criteria := " AND ((o.state IN ('OPEN') AND o.brand_status NOT IN('ONLINE_PAYMENT_PENDING')) OR ds.id IS NOT NULL) "

	// Ajout filtre APP
	if app == "1" || app == "WR_DELIVERY" {
		criteria += " AND o.order_type = 'DELIVERY' AND o.fulfillment_type = 'DELIVERY_BY_RESTAURANT' "
	} else if app == "2" || app == "WR_WAITER" {
		criteria += " AND o.order_type NOT IN ('DELIVERY','TAKE_AWAY') "
	}

	// 1.b. Requête légère pour récupérer UNIQUEMENT les IDs
	// On doit inclure les JOINs ici pour que le filtre fonctionne (alias 'o' et 'ds')
	qIDs := `SELECT DISTINCT o.order_id
             FROM orders o
             LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id
             LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id AND ds.status IN ('1','PENDING')
             WHERE o.merchant_id = ? ` + criteria

	rows, err := r.db.QueryContext(ctx, qIDs, merchantID)
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

	// ========================================================================
	// CAS VIDE : Si aucune commande ne correspond, on sort tout de suite
	// ========================================================================
	if len(orderIDs) == 0 {
		// On retourne vide, mais on récupère quand même les sessions vides si nécessaire,
		// ou on retourne tout vide. Selon ton besoin métier.
		// Ici je retourne tout vide pour être rapide.
		return &models.PendingOrdersResponse{
			Orders:           []models.Order{},
			DeliverySessions: []models.DeliverySession{},
		}, nil
	}

	// ========================================================================
	// ÉTAPE 2 : Appeler le constructeur avec le filtre OPTIMISÉ (IN)
	// ========================================================================

	// Construction de la chaîne "IN ('id1', 'id2')"
	idsStr := ""
	for i, oid := range orderIDs {
		if i > 0 {
			idsStr += ","
		}
		idsStr += fmt.Sprintf("'%s'", oid)
	}

	// Le filtre magique qui va rendre les 11 requêtes suivantes instantanées
	filterOptimized := fmt.Sprintf(" AND o.order_id IN (%s) ", idsStr)

	orders, err := r.ordersFetcher.fetchAndBuildOrders(ctx, merchantID, filterOptimized)
	if err != nil {
		return nil, err
	}

	// ========================================================================
	// ÉTAPE 3 : Récupérer les sessions et finaliser
	// ========================================================================

	// Récupérer les sessions (spécifique à cet endpoint)
	// Note : comme on est dans le même package 'repositories', on a accès aux méthodes privées (minuscule)
	sessions, err := deliverySessionRepo.fetchDeliverySessions(ctx, merchantID, "status IN ('1','PENDING')")
	if err != nil {
		return nil, err
	}

	// Assemblage final
	return &models.PendingOrdersResponse{
		Orders:           orders,
		DeliverySessions: sessions,
	}, nil
}

func (r *OrdersRepository) GetOrder(ctx context.Context, merchantID string, orderID string) (*models.Order, error) {
	r.log.Info("GetOrder START", zap.String("order_id", orderID))

	// Filtre strict sur l'ID
	filter := fmt.Sprintf(" AND o.order_id = '%s' ", orderID)

	orders, err := r.ordersFetcher.fetchAndBuildOrders(ctx, merchantID, filter)
	if err != nil {
		return nil, err
	}
	if len(orders) == 0 {
		return nil, sql.ErrNoRows
	}

	return &orders[0], nil
}

func (r *OrdersRepository) ReopenClosedOrder(ctx context.Context, merchantID, orderID, userID string) error {
	r.log.Info("ReopenClosedOrder START", zap.String("order_id", orderID))

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// -------------------------
	//  FUTURE VALIDATIONS HERE
	// -------------------------
	// Exemple :
	// - vérifier que la commande existe
	// - vérifier qu’elle est bien "CLOSED"
	// - vérifier que userID a le droit
	// - vérifier registre de caisse
	// --------------------------------------

	// ---- 1. Avant
	var beforeState string
	err = tx.QueryRowContext(ctx, `
		SELECT state FROM orders WHERE order_id = ?
	`, orderID).Scan(&beforeState)
	if err != nil {
		return fmt.Errorf("cannot load before state: %w", err)
	}

	// ---- 2. Update
	_, err = tx.ExecContext(ctx, `
		UPDATE orders 
		SET state = 'OPEN'
		WHERE order_id = ? AND merchant_id = ?
	`, orderID, merchantID)
	if err != nil {
		return fmt.Errorf("reopen update failed: %w", err)
	}

	// ---- 3. Après
	var afterState string
	err = tx.QueryRowContext(ctx, `
		SELECT state FROM orders WHERE order_id = ?
	`, orderID).Scan(&afterState)
	if err != nil {
		return fmt.Errorf("cannot load after state: %w", err)
	}

	// ---- 4. Log si changement
	if beforeState != afterState {
		r.log.Info("Order state changed",
			zap.String("order_id", orderID),
			zap.String("old", beforeState),
			zap.String("new", afterState),
			zap.String("user_id", userID),
		)

		// TODO : appeler équivalent Go de logOrderChange(...)
	}

	// commit
	if err := tx.Commit(); err != nil {
		return err
	}

	// TODO: Send update notification (équivalent sendUpdateOrderNotification)
	return nil
}

func (r *OrdersRepository) GetHistory(ctx context.Context, merchantID string, req models.OrderHistoryRequest) ([]models.Order, error) {
	r.log.Info("GetHistory START", zap.String("merchant_id", merchantID))

	filter := fmt.Sprintf(
		" AND o.state = 'CLOSED' "+
			"AND o.creation_date BETWEEN '%s' AND '%s' ",
		req.DateFrom, req.DateTo,
	)

	return r.ordersFetcher.fetchAndBuildOrders(ctx, merchantID, filter)
}

func (r *OrdersRepository) AddPayment(ctx context.Context, merchantID, userID string, req *models.PaymentRequest) error {
	r.log.Info("AddPayment START", zap.String("order_id", req.OrderID))

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx failed: %w", err)
	}

	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}

	// ---------------------------------------------------
	// 🎯 SECTION FUTURE : tes vérifications métier ici !
	//
	// Exemple à venir :
	// - vérifier si la commande est déjà totalement payée
	// - vérifier si le MOP est autorisé
	// - vérifier si le caissier a le droit
	//
	// ---------------------------------------------------

	// 1. Trouver cash_register_id
	var cashRegisterID sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT cr.cash_register_id
		FROM cash_registers cr
		LEFT JOIN sub_cash_registers scr ON scr.cash_register_id = cr.cash_register_id
		WHERE (cr.device_id = ? OR scr.device_id = ?)
		AND cr.end_date IS NULL
	`, req.DeviceID, req.DeviceID).Scan(&cashRegisterID)
	if err == sql.ErrNoRows {
		cashRegisterID.String = req.DeviceID
		cashRegisterID.Valid = true
	} else if err != nil {
		return rollback(err)
	}

	// 2. Paiement total déjà effectué ?
	var totalPrice, alreadyPaid float64
	_ = tx.QueryRowContext(ctx, `
		SELECT o.price, COALESCE(SUM(p.amount),0)
		FROM orders o
		LEFT JOIN payments p ON p.order_id = o.order_id AND p.enabled = 1
		WHERE o.order_id = ?
		GROUP BY o.order_id
	`, req.OrderID).Scan(&totalPrice, &alreadyPaid)

	// 3. Si MOP != CURRENCY/PERCENTAGE ⇒ gérer les orderitems
	if req.MOP != "CURRENCY" && req.MOP != "PERCENTAGE" {

		if len(req.Items) == 0 {
			// Paiement total
			_, err := tx.ExecContext(ctx, `
            UPDATE orderitems
            SET isPaid = 1, paid_quantity = quantity
            WHERE order_id = ? AND merchant_id = ?
        `, req.OrderID, merchantID)
			if err != nil {
				return fmt.Errorf("update full payment error: %w", err)
			}

		} else {
			// Paiement partiel
			for _, itm := range req.Items {

				itemID := itm.OrderItemID
				qty := itm.Quantity

				_, err := tx.ExecContext(ctx, `
                UPDATE orderitems
                SET 
                    paid_quantity = paid_quantity + ?,
                    isPaid = (quantity <= paid_quantity + ?)
                WHERE 
                    order_id = ?
                    AND order_item_id = ?
                    AND merchant_id = ?
            `, qty, qty, req.OrderID, itemID, merchantID)

				if err != nil {
					return fmt.Errorf("update partial payment error (item %s): %w", itemID, err)
				}
			}
		}
	}

	// 4. Insérer le paiement
	res, err := tx.ExecContext(ctx, `
		INSERT INTO payments
		(merchant_id, cash_register_id, order_id, amount, mop, comment, payment_date, user_id, status_check)
		VALUES (?, ?, ?, ROUND(?,2), ?, ?, UTC_TIMESTAMP, ?, ?)
	`, merchantID, cashRegisterID.String, req.OrderID, req.Amount, req.MOP, req.DiscountComment, userID, req.StatusCheck)
	if err != nil {
		return rollback(err)
	}

	paymentID, _ := res.LastInsertId()

	// 5. Ticket restaurant (TR)
	if req.MOP == "TR" && req.Code != "" {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO restaurant_ticket (merchant_id, payment_id, barcode)
			VALUES (?, ?, ?)
		`, merchantID, paymentID, req.Code)
		if err != nil {
			return rollback(err)
		}
	}

	// 6. Mettre à jour orders.isPaid
	_, err = tx.ExecContext(ctx, `
		UPDATE orders o
		INNER JOIN (
			SELECT order_id, SUM(amount) AS paid
			FROM payments
			WHERE enabled = 1 AND order_id = ?
			GROUP BY order_id
		) p ON p.order_id = o.order_id
		SET o.isPaid = (o.price <= p.paid)
		WHERE o.order_id = ?
	`, req.OrderID, req.OrderID)
	if err != nil {
		return rollback(err)
	}

	err = tx.Commit()
	if err != nil {
		return rollback(err)
	}

	r.log.Info("AddPayment DONE", zap.String("order_id", req.OrderID))
	return nil
}

func (r *OrdersRepository) GetPaymentsForOrder(ctx context.Context, orderID string) ([]models.Payment, error) {
	r.log.Info("GetPaymentsForOrder START", zap.String("order_id", orderID))

	q := `
		SELECT order_id, payment_id, mop, amount, payment_date, enabled
		FROM payments
		WHERE order_id = ?
		ORDER BY payment_date ASC
	`

	rows, err := r.db.QueryContext(ctx, q, orderID)
	if err != nil {
		r.log.Error("GetPaymentsForOrder ERROR", zap.Error(err))
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
			p.PaymentDate = &paymentDate.Time
		}

		payments = append(payments, p)
	}

	return payments, nil
}

func (r *OrdersRepository) DisablePayment(ctx context.Context, paymentID string) error {
	r.log.Info("DisablePayment START", zap.String("payment_id", paymentID))

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	// Disable payment
	_, err = tx.ExecContext(ctx, `
		UPDATE payments SET enabled = 0 WHERE payment_id = ?
	`, paymentID)
	if err != nil {
		tx.Rollback()
		return err
	}

	// Refresh order as unpaid
	_, err = tx.ExecContext(ctx, `
		UPDATE orders o 
		JOIN payments p ON o.order_id = p.order_id
		SET o.isPaid = false, o.last_update = UTC_TIMESTAMP()
		WHERE p.payment_id = ?
	`, paymentID)
	if err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (r *OrdersRepository) SetDistributedProducts(ctx context.Context, userID string, merchantID string, req *models.SetDistributedProductsRequest) error {

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	orderID := req.OrderID

	for _, p := range req.Products {

		// ----- BEFORE value -----
		var beforeIsDistributed sql.NullInt64
		err = tx.QueryRowContext(ctx, `
			SELECT isDistributed
			FROM orderitems
			WHERE order_id = ? AND order_item_id = ?
		`, orderID, p.OrderItemID).Scan(&beforeIsDistributed)

		if err != nil {
			return err
		}

		// ----- UPDATE ORDER ITEM -----
		_, err = tx.ExecContext(ctx, `
			UPDATE orderitems
			SET 
				isDistributed = 1,
				distributed_quantity = quantity,
				ready_for_distribution_quantity = quantity,
				distributed_on = UTC_TIMESTAMP
			WHERE order_id = ? AND order_item_id = ?
		`, orderID, p.OrderItemID)
		if err != nil {
			return err
		}

		// ----- Check if all items distributed -----
		var existsNotDistributed int
		err = tx.QueryRowContext(ctx, `
			SELECT 1
			FROM orders
			INNER JOIN orderitems ON orderitems.order_id = orders.order_id
			WHERE orders.order_id = ?
			AND orders.merchant_id = ?
			AND orderitems.isDistributed = 0
			LIMIT 1
		`, orderID, merchantID).Scan(&existsNotDistributed)

		if err != nil && err != sql.ErrNoRows {
			return err
		}

		orderFullyDistributed := "1"
		if existsNotDistributed == 1 {
			orderFullyDistributed = "0"
		}

		// ----- UPDATE ORDER -----
		_, err = tx.ExecContext(ctx, `
			UPDATE orders
			SET 
				isDistributed = ?,
				delivered_on = CASE 
					WHEN ? = '0' OR order_type = 'DELIVERY' THEN delivered_on
					ELSE UTC_TIMESTAMP
				END,
				brand_status = CASE
					WHEN order_type = 'DELIVERY' AND ? = '1' THEN 'READY_FOR_HANDOFF'
					WHEN order_type = 'TAKE_AWAY' AND ? = '1' THEN 'READY_FOR_TAKE_AWAY'
					WHEN ? = '0' THEN 'PENDING'
					ELSE 'DONE'
				END,
				last_update = UTC_TIMESTAMP
			WHERE order_id = ? AND merchant_id = ?
		`, orderFullyDistributed, orderFullyDistributed, orderFullyDistributed, orderFullyDistributed, orderFullyDistributed, orderID, merchantID)

		if err != nil {
			return err
		}

		// ----- Log order change (replicates PHP) -----
		r.log.Info("Order change logged",
			zap.String("order_id", orderID),
			zap.String("changed_by_user_id", userID),
			zap.String("field", "isDistributed"),
			zap.String("old_value", strconv.Itoa(int(beforeIsDistributed.Int64))),
			zap.String("new_value", "1"),
		)
	}

	// ------ Get brand ------
	var brand sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT brand
		FROM orders
		WHERE order_id = ?
	`, orderID).Scan(&brand)

	if err != nil {
		return err
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	// ------ Notifications / Integrations ------
	if brand.String == "UBER_EATS" {
		r.log.Info("Would call UberEats setOrderReady", zap.String("order_id", orderID))
	} else if brand.String == "DELIVEROO" {
		r.log.Info("Would call Deliveroo setOrderReady", zap.String("order_id", orderID))
	} else {
		r.log.Info("Sending update order notification", zap.String("order_id", orderID))
		// r.sendOrderUpdateNotification(merchantID, orderID)
	}

	return nil
}

func (s *OrdersRepository) CreateOrder(ctx context.Context, req *models.CreateOrderRequest) (*models.CreateOrderResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}

	// --- Étapes (appelées dans service_steps.go) -------------------------

	unavailable, err := s.validateProductAvailability(ctx, tx, req)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if len(unavailable) > 0 {
		tx.Rollback()
		return &models.CreateOrderResult{Status: "2"}, nil
	}

	customerID, err := s.upsertCustomer(ctx, tx, req)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	orderID, orderNum, err := s.insertOrderBase(ctx, tx, req, customerID)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	usedItems, err := s.insertOrderItems(ctx, tx, req, orderID)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	if err := s.insertExtrasWithoutsConfigs(ctx, tx, req, usedItems); err != nil {
		tx.Rollback()
		return nil, err
	}

	if err := s.insertPayments(ctx, tx, req, orderID); err != nil {
		tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Post-commit async
	// go s.notifier.SendNewOrderNotification(orderID)

	return &models.CreateOrderResult{
		Status:     "1",
		OrderID:    orderID,
		OrderNum:   orderNum,
		OrderItems: usedItems,
		Action:     "waiting",
	}, nil
}

// validateProductAvailability checks for products that become unavailable because of components status = 0
func (s *OrdersRepository) validateProductAvailability(ctx context.Context, tx *sql.Tx, req *models.CreateOrderRequest) ([]int64, error) {
	// build list of product ids from request
	if len(req.Order.Products) == 0 {
		return nil, nil
	}
	ids := make([]interface{}, 0, len(req.Order.Products))
	placeholders := make([]int, 0, len(req.Order.Products))
	for i, p := range req.Order.Products {
		ids = append(ids, p.ProductID)
		placeholders = append(placeholders, i)
	}
	// SQL: find products that have missing components (source: PHP query)
	// We adapt to a parameterized query (Postgres style with $n). If you use MySQL, replace placeholders with ? and adapt Exec accordingly.
	query := fmt.Sprintf(`
SELECT DISTINCT p.product_id
FROM products p
LEFT JOIN (
    SELECT DISTINCT r.product_id
    FROM requires rq
    INNER JOIN recipes r ON r.recipe_id = rq.recipe_id
    INNER JOIN components c ON rq.component_id = c.component_id AND c.status = 0 AND rq.enabled = true
) a ON a.product_id = p.product_id
WHERE p.product_id IN (?)
AND (CASE WHEN a.product_id IS NOT NULL THEN 0 ELSE p.status END) = 0
`, joinPlaceholders(len(ids), 1))

	rows, err := tx.QueryContext(ctx, query, ids...)
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

// upsertCustomer calls the customer repository to create/update the customer and returns numeric ID (nil if none)
func (s *OrdersRepository) upsertCustomer(ctx context.Context, tx *sql.Tx, req *models.CreateOrderRequest) (*int64, error) {
	if req.Order.Customer == nil {
		return nil, nil
	}

	// Convert our Order CustomerPayload to the models.Customer expected by CustomerRepository
	cust := &models.Customer{
		MerchantID: req.MerchantID,
	}
	if req.Order.Customer.CustomerID != nil {
		// CustomerRepository expects string id often; adapt if needed
		idStr := *req.Order.Customer.CustomerID
		cust.CustomerID = &idStr
	}
	if req.Order.Customer.Name != nil {
		cust.CustomerName = req.Order.Customer.Name
	}
	if req.Order.Customer.Tel != nil {
		cust.CustomerTel = req.Order.Customer.Tel
	}
	if req.Order.Customer.Address != nil {
		cust.CustomerAddress = req.Order.Customer.Address
	}
	if req.Order.Customer.Lat != nil {
		cust.CustomerLat = req.Order.Customer.Lat
	}
	if req.Order.Customer.Lng != nil {
		cust.CustomerLng = req.Order.Customer.Lng
	}
	// ... map other fields as needed

	// CustomerRepository.UpdateOrCreateCustomer should be transaction-aware; if not, it will open its own transaction.
	// We call it directly. It returns ID as string.
	custoRepo := NewCustomerRepository(s.db, s.log)
	newIDStr, err := custoRepo.UpdateOrCreateCustomer(ctx, cust)
	if err != nil {
		return nil, fmt.Errorf("failed to update/create customer: %w", err)
	}
	if newIDStr == "" {
		return nil, nil
	}
	newIDInt, err := strconv.ParseInt(newIDStr, 10, 64)
	if err != nil {
		// return the string wrapped if parse fails
		return nil, fmt.Errorf("customer id parse error: %w", err)
	}
	return &newIDInt, nil
}

// insertOrderBase inserts the orders row and returns orderID and orderNum
func (s *OrdersRepository) insertOrderBase(ctx context.Context, tx *sql.Tx, req *models.CreateOrderRequest, customerID *int64) (orderID string, orderNum int64, err error) {
	// determine orderNum (simple approach: take max + 1). For performance you may want a sequence.
	var lastOrderNum sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT order_num
		FROM orders
		WHERE merchant_id = ?
		ORDER BY order_id DESC
		LIMIT 1
		`, req.MerchantID).Scan(&lastOrderNum)
	if err != nil && err != sql.ErrNoRows {
		return "0", 0, err
	}
	if lastOrderNum.Valid {
		orderNum = lastOrderNum.Int64 + 1
	} else {
		orderNum = 1
	}

	// default fields and estimated_ready handling simplified: use UTC_TIMESTAMP equivalent in SQL
	res, err := tx.ExecContext(ctx, `
		INSERT INTO orders(cash_register_id, merchant_id, customer_id, order_num, price, TVA, HT, isDelivery, merchant_approval, means_of_payement, scheduled, creation_date, dateCall, last_update, responsible, created_by, delivery_fees, estimated_ready, use_customer_temporary_address, brand_status, order_type, places_settings, pager_number)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, UTC_TIMESTAMP, UTC_TIMESTAMP, UTC_TIMESTAMP, ?, ?, ?, UTC_TIMESTAMP, ?, ?, ?, ?, ?)`,
		req.DeviceID, req.MerchantID, nullableInt64(customerID), orderNum, req.Order.TTC, req.Order.TVA, req.Order.HT,
		false, // isDelivery simplified, adapt from req.Order.OrderType if needed
		req.Order.MerchantApproval, nil, boolToInt(req.Order.IsScheduled),
		req.Order.Responsible, req.Order.CreatedBy, req.Order.DeliveryFees, req.Order.EstimatedReady,
		boolToInt(req.Order.UseCustomerTemporaryAddress), req.Order.BrandStatus, req.Order.OrderType, req.Order.PlacesSettings, req.Order.PagerNumber,
	)
	if err != nil {
		return "0", 0, err
	}
	lastID, err := res.LastInsertId()
	if err != nil {
		return "0", 0, err
	}
	return strconv.FormatInt(lastID, 10), orderNum, nil
}

// insertOrderItems inserts each orderitem and returns list of UsedItem (order_item_id + qty)
func (s *OrdersRepository) insertOrderItems(ctx context.Context, tx *sql.Tx, req *models.CreateOrderRequest, orderID string) ([]models.UsedItem, error) {
	used := make([]models.UsedItem, 0, len(req.Order.Products))
	for _, p := range req.Order.Products {
		if p.Quantity == 0 {
			continue
		}
		item := &OrderItemInsert{
			OrderID:    orderID,
			ProductID:  p.ProductID,
			MerchantID: req.MerchantID,
			Quantity:   p.Quantity,
			DiscountID: p.DiscountID,
			Price:      p.Price,
			DelayID:    p.DelayID,
		}
		oid, err := s.InsertOrderItem(ctx, tx, item)
		if err != nil {
			return nil, err
		}
		used = append(used, models.UsedItem{OrderItemID: strconv.FormatInt(oid, 10), Quantity: p.Quantity})
	}
	return used, nil
}

// insertExtrasWithoutsConfigs does bulk inserts for extras, withouts, configurations
func (s *OrdersRepository) insertExtrasWithoutsConfigs(ctx context.Context, tx *sql.Tx, req *models.CreateOrderRequest, items []models.UsedItem) error {
	// Build maps from product iteration to order_item ids; we used ordering to match the order of products to items
	// Simpler approach: while inserting items we could have returned corresponding mapping; for now assume order preserved.
	extras := []ExtraInsert{}
	withouts := []WithoutInsert{}
	configs := []ConfigInsert{}

	itemIdx := 0
	for _, p := range req.Order.Products {
		if p.Quantity == 0 {
			continue
		}
		if itemIdx >= len(items) {
			return fmt.Errorf("internal mapping error: items length mismatch")
		}
		oid := items[itemIdx].OrderItemID
		// extras
		for _, e := range p.Extra {
			extras = append(extras, ExtraInsert{
				OrderID:     items[itemIdx].OrderItemID, // in DB extra has order_id and order_item_id; we'll provide both
				OrderItemID: oid,
				ComponentID: e.ComponentID,
				ProductID:   p.ProductID,
				MerchantID:  req.MerchantID,
				Price:       e.Price,
			})
		}
		// withouts
		for _, w := range p.Without {
			withouts = append(withouts, WithoutInsert{
				OrderID:     items[itemIdx].OrderItemID,
				OrderItemID: oid,
				ComponentID: w.ComponentID,
				ProductID:   p.ProductID,
				MerchantID:  req.MerchantID,
			})
		}
		// configs
		if p.Config != nil {
			for _, attr := range p.Config.Attributes {
				for _, opt := range attr.Options {
					configs = append(configs, ConfigInsert{
						OrderItemID: oid,
						AttributeID: attr.ID,
						OptionID:    opt.ID,
						Quantity:    opt.Quantity,
					})
				}
			}
		}
		itemIdx++
	}

	if len(extras) > 0 {
		if err := s.BulkInsertExtras(ctx, tx, extras); err != nil {
			return err
		}
	}
	if len(withouts) > 0 {
		if err := s.BulkInsertWithouts(ctx, tx, withouts); err != nil {
			return err
		}
	}
	if len(configs) > 0 {
		if err := s.BulkInsertConfigs(ctx, tx, configs); err != nil {
			return err
		}
	}
	return nil
}

// insertPayments inserts payments
func (s *OrdersRepository) insertPayments(ctx context.Context, tx *sql.Tx, req *models.CreateOrderRequest, orderID string) error {
	for _, p := range req.Order.Payments {
		pi := &PaymentInsert{
			MerchantID:     req.MerchantID,
			CashRegisterID: req.DeviceID,
			OrderID:        orderID,
			Amount:         p.Amount,
			MOP:            p.MOP,
			UserID:         req.Order.CreatedBy,
		}
		if err := s.InsertPayment(ctx, tx, pi); err != nil {
			return err
		}
	}
	return nil
}

// ----------------- helpers -----------------
func joinPlaceholders(n int, start int) string {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = fmt.Sprintf("$%d", start+i)
	}
	return strings.Join(out, ", ")
}

func nullableInt64(i *int64) interface{} {
	if i == nil {
		return nil
	}
	return *i
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
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

// InsertOrder inserts order and returns order_id
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

// OrderItemInsert represents an order item insert
type OrderItemInsert struct {
	OrderID    string
	ProductID  string
	MerchantID string
	Quantity   int
	DiscountID *string
	Price      int
	DelayID    *string
}

// InsertOrderItem inserts a single orderitem and returns its id
func (r *OrdersRepository) InsertOrderItem(ctx context.Context, tx *sql.Tx, item *OrderItemInsert) (int64, error) {
	res, err := tx.ExecContext(ctx, `
		INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, discount_id, price, ordered_on, delay_id)
		VALUES (?, ?, ?, ?, ?, ?, UTC_TIMESTAMP, ?)
		`, item.OrderID, item.ProductID, item.MerchantID, item.Quantity, item.DiscountID, item.Price, item.DelayID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

type ExtraInsert struct {
	OrderID     string
	OrderItemID string
	ComponentID string
	ProductID   string
	MerchantID  string
	Price       int
}
type WithoutInsert struct {
	OrderID     string
	OrderItemID string
	ComponentID string
	ProductID   string
	MerchantID  string
}
type ConfigInsert struct {
	OrderItemID string
	AttributeID string
	OptionID    string
	Quantity    int
}

// BulkInsertExtras performs multi-value insert for extras
func (r *OrdersRepository) BulkInsertExtras(ctx context.Context, tx *sql.Tx, list []ExtraInsert) error {
	if len(list) == 0 {
		return nil
	}
	parts := make([]string, 0, len(list))
	args := make([]interface{}, 0, len(list)*6)
	for _, e := range list {
		parts = append(parts, "(?, ?, ?, ?, ?, ?)")
		args = append(args, e.OrderID, e.OrderItemID, e.ComponentID, e.ProductID, e.MerchantID, e.Price)
	}
	query := "INSERT INTO extra (order_id, order_item_id, component_id, product_id, merchant_id, price) VALUES " + strings.Join(parts, ",")
	_, err := tx.ExecContext(ctx, query, args...)
	return err
}

func (r *OrdersRepository) BulkInsertWithouts(ctx context.Context, tx *sql.Tx, list []WithoutInsert) error {
	if len(list) == 0 {
		return nil
	}
	parts := make([]string, 0, len(list))
	args := make([]interface{}, 0, len(list)*5)
	for _, e := range list {
		parts = append(parts, "(?, ?, ?, ?, ?)")
		args = append(args, e.OrderID, e.OrderItemID, e.ComponentID, e.ProductID, e.MerchantID)
	}
	query := "INSERT INTO without (order_id, order_item_id, component_id, product_id, merchant_id) VALUES " + strings.Join(parts, ",")
	_, err := tx.ExecContext(ctx, query, args...)
	return err
}

func (r *OrdersRepository) BulkInsertConfigs(ctx context.Context, tx *sql.Tx, list []ConfigInsert) error {
	if len(list) == 0 {
		return nil
	}
	parts := make([]string, 0, len(list))
	args := make([]interface{}, 0, len(list)*4)
	for _, c := range list {
		parts = append(parts, "(?, ?, ?, ?)")
		args = append(args, c.OrderItemID, c.AttributeID, c.OptionID, c.Quantity)
	}
	query := "INSERT INTO order_item_configuration (order_item_id, configuration_attribute_id, configuration_attribute_option_id, quantity) VALUES " + strings.Join(parts, ",")
	_, err := tx.ExecContext(ctx, query, args...)
	return err
}

// Payment insert
type PaymentInsert struct {
	MerchantID     string
	CashRegisterID interface{}
	OrderID        string
	Amount         int
	MOP            string
	UserID         *string
}

func (r *OrdersRepository) InsertPayment(ctx context.Context, tx *sql.Tx, p *PaymentInsert) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO payments (merchant_id, cash_register_id, order_id, amount, mop, payment_date, user_id)
VALUES (?, ?, ?, ?, ?, UTC_TIMESTAMP, ?)
`, p.MerchantID, p.CashRegisterID, p.OrderID, p.Amount, p.MOP, p.UserID)
	return err
}
