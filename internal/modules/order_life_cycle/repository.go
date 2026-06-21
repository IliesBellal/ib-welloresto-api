package order_life_cycle

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/customers"
	"welloresto-api/internal/utils/dbutils"
	"welloresto-api/internal/utils/security"

	"go.uber.org/zap"
)

type OrdersLifeCycleRepository struct {
	database  *sql.DB
	custoRepo *customers.CustomersRepository
}

type OrderIntegrationInfo struct {
	MerchantID   string
	Brand        string
	BrandOrderID string
}

func NewOrdersLifeCycleRepository(db *sql.DB, custoRepo *customers.CustomersRepository) *OrdersLifeCycleRepository {
	return &OrdersLifeCycleRepository{
		database:  db,
		custoRepo: custoRepo,
	}
}

// LinkCustomerToOrder rattache un client à une commande (écrase le client déjà rattaché s'il y en a un)
func (r *OrdersLifeCycleRepository) LinkCustomerToOrder(ctx context.Context, orderID, customerID, merchantID string) error {
	db := dbutils.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `
		UPDATE orders
		SET customer_id = ?
		WHERE order_id = ? AND merchant_id = ?
	`, customerID, orderID, merchantID)
	if err != nil {
		return fmt.Errorf("link customer to order failed: %w", err)
	}

	return nil
}

func (r *OrdersLifeCycleRepository) ReopenClosedOrder(ctx context.Context, merchantID, orderID, userID string) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// -------------------------
	//  FUTURE VALIDATIONS HERE
	// -------------------------
	// Exemple :
	// - vérifier que la commande existe
	// - vérifier qu’elle est bien "CLOSED"
	// - vérifier que userID a le droit
	// - vérifier registre de caisse
	// --------------------------------------

	// ---- 2. Update
	_, err := db.ExecContext(ctx, `
		UPDATE orders 
		SET state = 'OPEN'
		WHERE order_id = ? AND merchant_id = ?
	`, orderID, merchantID)
	if err != nil {
		log.Error(err.Error())
		return fmt.Errorf("reopen update failed: %w", err)
	}
	return nil
}

func (r *OrdersLifeCycleRepository) GetActiveCashRegisterID(ctx context.Context, deviceID string) (string, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	var cashRegisterID sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT cr.cash_register_id
		FROM cash_registers cr
		WHERE cr.device_id = ?
		AND cr.end_date IS NULL
	`, deviceID).Scan(&cashRegisterID)

	if err == sql.ErrNoRows {
		err = db.QueryRowContext(ctx, `
			SELECT cr.cash_register_id
			FROM cash_registers cr
			INNER JOIN device_link dl on dl.on_behalf_of = cr.device_id
			WHERE dl.device_id = ?
			AND cr.end_date IS NULL
		`, deviceID).Scan(&cashRegisterID)

		if err == sql.ErrNoRows {
			return "", models.ErrNoCashRegisterOpen
		}
	}

	if err != nil {
		log.Error("Error finding cash register: " + err.Error())
		return "", err
	}

	return cashRegisterID.String, nil
}

// Nouvelle version qui retourne l'ID du paiement créé
func (r *OrdersLifeCycleRepository) AddPaymentAndReturnID(ctx context.Context, payment models.Payment) (int64, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// 1. Vérification du montant (Paiement total déjà effectué ?)
	var totalPrice, alreadyPaid int
	err := db.QueryRowContext(ctx, `
		SELECT o.price, COALESCE(SUM(p.amount),0)
		FROM orders o
		LEFT JOIN payments p ON p.order_id = o.order_id AND p.enabled = 1
		WHERE o.order_id = ?
		GROUP BY o.order_id
	`, payment.OrderID).Scan(&totalPrice, &alreadyPaid)

	if err != nil {
		log.Error("Error checking payment status: " + err.Error())
		return 0, fmt.Errorf("failed to check order payment status: %w", err)
	}

	if (alreadyPaid >= totalPrice && payment.OperationType == models.OperationTypeSale) || alreadyPaid+payment.Amount > totalPrice {
		return 0, &models.OrderNotFullyPaidError{
			OrderID:    payment.OrderID,
			PaidAmount: alreadyPaid,
			Price:      totalPrice,
		}
	}

	// 2. RÉCUPÉRATION DU HASH PRÉCÉDENT (Chaînage Fiscal)
	var prevHash sql.NullString
	_ = db.QueryRowContext(ctx, `
		SELECT hash FROM payments 
		WHERE merchant_id = ? 
		ORDER BY payment_date DESC LIMIT 1 
		FOR UPDATE
	`, payment.MerchantID).Scan(&prevHash)

	now := time.Now().UTC()
	paymentDate := now.Format(time.RFC3339)

	// Calcul du hash du nouveau paiement
	payload := fmt.Sprintf("%s|%s|%d|%s|%s", prevHash.String, paymentDate, payment.Amount, payment.MOP, payment.OrderID)
	newHash := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	signature := security.SignHash(newHash)

	// 3. Insérer le paiement avec son hash
	res, err := db.ExecContext(ctx, `
	INSERT INTO payments
	(merchant_id, cash_register_id, order_id, amount, mop, comment, payment_date, user_id, status_check, previous_hash, hash, signature, operation_type)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, payment.MerchantID, payment.CashRegisterID, payment.OrderID, payment.Amount, payment.MOP, payment.Comment, now, payment.UserID, payment.StatusCheck, prevHash.String, newHash, signature, payment.OperationType)

	if err != nil {
		log.Error("Error inserting payment: " + err.Error())
		return 0, err
	}

	paymentID, _ := res.LastInsertId()

	// 4. Ticket restaurant (TR)
	if payment.MOP == models.TicketRestoMOP {
		// On suppose que Code est un champ dans ta struct Payment, à adapter si besoin
		_, err = db.ExecContext(ctx, `
			INSERT INTO restaurant_ticket (merchant_id, payment_id, barcode)
			VALUES (?, ?, ?)
		`, payment.MerchantID, paymentID, payment.Code)
		if err != nil {
			log.Error("Error inserting TR: " + err.Error())
			return 0, err
		}
	} else if payment.MOP == models.StripeMOP {

		query := `INSERT INTO stripe_payments(order_id, payment_id, payment_intent_id, checkout_session_id, customer_email, stripe_session_date) 
				VALUES(?, ?, ?, ?, ?, UTC_TIMESTAMP())`
		_, err = db.ExecContext(ctx, query, payment.OrderID, paymentID, payment.PaymentIntentID, payment.CheckoutSessionID, payment.CustomerEmail)
	}

	// 5. Mettre à jour orders.isPaid
	_, err = db.ExecContext(ctx, `
		UPDATE orders o
		INNER JOIN (
			SELECT order_id, SUM(amount) AS paid
			FROM payments
			WHERE enabled = 1 AND order_id = ?
			GROUP BY order_id
		) p ON p.order_id = o.order_id
		SET o.isPaid = (o.price <= p.paid)
		WHERE o.order_id = ?
	`, payment.OrderID, payment.OrderID)

	return paymentID, err
}

// AddPayment inserts a payment and discards the generated ID (for backward compatibility)
func (r *OrdersLifeCycleRepository) AddPayment(ctx context.Context, payment models.Payment) error {
	_, err := r.AddPaymentAndReturnID(ctx, payment)
	return err
}

func (r *OrdersLifeCycleRepository) GetPaymentsForOrder(ctx context.Context, orderID string) ([]models.Payment, error) {
	db := dbutils.GetDB(ctx, r.database)

	q := `
		SELECT order_id, payment_id, mop, amount, payment_date, enabled
		FROM payments
		WHERE order_id = ?
		ORDER BY payment_date ASC
	`

	rows, err := db.QueryContext(ctx, q, orderID)
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

func (r *OrdersLifeCycleRepository) GetPayment(ctx context.Context, orderID string, paymentID int64) (*models.Payment, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	q := `
		SELECT p.order_id, p.payment_id, p.mop, p.amount, p.payment_date, p.enabled, sp.payment_intent_id, sa.account_id
		FROM payments p
		LEFT JOIN stripe_payments sp on sp.payment_id = p.payment_id
		LEFT JOIN stripe_accounts sa on sa.merchant_id = p.merchant_id
		WHERE p.order_id = ? AND p.payment_id = ?
	`

	var p models.Payment
	var paymentDate sql.NullTime

	err := db.QueryRowContext(ctx, q, orderID, paymentID).Scan(
		&p.OrderID, &p.PaymentID, &p.MOP, &p.Amount, &paymentDate, &p.Enabled, &p.IntentID, &p.AccountID,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("payment not found: order_id=%s, payment_id=%d", orderID, paymentID)
		}
		log.Error(err.Error())
		return nil, err
	}

	if paymentDate.Valid {
		p.PaymentDate = helpers.NullTimePtr(paymentDate).UTC().Unix()
	}

	return &p, nil
}

