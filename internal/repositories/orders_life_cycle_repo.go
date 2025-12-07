package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

type OrdersLifeCycleRepository struct {
	db            *sql.DB
	log           *zap.Logger
	ordersFetcher *OrdersFetcher
}

type OrderIntegrationInfo struct {
	MerchantID   string
	Brand        string
	BrandOrderID string
}

func NewOrdersLifeCycleRepository(db *sql.DB, log *zap.Logger) *OrdersLifeCycleRepository {
	temp := NewOrdersFetcher(db, log)
	return &OrdersLifeCycleRepository{
		db:            db,
		ordersFetcher: temp,
		log:           log}
}

func (r *OrdersLifeCycleRepository) ReopenClosedOrder(ctx context.Context, merchantID, orderID, userID string) error {
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

func (r *OrdersLifeCycleRepository) AddPayment(ctx context.Context, merchantID, userID string, req *models.PaymentRequest) error {
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

func (r *OrdersLifeCycleRepository) GetPaymentsForOrder(ctx context.Context, orderID string) ([]models.Payment, error) {
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

func (r *OrdersLifeCycleRepository) DisablePayment(ctx context.Context, paymentID string) error {
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

func (r *OrdersLifeCycleRepository) SetDistributedProducts(ctx context.Context, userID string, merchantID string, req *models.SetDistributedProductsRequest) error {

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

func (r *OrdersLifeCycleRepository) MarkProductsBackToProduction(ctx context.Context, userID, merchantID, orderID string, products []models.DistributedProduct) error {

	tx, err := r.db.BeginTx(ctx, nil)
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
	row := r.db.QueryRowContext(ctx, q, orderID)
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
	tx, err := r.db.BeginTx(ctx, nil)
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
	_, err := r.db.ExecContext(ctx, `
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
	row := r.db.QueryRowContext(ctx, `
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
	tx, err := r.db.BeginTx(ctx, nil)
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
	rows, err := r.db.QueryContext(ctx, `
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
	var brand string
	err := r.db.QueryRowContext(ctx, `
        SELECT brand
        FROM orders
        WHERE order_id = ? LIMIT 1`,
		orderID,
	).Scan(&brand)
	return brand, err
}

func (r *OrdersLifeCycleRepository) SetReadyForDistribution(ctx context.Context, orderID, merchantID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	// Update orders
	_, err = tx.ExecContext(ctx, `
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
		tx.Rollback()
		return err
	}

	// Update items
	_, err = tx.ExecContext(ctx, `
        UPDATE orderitems
        SET ready_for_distribution_quantity = quantity
        WHERE order_id = ? AND merchant_id = ?`,
		orderID, merchantID,
	)
	if err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (r *OrdersLifeCycleRepository) DeleteOrderLocal(
	ctx context.Context,
	orderID string,
	reasonID string,
	comment string,
) error {

	_, err := r.db.ExecContext(ctx, `
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

// Disable payments
func (r *OrdersLifeCycleRepository) DisablePayments(ctx context.Context, orderID string) error {
	_, err := r.db.ExecContext(ctx, `
        UPDATE payments
        SET enabled = 0
        WHERE order_id = ?`,
		orderID,
	)
	return err
}

// Delete QR codes
func (r *OrdersLifeCycleRepository) DeleteQRCode(ctx context.Context, orderID string) error {
	_, err := r.db.ExecContext(ctx, `
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
	_, err := r.db.ExecContext(ctx, `
        UPDATE bookings
        SET order_id = NULL,
            status NOT IN ('DONE')
        WHERE order_id = ?`,
		orderID,
	)
	return err
}
