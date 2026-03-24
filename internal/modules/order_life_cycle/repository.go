package order_life_cycle

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/orders"
	"welloresto-api/internal/utils/dbutils"
	"welloresto-api/internal/utils/security"
)

type OrdersLifeCycleRepository struct {
	database      *sql.DB
	ordersFetcher *orders.OrdersFetcher
}

type OrderIntegrationInfo struct {
	MerchantID   string
	Brand        string
	BrandOrderID string
}

func NewOrdersLifeCycleRepository(db *sql.DB, ordersF *orders.OrdersFetcher) *OrdersLifeCycleRepository {
	return &OrdersLifeCycleRepository{
		database:      db,
		ordersFetcher: ordersF}
}

func (r *OrdersLifeCycleRepository) ReopenClosedOrder(ctx context.Context, merchantID, orderID, userID string) error {
	tx, err := r.database.BeginTx(ctx, nil)
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
		// TODO : appeler équivalent Go de logOrderChange(...)
	}

	// commit
	if err := tx.Commit(); err != nil {
		return err
	}

	// TODO: Send update notification (équivalent sendUpdateOrderNotification)
	return nil
}

func (r *OrdersLifeCycleRepository) GetActiveCashRegisterID(ctx context.Context, deviceID string) (string, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	var cashRegisterID sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT cr.cash_register_id
		FROM cash_registers cr
		LEFT JOIN sub_cash_registers scr ON scr.cash_register_id = cr.cash_register_id
		WHERE (cr.device_id = ? OR scr.device_id = ?)
		AND cr.end_date IS NULL
	`, deviceID, deviceID).Scan(&cashRegisterID)

	if err == sql.ErrNoRows {
		log.Error("Impossible de trouver le registre de caisse. Fallback sur le device ID")
		// Fallback sur le deviceID si aucun registre n'est ouvert
		return deviceID, nil
	} else if err != nil {
		log.Error("Error finding cash register: " + err.Error())
		return "", err
	}

	return cashRegisterID.String, nil
}

func (r *OrdersLifeCycleRepository) AddPayment(ctx context.Context, payment models.Payment) error {
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
		return fmt.Errorf("failed to check order payment status: %w", err)
	}

	if (alreadyPaid >= totalPrice && payment.OperationType == models.OperationTypeSale) || alreadyPaid+payment.Amount > totalPrice {
		return &models.OrderNotFullyPaidError{
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
		ORDER BY payment_id DESC LIMIT 1 
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
		return err
	}

	paymentID, _ := res.LastInsertId()

	// 4. Ticket restaurant (TR)
	if payment.MOP == "TR" {
		// On suppose que Code est un champ dans ta struct Payment, à adapter si besoin
		_, err = db.ExecContext(ctx, `
			INSERT INTO restaurant_ticket (merchant_id, payment_id, barcode)
			VALUES (?, ?, ?)
		`, payment.MerchantID, paymentID, payment.Code)
		if err != nil {
			log.Error("Error inserting TR: " + err.Error())
			return err
		}
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

	return err
}

func (r *OrdersLifeCycleRepository) AddPaymentOld(ctx context.Context, payment models.Payment) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// 1. Trouver cash_register_id
	var cashRegisterID sql.NullString
	err := db.QueryRowContext(ctx, `
        SELECT cr.cash_register_id
        FROM cash_registers cr
        LEFT JOIN sub_cash_registers scr ON scr.cash_register_id = cr.cash_register_id
        WHERE (cr.device_id = ? OR scr.device_id = ?)
        AND cr.end_date IS NULL
    `, payment.AccountID, payment.AccountID).Scan(&cashRegisterID)

	if err == sql.ErrNoRows {
		cashRegisterID.String = *payment.AccountID
		cashRegisterID.Valid = true
	} else if err != nil {
		log.Error("Error in sql " + err.Error())
		return err
	}

	// 2. Vérification du montant (Paiement total déjà effectué ?)
	var totalPrice, alreadyPaid int
	err = db.QueryRowContext(ctx, `
       SELECT o.price, COALESCE(SUM(p.amount),0)
       FROM orders o
       LEFT JOIN payments p ON p.order_id = o.order_id AND p.enabled = 1
       WHERE o.order_id = ?
       GROUP BY o.order_id
    `, payment.OrderID).Scan(&totalPrice, &alreadyPaid)

	if err != nil {
		log.Error("Error in sql " + err.Error())
		return fmt.Errorf("failed to check order payment status: %w", err)
	}

	if alreadyPaid >= totalPrice || alreadyPaid+payment.Amount > totalPrice {
		// On renvoie juste l'erreur, RunInTx s'occupe du reste
		return &models.OrderNotFullyPaidError{
			OrderID:    payment.OrderID,
			PaidAmount: alreadyPaid,
			Price:      totalPrice,
		}
	}

	// 2.bis : RÉCUPÉRATION DU HASH PRÉCÉDENT (Chaînage Fiscal)
	var prevHash sql.NullString
	_ = db.QueryRowContext(ctx, `
        SELECT hash FROM payments 
        WHERE merchant_id = ? 
        ORDER BY payment_id DESC LIMIT 1 
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
        (merchant_id, cash_register_id, order_id, amount, mop, comment, payment_date, user_id, status_check, previous_hash, hash, signature)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, payment.MerchantID, cashRegisterID.String, payment.OrderID, payment.Amount, payment.MOP, payment.Comment, now, payment.UserID, payment.StatusCheck, prevHash.String, newHash, signature)

	if err != nil {
		log.Error("Error in sql " + err.Error())
		return err
	}

	paymentID, _ := res.LastInsertId()

	// 4. Ticket restaurant (TR)
	if payment.MOP == "TR" {
		_, err = db.ExecContext(ctx, `
            INSERT INTO restaurant_ticket (merchant_id, payment_id, barcode)
            VALUES (?, ?, ?)
        `, payment.MerchantID, paymentID, payment.Code)
		if err != nil {
			log.Error("Error in sql " + err.Error())
			return err
		}
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

	return err // Terminé !
}