func (r *OrdersLifeCycleRepository) DisablePayment(ctx context.Context, paymentID string) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// TODO
	// Vérifier qu'il ne s'agit pas d'un paiement Uber Eats ou Deliveroo qui ne sont pas anulables
	// Le client s'en occupe déjà, mais une double vérification côté serveur est nécessaire

	// Disable payment
	_, err := db.ExecContext(ctx, `
		UPDATE payments SET enabled = 0 WHERE payment_id = ?
	`, paymentID)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	// Refresh order as unpaid
	_, err = db.ExecContext(ctx, `
		UPDATE orders o 
		JOIN payments p ON o.order_id = p.order_id
		SET o.isPaid = false, o.last_update = UTC_TIMESTAMP()
		WHERE p.payment_id = ?
	`, paymentID)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	return nil
}

func (r *OrdersLifeCycleRepository) SetDistributedProducts(ctx context.Context, userID string, merchantID string, req *models.SetDistributedProductsRequest) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// 1. Préparation des outils de compatibilité
	now := time.Now().UTC()
	orderID := req.OrderID

	// On détermine le driver une seule fois (à adapter selon votre config r.db)
	// Idéalement, stockez r.isPostgres lors de l'initialisation du repo
	isPostgres := false

	// 2. Mise à jour des items (Boucle)
	for _, p := range req.Products {

		// UPDATE ITEM : On injecte 'now' depuis Go
		queryUpdateItem := r.formatQuery(`
			UPDATE orderitems
			SET isDistributed = 1,
			    distributed_quantity = quantity,
			    ready_for_distribution_quantity = quantity,
			    distributed_on = ?
			WHERE order_id = ? AND order_item_id = ?`, isPostgres)

		_, _ = db.ExecContext(ctx, queryUpdateItem, now, orderID, p.OrderItemID)
	}

	// 3. Calcul de l'état global (Optimisé : hors de la boucle précédente)
	var countNotDistributed int
	queryCheck := r.formatQuery(`SELECT COUNT(*) FROM orderitems WHERE order_id = ? AND isDistributed = 0`, isPostgres)
	err := db.QueryRowContext(ctx, queryCheck, orderID).Scan(&countNotDistributed)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	// On utilise des int pour la compatibilité des types (Postgres est strict)
	orderFullyDistributedInt := 1
	if countNotDistributed > 0 {
		orderFullyDistributedInt = 0
	}

	// 4. UPDATE ORDER : Une seule fois après la boucle
	// On passe toutes les valeurs de statuts en paramètres pour éviter les erreurs de collation
	queryUpdateOrder := r.formatQuery(`
		UPDATE orders
		SET isDistributed = ?,
		    delivered_on = CASE 
		        WHEN ? = 0 OR order_type = 'DELIVERY' THEN delivered_on
		        ELSE ?
		    END,
		    brand_status = CASE
		        WHEN order_type = 'DELIVERY' AND ? = 1 THEN 'READY_FOR_HANDOFF'
		        WHEN order_type = 'TAKE_AWAY' AND ? = 1 THEN 'READY_FOR_TAKE_AWAY'
		        WHEN ? = 0 THEN 'PENDING'
		        ELSE 'DONE'
		    END,
		    last_update = ?
		WHERE order_id = ? AND merchant_id = ?`, isPostgres)

	_, err = db.ExecContext(ctx, queryUpdateOrder,
		orderFullyDistributedInt, // isDistributed
		orderFullyDistributedInt, // CASE delivered_on (comparaison)
		now,                      // delivered_on (valeur)
		orderFullyDistributedInt, // CASE handoff
		orderFullyDistributedInt, // CASE takeaway
		orderFullyDistributedInt, // CASE pending
		now,                      // last_update
		orderID,
		merchantID,
	)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	// 5. Récupération de la marque pour notification
	var brand sql.NullString
	queryBrand := r.formatQuery(`SELECT brand FROM orders WHERE order_id = ?`, isPostgres)
	err = db.QueryRowContext(ctx, queryBrand, orderID).Scan(&brand)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	return nil
}

// formatQuery remplace les ? par $1, $2, etc. si Postgres est utilisé
func (r *OrdersLifeCycleRepository) formatQuery(q string, isPostgres bool) string {
	if !isPostgres {
		return q
	}
	parts := strings.Split(q, "?")
	var result strings.Builder
	for i := 0; i < len(parts)-1; i++ {
		result.WriteString(parts[i])
		result.WriteString(fmt.Sprintf("$%d", i+1))
	}
	result.WriteString(parts[len(parts)-1])
	return result.String()
}

func (r *OrdersLifeCycleRepository) MarkProductsBackToProduction(ctx context.Context, userID, merchantID, orderID string, products []models.DistributedProduct) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	for _, p := range products {

		_, err := db.ExecContext(ctx, `
            UPDATE orderitems
            SET
                isDistributed = 0,

                distributed_quantity = CASE
                    WHEN isDistributed = 1 AND ready_for_distribution_quantity = 0 THEN quantity
                    WHEN isDistributed = 1 AND ready_for_distribution_quantity > 0 THEN ready_for_distribution_quantity
                    ELSE 0
                END,

                ready_for_distribution_quantity = CASE
                    WHEN isDistributed = 0 THEN 0
                    WHEN ready_for_distribution_quantity = 0 THEN quantity
                    ELSE ready_for_distribution_quantity
                END,

                distributed_on = UTC_TIMESTAMP

            WHERE order_id = ?
            AND order_item_id = ?
            AND merchant_id = ?
        `, orderID, p.OrderItemID, merchantID)
		if err != nil {
			log.Error(err.Error())
			return err
		}
	}

	// Check if any undistributed items left
	var remaining int
	err := db.QueryRowContext(ctx, `
        SELECT COUNT(*)
        FROM orderitems
        WHERE order_id = ? AND isDistributed = 0
    `, orderID).Scan(&remaining)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	fullyDistributed := remaining == 0

	// Update orders table
	_, err = db.ExecContext(ctx, `
        UPDATE orders
        SET 
            isDistributed = ?,

            delivered_on = CASE
                WHEN ? = 0 OR order_type = 'DELIVERY' THEN delivered_on
                ELSE UTC_TIMESTAMP
            END,

            brand_status = CASE
                WHEN order_type = 'DELIVERY' AND ? = 1 THEN 'READY_FOR_HANDOFF'
                WHEN order_type = 'TAKE_AWAY' AND ? = 1 THEN 'READY_FOR_TAKE_AWAY'
                WHEN ? = 0 THEN 'PENDING'
                ELSE 'CLOSED'
            END,

            last_update = UTC_TIMESTAMP

        WHERE order_id = ? AND merchant_id = ?
    `,
		fullyDistributed,
		fullyDistributed,
		fullyDistributed,
		fullyDistributed,
		fullyDistributed,
		orderID, merchantID)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	return nil
}

func (r *OrdersLifeCycleRepository) GetOrderBrandAndMerchant(ctx context.Context, orderID string) (*models.OrderMeta, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	const q = `
		SELECT o.brand, o.merchant_id, o.brand_order_id, o.creation_date
		FROM orders o
		WHERE o.order_id = ?
		LIMIT 1;
	`
	row := db.QueryRowContext(ctx, q, orderID)
	var m models.OrderMeta
	var merchantID sql.NullInt64
	var brand sql.NullString
	var brandOrder sql.NullString
	var creation sql.NullTime

	if err := row.Scan(&brand, &merchantID, &brandOrder, &creation); err != nil {
		log.Error(err.Error())
		return nil, fmt.Errorf("get order meta: %w", err)
	}
	if brand.Valid {
		m.Brand = brand.String
	}
	if merchantID.Valid {
		// convert int64 -> string to match your models (in PHP merchant_id was int)
		m.MerchantID = fmt.Sprintf("%d", merchantID.Int64)
	}
	if brandOrder.Valid {
		m.BrandOrderID = brandOrder.String
	}
	if creation.Valid {
		m.CreationDate = creation.Time
	}
	return &m, nil
}

// SetOrderAcceptedLocal : mirrors PHP update: state = 'OPEN', brand_status = 'PENDING', merchant_approval = 'ACCEPTED', last_update = UTC_TIMESTAMP
func (r *OrdersLifeCycleRepository) SetOrderAcceptedLocal(ctx context.Context, orderID string) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	q := `
		UPDATE orders
		SET last_update = UTC_TIMESTAMP(),
		    state = 'OPEN',
		    brand_status = 'PENDING',
		    merchant_approval = 'ACCEPTED'
		WHERE order_id = ?;
	`
	if _, err := db.ExecContext(ctx, q, orderID); err != nil {
		log.Error(err.Error())
		return err
	}

	return nil
}

