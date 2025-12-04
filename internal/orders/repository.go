package orders

import (
	"context"
	"database/sql"
)

type OrdersRepository interface {
	ValidateProducts(ctx context.Context, tx *sql.Tx, merchantID int64, productIDs []int64) ([]int64, error)

	InsertOrder(ctx context.Context, tx *sql.Tx, o *OrderInsert) (int64, error)
	InsertOrderItem(ctx context.Context, tx *sql.Tx, item *OrderItemInsert) (int64, error)

	BulkInsertExtras(ctx context.Context, tx *sql.Tx, list []ExtraInsert) error
	BulkInsertWithouts(ctx context.Context, tx *sql.Tx, list []WithoutInsert) error
	BulkInsertConfigs(ctx context.Context, tx *sql.Tx, list []ConfigInsert) error

	InsertPayment(ctx context.Context, tx *sql.Tx, p *PaymentInsert) error
}
