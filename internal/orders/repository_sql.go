package orders

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// concrete repo implementation (MySQL flavored with ? placeholders)
type OrdersRepoSQL struct {
	db *sql.DB
}

func NewOrdersRepoSQL(db *sql.DB) *OrdersRepoSQL {
	return &OrdersRepoSQL{db: db}
}

// ValidateProducts: check which products are blocked (return slice of product ids that are blocked)
func (r *OrdersRepoSQL) ValidateProducts(ctx context.Context, tx *sql.Tx, merchantID int64, productIDs []int64) ([]int64, error) {
	if len(productIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(productIDs))
	args := make([]interface{}, 0, len(productIDs)+1)
	for i, id := range productIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	// merchant id as first arg
	args = append([]interface{}{merchantID}, args...)
	query := fmt.Sprintf(`
SELECT DISTINCT p.product_id
FROM products p
LEFT JOIN (
    SELECT DISTINCT r.product_id
    FROM requires rq
    INNER JOIN recipes r ON r.recipe_id = rq.recipe_id
    INNER JOIN components c ON rq.component_id = c.component_id AND c.status = 0 AND rq.enabled = true
) a ON a.product_id = p.product_id
WHERE p.merchant_id = ?
AND p.product_id IN (%s)
AND (CASE WHEN a.product_id IS NOT NULL THEN 0 ELSE p.status END) = 0
`, strings.Join(placeholders, ","))
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var blocked []int64
	for rows.Next() {
		var pid int64
		if err := rows.Scan(&pid); err != nil {
			return nil, err
		}
		blocked = append(blocked, pid)
	}
	return blocked, nil
}

// OrderInsert is the minimal data to create an order row
type OrderInsert struct {
	CashRegisterID interface{}
	MerchantID     int64
	CustomerID     interface{}
	OrderNum       int64
	Price          float64
	TVA            float64
	HT             float64
	// other fields omitted for brevity
}

// InsertOrder inserts order and returns order_id
func (r *OrdersRepoSQL) InsertOrder(ctx context.Context, tx *sql.Tx, o *OrderInsert) (int64, error) {
	res, err := tx.ExecContext(ctx, `
INSERT INTO orders (cash_register_id, merchant_id, customer_id, order_num, price, TVA, HT, creation_date, dateCall, last_update)
VALUES (?, ?, ?, ?, ?, ?, ?, UTC_TIMESTAMP, UTC_TIMESTAMP, UTC_TIMESTAMP)
`, o.CashRegisterID, o.MerchantID, o.CustomerID, o.OrderNum, o.Price, o.TVA, o.HT)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// OrderItemInsert represents an order item insert
type OrderItemInsert struct {
	OrderID    int64
	ProductID  int64
	MerchantID string
	Quantity   float64
	DiscountID *int64
	Price      float64
	DelayID    *int64
}

// InsertOrderItem inserts a single orderitem and returns its id
func (r *OrdersRepoSQL) InsertOrderItem(ctx context.Context, tx *sql.Tx, item *OrderItemInsert) (int64, error) {
	res, err := tx.ExecContext(ctx, `
INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, discount_id, price, ordered_on, delay_id)
VALUES (?, ?, ?, ?, ?, ?, UTC_TIMESTAMP, ?)
`, item.OrderID, item.ProductID, item.MerchantID, item.Quantity, nullableInt64(item.DiscountID), item.Price, nullableInt64(item.DelayID))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

type ExtraInsert struct {
	OrderID     int64
	OrderItemID int64
	ComponentID int64
	ProductID   int64
	MerchantID  string
	Price       float64
}
type WithoutInsert struct {
	OrderID     int64
	OrderItemID int64
	ComponentID int64
	ProductID   int64
	MerchantID  string
}
type ConfigInsert struct {
	OrderItemID int64
	AttributeID int64
	OptionID    int64
	Quantity    float64
}

// BulkInsertExtras performs multi-value insert for extras
func (r *OrdersRepoSQL) BulkInsertExtras(ctx context.Context, tx *sql.Tx, list []ExtraInsert) error {
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
	_, err := tx.ExecContext(ctx, query, args...)
	return err
}

func (r *OrdersRepoSQL) BulkInsertWithouts(ctx context.Context, tx *sql.Tx, list []WithoutInsert) error {
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
	_, err := tx.ExecContext(ctx, query, args...)
	return err
}

func (r *OrdersRepoSQL) BulkInsertConfigs(ctx context.Context, tx *sql.Tx, list []ConfigInsert) error {
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
	_, err := tx.ExecContext(ctx, query, args...)
	return err
}

// Payment insert
type PaymentInsert struct {
	MerchantID     string
	CashRegisterID interface{}
	OrderID        int64
	Amount         float64
	MOP            string
	UserID         *string
}

func (r *OrdersRepoSQL) InsertPayment(ctx context.Context, tx *sql.Tx, p *PaymentInsert) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO payments (merchant_id, cash_register_id, order_id, amount, mop, payment_date, user_id)
VALUES (?, ?, ?, ?, ?, UTC_TIMESTAMP, ?)
`, p.MerchantID, p.CashRegisterID, p.OrderID, p.Amount, p.MOP, p.UserID)
	return err
}