func (r *OrdersLifeCycleRepository) MarkOrderAsDeliveryStarted(ctx context.Context, orderID string, userID string) (*OrderIntegrationInfo, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// Update order
	_, err := db.ExecContext(ctx, `
		UPDATE orders
		SET last_update = UTC_TIMESTAMP,
			brand_status = 'EN_ROUTE_TO_DROPOFF',
			delivery_start = UTC_TIMESTAMP,
			responsible = ?
		WHERE order_id = ?
	`, userID, orderID)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	// Load integration info
	row := db.QueryRowContext(ctx, `
		SELECT o.merchant_id, o.brand, o.brand_order_id
		FROM orders o
		WHERE o.order_id = ?
	`, orderID)

	var info OrderIntegrationInfo
	err = row.Scan(&info.MerchantID, &info.Brand, &info.BrandOrderID)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	return &info, nil
}

func (r *OrdersLifeCycleRepository) DenyOrderLocal(ctx context.Context, orderID, deletionReasonID, comment string) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	_, err := db.ExecContext(ctx, `
        UPDATE orders
        SET last_update = UTC_TIMESTAMP,
            brand_status = 'DENIED',
            merchant_approval = 'DENIED',
            state = 'CLOSED',
            deletion_reason_id = ?,
            deletion_comment = ?
        WHERE order_id = ?`,
		deletionReasonID, comment, orderID,
	)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	_, err = db.ExecContext(ctx, `
        UPDATE customer_rewards
        SET is_used = false,
            usage_date = null,
            used_on_order_id = null
        WHERE used_on_order_id = ?`,
		orderID,
	)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	return nil
}

func (r *OrdersLifeCycleRepository) GetOrderBrand(ctx context.Context, orderID string) (string, error) {
	db := dbutils.GetDB(ctx, r.database)

	var brand string
	err := db.QueryRowContext(ctx, `
        SELECT brand
        FROM orders
        WHERE order_id = ? LIMIT 1`,
		orderID,
	).Scan(&brand)
	return brand, err
}

func (r *OrdersLifeCycleRepository) SetReadyForDistribution(ctx context.Context, orderID, merchantID string) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// Update orders
	_, err := db.ExecContext(ctx, `
        UPDATE orders
        SET 
            brand_status = CASE 
                WHEN order_type = 'DELIVERY' THEN 'READY_FOR_HANDOFF'
                WHEN order_type = 'TAKE_AWAY' THEN 'READY_FOR_TAKE_AWAY'
                ELSE brand_status
            END,
            last_update = UTC_TIMESTAMP
        WHERE order_id = ? AND merchant_id = ?`,
		orderID, merchantID,
	)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	// Update items
	_, err = db.ExecContext(ctx, `
        UPDATE orderitems
        SET ready_for_distribution_quantity = quantity
        WHERE order_id = ? AND merchant_id = ?`,
		orderID, merchantID,
	)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	return nil
}

func (r *OrdersLifeCycleRepository) OrderStillOpen(ctx context.Context, orderID string) (bool, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	var count int
	err := db.QueryRowContext(ctx, `
        SELECT COUNT(*)
        FROM orders
        WHERE order_id = ? AND state = 'OPEN'`,
		orderID,
	).Scan(&count)
	if err != nil {
		log.Error(err.Error())
		return false, err
	}

	return count > 0, nil
}

func (r *OrdersLifeCycleRepository) DeleteOrderLocal(ctx context.Context, orderID string, reasonID string, comment string) error {
	db := dbutils.GetDB(ctx, r.database)

	// 1) Get metadata
	qOrder := `SELECT brand, brand_order_id, merchant_id, fulfillment_type, price FROM orders WHERE order_id = ?`
	meta := &DeliveredOrderMetadata{}
	var currentPrice int
	if err := db.QueryRowContext(ctx, qOrder, orderID).Scan(&meta.Brand, &meta.BrandOrderID, &meta.MerchantID, &meta.FulfillmentType, &currentPrice); err != nil {
		return err
	}

	// 1.bis : RÉCUPÉRATION DU HASH PRÉCÉDENT (Chaînage Fiscal pour Orders)
	var prevHash sql.NullString
	_ = db.QueryRowContext(ctx, `
        SELECT hash FROM orders 
        WHERE merchant_id = ? AND state = 'CLOSED' 
        ORDER BY delivered_on DESC, order_id DESC LIMIT 1 
        FOR UPDATE
    `, meta.MerchantID).Scan(&prevHash)

	now := time.Now().UTC()
	deliveredOn := now.Format(time.RFC3339)

	// Calcul du hash de clôture de commande
	payload := fmt.Sprintf("%s|%s|%d|%s", prevHash.String, deliveredOn, currentPrice, orderID)
	newHash := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	signature := security.SignHash(newHash)

	// 2) Update orders table avec Hash de clôture

	_, err := db.ExecContext(ctx, `
        UPDATE orders
        SET deletion_reason_id = ?,
            deletion_comment = ?,
            last_update = UTC_TIMESTAMP,
            state = 'CLOSED',
            brand_status = 'CANCELED',
            delivered_on = UTC_TIMESTAMP,
			previous_hash = ?,
			hash = ?,
			signature = ?
        WHERE order_id = ?`,
		reasonID, comment, prevHash, newHash, signature, orderID,
	)

	return err
}

func (r *OrdersLifeCycleRepository) SetDeliveredLocal(ctx context.Context, orderID string) (*DeliveredOrderMetadata, error) {
	db := dbutils.GetDB(ctx, r.database)

	// 0.1 Lock order row
	const qLockOrder = `
		SELECT price
		FROM orders
		WHERE order_id = ?
		FOR UPDATE
		`

	var price int
	if err := db.QueryRowContext(ctx, qLockOrder, orderID).Scan(&price); err != nil {
		return nil, err
	}

	const qSumPayments = `
SELECT COALESCE(SUM(amount), 0)
FROM payments
WHERE order_id = ?
  AND enabled = 1
`

	var paidAmount int
	if err := db.QueryRowContext(ctx, qSumPayments, orderID).
		Scan(&paidAmount); err != nil {
		return nil, err
	}

	if paidAmount != price {
		return nil, &models.OrderNotFullyPaidError{
			OrderID:    orderID,
			PaidAmount: paidAmount,
			Price:      price,
		}
	}

	// 1) Get metadata
	qOrder := `SELECT brand, brand_order_id, merchant_id, fulfillment_type, price FROM orders WHERE order_id = ?`
	meta := &DeliveredOrderMetadata{}
	var currentPrice int
	if err := db.QueryRowContext(ctx, qOrder, orderID).Scan(&meta.Brand, &meta.BrandOrderID, &meta.MerchantID, &meta.FulfillmentType, &currentPrice); err != nil {
		return nil, err
	}

	// 1.bis : RÉCUPÉRATION DU HASH PRÉCÉDENT (Chaînage Fiscal pour Orders)
	var prevHash sql.NullString
	_ = db.QueryRowContext(ctx, `
        SELECT hash FROM orders 
        WHERE merchant_id = ? AND state = 'CLOSED' 
        ORDER BY delivered_on DESC, order_id DESC LIMIT 1 
        FOR UPDATE
    `, meta.MerchantID).Scan(&prevHash)

	now := time.Now().UTC()
	deliveredOn := now.Format(time.RFC3339)

	// Calcul du hash de clôture de commande
	payload := fmt.Sprintf("%s|%s|%d|%s", prevHash.String, deliveredOn, currentPrice, orderID)
	newHash := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	signature := security.SignHash(newHash)

	// 2) Update orders table avec Hash de clôture
	qUpd := `
    UPDATE orders
    SET last_update = ?,
        brand_status = 'CLOSED',
        state = 'CLOSED',
        isPaid = 1,
        isDistributed = 1,
        delivered_on = ?,
        previous_hash = ?,
        hash = ?,
		signature = ?
    WHERE order_id = ?
    `
	if _, err := db.ExecContext(ctx, qUpd, now, now, prevHash.String, newHash, signature, orderID); err != nil {
		return nil, err
	}

	// 3) Delete qrcodes
	qDelQR := `
	DELETE qr
	FROM qrcodes qr
	INNER JOIN order_location ol ON qr.location_id = ol.location_id
	INNER JOIN orders o ON o.order_id = ol.order_id AND o.merchant_id = qr.merchant_id
	WHERE o.order_id = ?
	`
	if _, err := db.ExecContext(ctx, qDelQR, orderID); err != nil {
		return nil, err
	}

	// 4) Set bookings status = 0
	qUpdBook := `UPDATE bookings SET status = '0' WHERE order_id = ?`
	if _, err := db.ExecContext(ctx, qUpdBook, orderID); err != nil {
		return nil, err
	}

	// 5) Close delivery_session if last order
	const qCloseDS = `
		UPDATE delivery_session ds
		JOIN delivery_session_order dso ON dso.delivery_session_id = ds.id
		SET ds.status = 'done'
		WHERE dso.order_id = ?
		  AND NOT EXISTS (
			  SELECT 1
			  FROM delivery_session_order dso_other
			  JOIN orders o_other ON o_other.order_id = dso_other.order_id
			  WHERE dso_other.delivery_session_id = ds.id
			    AND dso_other.order_id <> ?
			    AND o_other.state = 'OPEN'
		  )
	`
	if _, err := db.ExecContext(ctx, qCloseDS, orderID, orderID); err != nil {
		return nil, err
	}

	// 6) Update orderitems (distributed)
	qUpdItems := `
	UPDATE orderitems oi
	LEFT JOIN delays d ON oi.delay_id = d.id
	SET 
		isDistributed = CASE WHEN ready_for_distribution_quantity >= quantity OR ready_for_distribution_quantity = 0 THEN 1 ELSE 0 END,
		distributed_quantity = CASE WHEN ready_for_distribution_quantity = 0 THEN quantity ELSE ready_for_distribution_quantity END,
		ready_for_distribution_quantity = CASE WHEN ready_for_distribution_quantity = 0 THEN quantity ELSE ready_for_distribution_quantity END,
		distributed_on = UTC_TIMESTAMP()
	WHERE order_id = ?
	  AND oi.distributed_on IS NULL
	  AND TIMESTAMPADD(SECOND, IFNULL(d.duration,0), oi.ordered_on) <= UTC_TIMESTAMP()
	`
	if _, err := db.ExecContext(ctx, qUpdItems, orderID); err != nil {
		return nil, err
	}

	/*
		if err := tx.Commit(); err != nil {
			return nil, err
		}
	*/

	return meta, nil
}

