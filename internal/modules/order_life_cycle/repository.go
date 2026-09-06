package order_life_cycle

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/customers"
	"welloresto-api/internal/modules/deliverytime"
	"welloresto-api/internal/modules/distributiontime"
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
	db := dbx.GetDB(ctx, r.database)

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
	db := dbx.GetDB(ctx, r.database)
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

func (r *OrdersLifeCycleRepository) GetActiveCashRegisterID(ctx context.Context, merchantID, deviceID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	var cashRegisterID sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT cr.cash_register_id
		FROM cash_registers cr
		WHERE cr.device_id = ?
		AND cr.merchant_id = ?
		AND cr.end_date IS NULL
	`, deviceID, merchantID).Scan(&cashRegisterID)

	if err == sql.ErrNoRows {
		err = db.QueryRowContext(ctx, `
			SELECT cr.cash_register_id
			FROM cash_registers cr
			INNER JOIN device_link dl on dl.on_behalf_of = cr.device_id
			WHERE dl.device_id = ?
			AND cr.merchant_id = ?
			AND cr.end_date IS NULL
		`, deviceID, merchantID).Scan(&cashRegisterID)

		if err == sql.ErrNoRows {
			var linkedDevice string
			linkErr := db.QueryRowContext(ctx, `
				SELECT on_behalf_of FROM device_link WHERE device_id = ?
			`, deviceID).Scan(&linkedDevice)

			if linkErr == nil {
				return "", models.ErrLinkedDeviceRegisterClosed
			}
			if linkErr != sql.ErrNoRows {
				log.Error("Error checking device_link: " + linkErr.Error())
				return "", linkErr
			}

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
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// 1. Vérification du montant (Paiement total déjà effectué ?)
	var totalPrice, alreadyPaid int
	err := db.QueryRowContext(ctx, `
		SELECT o.price, COALESCE(SUM(p.amount),0)
		FROM orders o
		LEFT JOIN payments p ON p.order_id = o.order_id AND p.enabled = TRUE
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

	// cash_register_id vide -> NULL : un paiement sans caisse (ex. borne Kiosk /
	// Stripe Terminal) ne rattache pas d'identifiant de caisse. Les appelants
	// existants passent toujours une valeur non vide (caisse réelle,
	// ScanNOrderCashRegisterID, ...), donc leur comportement est inchangé.
	cashRegisterID := sql.NullString{String: payment.CashRegisterID, Valid: payment.CashRegisterID != ""}

	// net_amount est initialisé à amount (valeur provisoire, avant réception des
	// frais réels Stripe) pour TOUS les paiements, sans toucher aucun appelant :
	// il est recalculé à amount - fee par le webhook charge.captured qui
	// renseigne déjà payments.fee (voir internal/webhook/stripe, UpdateFees).
	// 3. Insérer le paiement avec son hash
	paymentID, err := db.InsertReturningID(ctx, `
	INSERT INTO payments
	(merchant_id, cash_register_id, order_id, amount, net_amount, mop, comment, payment_date, user_id, status_check, previous_hash, hash, signature, operation_type)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, "payment_id", payment.MerchantID, cashRegisterID, payment.OrderID, payment.Amount, payment.Amount, payment.MOP, payment.Comment, now, payment.UserID, payment.StatusCheck, prevHash.String, newHash, signature, payment.OperationType)

	if err != nil {
		log.Error("Error inserting payment: " + err.Error())
		return 0, err
	}

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
	} else if payment.MOP == models.StripeMOP || (payment.PaymentIntentID != nil && *payment.PaymentIntentID != "") {
		// La ligne stripe_payments est aussi requise pour les encaissements
		// Stripe Terminal (borne Kiosk), enregistrés en MOP 'CB' : sans elle, le
		// webhook charge.captured ne peut pas retrouver le paiement pour écrire
		// fee/net_amount, ni le refund le désactiver.
		//
		// Un paiement Terminal a déjà pré-créé cette ligne (order_id,
		// payment_intent_id, payment_id=NULL) à la création du PaymentIntent —
		// voir stripeclient.TerminalPaymentStore.CreateMapping,
		// docs/KIOSK_DECISIONS.md, "Retrait de Redis du mapping
		// order_id/payment_intent_id". On complète cette même ligne par UPDATE
		// plutôt que d'en insérer une seconde : le Checkout web ne pré-crée
		// jamais de ligne pour son payment_intent_id, donc cet UPDATE affecte
		// toujours 0 lignes pour ce flux et retombe sur l'INSERT existant
		// (comportement strictement inchangé pour Checkout).
		mappingCompleted := false
		if payment.PaymentIntentID != nil && *payment.PaymentIntentID != "" {
			res, updErr := db.ExecContext(ctx,
				`UPDATE stripe_payments SET payment_id = ? WHERE payment_intent_id = ? AND payment_id IS NULL`,
				paymentID, *payment.PaymentIntentID)
			if updErr != nil {
				log.Error("Error completing stripe_payments mapping: " + updErr.Error())
				return 0, updErr
			}
			if n, _ := res.RowsAffected(); n > 0 {
				mappingCompleted = true
			}
		}

		if !mappingCompleted {
			// success_key est NOT NULL sans défaut (MySQL non-strict insérait '') :
			// '' explicite pour la parité Postgres — sans quoi cette insertion
			// échouerait silencieusement (l'erreur était auparavant écrasée par le
			// refresh isPaid plus bas, corrigé ici : on retourne l'erreur
			// immédiatement) et le webhook charge.captured ne retrouverait jamais
			// le paiement.
			query := `INSERT INTO stripe_payments(order_id, payment_id, payment_intent_id, checkout_session_id, customer_email, success_key, stripe_session_date)
					VALUES(?, ?, ?, ?, ?, '', ` + dbx.UTCNow() + `)`
			if _, insErr := db.ExecContext(ctx, query, payment.OrderID, paymentID, payment.PaymentIntentID, payment.CheckoutSessionID, payment.CustomerEmail); insErr != nil {
				log.Error("Error inserting stripe_payments: " + insErr.Error())
				return 0, insErr
			}
		}
	}

	// 5. Mettre à jour orders.isPaid
	// UPDATE multi-table MySQL -> UPDATE ... FROM (cible SET non qualifiée)
	refreshPaid := `
		UPDATE orders o
		INNER JOIN (
			SELECT order_id, SUM(amount) AS paid
			FROM payments
			WHERE enabled = TRUE AND order_id = ?
			GROUP BY order_id
		) p ON p.order_id = o.order_id
		SET o.isPaid = (o.price <= p.paid)
		WHERE o.order_id = ?
	`
	if dbx.ActiveDialect() == dbx.Postgres {
		refreshPaid = `
		UPDATE orders
		SET isPaid = (orders.price <= p.paid)
		FROM (
			SELECT order_id, SUM(amount) AS paid
			FROM payments
			WHERE enabled = TRUE AND order_id = ?
			GROUP BY order_id
		) p
		WHERE p.order_id = orders.order_id AND orders.order_id = ?
	`
	}
	_, err = db.ExecContext(ctx, refreshPaid, payment.OrderID, payment.OrderID)

	return paymentID, err
}

// AddPayment inserts a payment and discards the generated ID (for backward compatibility)
func (r *OrdersLifeCycleRepository) AddPayment(ctx context.Context, payment models.Payment) error {
	_, err := r.AddPaymentAndReturnID(ctx, payment)
	return err
}

func (r *OrdersLifeCycleRepository) GetPaymentsForOrder(ctx context.Context, orderID string) ([]models.Payment, error) {
	db := dbx.GetDB(ctx, r.database)

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
	db := dbx.GetDB(ctx, r.database)
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
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// TODO
	// Vérifier qu'il ne s'agit pas d'un paiement Uber Eats ou Deliveroo qui ne sont pas anulables
	// Le client s'en occupe déjà, mais une double vérification côté serveur est nécessaire

	// Disable payment
	_, err := db.ExecContext(ctx, `
		UPDATE payments SET enabled = FALSE WHERE payment_id = ?
	`, paymentID)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	// Refresh order as unpaid (UPDATE multi-table MySQL -> UPDATE ... FROM)
	unpaidQuery := `
		UPDATE orders o 
		JOIN payments p ON o.order_id = p.order_id
		SET o.isPaid = false, o.last_update = UTC_TIMESTAMP()
		WHERE p.payment_id = ?
	`
	if dbx.ActiveDialect() == dbx.Postgres {
		unpaidQuery = `
		UPDATE orders
		SET isPaid = false, last_update = now()
		FROM payments p
		WHERE orders.order_id = p.order_id AND p.payment_id = ?
	`
	}
	_, err = db.ExecContext(ctx, unpaidQuery, paymentID)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	return nil
}

func (r *OrdersLifeCycleRepository) SetDistributedProducts(ctx context.Context, userID string, merchantID string, req *models.SetDistributedProductsRequest) error {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// 1. Préparation des outils de compatibilité
	now := time.Now().UTC()
	orderID := req.OrderID

	// 2. Mise à jour des items — le rebind des placeholders est géré par dbx
	// (l'ancien hack formatQuery/isPostgres est retiré). isDistributed est
	// boolean en cible : littéraux TRUE/FALSE et paramètres bool Go.
	for _, p := range req.Products {
		_, _ = db.ExecContext(ctx, `
			UPDATE orderitems
			SET isDistributed = TRUE,
			    distributed_quantity = quantity,
			    ready_for_distribution_quantity = quantity,
			    distributed_on = ?
			WHERE order_id = ? AND order_item_id = ?`, now, orderID, p.OrderItemID)
	}

	// 3. Calcul de l'état global
	var countNotDistributed int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM orderitems WHERE order_id = ? AND isDistributed = FALSE`, orderID).Scan(&countNotDistributed)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	orderFullyDistributed := countNotDistributed == 0

	// 4. UPDATE ORDER : Une seule fois après la boucle
	_, err = db.ExecContext(ctx, `
		UPDATE orders
		SET isDistributed = ?,
		    delivered_on = CASE
		        WHEN ? = FALSE OR order_type = 'DELIVERY' THEN delivered_on
		        ELSE ?
		    END,
		    brand_status = CASE
		        WHEN order_type = 'DELIVERY' AND ? = TRUE THEN 'READY_FOR_HANDOFF'
		        WHEN order_type = 'TAKE_AWAY' AND ? = TRUE THEN 'READY_FOR_TAKE_AWAY'
		        WHEN ? = FALSE THEN 'PENDING'
		        ELSE 'DONE'
		    END,
		    last_update = ?
		WHERE order_id = ? AND merchant_id = ?`,
		orderFullyDistributed,
		orderFullyDistributed,
		now,
		orderFullyDistributed,
		orderFullyDistributed,
		orderFullyDistributed,
		now,
		orderID,
		merchantID,
	)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	// 5. Récupération de la marque pour notification
	var brand sql.NullString
	err = db.QueryRowContext(ctx, `SELECT brand FROM orders WHERE order_id = ?`, orderID).Scan(&brand)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	return nil
}

