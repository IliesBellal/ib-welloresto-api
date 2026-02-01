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
	_, err := tx.ExecContext(ctx, `
		UPDATE orders
		SET brand_status = 'EN_ROUTE_TO_DROPOFF',
		    delivery_start = UTC_TIMESTAMP()
		WHERE brand_order_id = ?
		AND state = 'OPEN'
	`, brandOrderID)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE orderitems oi
		INNER JOIN orders o ON o.order_id = oi.order_id
		SET oi.distributed_on = UTC_TIMESTAMP(),
		    oi.isDistributed = 1
		WHERE o.brand_order_id = ?
	`, brandOrderID)

	return err
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