// Disable payments
func (r *OrdersLifeCycleRepository) DisablePayments(ctx context.Context, orderID string) error {
	db := dbutils.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `
        UPDATE payments
        SET enabled = 0
        WHERE order_id = ?`,
		orderID,
	)
	return err
}

// Delete QR codes
func (r *OrdersLifeCycleRepository) DeleteQRCode(ctx context.Context, orderID string) error {
	db := dbutils.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `
        DELETE qr
        FROM qrcodes qr
        INNER JOIN order_location ol ON qr.location_id = ol.location_id
        INNER JOIN orders o ON o.order_id = ol.order_id AND o.merchant_id = qr.merchant_id
        WHERE o.order_id = ?`,
		orderID,
	)
	return err
}

// Clear bookings
func (r *OrdersLifeCycleRepository) ClearBookings(ctx context.Context, orderID string) error {
	db := dbutils.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `
        UPDATE bookings
        SET order_id = NULL
        WHERE order_id = ?`,
		orderID,
	)
	return err
}

func (r *OrdersLifeCycleRepository) UpdateProductionStatus(ctx context.Context, merchantID string, req *UpdateProductionStatusRequest) ([]string, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// Collect unique order IDs
	orderIDMap := make(map[string]bool)
	for _, product := range req.Products {
		orderIDMap[product.OrderID] = true
	}

	// Update each product's production status
	for _, product := range req.Products {
		stmt, err := db.PrepareContext(ctx, `
			UPDATE orderitems
			SET production_status = ?,
			    production_status_done_quantity = CASE
			        WHEN ? = 'DONE' THEN quantity
			        ELSE ready_for_distribution_quantity
			    END,
				isDistributed = CASE
			        WHEN ? = 'DONE' THEN 1
			        ELSE 0
			    END
			WHERE order_item_id = ? AND order_id = ?
		`)
		if err != nil {
			log.Error(err.Error())
			return nil, fmt.Errorf("prepare statement failed: %w", err)
		}
		defer stmt.Close()

		_, err = stmt.ExecContext(ctx,
			product.ProductionStatus,
			product.ProductionStatus,
			product.ProductionStatus,
			product.OrderItemID,
			product.OrderID,
		)
		if err != nil {
			log.Error(err.Error())
			return nil, fmt.Errorf("execute update failed for order_item_id %s: %w", product.OrderItemID, err)
		}
	}

	// Convert order ID map to slice
	affectedOrderIDs := make([]string, 0, len(orderIDMap))
	for orderID := range orderIDMap {
		affectedOrderIDs = append(affectedOrderIDs, orderID)
	}

	return affectedOrderIDs, nil
}

func (r *OrdersLifeCycleRepository) CreateOrder(ctx context.Context, req *models.RequestObject) (*models.CreateOrderResult, error) {
	log := logger.FromContext(ctx)

	unavailable, err := r.validateProductAvailability(ctx, req)

	if err != nil {
		//tx.Rollback()
		log.Error("Error validating products availability - " + err.Error())
		return nil, err
	}
	if len(unavailable) > 0 {
		//tx.Rollback()
		return &models.CreateOrderResult{Status: "unavailable_products"}, nil
	}

	if req.Order.Customer != nil {
		customerID, err := r.upsertCustomer(ctx, req)
		if err != nil {
			//tx.Rollback()
			return nil, err
		}
		req.Order.Customer.CustomerID = customerID
	}

	// compute estimated ready if not provided
	estimatedReady := req.Order.EstimatedReady // string or empty
	if estimatedReady == "" {
		est, err := r.ComputeEstimatedReady(ctx, req.MerchantID, len(req.Order.Products))
		if err != nil {
			log.Error("Compute Estimated Ready warning : " + err.Error())
		}
		if est != "" {
			estimatedReady = est
		}
	}
	req.Order.EstimatedReady = estimatedReady

	// get next order number
	orderNum, err := r.GetNextOrderNum(ctx, req.MerchantID)
	if err != nil {
		log.Error("Cannot retrieve last order num: " + err.Error())
		orderNum = "0"
	}
	req.Order.OrderNum = &orderNum

	if req.DeviceID != nil && *req.DeviceID != "" {
		activeRegister, err := r.GetActiveCashRegisterID(ctx, *req.DeviceID)
		if err != nil {
			return nil, err
		}
		req.Order.CashRegisterId = &activeRegister
	} else if req.DeviceID == nil && req.Order.CashRegisterId != nil {
		// Vérifier que la commande vient d'Uber Eats, Deliveroo ou ScanNOrder afin d'éviter l'injection de commande non autorisée

	} else {
		return nil, models.ErrDeviceIDMissing
	}

	r.setOrderDefaults(ctx, req)

	orderID, err := r.insertOrderBase(ctx, req)
	if err != nil {
		log.Error("insertOrderBase failure" + err.Error())
		return nil, err
	}
	req.Order.OrderID = &orderID

	usedItems, err := r.insertOrderItems(ctx, req)
	if err != nil {
		log.Error("insertOrderItems failure" + err.Error())
		return nil, err
	}

	// Supposons que tu aies ton objet req.Order disponible
	err = r.InsertOrderLocations(ctx, req.Order)
	if err != nil {
		return nil, err // Gérer l'erreur proprement
	}

	if err := r.insertExtrasWithoutsConfigs(ctx, req, usedItems); err != nil {
		//tx.Rollback()
		log.Error("insertExtrasWithoutsConfigs failure " + err.Error())
		return nil, err
	}

	if err := r.insertPayments(ctx, req); err != nil {
		//tx.Rollback()
		log.Error("insertPayments failure" + err.Error())
		return nil, err
	}
	/*
		if err := tx.Commit(); err != nil {
			return nil, err
		}
	*/

	var action string

	if req.Order.OnlinePayment && (req.Order.Locations == nil || len(req.Order.Locations) == 0) {
		action = "payment"
	} else if req.Order.Locations == nil || len(req.Order.Locations) == 0 {
		action = "waiting"
	} else {
		action = "get_order"
	}

	return &models.CreateOrderResult{
		Status:   "success",
		OrderID:  orderID,
		OrderNum: &orderNum,
		Action:   action,
	}, nil
}