func (r *OrdersLifeCycleRepository) AddPaymentVeryOld(ctx context.Context, merchantID, userID string, req *models.PaymentRequest) error {
	tx, err := r.database.BeginTx(ctx, nil)
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
	var totalPrice, alreadyPaid int
	err = tx.QueryRowContext(ctx, `
       SELECT o.price, COALESCE(SUM(p.amount),0)
       FROM orders o
       LEFT JOIN payments p ON p.order_id = o.order_id AND p.enabled = 1
       WHERE o.order_id = ?
       GROUP BY o.order_id
    `, req.OrderID).Scan(&totalPrice, &alreadyPaid)

	if err != nil {
		// Gérer le cas où la commande n'existe pas ou autre erreur SQL
		return rollback(fmt.Errorf("failed to check order payment status: %w", err))
	}

	// 👉 LA VÉRIFICATION MÉTIER EST ICI :
	if alreadyPaid >= totalPrice || alreadyPaid+req.Amount > totalPrice {
		tx.Rollback()
		return &models.OrderNotFullyPaidError{
			OrderID:    req.OrderID,
			PaidAmount: alreadyPaid,
			Price:      totalPrice,
		}
	}

	// 3. Insérer le paiement
	res, err := tx.ExecContext(ctx, `
		INSERT INTO payments
		(merchant_id, cash_register_id, order_id, amount, mop, comment, payment_date, user_id, status_check)
		VALUES (?, ?, ?, ROUND(?,2), ?, ?, UTC_TIMESTAMP, ?, ?)
	`, merchantID, cashRegisterID.String, req.OrderID, req.Amount, req.MOP, req.Comment, userID, req.StatusCheck)
	if err != nil {
		return rollback(err)
	}

	paymentID, _ := res.LastInsertId()

	logger.FromContext(ctx).Info("💵 New payment " + *helpers.Int64ToStringPtr(paymentID) + " created for merchant " + merchantID)

	// 4. Ticket restaurant (TR)
	if req.MOP == "TR" && req.Code != "" {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO restaurant_ticket (merchant_id, payment_id, barcode)
			VALUES (?, ?, ?)
		`, merchantID, paymentID, req.Code)
		if err != nil {
			return rollback(err)
		}
	}

	// 5. Mettre à jour orders.isPaid
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
	return nil
}

func (r *OrdersLifeCycleRepository) GetPaymentsForOrder(ctx context.Context, orderID string) ([]models.Payment, error) {
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

func (r *OrdersLifeCycleRepository) GetPayment(ctx context.Context, orderID string, paymentID int64) (*models.Payment, error) {
	q := `
		SELECT p.order_id, p.payment_id, p.mop, p.amount, p.payment_date, p.enabled, sp.payment_intent_id, sa.account_id
		FROM payments p
		LEFT JOIN stripe_payments sp on sp.payment_id = p.payment_id
		LEFT JOIN stripe_accounts sa on sa.merchant_id = p.merchant_id
		WHERE p.order_id = ? AND p.payment_id = ?
	`

	var p models.Payment
	var paymentDate sql.NullTime

	err := r.database.QueryRowContext(ctx, q, orderID, paymentID).Scan(
		&p.OrderID, &p.PaymentID, &p.MOP, &p.Amount, &paymentDate, &p.Enabled, &p.IntentID, &p.AccountID,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("payment not found: order_id=%s, payment_id=%d", orderID, paymentID)
		}
		return nil, err
	}

	if paymentDate.Valid {
		p.PaymentDate = helpers.NullTimePtr(paymentDate).UTC().Unix()
	}

	return &p, nil
}

func (r *OrdersLifeCycleRepository) DisablePayment(ctx context.Context, paymentID string) error {
	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	// TODO
	// Vérifier qu'il ne s'agit pas d'un paiement Uber Eats ou Deliveroo qui ne sont pas anulables
	// Le client s'en occupe déjà, mais une double vérification côté serveur est nécessaire

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

func (r *OrdersLifeCycleRepository) SetDistributedProducts(ctx context.Context, userID string, merchantID string, req *models.SetDistributedProductsRequest) error {
	// 1. Préparation des outils de compatibilité
	now := time.Now().UTC()
	orderID := req.OrderID

	// On détermine le driver une seule fois (à adapter selon votre config r.db)
	// Idéalement, stockez r.isPostgres lors de l'initialisation du repo
	isPostgres := false

	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 2. Mise à jour des items (Boucle)
	for _, p := range req.Products {
		var beforeIsDistributed sql.NullInt64

		// SELECT : On récupère l'ancienne valeur pour le log
		querySelect := r.formatQuery(`SELECT isDistributed FROM orderitems WHERE order_id = ? AND order_item_id = ?`, isPostgres)
		err = tx.QueryRowContext(ctx, querySelect, orderID, p.OrderItemID).Scan(&beforeIsDistributed)
		if err != nil {
			return err
		}

		// UPDATE ITEM : On injecte 'now' depuis Go
		queryUpdateItem := r.formatQuery(`
			UPDATE orderitems
			SET isDistributed = 1,
			    distributed_quantity = quantity,
			    ready_for_distribution_quantity = quantity,
			    distributed_on = ?
			WHERE order_id = ? AND order_item_id = ?`, isPostgres)

		_, err = tx.ExecContext(ctx, queryUpdateItem, now, orderID, p.OrderItemID)
		if err != nil {
			return err
		}
	}

	// 3. Calcul de l'état global (Optimisé : hors de la boucle précédente)
	var countNotDistributed int
	queryCheck := r.formatQuery(`SELECT COUNT(*) FROM orderitems WHERE order_id = ? AND isDistributed = 0`, isPostgres)
	err = tx.QueryRowContext(ctx, queryCheck, orderID).Scan(&countNotDistributed)
	if err != nil {
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

	_, err = tx.ExecContext(ctx, queryUpdateOrder,
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
		return err
	}

	// 5. Récupération de la marque pour notification
	var brand sql.NullString
	queryBrand := r.formatQuery(`SELECT brand FROM orders WHERE order_id = ?`, isPostgres)
	err = tx.QueryRowContext(ctx, queryBrand, orderID).Scan(&brand)
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
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

/*
func (r *OrdersLifeCycleRepository) SetDistributedProductsOld(ctx context.Context, userID string, merchantID string, req *models.SetDistributedProductsRequest) error {

		//log := logger.FromContext(ctx)

		tx, err := r.database.BeginTx(ctx, nil)
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
			// TODO : appeler équivalent Go de logOrderChange(...)
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

		return nil
	}
*/
func (r *OrdersLifeCycleRepository) MarkProductsBackToProduction(ctx context.Context, userID, merchantID, orderID string, products []models.DistributedProduct) error {

	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, p := range products {

		_, err = tx.ExecContext(ctx, `
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
			return err
		}
	}

	// Check if any undistributed items left
	var remaining int
	err = tx.QueryRowContext(ctx, `
        SELECT COUNT(*)
        FROM orderitems
        WHERE order_id = ? AND isDistributed = 0
    `, orderID).Scan(&remaining)
	if err != nil {
		return err
	}

	fullyDistributed := remaining == 0

	// Update orders table
	_, err = tx.ExecContext(ctx, `
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
		return err
	}

	return tx.Commit()
}

func (r *OrdersLifeCycleRepository) GetOrderBrandAndMerchant(ctx context.Context, orderID string) (*models.OrderMeta, error) {
	const q = `
		SELECT o.brand, o.merchant_id, o.brand_order_id, o.creation_date
		FROM orders o
		WHERE o.order_id = ?
		LIMIT 1;
	`
	row := r.database.QueryRowContext(ctx, q, orderID)
	var m models.OrderMeta
	var merchantID sql.NullInt64
	var brand sql.NullString
	var brandOrder sql.NullString
	var creation sql.NullTime

	if err := row.Scan(&brand, &merchantID, &brandOrder, &creation); err != nil {
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
	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	q := `
		UPDATE orders
		SET last_update = UTC_TIMESTAMP(),
		    state = 'OPEN',
		    brand_status = 'PENDING',
		    merchant_approval = 'ACCEPTED'
		WHERE order_id = ?;
	`
	if _, err := tx.ExecContext(ctx, q, orderID); err != nil {
		return err
	}

	return tx.Commit()
}

func (r *OrdersLifeCycleRepository) MarkOrderAsDeliveryStarted(ctx context.Context, orderID string, userID string) (*OrderIntegrationInfo, error) {

	// Update order
	_, err := r.database.ExecContext(ctx, `
		UPDATE orders
		SET last_update = UTC_TIMESTAMP,
			brand_status = 'EN_ROUTE_TO_DROPOFF',
			delivery_start = UTC_TIMESTAMP,
			responsible = ?
		WHERE order_id = ?
	`, userID, orderID)
	if err != nil {
		return nil, err
	}

	// Load integration info
	row := r.database.QueryRowContext(ctx, `
		SELECT o.merchant_id, o.brand, o.brand_order_id
		FROM orders o
		WHERE o.order_id = ?
	`, orderID)

	var info OrderIntegrationInfo
	err = row.Scan(&info.MerchantID, &info.Brand, &info.BrandOrderID)
	if err != nil {
		return nil, err
	}

	return &info, nil
}

func (r *OrdersLifeCycleRepository) DenyOrderLocal(ctx context.Context, orderID, deletionReasonID, comment string) error {
	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
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
		tx.Rollback()
		return err
	}

	_, err = tx.ExecContext(ctx, `
        UPDATE customer_rewards
        SET is_used = false,
            usage_date = null,
            used_on_order_id = null
        WHERE used_on_order_id = ?`,
		orderID,
	)
	if err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

// Cancel Stripe Payments
func (r *OrdersLifeCycleRepository) CancelStripePayments(ctx context.Context, orderID string) error {
	rows, err := r.database.QueryContext(ctx, `
        SELECT p.mop, sp.checkout_session_id, sa.account_id, sp.payment_intent_id
        FROM payments p
        INNER JOIN stripe_payments sp ON sp.payment_id = p.payment_id
        INNER JOIN stripe_accounts sa ON sa.merchant_id = p.merchant_id
        WHERE p.order_id = ?`,
		orderID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var mop, sessionID, accountID, paymentIntent string
		if err := rows.Scan(&mop, &sessionID, &accountID, &paymentIntent); err != nil {
			return err
		}

		if mop == "STRIPE" {
			/*
				if err := stripeSvc.CancelPayment(ctx, sessionID, accountID, paymentIntent); err != nil {
					return err
				}

			*/
		}
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

	/*	tx, err := r.database.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
	*/
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

func (r *OrdersLifeCycleRepository) DeleteOrderLocal(ctx context.Context, orderID string, reasonID string, comment string) error {
	db := dbutils.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `
        UPDATE orders
        SET deletion_reason_id = ?,
            deletion_comment = ?,
            last_update = UTC_TIMESTAMP,
            state = 'CLOSED',
            brand_status = 'CANCELED',
            delivered_on = UTC_TIMESTAMP
        WHERE order_id = ?`,
		reasonID, comment, orderID,
	)

	return err
}

func (r *OrdersLifeCycleRepository) SetDeliveredLocal(ctx context.Context, orderID string) (*DeliveredOrderMetadata, error) {
	db := dbutils.GetDB(ctx, r.database)

	/*
		tx, err := r.database.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
	*/
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
	qCheck := `
	SELECT o.order_id
	FROM delivery_session_order dso
	INNER JOIN delivery_session_order dso2 ON dso.delivery_session_id = dso2.delivery_session_id
	INNER JOIN orders o ON o.order_id = dso2.order_id AND o.status > 0
	WHERE dso.order_id = ?
	`
	rows, err := db.QueryContext(ctx, qCheck, orderID)
	if err == nil {
		var tmp []string
		for rows.Next() {
			var oid string
			rows.Scan(&oid)
			tmp = append(tmp, oid)
		}
		rows.Close()
		if len(tmp) == 0 {
			const qCloseDS = `
				UPDATE delivery_session
				JOIN delivery_session_order ON delivery_session_order.delivery_session_id = delivery_session.id
				SET delivery_session.status = 0
				WHERE delivery_session_order.order_id = ?
			`
			if _, err := db.ExecContext(ctx, qCloseDS, orderID); err != nil {
				return nil, err
			}
		}
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
	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx failed: %w", err)
	}

	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// Collect unique order IDs
	orderIDMap := make(map[string]bool)
	for _, product := range req.Products {
		orderIDMap[product.OrderID] = true
	}

	// Update each product's production status
	for _, product := range req.Products {
		stmt, err := tx.PrepareContext(ctx, `
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
			return nil, fmt.Errorf("execute update failed for order_item_id %s: %w", product.OrderItemID, err)
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit failed: %w", err)
	}

	// Convert order ID map to slice
	affectedOrderIDs := make([]string, 0, len(orderIDMap))
	for orderID := range orderIDMap {
		affectedOrderIDs = append(affectedOrderIDs, orderID)
	}

	return affectedOrderIDs, nil
}
