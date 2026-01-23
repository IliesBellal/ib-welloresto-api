package orders

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/customers"

	"go.uber.org/zap"
)

type OrdersRepository struct {
	db            *sql.DB
	log           *zap.Logger
	ordersFetcher *OrdersFetcher
}

func NewOrdersRepository(db *sql.DB, log *zap.Logger) *OrdersRepository {
	return &OrdersRepository{
		db:            db,
		ordersFetcher: NewOrdersFetcher(db),
		log:           log}
}

// ==================================================================================
// PUBLIC METHODS
// ==================================================================================

// GetPendingOrders : Récupère toutes les commandes en cours (Optimisé)
func (r *OrdersRepository) GetPendingOrders(ctx context.Context, merchantID, app string) (*models.PendingOrdersResponse, error) {
	r.log.Info("GetPendingOrders START", zap.String("merchant_id", merchantID))

	// On a besoin du repo session pour récupérer les sessions à la fin
	//deliverySessionRepo := delivery_sessions.NewDeliverySessionsRepository(r.db, r.log)

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

	orders, err := r.ordersFetcher.FetchAndBuildOrders(ctx, merchantID, filterOptimized, "", "")
	if err != nil {
		return nil, err
	}

	// ========================================================================
	// ÉTAPE 3 : Récupérer les sessions et finaliser
	// ========================================================================

	// Récupérer les sessions (spécifique à cet endpoint)
	// Note : comme on est dans le même package 'repositories', on a accès aux méthodes privées (minuscule)
	sessions, err := []models.DeliverySession{}, nil /*deliverySessionRepo.fetchDeliverySessions(ctx, merchantID, "status IN ('1','PENDING')")*/
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

	// Filtre strict sur l'MerchantID
	filter := fmt.Sprintf(" AND o.order_id = '%s' ", orderID)

	orders, err := r.ordersFetcher.FetchAndBuildOrders(ctx, merchantID, filter, "", "")
	if err != nil {
		return nil, err
	}
	if len(orders) == 0 {
		return nil, sql.ErrNoRows
	}

	return &orders[0], nil
}