// InsertOrderLocations insère toutes les locations liées à une commande en une seule requête (Bulk Insert).
func (r *OrdersLifeCycleRepository) InsertOrderLocations(ctx context.Context, order models.OrderRequest) error {
	// S'il n'y a pas de location, on ne fait rien (équivalent du sizeof > 0 en PHP)
	if len(order.Locations) == 0 {
		return nil
	}

	db := dbutils.GetDB(ctx, r.database)

	// Préparation de la requête et des arguments
	valueStrings := make([]string, 0, len(order.Locations))
	valueArgs := make([]interface{}, 0, len(order.Locations)*2)

	for _, loc := range order.Locations {
		valueStrings = append(valueStrings, "(?, ?)")
		valueArgs = append(valueArgs, order.OrderID, loc.LocationID)
	}

	// Construction de la requête finale
	// Resultat: INSERT INTO order_location (order_id, location_id) VALUES (?, ?), (?, ?)...
	query := fmt.Sprintf("INSERT INTO order_location (order_id, location_id) VALUES %s", strings.Join(valueStrings, ","))

	// Exécution de la requête unique
	_, err := db.ExecContext(ctx, query, valueArgs...)
	if err != nil {
		logger.FromContext(ctx).Error("failed to bulk insert order locations", zap.Error(err))
		return err
	}

	return nil
}