func (r *OrdersLifeCycleRepository) MarkProductsBackToProduction(ctx context.Context, userID, merchantID, orderID string, products []models.DistributedProduct) error {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	for _, p := range products {

		_, err := db.ExecContext(ctx, `
            UPDATE orderitems
            SET
                isDistributed = FALSE,

                distributed_quantity = CASE
                    WHEN isDistributed = TRUE AND ready_for_distribution_quantity = 0 THEN quantity
                    WHEN isDistributed = TRUE AND ready_for_distribution_quantity > 0 THEN ready_for_distribution_quantity
                    ELSE 0
                END,

                ready_for_distribution_quantity = CASE
                    WHEN isDistributed = FALSE THEN 0
                    WHEN ready_for_distribution_quantity = 0 THEN quantity
                    ELSE ready_for_distribution_quantity
                END,

                distributed_on = `+dbx.UTCNow()+`

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
        WHERE order_id = ? AND isDistributed = FALSE
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
                WHEN ? = FALSE OR order_type = 'DELIVERY' THEN delivered_on
                ELSE `+dbx.UTCNow()+`
            END,

            brand_status = CASE
                WHEN order_type = 'DELIVERY' AND ? = TRUE THEN 'READY_FOR_HANDOFF'
                WHEN order_type = 'TAKE_AWAY' AND ? = TRUE THEN 'READY_FOR_TAKE_AWAY'
                WHEN ? = FALSE THEN 'PENDING'
                ELSE 'CLOSED'
            END,

            last_update = `+dbx.UTCNow()+`

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
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	const q = `
		SELECT o.brand, o.merchant_id, o.brand_order_id, o.creation_date
		FROM orders o
		WHERE o.order_id = ?
		LIMIT 1;
	`
	row := db.QueryRowContext(ctx, q, orderID)
	var m models.OrderMeta
	var merchantID sql.NullString
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
		m.MerchantID = merchantID.String
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
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	q := `
		UPDATE orders
		SET last_update = ` + dbx.UTCNow() + `,
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

// olcResponsible reproduit la coercition MySQL non-strict d'un user_id vers la
// colonne integer orders.responsible : non numérique -> 0 (les user_id de prod
// sont numériques ; Postgres rejetterait une chaîne arbitraire).
func olcResponsible(userID *string) interface{} {
	if userID == nil {
		return nil
	}
	if _, err := strconv.Atoi(strings.TrimSpace(*userID)); err != nil {
		return 0
	}
	return *userID
}

func (r *OrdersLifeCycleRepository) MarkOrderAsDeliveryStarted(ctx context.Context, orderID string, userID string) (*OrderIntegrationInfo, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// Update order
	_, err := db.ExecContext(ctx, `
		UPDATE orders
		SET last_update = `+dbx.UTCNow()+`,
			brand_status = 'EN_ROUTE_TO_DROPOFF',
			delivery_start = `+dbx.UTCNow()+`,
			responsible = ?
		WHERE order_id = ?
	`, olcResponsible(&userID), orderID)
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

func (r *OrdersLifeCycleRepository) DenyOrderLocal(ctx context.Context, orderID, deletionReasonID, comment, userID string) error {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	_, err := db.ExecContext(ctx, `
        UPDATE orders
        SET last_update = `+dbx.UTCNow()+`,
            brand_status = 'DENIED',
            merchant_approval = 'DENIED',
            state = 'CLOSED',
            deletion_reason_id = ?,
            deletion_comment = ?,
            cancelled_by_type = ?
        WHERE order_id = ?`,
		deletionReasonID, comment, classifyCancelledByType(userID), orderID,
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
	db := dbx.GetDB(ctx, r.database)

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
	db := dbx.GetDB(ctx, r.database)
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
            last_update = `+dbx.UTCNow()+`
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
	db := dbx.GetDB(ctx, r.database)
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

func (r *OrdersLifeCycleRepository) DeleteOrderLocal(ctx context.Context, orderID string, reasonID string, comment string, userID string) error {
	db := dbx.GetDB(ctx, r.database)

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
            last_update = `+dbx.UTCNow()+`,
            state = 'CLOSED',
            brand_status = 'CANCELED',
            delivered_on = `+dbx.UTCNow()+`,
			previous_hash = ?,
			hash = ?,
			signature = ?,
			cancelled_by_type = ?
        WHERE order_id = ?`,
		reasonID, comment, prevHash, newHash, signature, classifyCancelledByType(userID), orderID,
	)

	return err
}

// GetOrderPaymentBalance retourne le prix de la commande et le total encaisse
// (paiements actifs). Lecture seule, sans FOR UPDATE : c'est un pre-controle
// consultatif, pas la barriere fiscale.
//
// La barriere, elle, reste le meme controle execute sous verrou de ligne au
// debut de SetDeliveredLocal : cette fonction sert seulement a repondre "cette
// commande passera-t-elle ?" avant d'entamer une cloture multi-commandes, pour
// eviter de fermer la moitie d'une tournee puis d'echouer.
func (r *OrdersLifeCycleRepository) GetOrderPaymentBalance(ctx context.Context, orderID string) (price int, paidAmount int, err error) {
	db := dbx.GetDB(ctx, r.database)

	if err = db.QueryRowContext(ctx, `
		SELECT price FROM orders WHERE order_id = ?
	`, orderID).Scan(&price); err != nil {
		return 0, 0, err
	}

	if err = db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(amount), 0)
		FROM payments
		WHERE order_id = ?
		  AND enabled = TRUE
	`, orderID).Scan(&paidAmount); err != nil {
		return 0, 0, err
	}

	return price, paidAmount, nil
}

func (r *OrdersLifeCycleRepository) SetDeliveredLocal(ctx context.Context, orderID string) (*DeliveredOrderMetadata, error) {
	db := dbx.GetDB(ctx, r.database)

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
  AND enabled = TRUE
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
        isPaid = TRUE,
        isDistributed = TRUE,
        delivered_on = ?,
        previous_hash = ?,
        hash = ?,
		signature = ?
    WHERE order_id = ?
    `
	if _, err := db.ExecContext(ctx, qUpd, now, now, prevHash.String, newHash, signature, orderID); err != nil {
		return nil, err
	}

	// 3) Delete qrcodes (DELETE multi-table MySQL -> DELETE ... USING)
	qDelQR := `
	DELETE qr
	FROM qrcodes qr
	INNER JOIN order_location ol ON qr.location_id = ol.location_id
	INNER JOIN orders o ON o.order_id = ol.order_id AND o.merchant_id = qr.merchant_id
	WHERE o.order_id = ?
	`
	if dbx.ActiveDialect() == dbx.Postgres {
		qDelQR = `
	DELETE FROM qrcodes qr
	USING order_location ol, orders o
	WHERE qr.location_id = ol.location_id
	  AND o.order_id = ol.order_id AND o.merchant_id = qr.merchant_id
	  AND o.order_id = ?
	`
	}
	if _, err := db.ExecContext(ctx, qDelQR, orderID); err != nil {
		return nil, err
	}

	// Historique : cette etape ecrivait autrefois `UPDATE bookings SET
	// status = '0' WHERE order_id = ?` directement ici. bookings.order_id
	// n'etant renseigne par aucun chemin de code Go, cette ligne n'a jamais
	// matche la moindre ligne en production. Le pont seated -> completed
	// est desormais gere par OrdersLifeCycleService.DeliverOrder via
	// bookingsSvc.AutoCompleteForOrder (transition via bookingcore,
	// booking_events, notification POS).

	// 5) Close delivery_session if last order
	qCloseDS := `
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
	if dbx.ActiveDialect() == dbx.Postgres {
		qCloseDS = `
		UPDATE delivery_session ds
		SET status = 'done'
		FROM delivery_session_order dso
		WHERE dso.delivery_session_id = ds.id
		  AND dso.order_id = ?
		  AND NOT EXISTS (
			  SELECT 1
			  FROM delivery_session_order dso_other
			  JOIN orders o_other ON o_other.order_id = dso_other.order_id
			  WHERE dso_other.delivery_session_id = ds.id
			    AND dso_other.order_id <> ?
			    AND o_other.state = 'OPEN'
		  )
	`
	}
	if _, err := db.ExecContext(ctx, qCloseDS, orderID, orderID); err != nil {
		return nil, err
	}

	// 6) Update orderitems (distributed)
	// UPDATE ... LEFT JOIN MySQL (le delay peut manquer) : côté PG le délai
	// est résolu par sous-requête corrélée, même résultat ; isDistributed est
	// boolean en cible (CASE TRUE/FALSE).
	qUpdItems := `
	UPDATE orderitems oi
	LEFT JOIN delays d ON oi.delay_id = d.id
	SET 
		isDistributed = CASE WHEN ready_for_distribution_quantity >= quantity OR ready_for_distribution_quantity = 0 THEN TRUE ELSE FALSE END,
		distributed_quantity = CASE WHEN ready_for_distribution_quantity = 0 THEN quantity ELSE ready_for_distribution_quantity END,
		ready_for_distribution_quantity = CASE WHEN ready_for_distribution_quantity = 0 THEN quantity ELSE ready_for_distribution_quantity END,
		distributed_on = UTC_TIMESTAMP()
	WHERE order_id = ?
	  AND oi.distributed_on IS NULL
	  AND TIMESTAMPADD(SECOND, IFNULL(d.duration,0), oi.ordered_on) <= UTC_TIMESTAMP()
	`
	if dbx.ActiveDialect() == dbx.Postgres {
		qUpdItems = `
	UPDATE orderitems
	SET
		isDistributed = CASE WHEN ready_for_distribution_quantity >= quantity OR ready_for_distribution_quantity = 0 THEN TRUE ELSE FALSE END,
		distributed_quantity = CASE WHEN ready_for_distribution_quantity = 0 THEN quantity ELSE ready_for_distribution_quantity END,
		ready_for_distribution_quantity = CASE WHEN ready_for_distribution_quantity = 0 THEN quantity ELSE ready_for_distribution_quantity END,
		distributed_on = now()
	WHERE order_id = ?
	  AND distributed_on IS NULL
	  AND ordered_on + COALESCE((SELECT d.duration FROM delays d WHERE d.id = orderitems.delay_id), 0) * INTERVAL '1 second' <= now()
	`
	}
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
	db := dbx.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `
        UPDATE payments
        SET enabled = FALSE
        WHERE order_id = ?`,
		orderID,
	)
	return err
}

// Delete QR codes
func (r *OrdersLifeCycleRepository) DeleteQRCode(ctx context.Context, orderID string) error {
	db := dbx.GetDB(ctx, r.database)

	delQuery := `
        DELETE qr
        FROM qrcodes qr
        INNER JOIN order_location ol ON qr.location_id = ol.location_id
        INNER JOIN orders o ON o.order_id = ol.order_id AND o.merchant_id = qr.merchant_id
        WHERE o.order_id = ?`
	if dbx.ActiveDialect() == dbx.Postgres {
		delQuery = `
        DELETE FROM qrcodes qr
        USING order_location ol, orders o
        WHERE qr.location_id = ol.location_id
          AND o.order_id = ol.order_id AND o.merchant_id = qr.merchant_id
          AND o.order_id = ?`
	}
	_, err := db.ExecContext(ctx, delQuery, orderID)
	return err
}

// Clear bookings
func (r *OrdersLifeCycleRepository) ClearBookings(ctx context.Context, orderID string) error {
	db := dbx.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `
        UPDATE bookings
        SET order_id = NULL
        WHERE order_id = ?`,
		orderID,
	)
	return err
}

func (r *OrdersLifeCycleRepository) UpdateProductionStatus(ctx context.Context, merchantID string, req *UpdateProductionStatusRequest) ([]string, error) {
	db := dbx.GetDB(ctx, r.database)
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
			        WHEN ? = 'DONE' THEN TRUE
			        ELSE FALSE
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
			// Valeur auto-calculée (temps cuisine seul) : ne peut pas représenter
			// un choix de créneau client/staff, même si le payload demandait
			// is_scheduled=true — sinon resolveIsScheduled (plus bas) ne verrait
			// plus une valeur vide et laisserait scheduled=true persister avec une
			// date qui n'est pas une heure de livraison.
			req.Order.IsScheduled = false
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
		activeRegister, err := r.GetActiveCashRegisterID(ctx, req.MerchantID, *req.DeviceID)
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

	usedItems, optionCosts, err := r.insertOrderItems(ctx, req)
	if err != nil {
		log.Error("insertOrderItems failure" + err.Error())
		return nil, err
	}

	// Supposons que tu aies ton objet req.Order disponible
	err = r.InsertOrderLocations(ctx, req.Order)
	if err != nil {
		return nil, err // Gérer l'erreur proprement
	}

	if err := r.insertExtrasWithoutsConfigs(ctx, req, usedItems, optionCosts); err != nil {
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

	db := dbx.GetDB(ctx, r.database)

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

	// A6b : acquisition_source n'est capté qu'à la création — seulement
	// lorsque CustomerID est absent, ce qui fait passer UpdateOrCreateCustomer
	// dans sa branche INSERT (elle exclut de toute façon cette colonne de sa
	// branche UPDATE, en ceinture et bretelles).
	if cust.CustomerID == nil {
		cust.AcquisitionSource = resolveOrderSource(req.Order.Brand, req.Order.CreatedBy)
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
	db := dbx.GetDB(ctx, r.database)

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
                  AND c.status IN ('0','out_of_stock','not_available')
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
	db := dbx.GetDB(ctx, r.database)

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
	// order_item_id est un integer identity : Postgres refuse une valeur
	// explicite sans OVERRIDING SYSTEM VALUE, et un id NULL n'est upsertable
	// dans aucun dialecte — les deux chemins (item existant / nouveau) sont
	// donc séparés côté PG, à comportement identique.
	upsertItemQuery := `
		INSERT INTO orderitems (order_item_id, order_id, product_id, merchant_id, quantity, discount_id, base_price, price, delay_id, is_upsell, cost_price_unit, cost_price_reason, ordered_on)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, UTC_TIMESTAMP())
		ON DUPLICATE KEY UPDATE
			-- Remet isDistributed et le statut de production à zéro seulement si la
			-- quantité distribuée ne correspond plus à la nouvelle quantité commandée
			-- (ex: un serveur recommande un plat déjà partiellement servi) : la ligne
			-- redevient à produire pour la quantité restante.
			isDistributed = CASE WHEN distributed_quantity = VALUES(quantity) THEN isDistributed ELSE 0 END,
			production_status = CASE WHEN distributed_quantity = VALUES(quantity) THEN production_status ELSE 'CREATION' END,
			production_status_done_quantity = CASE WHEN distributed_quantity = VALUES(quantity) THEN production_status_done_quantity ELSE 0 END,
			quantity        = VALUES(quantity),
			base_price      = VALUES(base_price),
			price           = VALUES(price),
			discount_id     = VALUES(discount_id),
			delay_id        = VALUES(delay_id),
			is_upsell       = VALUES(is_upsell),
			cost_price_unit   = VALUES(cost_price_unit),
			cost_price_reason = VALUES(cost_price_reason),
			ordered_on      = VALUES(ordered_on)`
	insertItemQuery := `
		INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, discount_id, base_price, price, delay_id, is_upsell, cost_price_unit, cost_price_reason, ordered_on)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ` + dbx.UTCNow() + `)`
	if dbx.ActiveDialect() == dbx.Postgres {
		upsertItemQuery = `
		INSERT INTO orderitems (order_item_id, order_id, product_id, merchant_id, quantity, discount_id, base_price, price, delay_id, is_upsell, cost_price_unit, cost_price_reason, ordered_on)
		OVERRIDING SYSTEM VALUE
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, now())
		ON CONFLICT (order_item_id, order_id, product_id) DO UPDATE SET
			isDistributed = CASE WHEN orderitems.distributed_quantity = EXCLUDED.quantity THEN orderitems.isDistributed ELSE FALSE END,
			production_status = CASE WHEN orderitems.distributed_quantity = EXCLUDED.quantity THEN orderitems.production_status ELSE 'CREATION' END,
			production_status_done_quantity = CASE WHEN orderitems.distributed_quantity = EXCLUDED.quantity THEN orderitems.production_status_done_quantity ELSE 0 END,
			quantity        = EXCLUDED.quantity,
			base_price      = EXCLUDED.base_price,
			price           = EXCLUDED.price,
			discount_id     = EXCLUDED.discount_id,
			delay_id        = EXCLUDED.delay_id,
			is_upsell       = EXCLUDED.is_upsell,
			cost_price_unit   = EXCLUDED.cost_price_unit,
			cost_price_reason = EXCLUDED.cost_price_reason,
			ordered_on      = EXCLUDED.ordered_on`
	}

	for i := range req.Order.Products {
		p := &req.Order.Products[i]

		// ── A. Upsert de l'item principal ─────────────────────────────────────────
		// Calcular le prix final : si discounted_price est fourni, l'utiliser, sinon utiliser price
		finalPrice := p.Price
		if p.DiscountedPrice != nil {
			finalPrice = *p.DiscountedPrice
		}

		// Recalculé à chaque écriture de la ligne (insert ou upsert), comme
		// price/base_price : la commande reste ouverte tant qu'elle n'est pas
		// payée/fermée, donc une modification de quantité/produit ici est
		// encore "la vente" au sens du lot B2, pas une réécriture a posteriori.
		costPriceUnit, costPriceReason := r.resolveOrderItemCost(ctx, req.MerchantID, p.ProductID, selectedOptionsOf(p.Config))

		if p.OrderItemID == nil {
			// Nouveau produit : insertion simple, ID auto-généré récupéré
			// (RETURNING côté PG, LastInsertId côté MySQL).
			newID, err := db.InsertReturningID(ctx, insertItemQuery, "order_item_id",
				req.Order.OrderID, p.ProductID, req.MerchantID,
				p.Quantity, p.DiscountID, p.Price, finalPrice, p.DelayID, p.IsUpsell, costPriceUnit, costPriceReason)
			if err != nil {
				return fmt.Errorf("product insert failed (product_id=%s): %w", p.ProductID, err)
			}
			if newID == 0 {
				return fmt.Errorf("unexpected: generated id is 0 for product_id=%s", p.ProductID)
			}
			p.OrderItemID = helpers.Int64ToStringPtr(newID)
		} else {
			if _, err := db.ExecContext(ctx, upsertItemQuery,
				p.OrderItemID, req.Order.OrderID, p.ProductID, req.MerchantID,
				p.Quantity, p.DiscountID, p.Price, finalPrice, p.DelayID, p.IsUpsell, costPriceUnit, costPriceReason); err != nil {
				return fmt.Errorf("product upsert failed (product_id=%s): %w", p.ProductID, err)
			}
		}

		r.upsertOrderItemDiscountRedemption(ctx, *req.Order.OrderID, *p.OrderItemID, req.MerchantID, p.DiscountID, p.Price, finalPrice)

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

		// Extras (suppléments payants) — coûts résolus par ligne (pas de batch
		// pleine commande ici : UpdateOrder édite typiquement 1-2 lignes à la
		// fois, même asymmetrie que resolveOrderItemCost ci-dessus). Quantité
		// toujours 1 : voir insertExtrasWithoutsConfigs pour pourquoi.
		if len(p.Extra) > 0 {
			extraComponentIDs := make([]string, 0, len(p.Extra))
			for _, e := range p.Extra {
				extraComponentIDs = append(extraComponentIDs, e.ComponentID)
			}
			extraCosts := r.resolveExtraCostsBatch(ctx, req.MerchantID, extraComponentIDs)
			for _, e := range p.Extra {
				costPriceUnit, costPriceReason := freezeExtraCost(extraCosts, e.ComponentID, 1)
				extrasArgs = append(extrasArgs, p.OrderItemID, req.Order.OrderID, e.ComponentID, p.ProductID, req.MerchantID, e.Price, costPriceUnit, costPriceReason)
			}
		}

		// Withouts (exclusions d'ingrédients)
		for _, w := range p.Without {
			withoutsArgs = append(withoutsArgs, p.OrderItemID, req.Order.OrderID, w.ComponentID, p.ProductID, req.MerchantID)
		}

		// Configurations (options de personnalisation) — coûts résolus par
		// ligne, même raison que pour les extras ci-dessus.
		if p.Config != nil {
			var configOptionIDs []string
			for _, attr := range p.Config.Attributes {
				for _, opt := range attr.Options {
					configOptionIDs = append(configOptionIDs, opt.ID)
				}
			}
			lineOptionCosts := r.resolveOptionCostsBatch(ctx, req.MerchantID, configOptionIDs)
			for _, attr := range p.Config.Attributes {
				for _, opt := range attr.Options {
					costPriceUnit, costPriceReason := freezeOptionCost(lineOptionCosts, opt.ID, opt.Quantity)
					configsArgs = append(configsArgs, p.OrderItemID, attr.ID, opt.ID, opt.Quantity, costPriceUnit, costPriceReason)
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
			"INSERT INTO extra (order_item_id, order_id, component_id, product_id, merchant_id, price, cost_price_unit, cost_price_reason) VALUES",
			8, extrasArgs); err != nil {
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
			"INSERT INTO order_item_configuration (order_item_id, configuration_attribute_id, configuration_attribute_option_id, quantity, cost_price_unit, cost_price_reason) VALUES",
			6, configsArgs); err != nil {
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
			// Voir le même garde-fou dans CreateOrder : une valeur auto-calculée
			// ne peut pas représenter un choix de créneau client/staff.
			req.Order.IsScheduled = false
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
	db := dbx.GetDB(ctx, r.database)

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
	db := dbx.GetDB(ctx, r.database)

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
	db := dbx.GetDB(ctx, r.database)

	var customer_id *string
	if req.Order.Customer != nil {
		customer_id = req.Order.Customer.CustomerID
	}
	PublicID := helpers.GeneratePrefixedID("order-")
	estimatedReady := normalizeEstimatedReady(req.Order.EstimatedReady)
	isScheduled := resolveIsScheduled(req.Order.IsScheduled, estimatedReady)
	deliveryTravelSeconds := r.resolveDeliveryTravelSeconds(ctx, req)
	productionReadyAt := resolveProductionReadyAt(estimatedReady, isScheduled, deliveryTravelSeconds)
	deliveryArrivalAt := resolveDeliveryArrivalAt(req.Order.OrderType, estimatedReady, isScheduled, deliveryTravelSeconds)
	orderSource := resolveOrderSource(req.Order.Brand, req.Order.CreatedBy)
	// default fields and estimated_ready handling simplified: use UTC_TIMESTAMP equivalent in SQL
	lastID, err := db.InsertReturningID(ctx, `
		INSERT INTO orders(public_id, brand, brand_order_id, brand_order_num, cash_register_id, merchant_id, customer_id, order_num, price, TVA, HT, merchant_approval, scheduled, creation_date,
		                   last_update, responsible, created_by, delivery_fees, estimated_ready, delivery_travel_seconds, production_ready_at, delivery_arrival_at, use_customer_temporary_address,
		                   brand_status, order_type, places_settings, pager_number, fulfillment_type, isPaid, brand_store_id, order_source)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, `+dbx.UTCNow()+`, `+dbx.UTCNow()+`, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"order_id",
		PublicID, req.Order.Brand, req.Order.BrandOrderID, req.Order.BrandOrderNum, req.Order.CashRegisterId, req.MerchantID, customer_id, req.Order.OrderNum, req.Order.TTC, req.Order.TVA, req.Order.HT,
		req.Order.MerchantApproval, isScheduled,
		olcResponsible(req.Order.Responsible), req.Order.CreatedBy, req.Order.DeliveryFees, estimatedReady, deliveryTravelSeconds, productionReadyAt, deliveryArrivalAt,
		req.Order.UseCustomerTemporaryAddress, req.Order.BrandStatus, req.Order.OrderType, req.Order.PlacesSettings, req.Order.PagerNumber, req.Order.FulfillmentType, req.Order.IsPaid,
		req.Order.BrandStoreID, orderSource,
	)
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
	db := dbx.GetDB(ctx, r.database)
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
	// Comme avec l'ancien CALL, toute erreur SQL est avalée : estimated_ready
	// reste simplement vide.
	seconds, found, err := distributiontime.EstimatedSeconds(ctx, r.database, merchantID, productsCount)
	if err != nil || !found || seconds <= 0 {
		return "", nil
	}

	t := time.Now().UTC().Add(time.Duration(seconds) * time.Second)
	return t.Format(time.RFC3339), nil
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

// resolveIsScheduled empêche l'état incohérent scheduled=true sans estimated_ready
// exploitable : sans date, l'app (OrderDto.scheduleDateTime) ne peut afficher la
// commande ni dans la liste principale ni dans la bannière programmée, elle
// devient invisible en production. On force donc scheduled=false dans ce cas.
func resolveIsScheduled(isScheduled bool, estimatedReady interface{}) bool {
	if isScheduled && estimatedReady == nil {
		return false
	}
	return isScheduled
}

// resolveDeliveryTravelSeconds returns the travel-time estimate to persist
// alongside estimated_ready. The POS (Google Maps) and ScanNOrder (OSRM)
// clients already compute this live when a delivery address is entered and
// send it in the payload — this function is only a fallback: when a delivery
// order has an address but the client didn't provide a value (older app
// version, failed live call, address added via a flow that doesn't recompute
// it), it falls back to the merchant's rolling average rather than silently
// storing nil, which would make the production deadline default back to the
// raw (wrong) estimated_ready. Non-delivery orders and delivery orders
// without an address get nil (no travel leg to subtract).
func (r *OrdersLifeCycleRepository) resolveDeliveryTravelSeconds(ctx context.Context, req *models.RequestObject) *int {
	// Le type de commande gagne toujours : un client qui n'a pas vidé son état
	// local (adresse saisie puis commande repassée en emporter/sur place, par
	// exemple) ne doit jamais faire persister un travel time pour un ordre non
	// livré.
	if req.Order.OrderType != models.OrderTypeDelivery {
		return nil
	}
	if req.Order.DeliveryTravelSeconds != nil {
		return req.Order.DeliveryTravelSeconds
	}
	if req.Order.Customer == nil || req.Order.Customer.Address == nil || *req.Order.Customer.Address == "" {
		return nil
	}
	seconds, found, err := deliverytime.AverageSeconds(ctx, r.database, req.MerchantID)
	if err != nil || !found {
		return nil
	}
	return &seconds
}

// resolveProductionReadyAt calcule la deadline cuisine, renseignée pour
// toute commande (livraison ou non) : estimated_ready est la date de
// livraison promise (heure choisie par le client/staff, livreur à la porte)
// pour une commande programmée, donc la deadline cuisine réelle est plus tôt
// de la durée du trajet ; pour une commande non programmée, estimated_ready
// EST déjà la deadline cuisine (ComputeEstimatedReady ne calcule que le temps
// de prépa, sans trajet) — aucun ajustement à faire. Repli sur estimated_ready
// tel quel si programmée mais trajet pas encore connu (commande non-livrée,
// ou adresse sans moyenne merchant disponible) : c'est la meilleure valeur
// disponible, jamais nil tant qu'une date existe.
func resolveProductionReadyAt(estimatedReady interface{}, scheduled bool, travelSeconds *int) interface{} {
	t, ok := estimatedReady.(time.Time)
	if !ok {
		return nil
	}
	if scheduled && travelSeconds != nil {
		return t.Add(-time.Duration(*travelSeconds) * time.Second)
	}
	return t
}

// resolveDeliveryArrivalAt calcule l'heure d'arrivée livreur estimée,
// uniquement pour les commandes livraison (nil sinon — un NULL qui signifie
// "pas de livraison", pas une donnée manquante). Pour une commande
// programmée, estimated_ready EST par définition cette heure (le client/staff
// l'a choisie comme heure de livraison). Pour une commande non programmée,
// c'est estimated_ready (temps cuisine auto-calculé) + le trajet ; nil si le
// trajet n'est pas encore connu (rien de fiable à afficher).
func resolveDeliveryArrivalAt(orderType string, estimatedReady interface{}, scheduled bool, travelSeconds *int) interface{} {
	if orderType != models.OrderTypeDelivery {
		return nil
	}
	t, ok := estimatedReady.(time.Time)
	if !ok {
		return nil
	}
	if scheduled {
		return t
	}
	if travelSeconds == nil {
		return nil
	}
	return t.Add(time.Duration(*travelSeconds) * time.Second)
}

// IsCashRegisterRequiredForOrdering checks merchant parameter cash_register_required_for_ordering == 1
func (r *OrdersLifeCycleRepository) IsCashRegisterRequiredForOrdering(ctx context.Context, merchantID string) (bool, error) {
	db := dbx.GetDB(ctx, r.database)
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
	db := dbx.GetDB(ctx, r.database)

	if item.Comment == nil {
		return nil
	}

	// order_comments n'a qu'une PK auto-incrémentée : l'ancien ON DUPLICATE ne
	// se déclenchait jamais (les appelants suppriment avant de réinsérer) —
	// INSERT simple, comportement identique.
	_, err := db.ExecContext(ctx, `
		INSERT INTO order_comments(order_id, order_item_id, user_id, content, creation_date)
		VALUES (?,?, ?,?,`+dbx.UTCNow()+`)`,
		item.OrderID, item.OrderItemID, item.CreatedBy, item.Comment,
	)
	return err
}

// insertOrderComment inserts the orders comments
func (r *OrdersLifeCycleRepository) insertOrderComment(ctx context.Context, req *models.RequestObject) (err error) {
	db := dbx.GetDB(ctx, r.database)

	resolvedComment := resolveOrderComment(&req.Order)
	if resolvedComment == nil {
		return nil
	}
	// default fields and estimated_ready handling simplified: use UTC_TIMESTAMP equivalent in SQL
	// Même clause ON DUPLICATE morte que insertOrderItemComment — INSERT simple.
	_, err = db.ExecContext(ctx, `
		INSERT INTO order_comments(order_id, user_id, content, creation_date)
		VALUES (?,?,?,`+dbx.UTCNow()+`)`,
		req.Order.OrderID, req.Order.CreatedBy, resolvedComment,
	)
	if err != nil {
		return err
	}
	return nil
}

func resolveOrderComment(order *models.OrderRequest) *string {
	if order.Comment == nil {
		return nil
	}

	content := strings.TrimSpace(*order.Comment)
	if content == "" {
		return nil
	}

	return &content
}

// updateOrderBase inserts the orders row and returns orderID and orderNum
func (r *OrdersLifeCycleRepository) updateOrderBase(ctx context.Context, req *models.RequestObject) (err error) {
	db := dbx.GetDB(ctx, r.database)

	var customerID *string
	if req.Order.Customer != nil {
		customerID = req.Order.Customer.CustomerID
	}

	estimatedReady := normalizeEstimatedReady(req.Order.EstimatedReady)
	isScheduled := resolveIsScheduled(req.Order.IsScheduled, estimatedReady)
	deliveryTravelSeconds := r.resolveDeliveryTravelSeconds(ctx, req)
	productionReadyAt := resolveProductionReadyAt(estimatedReady, isScheduled, deliveryTravelSeconds)
	deliveryArrivalAt := resolveDeliveryArrivalAt(req.Order.OrderType, estimatedReady, isScheduled, deliveryTravelSeconds)

	// default fields and estimated_ready handling simplified: use UTC_TIMESTAMP equivalent in SQL
	_, err = db.ExecContext(ctx, `
		UPDATE orders
			SET
			    price = ?,
			    tva = ?,
			    ht = ?,
				isDistributed = FALSE,
				isPaid = FALSE,
				last_update = `+dbx.UTCNow()+`,
				delivery_fees = ?,
				use_customer_temporary_address = ?,
				order_type = ?,
				scheduled = ?,
				estimated_ready = ?,
				delivery_travel_seconds = ?,
				production_ready_at = ?,
				delivery_arrival_at = ?,
				places_settings = ?,
				customer_id = ?
			WHERE order_id = ?`,
		req.Order.TTC,
		req.Order.TVA,
		req.Order.HT,
		req.Order.DeliveryFees,
		req.Order.UseCustomerTemporaryAddress,
		req.Order.OrderType,
		isScheduled,
		estimatedReady,
		deliveryTravelSeconds,
		productionReadyAt,
		deliveryArrivalAt,
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
	if resolveOrderComment(&req.Order) != nil {
		if err := r.insertOrderComment(ctx, req); err != nil {
			return fmt.Errorf("insert order comment failed: %w", err)
		}
	}

	return nil
}

// insertOrderItems inserts each orderitem and returns list of UsedItem (order_item_id + qty)
// insertOrderItems also returns the batched optionCosts map (PROMPT 11, §3):
// insertExtrasWithoutsConfigs reuses it to freeze order_item_configuration
// rows without re-querying configurable_attribute_options a second time.
func (r *OrdersLifeCycleRepository) insertOrderItems(ctx context.Context, req *models.RequestObject) ([]models.UsedItem, map[string]optionCostEntry, error) {
	// Coûts résolus pour toutes les lignes en un seul aller-retour batché
	// plutôt qu'un par ligne (~2 requêtes par ligne sinon) : voir
	// docs/decisions.md, impact mesuré sur staging.
	costResults, optionCosts := r.resolveOrderItemCostsForOrder(ctx, req.MerchantID, req.Order.Products)

	used := make([]models.UsedItem, 0, len(req.Order.Products))
	for i, p := range req.Order.Products {
		if p.Quantity == 0 {
			continue
		}

		// Calcul du prix final : si discounted_price est fourni, l'utiliser, sinon utiliser price
		finalPrice := p.Price
		if p.DiscountedPrice != nil {
			finalPrice = *p.DiscountedPrice
		}

		costPriceUnit, costPriceReason := costResults[i].costPriceUnit, costResults[i].costPriceReason

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
			IsUpsell:        p.IsUpsell,
			CostPriceUnit:   costPriceUnit,
			CostPriceReason: costPriceReason,
		}
		if p.Comment != nil && p.Comment.Content != "" {
			item.Comment = &p.Comment.Content
		}
		oid, err := r.InsertOrderItem(ctx, item)

		if err != nil {
			return nil, nil, err
		}

		used = append(used, models.UsedItem{OrderItemID: strconv.FormatInt(oid, 10), Quantity: p.Quantity})
	}
	return used, optionCosts, nil
}

// InsertOrderItem inserts a single orderitem and returns its id
func (r *OrdersLifeCycleRepository) InsertOrderItem(ctx context.Context, item *models.OrderItemInsert) (int64, error) {
	db := dbx.GetDB(ctx, r.database)

	lastID, err := db.InsertReturningID(ctx, `
		INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, discount_id, base_price, price, ordered_on, delay_id, is_upsell, cost_price_unit, cost_price_reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, `+dbx.UTCNow()+`, ?, ?, ?, ?)
		`, "order_item_id", item.OrderID, item.ProductID, item.MerchantID, item.Quantity, item.DiscountID, item.BasePrice, item.Price, item.DelayID, item.IsUpsell, item.CostPriceUnit, item.CostPriceReason)
	if err != nil {
		return 0, err
	}
	item.OrderItemID = helpers.Int64ToStringPtr(lastID)

	if err := r.insertOrderItemComment(ctx, item); err != nil {
		// Non bloquant : on logue mais on ne fait pas échouer la création de commande.
		log := logger.FromContext(ctx)
		log.Warn("insertOrderItemComment failed", zap.String("order_item_id", *item.OrderItemID), zap.Error(err))
	}

	r.upsertOrderItemDiscountRedemption(ctx, item.OrderID, *item.OrderItemID, item.MerchantID, item.DiscountID, item.BasePrice, item.Price)

	return lastID, nil
}

// upsertOrderItemDiscountRedemption tient discount_redemptions à jour pour UNE
// ligne de commande (PROMPT 21 Phase 3 — table de liaison commande×remise).
// Non bloquant : une erreur ici ne doit jamais faire échouer l'écriture de la
// ligne de commande elle-même, seulement être loguée.
//
// Efface la ligne de liaison (le cas échéant) si la remise a été retirée ou
// si le prix n'est finalement plus remisé (édition d'une commande ouverte) ;
// sinon upsert avec is_reconstructed=false — y compris pour "graduer" une
// ligne reconstituée rétroactivement (migration 119) si une commande fermée
// est rouverte puis réellement réécrite par ce chemin.
func (r *OrdersLifeCycleRepository) upsertOrderItemDiscountRedemption(ctx context.Context, orderID, orderItemID, merchantID string, discountID *string, basePrice, price int) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	if discountID == nil || basePrice == price {
		if _, err := db.ExecContext(ctx, `
			DELETE FROM discount_redemptions WHERE order_item_id = ? AND scope = 'PRODUCT_LINE'
		`, orderItemID); err != nil {
			log.Warn("discount_redemptions delete failed", zap.String("order_item_id", orderItemID), zap.Error(err))
		}
		return
	}

	discountIDNew, err := strconv.Atoi(*discountID)
	if err != nil {
		// Non convertible en entier : orderitems.discount_id est une colonne
		// integer — un "discount-<uuid>" (Sprint 2) n'y a structurellement
		// jamais pu être écrit (PROMPT 21 Phase 1), rien à faire ici.
		return
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO discount_redemptions (scope, discount_id, order_id, order_item_id, merchant_id, amount_applied_cents, is_reconstructed)
		VALUES ('PRODUCT_LINE', ?, ?, ?, ?, ?, false)
		ON CONFLICT (order_item_id) WHERE scope = 'PRODUCT_LINE' DO UPDATE SET
			discount_id = EXCLUDED.discount_id,
			amount_applied_cents = EXCLUDED.amount_applied_cents,
			is_reconstructed = false
	`, discountIDNew, orderID, orderItemID, merchantID, basePrice-price); err != nil {
		log.Warn("discount_redemptions upsert failed", zap.String("order_item_id", orderItemID), zap.Error(err))
	}
}

// insertExtrasWithoutsConfigs does bulk inserts for extras, withouts, configurations
//
// optionCosts is the batch already resolved by insertOrderItems (PROMPT 11,
// §3) — reused here to freeze order_item_configuration.cost_price_unit
// without a second configurable_attribute_options query for the same order.
// extras have no such existing batch (extras never rolled into
// orderitems.cost_price_unit — only options do, per lot 1), so their
// components are resolved once here via resolveExtraCostsBatch.
func (r *OrdersLifeCycleRepository) insertExtrasWithoutsConfigs(ctx context.Context, req *models.RequestObject, items []models.UsedItem, optionCosts map[string]optionCostEntry) error {
	// Build maps from product iteration to order_item ids; we used ordering to match the order of products to items
	// Simpler approach: while inserting items we could have returned corresponding mapping; for now assume order preserved.
	extras := []models.ExtraInsert{}
	withouts := []models.WithoutInsert{}
	configs := []models.ConfigInsert{}

	var allExtraComponentIDs []string
	for _, p := range req.Order.Products {
		for _, e := range p.Extra {
			allExtraComponentIDs = append(allExtraComponentIDs, e.ComponentID)
		}
	}
	extraCosts := r.resolveExtraCostsBatch(ctx, req.MerchantID, allExtraComponentIDs)

	itemIdx := 0
	for _, p := range req.Order.Products {
		if p.Quantity == 0 {
			continue
		}
		if itemIdx >= len(items) {
			return fmt.Errorf("internal mapping error: items length mismatch")
		}
		oid := items[itemIdx].OrderItemID
		// extras — quantity is always 1: extra.quantity has no column in the
		// incoming payload (models.OrderExtraPayload carries only
		// ComponentID/Price) and this write path never sets it explicitly,
		// relying on the DB default (see migration 116).
		for _, e := range p.Extra {
			costPriceUnit, costPriceReason := freezeExtraCost(extraCosts, e.ComponentID, 1)
			extras = append(extras, models.ExtraInsert{
				OrderID:         items[itemIdx].OrderItemID, // in DB extra has order_id and order_item_id; we'll provide both
				OrderItemID:     oid,
				ComponentID:     e.ComponentID,
				ProductID:       p.ProductID,
				MerchantID:      req.MerchantID,
				Price:           e.Price,
				CostPriceUnit:   costPriceUnit,
				CostPriceReason: costPriceReason,
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
					costPriceUnit, costPriceReason := freezeOptionCost(optionCosts, opt.ID, opt.Quantity)
					configs = append(configs, models.ConfigInsert{
						OrderItemID:     oid,
						AttributeID:     attr.ID,
						OptionID:        opt.ID,
						Quantity:        opt.Quantity,
						CostPriceUnit:   costPriceUnit,
						CostPriceReason: costPriceReason,
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
	db := dbx.GetDB(ctx, r.database)
	if len(list) == 0 {
		return nil
	}
	parts := make([]string, 0, len(list))
	args := make([]interface{}, 0, len(list)*8)
	for _, e := range list {
		parts = append(parts, "(?, ?, ?, ?, ?, ?, ?, ?)")
		args = append(args, e.OrderID, e.OrderItemID, e.ComponentID, e.ProductID, e.MerchantID, e.Price, e.CostPriceUnit, e.CostPriceReason)
	}
	query := "INSERT INTO extra (order_id, order_item_id, component_id, product_id, merchant_id, price, cost_price_unit, cost_price_reason) VALUES " + strings.Join(parts, ",")
	_, err := db.ExecContext(ctx, query, args...)
	return err
}

func (r *OrdersLifeCycleRepository) BulkInsertWithouts(ctx context.Context, list []models.WithoutInsert) error {
	db := dbx.GetDB(ctx, r.database)

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
	db := dbx.GetDB(ctx, r.database)

	if len(list) == 0 {
		return nil
	}
	parts := make([]string, 0, len(list))
	args := make([]interface{}, 0, len(list)*6)
	for _, c := range list {
		parts = append(parts, "(?, ?, ?, ?, ?, ?)")
		args = append(args, c.OrderItemID, c.AttributeID, c.OptionID, c.Quantity, c.CostPriceUnit, c.CostPriceReason)
	}
	query := "INSERT INTO order_item_configuration (order_item_id, configuration_attribute_id, configuration_attribute_option_id, quantity, cost_price_unit, cost_price_reason) VALUES " + strings.Join(parts, ",")
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