func (r *OrdersRepository) GetOrders(ctx context.Context, merchantID string, req *models.OrderRequest) ([]models.Order, error) {
	r.log.Info("GetOrder START", zap.String("merchant_id", merchantID))

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

	rows, err := r.db.QueryContext(ctx, query, args...)
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

func (r *OrdersRepository) GetHistory(
	ctx context.Context,
	merchantID string,
	req models.OrderHistoryRequest,
) ([]models.Order, error) {

	r.log.Info("GetHistory START", zap.String("merchant_id", merchantID))

	// =========================
	// 1️⃣ BUILD WHERE + ARGS
	// =========================
	where := " WHERE o.merchant_id = ? AND o.state = 'CLOSED' "
	args := []interface{}{merchantID}

	if req.DateFrom != nil && req.DateTo != nil {
		where += " AND o.creation_date BETWEEN ? AND ? "
		args = append(args, *req.DateFrom, *req.DateTo)
	}

	// =========================
	// 2️⃣ PAGINATION (IDS ONLY)
	// =========================
	limit := 20
	if req.Limit != nil && *req.Limit > 0 {
		limit = *req.Limit
	}

	page := 1
	if req.Page != nil && *req.Page > 0 {
		page = *req.Page
	}

	offset := (page - 1) * limit

	// =========================
	// 3️⃣ FETCH ORDER IDS
	// =========================
	query := `
		SELECT o.order_id
		FROM orders o
	` + where + `
		ORDER BY o.creation_date DESC
		LIMIT ? OFFSET ?
	`

	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orderIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		orderIDs = append(orderIDs, id)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(orderIDs) == 0 {
		return []models.Order{}, nil
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
	return r.ordersFetcher.FetchAndBuildOrders(
		ctx,
		merchantID,
		whereFilter,
		orderBy,
		"",
	)
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

func (s *OrdersRepository) CreateOrder(ctx context.Context, req *models.RequestObject) (*models.CreateOrderResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)

	log := logger.FromContext(ctx)
	log.Info("Orders.Create - request received")

	defer cancel()

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}

	unavailable, err := s.validateProductAvailability(ctx, tx, req)

	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if len(unavailable) > 0 {
		tx.Rollback()
		return &models.CreateOrderResult{Status: "unavailable_products"}, nil
	}

	customerID, err := s.upsertCustomer(ctx, tx, req)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	// compute estimated ready if not provided
	estimatedReady := req.Order.EstimatedReady // string or empty
	if estimatedReady == "" {
		est, err := s.ComputeEstimatedReady(ctx, tx, req.MerchantID, len(req.Order.Products))
		if err != nil {
			s.log.Warn("ComputeEstimatedReady warning", zap.Error(err))
		}
		if est != "" {
			estimatedReady = est
		}
	}
	req.Order.EstimatedReady = estimatedReady

	// get next order number
	orderNum, err := s.GetNextOrderNum(ctx, tx, req.MerchantID)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	req.Order.OrderNum = &orderNum

	cashRegisterID := "0"
	if req.DeviceID != nil && *req.DeviceID != "" {
		id, found, err := s.GetActiveCashRegisterID(ctx, tx, *req.DeviceID, req.MerchantID)
		if err != nil {
			tx.Rollback()
			return nil, err
		}
		if found {
			cashRegisterID = id
		} else {
			// if no cash register found, check merchant parameter
			required, err := s.IsCashRegisterRequiredForOrdering(ctx, tx, req.MerchantID)
			if err != nil {
				tx.Rollback()
				return nil, err
			}
			if required {
				tx.Rollback()
				return &models.CreateOrderResult{Status: "no_cash_register_opened"}, nil
			}
		}
	}

	req.Order.CashRegisterId = &cashRegisterID

	s.setOrderDefaults(ctx, req)

	orderID, err := s.insertOrderBase(ctx, tx, req, customerID)
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

	return &models.CreateOrderResult{
		Status:     "success",
		OrderID:    orderID,
		OrderNum:   &orderNum,
		OrderItems: usedItems,
		Action:     "waiting",
	}, nil
}

// GetActiveCashRegisterID returns cash_register_id if an open cash register or sub_cash_register is found for this device and merchant.
// returns (id, true, nil) if found, (0, false, nil) if not found, or error.
func (r *OrdersRepository) GetActiveCashRegisterID(ctx context.Context, tx *sql.Tx, deviceID, merchantID string) (string, bool, error) {
	if deviceID == "" {
		return "0", false, nil
	}

	query := `
		SELECT DISTINCT cr.cash_register_id
		FROM cash_registers cr
		LEFT JOIN sub_cash_registers scr ON scr.cash_register_id = cr.cash_register_id
		INNER JOIN cash_desks cd ON (cd.cash_desk_id = cr.cash_desk_id OR cr.cash_desk_id = '-1') AND cr.end_date IS NULL
		WHERE (cr.device_id = ? OR scr.device_id = ?)
		  AND cd.merchant_id = ?
		  AND cr.end_date IS NULL
		LIMIT 1;
		`

	var id sql.NullString
	err := tx.QueryRowContext(ctx, query, deviceID, deviceID, merchantID).Scan(&id)
	if err != nil {
		if err == sql.ErrNoRows {
			return "0", false, nil
		}
		return "0", false, err
	}
	if !id.Valid {
		return "0", false, nil
	}
	return id.String, true, nil
}

// IsCashRegisterRequiredForOrdering checks merchant parameter cash_register_required_for_ordering == 1
func (r *OrdersRepository) IsCashRegisterRequiredForOrdering(ctx context.Context, tx *sql.Tx, merchantID string) (bool, error) {
	var required sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT mp.cash_register_required_for_ordering
		FROM merchant_parameters mp
		WHERE mp.merchant_id = ? LIMIT 1
		`, merchantID).Scan(&required)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	// DB may store '1' as string or tinyint -> be permissive
	if required.Valid && (required.String == "1" || required.String == "true") {
		return true, nil
	}
	return false, nil
}

func (s *OrdersRepository) validateProductAvailability(ctx context.Context, tx *sql.Tx, req *models.RequestObject) ([]int64, error) {

	if len(req.Order.Products) == 0 {
		return nil, nil
	}

	// Build product IDs and placeholders
	ids := make([]interface{}, 0, len(req.Order.Products))
	placeholders := make([]string, 0, len(req.Order.Products))

	for _, p := range req.Order.Products {
		ids = append(ids, p.ProductID)
		placeholders = append(placeholders, "?")
	}

	// JOIN placeholders: "?, ?, ?, ?"
	inClause := strings.Join(placeholders, ", ")

	// MySQL-compatible query
	query := fmt.Sprintf(`
        SELECT DISTINCT p.product_id
        FROM products p
        LEFT JOIN (
            SELECT DISTINCT r.product_id
            FROM requires rq
            INNER JOIN recipes r ON r.recipe_id = rq.recipe_id
            INNER JOIN components c 
                   ON rq.component_id = c.component_id
                  AND c.status = 0
                  AND rq.enabled = TRUE
        ) a ON a.product_id = p.product_id
        WHERE p.product_id IN (%s)
          AND (CASE WHEN a.product_id IS NOT NULL THEN 0 ELSE p.status END) = 0
    `, inClause)

	rows, err := tx.QueryContext(ctx, query, ids...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blocked []int64
	for rows.Next() {
		var productID int64
		if err := rows.Scan(&productID); err != nil {
			return nil, err
		}
		blocked = append(blocked, productID)
	}

	return blocked, rows.Err()
}

// upsertCustomer calls the customer repository to create/update the customer and returns numeric MerchantID (nil if none)
func (s *OrdersRepository) upsertCustomer(ctx context.Context, tx *sql.Tx, req *models.RequestObject) (*string, error) {
	if req.Order.Customer == nil {
		return nil, nil
	}

	// Convert our Order CustomerRequest to the models.Customer expected by CustomerRepository
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

	// CustomerRepository.UpdateOrCreateCustomer should be transaction-aware; if not, it will open its own transaction.
	// We call it directly. It returns MerchantID as string.
	custoRepo := customers.NewCustomerRepository(s.db, s.log)
	newIDStr, err := custoRepo.UpdateOrCreateCustomer(ctx, tx, cust)
	if err != nil {
		return nil, fmt.Errorf("failed to update/create customer: %w", err)
	}
	if newIDStr == nil {
		return nil, nil
	}
	/*
		newIDInt, err := strconv.ParseInt(newIDStr, 10, 64)
		if err != nil {
			// return the string wrapped if parse fails
			return nil, fmt.Errorf("customer id parse error: %w", err)
		}*/
	return newIDStr, nil
}

// GetNextOrderNum returns the next order_num following the PHP behaviour:
// - if last order_num is 99 or null -> return 1
// - otherwise last + 1
func (r *OrdersRepository) GetNextOrderNum(ctx context.Context, tx *sql.Tx, merchantID string) (string, error) {
	var last sql.NullInt64

	err := tx.QueryRowContext(ctx, `
		SELECT order_num
		FROM orders
		WHERE merchant_id = ?
		ORDER BY order_id DESC
		LIMIT 1
		`, merchantID).Scan(&last)

	// Toute erreur SQL sauf "aucune ligne"
	if err != nil && err != sql.ErrNoRows {
		return "1", err
	}

	// Si aucune commande trouvée → première valeur
	if !last.Valid {
		return "1", nil
	}

	// Cas spécifique si dernier = 99
	if last.Int64 == 99 {
		return "1", nil
	}

	// Valeur par défaut : incrémenter et convertir en string
	return strconv.FormatInt(last.Int64+1, 10), nil
}

func (r *OrdersRepository) ComputeEstimatedReady(ctx context.Context, tx *sql.Tx, merchantID string, productsCount int) (string, error) {
	// call stored proc; adapt if your DB driver requires different handling
	rows, err := tx.QueryContext(ctx, "CALL GET_AVERAGE_DISTRIBUTION_TIME(?, ?)", merchantID, productsCount)
	if err != nil {
		// do not fail hard on missing proc; log and continue
		r.log.Debug("GET_AVERAGE_DISTRIBUTION_TIME call failed", zap.Error(err))
		return "", nil
	}
	defer rows.Close()

	var seconds sql.NullInt64
	if rows.Next() {
		if err := rows.Scan(&seconds); err != nil {
			r.log.Debug("scan GET_AVERAGE_DISTRIBUTION_TIME failed", zap.Error(err))
			return "", nil
		}
	}

	if !seconds.Valid || seconds.Int64 <= 0 {
		return "", nil
	}

	// compute estimated ready as now UTC + seconds
	t := time.Now().UTC().Add(time.Duration(seconds.Int64) * time.Second)
	return t.Format("2006-01-02 15:04:05"), nil
}

// setOrderDefaults applique les règles métier par défaut (équivalent du bloc PHP)
func (s *OrdersRepository) setOrderDefaults(ctx context.Context, req *models.RequestObject) {
	/*
		AJOUTER LES VALEURS PAR DEFAUT ICI

		// INSERT ORDER
		// Default values
		$merchant_approval = $order_object->order->merchant_approval ?? "ACCEPTED";
		$order_object->order->brand_status = $order_object->order->brand_status ?? (($order_object->order->online_payment ?? false) ? "ONLINE_PAYMENT_PENDING": "PENDING");
		$order_object->order->is_scheduled = isset($order_object->order->is_scheduled) && $order_object->order->is_scheduled ? "1" : "0";
		$order_object->order->places_settings = $order_object->order->places_settings ?? 0;

	*/

	// PHP: $merchant_approval = ... ?? "ACCEPTED";
	log := logger.FromContext(ctx)
	log.Info("Orders.Create - Merchant Approval default : " + req.Order.MerchantApproval)
	if req.Order.MerchantApproval == "" {
		req.Order.MerchantApproval = "ACCEPTED"
	}
	log.Info("Orders.Create - Merchant Approval defined : " + req.Order.MerchantApproval)

	// PHP: $brand_status = ... ?? (($online_payment) ? "ONLINE_PAYMENT_PENDING" : "PENDING");
	if req.Order.BrandStatus == "" {
		// Note : Assure-toi que le champ OnlinePayment existe bien dans ton modèle Go
		if req.Order.OnlinePayment {
			req.Order.BrandStatus = "ONLINE_PAYMENT_PENDING"
		} else {
			req.Order.BrandStatus = "PENDING"
		}
	}

	// PHP: places_settings = ... ?? 0;
	// En Go, un int est par défaut à 0. Cette ligne est implicite,
	// mais on peut la garder si places_settings est un pointeur (*int).
	// Si c'est un int simple, rien à faire, c'est déjà 0 si absent du JSON.

	// PHP: is_scheduled = ... ? "1" : "0";
	// En Go, le booléen est "false" par défaut.
	// Le driver SQL convertira automatiquement le bool true/false en 1/0 (TINYINT) pour MySQL.
	// Pas besoin de conversion manuelle en string sauf si ta colonne DB est un VARCHAR.
}

// insertOrderBase inserts the orders row and returns orderID and orderNum
func (s *OrdersRepository) insertOrderBase(ctx context.Context, tx *sql.Tx, req *models.RequestObject, customerID *string) (orderID string, err error) {

	// default fields and estimated_ready handling simplified: use UTC_TIMESTAMP equivalent in SQL
	res, err := tx.ExecContext(ctx, `
		INSERT INTO orders(cash_register_id, merchant_id, customer_id, order_num, price, TVA, HT, merchant_approval, scheduled, creation_date,
		                   dateCall, last_update, responsible, created_by, delivery_fees, estimated_ready, use_customer_temporary_address,
		                   brand_status, order_type, places_settings, pager_number)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, UTC_TIMESTAMP, UTC_TIMESTAMP, UTC_TIMESTAMP, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.DeviceID, req.MerchantID, customerID, req.Order.OrderNum, req.Order.TTC, req.Order.TVA, req.Order.HT,
		req.Order.MerchantApproval, req.Order.IsScheduled,
		req.Order.Responsible, req.Order.CreatedBy, req.Order.DeliveryFees, req.Order.EstimatedReady,
		req.Order.UseCustomerTemporaryAddress, req.Order.BrandStatus, req.Order.OrderType, req.Order.PlacesSettings, req.Order.PagerNumber,
	)
	if err != nil {
		return "no_order_created", err
	}
	lastID, err := res.LastInsertId()
	if err != nil {
		return "no_order_created", err
	}
	return strconv.FormatInt(lastID, 10), nil
}