// upsertCustomer calls the customer repository to create/update the customer and returns numeric MerchantID (nil if none)
func (r *OrdersLifeCycleRepository) upsertCustomer(ctx context.Context, req *models.RequestObject) (*string, error) {
	log := logger.FromContext(ctx)
	if req.Order.Customer == nil {
		log.Warn("customer is required")
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
		cust.CustomerLastName = req.Order.Customer.Name
	} else if req.Order.Customer.FirstName != nil || req.Order.Customer.LastName != nil {
		cust.CustomerFirstName = req.Order.Customer.FirstName
		cust.CustomerLastName = req.Order.Customer.LastName

		fullName := strings.TrimSpace(fmt.Sprintf("%s %s", helpers.SafeString(req.Order.Customer.FirstName), helpers.SafeString(req.Order.Customer.LastName)))
		cust.CustomerName = &fullName
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
	if req.Order.Customer.CustomerBrand != "" {
		cust.CustomerBrand = &req.Order.Customer.CustomerBrand
	} else {
		brand := models.BrandWelloResto
		cust.CustomerBrand = &brand
	}
	cust.AdvertisingConsent = helpers.BoolPtr(false)
	if req.Order.Customer.AdvertisingConsent != nil {
		cust.AdvertisingConsent = req.Order.Customer.AdvertisingConsent
	}

	newIDStr, err := r.custoRepo.UpdateOrCreateCustomer(ctx, cust)
	if err != nil {
		log.Error("Failed to create - update customer - " + err.Error())
		return nil, fmt.Errorf("failed to update/create customer: %w", err)
	}
	if newIDStr == nil {
		return nil, nil
	}
	return newIDStr, nil
}

func (r *OrdersLifeCycleRepository) validateProductAvailability(ctx context.Context, req *models.RequestObject) ([]string, error) {
	db := dbutils.GetDB(ctx, r.database)

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
                  AND c.status IN ('0','out_of_stock')
                  AND rq.enabled = TRUE
        ) a ON a.product_id = p.product_id
        WHERE p.product_id IN (%s)
          AND (CASE WHEN a.product_id IS NOT NULL THEN 'out_of_stock' ELSE p.status END) = 'out_of_stock'
    `, inClause)

	rows, err := db.QueryContext(ctx, query, ids...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blocked []string
	for rows.Next() {
		var productID string
		if err := rows.Scan(&productID); err != nil {
			return nil, err
		}
		blocked = append(blocked, productID)
	}

	return blocked, rows.Err()
}

func (r *OrdersLifeCycleRepository) UpdateOrder(ctx context.Context, req *models.RequestObject) error {
	log := logger.FromContext(ctx)
	db := dbutils.GetDB(ctx, r.database)

	if len(req.Order.Products) == 0 {
		return models.ErrCartEmpty
	}

	// 2. Suppression des items retirés du panier et de tous leurs sous-éléments.
	//
	// STRATÉGIE : On calcule l'ensemble des order_item_id EXISTANTS envoyés dans le payload
	// (les nouveaux produits n'ont pas d'order_item_id). Tous les orderitems de cette commande
	// qui NE sont PAS dans cette liste sont considérés comme supprimés et doivent être retirés
	// de la DB, y compris leurs extras / withouts / configurations associés.
	if err := r.deleteRemovedOrderItems(ctx, req); err != nil {
		return fmt.Errorf("delete removed items failed: %w", err)
	}

	// Préparation des slices pour les Batch Inserts (évite les requêtes individuelles dans la boucle)
	var (
		extrasArgs    []interface{}
		withoutsArgs  []interface{}
		configsArgs   []interface{}
		customersArgs []interface{}
	)

	// 3. Traitement des produits (boucle principale)
	//
	// Pour chaque produit du payload :
	//   - S'il a un order_item_id  → produit EXISTANT : on met à jour la quantité/prix (UPSERT)
	//     puis on supprime + réinsère ses sous-éléments (extras, withouts, configs).
	//   - S'il n'a pas d'order_item_id → produit NOUVEAU : on l'insère et on récupère son ID généré.
	stmtItem, err := db.PrepareContext(ctx, `
		INSERT INTO orderitems (order_item_id, order_id, product_id, merchant_id, quantity, discount_id, base_price, price, delay_id, ordered_on)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, UTC_TIMESTAMP())
		ON DUPLICATE KEY UPDATE
			-- Remet isDistributed à 0 seulement si la quantité distribuée ne correspond plus
			isDistributed = CASE WHEN distributed_quantity = VALUES(quantity) THEN isDistributed ELSE 0 END,
			quantity      = VALUES(quantity),
			base_price    = VALUES(base_price),
			price         = VALUES(price),
			discount_id   = VALUES(discount_id),
			delay_id      = VALUES(delay_id),
			ordered_on    = VALUES(ordered_on)`)
	if err != nil {
		return fmt.Errorf("prepare orderitem upsert failed: %w", err)
	}
	defer stmtItem.Close()

	for i := range req.Order.Products {
		p := &req.Order.Products[i]

		// ── A. Upsert de l'item principal ─────────────────────────────────────────
		// Calcular le prix final : si discounted_price est fourni, l'utiliser, sinon utiliser price
		finalPrice := p.Price
		if p.DiscountedPrice != nil {
			finalPrice = *p.DiscountedPrice
		}

		res, err := stmtItem.ExecContext(ctx,
			p.OrderItemID, req.Order.OrderID, p.ProductID, req.MerchantID,
			p.Quantity, p.DiscountID, p.Price, finalPrice, p.DelayID)
		if err != nil {
			return fmt.Errorf("product upsert failed (product_id=%s): %w", p.ProductID, err)
		}

		// ── B. Récupération de l'ID généré pour les nouveaux produits ─────────────
		// On ne fait ce bloc QU'UNE SEULE FOIS, immédiatement après l'exécution,
		// et on retourne une erreur si on ne parvient pas à obtenir l'ID car toute
		// la suite (extras, withouts, configs, commentaire) en dépend.
		if p.OrderItemID == nil {
			newID, err := res.LastInsertId()
			if err != nil {
				return fmt.Errorf("failed to retrieve new order_item_id for product_id=%s: %w", p.ProductID, err)
			}
			if newID == 0 {
				// LastInsertId renvoie 0 si aucune ligne n'a été insérée (ne devrait pas arriver)
				return fmt.Errorf("unexpected: LastInsertId returned 0 for product_id=%s", p.ProductID)
			}
			p.OrderItemID = helpers.Int64ToStringPtr(newID)
		}

		// ── C. Nettoyage des sous-éléments de cet item ───────────────────────────
		// On supprime systématiquement extras / withouts / configs ET le commentaire
		// produit avant de les réinsérer selon l'état exact du payload.
		// Cela garantit qu'aucun résidu ne subsiste en base.
		for _, q := range []string{
			"DELETE FROM extra WHERE order_item_id = ?",
			"DELETE FROM without WHERE order_item_id = ?",
			"DELETE FROM order_item_configuration WHERE order_item_id = ?",
			// Supprime le commentaire lié à cet item (s'il existe) pour le réinsérer
			// proprement ci-dessous — ou le laisser absent si le payload n'en fournit pas.
			"DELETE FROM order_comments WHERE order_item_id = ?",
		} {
			if _, err := db.ExecContext(ctx, q, p.OrderItemID); err != nil {
				return fmt.Errorf("cleaning sub-items failed for order_item_id=%s: %w", *p.OrderItemID, err)
			}
		}

		// ── D. Commentaire de l'item ─────────────────────────────────────────────
		// Le DELETE ci-dessus a déjà nettoyé l'éventuel ancien commentaire.
		// On réinsère uniquement si le payload en fournit un.
		if p.Comment != nil && p.Comment.Content != "" {
			item := &models.OrderItemInsert{
				OrderID:     *req.Order.OrderID,
				OrderItemID: p.OrderItemID, // garanti non-nil à ce stade
				ProductID:   p.ProductID,
				MerchantID:  req.MerchantID,
				Quantity:    p.Quantity,
				DiscountID:  p.DiscountID,
				Price:       p.Price,
				DelayID:     p.DelayID,
				CreatedBy:   *req.Order.CreatedBy,
				Comment:     &p.Comment.Content,
			}
			if err := r.insertOrderItemComment(ctx, item); err != nil {
				// Non bloquant : on logue mais on ne fait pas échouer la transaction
				log.Warn("insertOrderItemComment failed", zap.String("order_item_id", *p.OrderItemID), zap.Error(err))
			}
		}

		// ── E. Accumulation des sous-éléments pour Batch Insert ──────────────────
		// On accumule TOUTES les valeurs dans des slices plates :
		// chaque groupe de N valeurs correspond à une ligne INSERT.

		// Extras (suppléments payants)
		for _, e := range p.Extra {
			extrasArgs = append(extrasArgs, p.OrderItemID, req.Order.OrderID, e.ComponentID, p.ProductID, req.MerchantID, e.Price)
		}

		// Withouts (exclusions d'ingrédients)
		for _, w := range p.Without {
			withoutsArgs = append(withoutsArgs, p.OrderItemID, req.Order.OrderID, w.ComponentID, p.ProductID, req.MerchantID)
		}

		// Configurations (options de personnalisation)
		if p.Config != nil {
			for _, attr := range p.Config.Attributes {
				for _, opt := range attr.Options {
					configsArgs = append(configsArgs, p.OrderItemID, attr.ID, opt.ID, opt.Quantity)
				}
			}
		}

		// Customers (session partagée) — décommentez si la fonctionnalité est activée
		// for _, c := range p.Customers {
		// 	customersArgs = append(customersArgs, c.UserCode, p.OrderItemID, c.Quantity)
		// }
	}

	// 4. Batch Inserts des sous-éléments
	//
	// On génère une seule requête multi-valeurs par table, ce qui est significativement
	// plus rapide que N requêtes individuelles (1 aller-retour réseau au lieu de N).

	if len(extrasArgs) > 0 {
		if err := r.bulkInsert(ctx,
			"INSERT INTO extra (order_item_id, order_id, component_id, product_id, merchant_id, price) VALUES",
			6, extrasArgs); err != nil {
			return fmt.Errorf("bulk insert extras failed: %w", err)
		}
	}
	if len(withoutsArgs) > 0 {
		if err := r.bulkInsert(ctx,
			"INSERT INTO without (order_item_id, order_id, component_id, product_id, merchant_id) VALUES",
			5, withoutsArgs); err != nil {
			return fmt.Errorf("bulk insert withouts failed: %w", err)
		}
	}
	if len(configsArgs) > 0 {
		if err := r.bulkInsert(ctx,
			"INSERT INTO order_item_configuration (order_item_id, configuration_attribute_id, configuration_attribute_option_id, quantity) VALUES",
			4, configsArgs); err != nil {
			return fmt.Errorf("bulk insert configs failed: %w", err)
		}
	}
	if len(customersArgs) > 0 {
		if err := r.bulkInsertWithSuffix(ctx,
			"INSERT INTO session_orderitem (user_code, order_item_id, quantity) VALUES",
			" ON DUPLICATE KEY UPDATE quantity=VALUES(quantity)",
			3, customersArgs); err != nil {
			return fmt.Errorf("bulk insert session_orderitem failed: %w", err)
		}
	}

	// 5. Calcul du temps estimé de préparation (si non fourni dans le payload)
	estimatedReady := req.Order.EstimatedReady
	if estimatedReady == "" {
		est, err := r.ComputeEstimatedReady(ctx, req.MerchantID, len(req.Order.Products))
		if err != nil {
			// Non bloquant : la commande peut être sauvegardée sans temps estimé
			log.Warn("ComputeEstimatedReady warning", zap.Error(err))
		}
		if est != "" {
			estimatedReady = est
		}
	}
	req.Order.EstimatedReady = estimatedReady

	// 6. Upsert du client (si fourni)
	if req.Order.Customer != nil {
		customerID, err := r.upsertCustomer(ctx, req)
		if err != nil {
			return fmt.Errorf("upsert customer failed: %w", err)
		}
		req.Order.Customer.CustomerID = customerID
	}

	// 7. Mise à jour de la commande principale (prix, type, etc.)
	if err := r.updateOrderBase(ctx, req); err != nil {
		return fmt.Errorf("update order base failed: %w", err)
	}

	// 8. Gestion des emplacements (table, salle…)
	// On supprime et réinsère entièrement pour refléter fidèlement le payload.
	if _, err := db.ExecContext(ctx, "DELETE FROM order_location WHERE order_id = ?", req.Order.OrderID); err != nil {
		return fmt.Errorf("delete order_location failed: %w", err)
	}
	if len(req.Order.Locations) > 0 {
		var locArgs []interface{}
		for _, loc := range req.Order.Locations {
			locArgs = append(locArgs, req.Order.OrderID, loc.LocationID)
		}
		if err := r.bulkInsert(ctx, "INSERT INTO order_location(order_id, location_id) VALUES", 2, locArgs); err != nil {
			return fmt.Errorf("bulk insert order_location failed: %w", err)
		}
	}

	// 9. Commit
	/*
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit failed: %w", err)
		}
	*/

	return nil
}

// deleteRemovedOrderItems supprime de la base les orderitems qui ne figurent plus dans le
// payload (produits retirés du panier), ainsi que tous leurs sous-éléments associés
// (extras, withouts, configurations).
//
// Stratégie :
//   - On collecte les order_item_id EXISTANTS présents dans le payload (les nouveaux produits
//     n'en ont pas).
//   - On supprime dans `orderitems` tous les items de cette commande dont l'ID n'est PAS
//     dans cette liste. La suppression en cascade (via les DELETEs explicites ci-dessous)
//     nettoie aussi extra / without / order_item_configuration.
//
// Note : si le payload ne contient AUCUN item existant (mise à jour ne conservant aucun
// ancien produit), on supprime tous les anciens items.
func (r *OrdersLifeCycleRepository) deleteRemovedOrderItems(ctx context.Context, req *models.RequestObject) error {
	db := dbutils.GetDB(ctx, r.database)

	// Collecte des order_item_id existants fournis dans le payload
	keptIDs := make([]interface{}, 0, len(req.Order.Products))
	for _, p := range req.Order.Products {
		if p.OrderItemID != nil && *p.OrderItemID != "" {
			keptIDs = append(keptIDs, *p.OrderItemID)
		}
	}

	// Requête de suppression des orderitems retirés, avec nettoyage en cascade des sous-tables.
	//
	// On utilise une sous-requête pour identifier les IDs à supprimer, puis on supprime
	// dans chaque sous-table avant de supprimer dans orderitems (contraintes de clé étrangère).
	var (
		whereClause string
		args        []interface{}
	)

	args = append(args, req.Order.OrderID)

	if len(keptIDs) == 0 {
		// Aucun item conservé → on supprime tous les anciens items de cette commande
		whereClause = "WHERE oi.order_id = ?"
	} else {
		// On supprime uniquement les items absents du payload
		placeholders := strings.Repeat(",?", len(keptIDs))[1:] // "?,?,?"
		whereClause = "WHERE oi.order_id = ? AND oi.order_item_id NOT IN (" + placeholders + ")"
		args = append(args, keptIDs...)
	}

	// 1. Récupère les IDs des items à supprimer pour nettoyer les sous-tables.
	//    On le fait en une seule requête SELECT pour éviter de répéter la condition.
	rows, err := db.QueryContext(ctx,
		"SELECT oi.order_item_id FROM orderitems oi "+whereClause,
		args...)
	if err != nil {
		return fmt.Errorf("select removed order_item_ids failed: %w", err)
	}

	var removedIDs []interface{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scan removed order_item_id failed: %w", err)
		}
		removedIDs = append(removedIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate removed order_item_ids failed: %w", err)
	}

	// Rien à supprimer → on sort immédiatement
	if len(removedIDs) == 0 {
		return nil
	}

	// 2. Nettoyage des sous-tables dans l'ordre (sous-éléments avant l'item parent)
	idPlaceholders := strings.Repeat(",?", len(removedIDs))[1:] // "?,?,?"
	subTables := []string{
		"DELETE FROM extra WHERE order_item_id IN (" + idPlaceholders + ")",
		"DELETE FROM without WHERE order_item_id IN (" + idPlaceholders + ")",
		"DELETE FROM order_item_configuration WHERE order_item_id IN (" + idPlaceholders + ")",
		"DELETE FROM session_orderitem WHERE order_item_id IN (" + idPlaceholders + ")",
		"DELETE FROM order_comments WHERE order_item_id IN (" + idPlaceholders + ")",
	}
	for _, q := range subTables {
		if _, err := db.ExecContext(ctx, q, removedIDs...); err != nil {
			return fmt.Errorf("delete sub-items for removed orderitems failed: %w", err)
		}
	}

	// 3. Suppression des items eux-mêmes
	delItemsQuery := "DELETE FROM orderitems WHERE order_item_id IN (" + idPlaceholders + ")"
	if _, err := db.ExecContext(ctx, delItemsQuery, removedIDs...); err != nil {
		return fmt.Errorf("delete removed orderitems failed: %w", err)
	}

	return nil
}

func (r *OrdersLifeCycleRepository) bulkInsert(ctx context.Context, queryPrefix string, numFields int, args []interface{}) error {
	return r.bulkInsertWithSuffix(ctx, queryPrefix, "", numFields, args)
}

func (r *OrdersLifeCycleRepository) bulkInsertWithSuffix(ctx context.Context, queryPrefix, querySuffix string, numFields int, args []interface{}) error {
	if len(args) == 0 {
		return nil
	}
	db := dbutils.GetDB(ctx, r.database)

	numRows := len(args) / numFields
	placeholders := make([]string, 0, numRows)

	rowPlaceholder := "(" + strings.Repeat("?,", numFields)
	rowPlaceholder = rowPlaceholder[:len(rowPlaceholder)-1] + ")"

	for i := 0; i < numRows; i++ {
		placeholders = append(placeholders, rowPlaceholder)
	}

	query := fmt.Sprintf("%s %s %s", queryPrefix, strings.Join(placeholders, ","), querySuffix)

	_, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("bulk insert failed: %w", err)
	}
	return nil
}

// insertOrderBase inserts the orders row and returns orderID and orderNum
func (r *OrdersLifeCycleRepository) insertOrderBase(ctx context.Context, req *models.RequestObject) (orderID string, err error) {
	db := dbutils.GetDB(ctx, r.database)

	var customer_id *string
	if req.Order.Customer != nil {
		customer_id = req.Order.Customer.CustomerID
	}
	PublicID := helpers.GeneratePrefixedID("order-")
	estimatedReady := normalizeEstimatedReady(req.Order.EstimatedReady)
	// default fields and estimated_ready handling simplified: use UTC_TIMESTAMP equivalent in SQL
	res, err := db.ExecContext(ctx, `
		INSERT INTO orders(public_id, brand, brand_order_id, brand_order_num, cash_register_id, merchant_id, customer_id, order_num, price, TVA, HT, merchant_approval, scheduled, creation_date,
		                   dateCall, last_update, responsible, created_by, delivery_fees, estimated_ready, use_customer_temporary_address,
		                   brand_status, order_type, places_settings, pager_number, fulfillment_type, isPaid)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, UTC_TIMESTAMP, UTC_TIMESTAMP, UTC_TIMESTAMP, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		PublicID, req.Order.Brand, req.Order.BrandOrderID, req.Order.BrandOrderNum, req.Order.CashRegisterId, req.MerchantID, customer_id, req.Order.OrderNum, req.Order.TTC, req.Order.TVA, req.Order.HT,
		req.Order.MerchantApproval, req.Order.IsScheduled,
		req.Order.Responsible, req.Order.CreatedBy, req.Order.DeliveryFees, estimatedReady,
		req.Order.UseCustomerTemporaryAddress, req.Order.BrandStatus, req.Order.OrderType, req.Order.PlacesSettings, req.Order.PagerNumber, req.Order.FulfillmentType, req.Order.IsPaid,
	)
	if err != nil {
		return "no_order_created", err
	}
	lastID, err := res.LastInsertId()
	if err != nil {
		return "no_order_created", err
	}
	req.Order.OrderID = helpers.Int64ToStringPtr(lastID)

	err = r.insertOrderComment(ctx, req)

	if err != nil {
		logger.FromContext(ctx).Error(err.Error())
	}

	return strconv.FormatInt(lastID, 10), nil
}

