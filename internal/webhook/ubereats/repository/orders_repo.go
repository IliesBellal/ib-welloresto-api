package repository

import (
	"context"
	"database/sql"
	"fmt"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/utils/dbutils"
)

type OrdersRepository struct {
	database *sql.DB
}

func NewOrdersRepository(db *sql.DB) *OrdersRepository {
	return &OrdersRepository{database: db}
}

// Utilisé pour récupérer merchant_id et order_id
func (r *OrdersRepository) GetOrderIDsByBrandOrderID(ctx context.Context, brandOrderID string) (merchantID string, orderID string, err error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	err = db.QueryRowContext(ctx, `
		SELECT merchant_id, order_id
		FROM orders
		WHERE brand_order_id = ?
	`, brandOrderID).Scan(&merchantID, &orderID)

	if err != nil {
		log.Error("Error fetching order IDs for brand_order_id " + brandOrderID + ": " + err.Error())
	}

	return
}

// --- CANCEL ORDER ---
//
// Lot A conformité caisse (C10) : les commandes encore ouvertes sont
// clôturées par la chaîne fiscale (fiscal.SealOrderClosure), dans une seule
// transaction avec la désactivation des paiements (inchangée : lot B).
func (r *OrdersRepository) CancelOrder(ctx context.Context, brandOrderID string) error {
	log := logger.FromContext(ctx)

	err := dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, r.database)

		// Guard against a late/duplicate webhook overwriting an order already
		// finalized (delivered, denied, etc.) - mirrors the internal OrderStillOpen
		// check used by DenyOrder/DeleteOrder.
		// cancelled_by_type hardcoded to PLATFORM: this handler is the direct
		// webhook write path (bypasses order_life_cycle.DeleteOrderLocal
		// entirely), unconditionally triggered by Uber Eats itself.
		open, err := fiscal.LockOpenOrdersByBrandOrderID(txCtx, db, brandOrderID)
		if err != nil {
			return err
		}
		for _, o := range open {
			seal, err := fiscal.SealOrderClosure(txCtx, db, o.OrderID)
			if err != nil {
				return err
			}
			args := append(seal.Args(), o.OrderID)
			if _, err := db.ExecContext(txCtx, `
				UPDATE orders
				SET brand_status = 'CANCELED',
				    deletion_reason_id = '39',
				    cancelled_by_type = 'PLATFORM',
				    `+fiscal.SealColumns+`
				WHERE order_id = ?
			`, args...); err != nil {
				log.Error("Error canceling order: " + err.Error())
				return err
			}
		}

		// MySQL's UPDATE...JOIN has no direct Postgres equivalent; Postgres uses
		// UPDATE...FROM instead.
		disablePaymentsQuery := `
			UPDATE payments p
			JOIN orders o ON p.order_id = o.order_id
			SET p.enabled = FALSE
			WHERE o.brand_order_id = ?
		`
		if dbx.ActiveDialect() == dbx.Postgres {
			disablePaymentsQuery = `
			UPDATE payments
			SET enabled = FALSE
			FROM orders
			WHERE payments.order_id = orders.order_id AND orders.brand_order_id = ?
		`
		}
		if _, err := db.ExecContext(txCtx, disablePaymentsQuery, brandOrderID); err != nil {
			log.Error("Error updating payment status: " + err.Error())
			return err
		}
		return nil
	})
	return err
}

// --- DELIVERY STATUS UPDATES ---
func (r *OrdersRepository) MarkEnRouteToDropoff(ctx context.Context, brandOrderID string) error {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	var orderID string

	// 1. Lock explicite de la commande (FOR UPDATE)
	err := db.QueryRowContext(ctx, `
		SELECT order_id
		FROM orders
		WHERE brand_order_id = ?
		FOR UPDATE
	`, brandOrderID).Scan(&orderID)

	if err == sql.ErrNoRows {
		return nil // ou une erreur métier si tu préfères
	}
	if err != nil {
		log.Error("Error fetching order ID: " + err.Error())
		return err
	}

	// 2. Update de la commande
	_, err = db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE orders
		SET brand_status = 'EN_ROUTE_TO_DROPOFF',
		    delivery_start = %s,
			isDistributed = true
		WHERE order_id = ?
	`, dbx.UTCNow()), orderID)
	if err != nil {
		log.Error("Error updating order status: " + err.Error())
		return err
	}

	// 3. Update des items (sans JOIN)
	_, err = db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE orderitems
		SET distributed_on = %s,
		    isDistributed = true
		WHERE order_id = ?
	`, dbx.UTCNow()), orderID)
	if err != nil {
		log.Error("Error updating order items: " + err.Error())
		return err
	}

	return nil
}

// MarkFailed clôture en échec (brand_status FAILED) une commande dont Uber
// signale l'échec de livraison. Lot A conformité caisse (C10) : les commandes
// encore ouvertes sont clôturées par la chaîne fiscale ; une commande déjà
// close ne reçoit que le statut, sans rechaînage ni nouvelle date de clôture.
func (r *OrdersRepository) MarkFailed(ctx context.Context, brandOrderID string) error {
	log := logger.FromContext(ctx)

	err := dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, r.database)
		if _, err := db.ExecContext(txCtx, `
			UPDATE orders
			SET brand_status = 'FAILED'
			WHERE brand_order_id = ? AND state <> 'OPEN'
		`, brandOrderID); err != nil {
			return err
		}
		open, err := fiscal.LockOpenOrdersByBrandOrderID(txCtx, db, brandOrderID)
		if err != nil {
			return err
		}
		for _, o := range open {
			seal, err := fiscal.SealOrderClosure(txCtx, db, o.OrderID)
			if err != nil {
				return err
			}
			if _, err := db.ExecContext(txCtx, `
				UPDATE orders
				SET brand_status = 'FAILED',
				    `+fiscal.SealColumns+`
				WHERE order_id = ?
			`, append(seal.Args(), o.OrderID)...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		log.Error("Error marking order as failed: " + err.Error())
	}

	return err
}
