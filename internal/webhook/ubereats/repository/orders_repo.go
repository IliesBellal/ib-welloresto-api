package repository

import (
	"context"
	"database/sql"
	"fmt"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/receipt"
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
// Conformité caisse (C10) : les commandes encore ouvertes sont clôturées
// (date de clôture, qui les rattache à leur clôture journalière scellée).
//
// Lot C conformité caisse (R7) : seules les commandes encore ouvertes sont
// touchées. Leurs paiements sont annulés par la fonction unique
// (fiscal.CancelOrderPayments, source UBER_EATS, trace d'audit), dans la même
// transaction que la clôture ; une commande rouverte après une vente reçoit
// l'avoir de cette vente. Une commande déjà close ne change pas (avant le lot
// C, ses paiements étaient désactivés, ce qui retirait une vente déjà au
// ticket et au Z) : c'est journalisé.
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
		if len(open) == 0 {
			log.Warn("Uber Eats cancel ignored: no open order for brand_order_id " + brandOrderID)
			return nil
		}
		receipts := receipt.NewReceiptService(receipt.NewReceiptRepository(r.database))
		for _, o := range open {
			if _, err := fiscal.CancelOrderPayments(txCtx, r.database, fiscal.PaymentCancellation{
				MerchantID: o.MerchantID, OrderID: o.OrderID,
				Source: fiscal.CancelSourceUberEats, UserID: models.UberEatsWebhookUserID,
				Reason: "Annulation Uber Eats",
			}, func(txCtx context.Context, hasSaleReceipt bool) error {
				if hasSaleReceipt {
					if err := receipts.CancelSaleReceipt(txCtx, o.MerchantID, o.OrderID); err != nil {
						return err
					}
				}
				args := append(fiscal.ClosureArgs(), o.OrderID)
				_, err := db.ExecContext(txCtx, `
					UPDATE orders
					SET brand_status = 'CANCELED',
					    deletion_reason_id = '39',
					    cancelled_by_type = 'PLATFORM',
					    `+fiscal.ClosureColumns+`
					WHERE order_id = ?
				`, args...)
				return err
			}); err != nil {
				log.Error("Error canceling order: " + err.Error())
				return err
			}
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
// signale l'échec de livraison. Conformité caisse (C10) : les commandes encore
// ouvertes sont clôturées (date de clôture) ; une commande déjà close ne
// reçoit que le statut, sans nouvelle date de clôture.
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
			if _, err := db.ExecContext(txCtx, `
				UPDATE orders
				SET brand_status = 'FAILED',
				    `+fiscal.ClosureColumns+`
				WHERE order_id = ?
			`, append(fiscal.ClosureArgs(), o.OrderID)...); err != nil {
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