// GetNextOrderNum returns the next order_num following the PHP behaviour:
// - if last order_num is 99 or null -> return 1
// - otherwise last + 1
func (r *OrdersLifeCycleRepository) GetNextOrderNum(ctx context.Context, merchantID string) (string, error) {
	db := dbutils.GetDB(ctx, r.database)
	var last sql.NullInt64

	err := db.QueryRowContext(ctx, `
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

func (r *OrdersLifeCycleRepository) ComputeEstimatedReady(ctx context.Context, merchantID string, productsCount int) (string, error) {
	db := dbutils.GetDB(ctx, r.database) // Gère tout seul si on est en transaction ou non

	rows, err := db.QueryContext(ctx, "CALL GET_AVERAGE_DISTRIBUTION_TIME(?, ?)", merchantID, productsCount)
	if err != nil {
		return "", nil
	}
	defer rows.Close()

	var seconds sql.NullInt64
	if rows.Next() {
		if err := rows.Scan(&seconds); err != nil {
			return "", nil
		}
	}

	if !seconds.Valid || seconds.Int64 <= 0 {
		return "", nil
	}

	t := time.Now().UTC().Add(time.Duration(seconds.Int64) * time.Second)
	return t.Format("2006-01-02 15:04:05"), nil
}

// setOrderDefaults applique les règles métier par défaut (équivalent du bloc PHP)
func (r *OrdersLifeCycleRepository) setOrderDefaults(ctx context.Context, req *models.RequestObject) {
	//log := logger.FromContext(ctx)
	/*
		AJOUTER LES VALEURS PAR DEFAUT ICI

		// INSERT ORDER
		// Default values
		$order_object->order->is_scheduled = isset($order_object->order->is_scheduled) && $order_object->order->is_scheduled ? "1" : "0";
		$order_object->order->places_settings = $order_object->order->places_settings ?? 0;

	*/

	if req.Order.MerchantApproval == "" {
		req.Order.MerchantApproval = "ACCEPTED"
	}

	defaultFulfillmentType := "DELIVERY_BY_RESTAURANT"
	if req.Order.FulfillmentType == nil {
		req.Order.FulfillmentType = &defaultFulfillmentType
	} else if *req.Order.FulfillmentType == "" {
		req.Order.FulfillmentType = &defaultFulfillmentType
	}

	if req.Order.Brand == "" {
		req.Order.Brand = models.BrandWelloResto
	}

	var totalPaid int = 0
	if req.Order.Payments != nil {
		for _, payment := range req.Order.Payments {
			totalPaid += payment.Amount
		}
	}

	req.Order.IsPaid = totalPaid == req.Order.TTC

	// PHP: $brand_status = ... ?? (($online_payment) ? "ONLINE_PAYMENT_PENDING" : "PENDING");
	if req.Order.BrandStatus == "" {
		// Note : Assure-toi que le champ OnlinePayment existe bien dans ton modèle Go
		if req.Order.OnlinePayment {
			req.Order.BrandStatus = "ONLINE_PAYMENT_PENDING"
		} else {
			req.Order.BrandStatus = "PENDING"
		}
	}

	//TODO
	// PHP: places_settings = ... ?? 0;
	// En Go, un int est par défaut à 0. Cette ligne est implicite,
	// mais on peut la garder si places_settings est un pointeur (*int).

	//TODO
	// PHP: is_scheduled = ... ? "1" : "0";
	// En Go, le booléen est "false" par défaut.
	// Le driver SQL convertira automatiquement le bool true/false en 1/0 (TINYINT) pour MySQL.
}

func normalizeEstimatedReady(value string) interface{} {
	if value == "" {
		return nil
	}

	// ISO 8601 (ex: "2026-04-26T18:00:00Z" ou "2026-04-26T20:00:00+02:00")
	// Format actuel — à conserver une fois la migration terminée.
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t.UTC()
	}

	// TODO: supprimer ce bloc une fois que tous les clients envoient de l'ISO 8601.
	// Unix timestamp (ex: "1777235400") — ancienne version.
	if unix, err := strconv.ParseInt(value, 10, 64); err == nil && unix > 1_000_000_000 {
		return time.Unix(unix, 0).UTC()
	}

	// Format non reconnu : on rejette plutôt que de laisser MySQL deviner la timezone.
	return nil
}

// IsCashRegisterRequiredForOrdering checks merchant parameter cash_register_required_for_ordering == 1
func (r *OrdersLifeCycleRepository) IsCashRegisterRequiredForOrdering(ctx context.Context, merchantID string) (bool, error) {
	db := dbutils.GetDB(ctx, r.database)
	var required sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT mp.cash_register_required_for_ordering
		FROM merchant_parameters mp
		WHERE mp.merchant_id = ? LIMIT 1
		`, merchantID).Scan(&required)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		log := logger.FromContext(ctx)
		log.Error("IsCashRegisterRequiredForOrdering query failed", zap.Error(err))
		return false, err
	}
	// DB may store '1' as string or tinyint -> be permissive
	if required.Valid && (required.String == "1" || required.String == "true") {
		return true, nil
	}
	return false, nil
}

