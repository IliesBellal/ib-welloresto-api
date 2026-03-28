package repository

import (
	"context"
	"database/sql"
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
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	err = db.QueryRowContext(ctx, `
		SELECT merchant_id, order_id
		FROM orders
		WHERE brand_order_id = ?
	`, brandOrderID).Scan(&merchantID, &orderID)

	if err != nil {
		log.Error(err.Error())
	}

	return
}

// --- CANCEL ORDER ---
func (r *OrdersRepository) CancelOrder(ctx context.Context, brandOrderID string) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	_, err := db.ExecContext(ctx, `
		UPDATE orders 
		SET brand_status = 'CANCELED',
		    deletion_reason_id = 39,
		    state = 'CLOSED'
		WHERE brand_order_id = ?
	`, brandOrderID)
	if err != nil {
		log.Error("Error canceling order: " + err.Error())
		return err
	}

	_, err = db.ExecContext(ctx, `
		UPDATE payments p
		JOIN orders o ON p.order_id = o.order_id
		SET p.enabled = FALSE
		WHERE o.brand_order_id = ?
	`, brandOrderID)

	if err != nil {
		log.Error("Error updating payment status: " + err.Error())
	}

	return err
}

// --- DELIVERY STATUS UPDATES ---
func (r *OrdersRepository) MarkEnRouteToDropoff(ctx context.Context, brandOrderID string) error {
	db := dbutils.GetDB(ctx, r.database)
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
	_, err = db.ExecContext(ctx, `
		UPDATE orders
		SET brand_status = 'EN_ROUTE_TO_DROPOFF',
		    delivery_start = UTC_TIMESTAMP(),
			isDistributed = 1
		WHERE order_id = ?
	`, orderID)
	if err != nil {
		log.Error("Error updating order status: " + err.Error())
		return err
	}

	// 3. Update des items (sans JOIN)
	_, err = db.ExecContext(ctx, `
		UPDATE orderitems
		SET distributed_on = UTC_TIMESTAMP(),
		    isDistributed = 1
		WHERE order_id = ?
	`, orderID)
	if err != nil {
		log.Error("Error updating order items: " + err.Error())
		return err
	}

	return nil
}

func (r *OrdersRepository) MarkFailed(ctx context.Context, brandOrderID string) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	_, err := db.ExecContext(ctx, `
		UPDATE orders
		SET state = 'CLOSED',
		    brand_status = 'FAILED'
		WHERE brand_order_id = ?
	`, brandOrderID)

	if err != nil {
		log.Error("Error marking order as failed: " + err.Error())
	}

	return err
}