// insertOrderItems inserts each orderitem and returns list of UsedItem (order_item_id + qty)
func (s *OrdersRepository) insertOrderItems(ctx context.Context, tx *sql.Tx, req *models.RequestObject, orderID string) ([]models.UsedItem, error) {
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
func (s *OrdersRepository) insertExtrasWithoutsConfigs(ctx context.Context, tx *sql.Tx, req *models.RequestObject, items []models.UsedItem) error {
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
func (s *OrdersRepository) insertPayments(ctx context.Context, tx *sql.Tx, req *models.RequestObject, orderID string) error {
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
AND p.product_id IN (?)
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
	row := r.db.QueryRowContext(ctx, q, MerchantID)
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
               WHEN a.product_id IS NOT NULL THEN 0 -- Composant manquant = Indisponible (0)
               ELSE p.status                        -- Sinon statut du produit
           END as status
       FROM products p
       LEFT JOIN (
           SELECT DISTINCT r.product_id
           FROM requires rq
           INNER JOIN recipes r ON r.recipe_id = rq.recipe_id
           INNER JOIN components c ON rq.component_id = c.component_id 
               AND c.status = 0      -- Composant inactif/épuisé
               AND rq.enabled = TRUE -- Recette active
       ) a ON a.product_id = p.product_id
       WHERE p.merchant_id = ?
       AND p.product_id IN (%s)
       HAVING status = 0
    `, placeholders)

	// 3. Préparation des arguments (MerchantID + Liste des ProductIDs)
	args := make([]interface{}, 0, len(productIDs)+1)
	args = append(args, req.MerchantID)
	args = append(args, productIDs...)

	// 4. Exécution
	rows, err := r.db.QueryContext(ctx, query, args...)
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

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
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

	rows, err := r.db.QueryContext(ctx, query, req.MerchantID)
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

	rows, err := r.db.QueryContext(ctx, query, merchantID)
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

	rows, err := r.db.QueryContext(ctx, query, merchantID)
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
		    cr.is_used
		FROM customer_rewards cr
		WHERE cr.reward_id IN (%s)
		  AND cr.usage_date IS NULL
		  AND cr.is_used = FALSE
	`, placeholders)

	args := make([]interface{}, len(rewardIDs))
	for i, id := range rewardIDs {
		args[i] = id
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	outMap := map[string]*models.DBReward{}

	for rows.Next() {
		rw := &models.DBReward{}
		err := rows.Scan(
			&rw.RewardID,
			&rw.RewardType,
			&rw.RewardOrderType,
			&rw.RewardValue,
			&rw.LoyaltyProgramID,
			&rw.CreationDate,
			&rw.IsUsed,
		)
		if err != nil {
			return nil, err
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

	rows2, err := r.db.QueryContext(ctx, query2, args2...)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()

	for rows2.Next() {
		var rewardID, productID string
		if err := rows2.Scan(&rewardID, &productID); err != nil {
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
	rows, err := r.db.QueryContext(ctx, "CALL GET_AVERAGE_DISTRIBUTION_TIME(?, ?)", req.MerchantID, count)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var sec int
	if rows.Next() {
		if err := rows.Scan(&sec); err != nil {
			return 0, err
		}
	}

	return sec, nil
}

func (r *OrdersRepository) GetConfigurationOptionPrices(
	ctx context.Context,
	optionIDs []string,
) (map[string]int, error) {

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

	rows, err := r.db.QueryContext(ctx, query, args...)
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

func (r *OrdersRepository) UpdateMultipleProductsStatus(ctx context.Context, req *models.MultipleProductsRequest) error {

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	orderIDs := map[string]bool{}

	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	query := `
		UPDATE orderitems
		SET 
			production_status = ?,
			production_status_done_quantity = CASE
				WHEN ? = 'DONE' THEN quantity
				ELSE ready_for_distribution_quantity
			END
		WHERE order_item_id = ?
		AND order_id = ?
	`

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, p := range req.Products {

		orderIDs[p.OrderID] = true

		_, err = stmt.ExecContext(ctx,
			p.ProductionStatus,
			p.ProductionStatus,
			p.OrderItemID,
			p.OrderID,
		)

		if err != nil {
			return err
		}
	}

	if err = tx.Commit(); err != nil {
		return err
	}

	return nil
}