// insertOrderCommentinsertOrderItemComment inserts the order items comments
func (r *OrdersLifeCycleRepository) insertOrderItemComment(ctx context.Context, item *models.OrderItemInsert) error {
	db := dbutils.GetDB(ctx, r.database)

	if item.Comment == nil {
		return nil
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO order_comments(order_id, order_item_id, user_id, content, creation_date)
		VALUES (?,?, ?,?,UTC_TIMESTAMP())
		ON DUPLICATE KEY UPDATE content = ?, creation_date = UTC_TIMESTAMP()`,
		item.OrderID, item.OrderItemID, item.CreatedBy, item.Comment, item.Comment,
	)
	return err
}

// insertOrderComment inserts the orders comments
func (r *OrdersLifeCycleRepository) insertOrderComment(ctx context.Context, req *models.RequestObject) (err error) {
	db := dbutils.GetDB(ctx, r.database)

	if req.Order.Comment == nil {
		return nil
	}
	// default fields and estimated_ready handling simplified: use UTC_TIMESTAMP equivalent in SQL
	_, err = db.ExecContext(ctx, `
		INSERT INTO order_comments(order_id, user_id, content, creation_date)
		VALUES (?,?,?,UTC_TIMESTAMP)
		ON DUPLICATE KEY UPDATE content = ?, creation_date = UTC_TIMESTAMP`,
		req.Order.OrderID, req.Order.CreatedBy, req.Order.Comment, req.Order.Comment,
	)
	if err != nil {
		return err
	}
	return nil
}

// updateOrderBase inserts the orders row and returns orderID and orderNum
func (r *OrdersLifeCycleRepository) updateOrderBase(ctx context.Context, req *models.RequestObject) (err error) {
	db := dbutils.GetDB(ctx, r.database)

	var customerID *string
	if req.Order.Customer != nil {
		customerID = req.Order.Customer.CustomerID
	}

	estimatedReady := normalizeEstimatedReady(req.Order.EstimatedReady)

	// default fields and estimated_ready handling simplified: use UTC_TIMESTAMP equivalent in SQL
	_, err = db.ExecContext(ctx, `
		UPDATE orders o
			SET
			    o.price = ?,
			    o.tva = ?,
			    o.ht = ?,
				o.isDistributed = 0, 
				o.isPaid = 0,
				o.last_update = UTC_TIMESTAMP,
				o.delivery_fees = ?,
				o.use_customer_temporary_address = ?,
				o.order_type = ?,
				o.scheduled = ?,
				o.estimated_ready = ?,
				o.places_settings = ?,
				o.customer_id = ?
			WHERE order_id = ?`,
		req.Order.TTC,
		req.Order.TVA,
		req.Order.HT,
		req.Order.DeliveryFees,
		req.Order.UseCustomerTemporaryAddress,
		req.Order.OrderType,
		req.Order.IsScheduled,
		estimatedReady,
		req.Order.PlacesSettings,
		customerID,
		req.Order.OrderID,
	)
	if err != nil {
		return err
	}

	// Commentaire de commande : on supprime systématiquement l'ancien puis on réinsère
	// uniquement si le payload en fournit un. Cela permet de supprimer un commentaire
	// existant lorsque le client l'efface (payload avec comment = nil ou vide).
	if _, err := db.ExecContext(ctx,
		"DELETE FROM order_comments WHERE order_id = ? AND order_item_id IS NULL",
		req.Order.OrderID); err != nil {
		return fmt.Errorf("delete order comment failed: %w", err)
	}
	if req.Order.Comment != nil && *req.Order.Comment != "" {
		if err := r.insertOrderComment(ctx, req); err != nil {
			return fmt.Errorf("insert order comment failed: %w", err)
		}
	}

	return nil
}

// insertOrderItems inserts each orderitem and returns list of UsedItem (order_item_id + qty)
func (r *OrdersLifeCycleRepository) insertOrderItems(ctx context.Context, req *models.RequestObject) ([]models.UsedItem, error) {
	used := make([]models.UsedItem, 0, len(req.Order.Products))
	for _, p := range req.Order.Products {
		if p.Quantity == 0 {
			continue
		}

		// Calcul du prix final : si discounted_price est fourni, l'utiliser, sinon utiliser price
		finalPrice := p.Price
		if p.DiscountedPrice != nil {
			finalPrice = *p.DiscountedPrice
		}

		item := &models.OrderItemInsert{
			OrderID:         *req.Order.OrderID,
			ProductID:       p.ProductID,
			MerchantID:      req.MerchantID,
			Quantity:        p.Quantity,
			DiscountID:      p.DiscountID,
			Price:           finalPrice,        // Final price to apply
			BasePrice:       p.Price,           // Original price before discounts
			DiscountedPrice: p.DiscountedPrice, // Discounted price (optional)
			DelayID:         p.DelayID,
			CreatedBy:       *req.Order.CreatedBy,
		}
		if p.Comment != nil && p.Comment.Content != "" {
			item.Comment = &p.Comment.Content
		}
		oid, err := r.InsertOrderItem(ctx, item)

		if err != nil {
			return nil, err
		}

		used = append(used, models.UsedItem{OrderItemID: strconv.FormatInt(oid, 10), Quantity: p.Quantity})
	}
	return used, nil
}

// InsertOrderItem inserts a single orderitem and returns its id
func (r *OrdersLifeCycleRepository) InsertOrderItem(ctx context.Context, item *models.OrderItemInsert) (int64, error) {
	db := dbutils.GetDB(ctx, r.database)

	res, err := db.ExecContext(ctx, `
		INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, discount_id, base_price, price, ordered_on, delay_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, UTC_TIMESTAMP, ?)
		`, item.OrderID, item.ProductID, item.MerchantID, item.Quantity, item.DiscountID, item.BasePrice, item.Price, item.DelayID)
	if err != nil {
		return 0, err
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		logger.FromContext(ctx).Error(err.Error())
		return 0, nil
	}
	item.OrderItemID = helpers.Int64ToStringPtr(lastID)

	r.insertOrderItemComment(ctx, item)

	return res.LastInsertId()
}

// insertExtrasWithoutsConfigs does bulk inserts for extras, withouts, configurations
func (r *OrdersLifeCycleRepository) insertExtrasWithoutsConfigs(ctx context.Context, req *models.RequestObject, items []models.UsedItem) error {
	// Build maps from product iteration to order_item ids; we used ordering to match the order of products to items
	// Simpler approach: while inserting items we could have returned corresponding mapping; for now assume order preserved.
	extras := []models.ExtraInsert{}
	withouts := []models.WithoutInsert{}
	configs := []models.ConfigInsert{}

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
			extras = append(extras, models.ExtraInsert{
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
			withouts = append(withouts, models.WithoutInsert{
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
					configs = append(configs, models.ConfigInsert{
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
		if err := r.BulkInsertExtras(ctx, extras); err != nil {
			return err
		}
	}
	if len(withouts) > 0 {
		if err := r.BulkInsertWithouts(ctx, withouts); err != nil {
			return err
		}
	}
	if len(configs) > 0 {
		if err := r.BulkInsertConfigs(ctx, configs); err != nil {
			return err
		}
	}
	return nil
}

// BulkInsertExtras performs multi-value insert for extras
func (r *OrdersLifeCycleRepository) BulkInsertExtras(ctx context.Context, list []models.ExtraInsert) error {
	db := dbutils.GetDB(ctx, r.database)
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
	_, err := db.ExecContext(ctx, query, args...)
	return err
}

func (r *OrdersLifeCycleRepository) BulkInsertWithouts(ctx context.Context, list []models.WithoutInsert) error {
	db := dbutils.GetDB(ctx, r.database)

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
	_, err := db.ExecContext(ctx, query, args...)
	return err
}

func (r *OrdersLifeCycleRepository) BulkInsertConfigs(ctx context.Context, list []models.ConfigInsert) error {
	db := dbutils.GetDB(ctx, r.database)

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
	_, err := db.ExecContext(ctx, query, args...)
	return err
}

// insertPayments inserts payments
func (r *OrdersLifeCycleRepository) insertPayments(ctx context.Context, req *models.RequestObject) error {
	for _, p := range req.Order.Payments {
		pi := &models.Payment{
			MerchantID:     req.MerchantID,
			CashRegisterID: *req.Order.CashRegisterId,
			OrderID:        *req.Order.OrderID,
			Amount:         p.Amount,
			MOP:            p.MOP,
			UserID:         *req.Order.CreatedBy,
			OperationType:  models.OperationTypeSale,
		}
		if err := r.AddPayment(ctx, *pi); err != nil {
			return err
		}
	}
	return nil
}
