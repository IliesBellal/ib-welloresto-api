package repository

import (
	"context"
	"database/sql"
)

type OrdersRepository struct {
	db *sql.DB
}

func NewOrdersRepository(db *sql.DB) *OrdersRepository {
	return &OrdersRepository{db: db}
}

// Utilisé pour récupérer merchant_id et order_id
func (r *OrdersRepository) GetOrderIDsByBrandOrderID(ctx context.Context, brandOrderID string) (merchantID string, orderID string, err error) {
	err = r.db.QueryRowContext(ctx, `
		SELECT merchant_id, order_id
		FROM orders
		WHERE brand_order_id = ?
	`, brandOrderID).Scan(&merchantID, &orderID)

	return
}

// --- CANCEL ORDER ---
func (r *OrdersRepository) CancelOrder(ctx context.Context, tx *sql.Tx, brandOrderID string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE orders 
		SET brand_status = 'CANCELED',
		    deletion_reason_id = 39,
		    state = 'CLOSED'
		WHERE brand_order_id = ?
	`, brandOrderID)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE payments p
		JOIN orders o ON p.order_id = o.order_id
		SET p.enabled = FALSE
		WHERE o.brand_order_id = ?
	`, brandOrderID)

	return err
}

// --- DELIVERY STATUS UPDATES ---
func (r *OrdersRepository) MarkEnRouteToDropoff(ctx context.Context, tx *sql.Tx, brandOrderID string) error {
	var orderID string

	// 1. Lock explicite de la commande (FOR UPDATE)
	err := tx.QueryRowContext(ctx, `
		SELECT order_id
		FROM orders
		WHERE brand_order_id = ?
		FOR UPDATE
	`, brandOrderID).Scan(&orderID)

	if err == sql.ErrNoRows {
		return nil // ou une erreur métier si tu préfères
	}
	if err != nil {
		return err
	}

	// 2. Update de la commande
	_, err = tx.ExecContext(ctx, `
		UPDATE orders
		SET brand_status = 'EN_ROUTE_TO_DROPOFF',
		    delivery_start = UTC_TIMESTAMP()
		WHERE order_id = ?
	`, orderID)
	if err != nil {
		return err
	}

	// 3. Update des items (sans JOIN)
	_, err = tx.ExecContext(ctx, `
		UPDATE orderitems
		SET distributed_on = UTC_TIMESTAMP(),
		    isDistributed = 1
		WHERE order_id = ?
	`, orderID)
	if err != nil {
		return err
	}

	return nil
}

func (r *OrdersRepository) MarkFailed(ctx context.Context, tx *sql.Tx, brandOrderID string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE orders
		SET state = 'CLOSED',
		    brand_status = 'FAILED'
		WHERE brand_order_id = ?
	`, brandOrderID)
	return err
}
