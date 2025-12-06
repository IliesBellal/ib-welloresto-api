package orders

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"welloresto-api/internal/models"
)

// validateProductAvailability checks for products that become unavailable because of components status = 0
func (s *OrderService) validateProductAvailability(ctx context.Context, tx *sql.Tx, req *CreateOrderRequest) ([]int64, error) {
	// build list of product ids from request
	if len(req.Order.Products) == 0 {
		return nil, nil
	}
	ids := make([]interface{}, 0, len(req.Order.Products))
	placeholders := make([]string, 0, len(req.Order.Products))
	for i, p := range req.Order.Products {
		ids = append(ids, p.ProductID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
	}
	// SQL: find products that have missing components (source: PHP query)
	// We adapt to a parameterized query (Postgres style with $n). If you use MySQL, replace placeholders with ? and adapt Exec accordingly.
	query := fmt.Sprintf(`
SELECT DISTINCT p.product_id
FROM products p
LEFT JOIN (
    SELECT DISTINCT r.product_id
    FROM requires rq
    INNER JOIN recipes r ON r.recipe_id = rq.recipe_id
    INNER JOIN components c ON rq.component_id = c.component_id AND c.status = 0 AND rq.enabled = true
) a ON a.product_id = p.product_id
WHERE p.product_id IN (%s)
AND (CASE WHEN a.product_id IS NOT NULL THEN 0 ELSE p.status END) = 0
`, joinPlaceholders(len(ids), 1))

	rows, err := tx.QueryContext(ctx, query, ids...)
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

// upsertCustomer calls the customer repository to create/update the customer and returns numeric ID (nil if none)
func (s *OrderService) upsertCustomer(ctx context.Context, tx *sql.Tx, req *CreateOrderRequest) (*int64, error) {
	if req.Order.Customer == nil {
		return nil, nil
	}

	// Convert our Order CustomerRequest to the models.Customer expected by CustomerRepository
	cust := &models.Customer{
		MerchantID: req.MerchantID,
	}
	if req.Order.Customer.CustomerID != nil {
		// CustomerRepository expects string id often; adapt if needed
		idStr := strconv.FormatInt(*req.Order.Customer.CustomerID, 10)
		cust.CustomerID = &idStr
	}
	if req.Order.Customer.Name != nil {
		cust.CustomerName = req.Order.Customer.Name
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
	// ... map other fields as needed

	// CustomerRepository.UpdateOrCreateCustomer should be transaction-aware; if not, it will open its own transaction.
	// We call it directly. It returns ID as string.
	newIDStr, err := s.custRepo.UpdateOrCreateCustomer(ctx, cust)
	if err != nil {
		return nil, fmt.Errorf("failed to update/create customer: %w", err)
	}
	if newIDStr == "" {
		return nil, nil
	}
	newIDInt, err := strconv.ParseInt(newIDStr, 10, 64)
	if err != nil {
		// return the string wrapped if parse fails
		return nil, fmt.Errorf("customer id parse error: %w", err)
	}
	return &newIDInt, nil
}

// insertOrderBase inserts the orders row and returns orderID and orderNum
func (s *OrderService) insertOrderBase(ctx context.Context, tx *sql.Tx, req *CreateOrderRequest, customerID *int64) (orderID int64, orderNum int64, err error) {
	// determine orderNum (simple approach: take max + 1). For performance you may want a sequence.
	var lastOrderNum sql.NullInt64
	err = tx.QueryRowContext(ctx, `
SELECT order_num
FROM orders
WHERE merchant_id = ?
ORDER BY order_id DESC
LIMIT 1
`, req.MerchantID).Scan(&lastOrderNum)
	if err != nil && err != sql.ErrNoRows {
		return 0, 0, err
	}
	if lastOrderNum.Valid {
		orderNum = lastOrderNum.Int64 + 1
	} else {
		orderNum = 1
	}

	// default fields and estimated_ready handling simplified: use UTC_TIMESTAMP equivalent in SQL
	res, err := tx.ExecContext(ctx, `
INSERT INTO orders(cash_register_id, merchant_id, customer_id, order_num, price, TVA, HT, isDelivery, merchant_approval, means_of_payement, scheduled, creation_date, dateCall, last_update, responsible, created_by, delivery_fees, estimated_ready, use_customer_temporary_address, brand_status, order_type, places_settings, pager_number)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, UTC_TIMESTAMP, UTC_TIMESTAMP, UTC_TIMESTAMP, ?, ?, ?, UTC_TIMESTAMP, ?, ?, ?, ?, ?)`,
		req.DeviceID, req.MerchantID, nullableInt64(customerID), orderNum, req.Order.TTC, req.Order.TVA, req.Order.HT,
		false, // isDelivery simplified, adapt from req.Order.OrderType if needed
		req.Order.MerchantApproval, nil, boolToInt(req.Order.IsScheduled),
		req.Order.Responsible, req.Order.CreatedBy, req.Order.DeliveryFees, req.Order.EstimatedReady,
		boolToInt(req.Order.UseCustomerTemporaryAddress), req.Order.BrandStatus, req.Order.OrderType, req.Order.PlacesSettings, req.Order.PagerNumber,
	)
	if err != nil {
		return 0, 0, err
	}
	lastID, err := res.LastInsertId()
	if err != nil {
		return 0, 0, err
	}
	return lastID, orderNum, nil
}

// insertOrderItems inserts each orderitem and returns list of UsedItem (order_item_id + qty)
func (s *OrderService) insertOrderItems(ctx context.Context, tx *sql.Tx, req *CreateOrderRequest, orderID int64) ([]UsedItem, error) {
	used := make([]UsedItem, 0, len(req.Order.Products))
	for _, p := range req.Order.Products {
		if p.Quantity == 0 {
			continue
		}
		item := &OrderItemInsert{
			OrderID:    orderID,
			ProductID:  p.ProductID,
			MerchantID: req.MerchantID,
			Quantity:   p.Quantity,
			DiscountID: p.DiscountID,
			Price:      p.Price,
			DelayID:    p.DelayID,
		}
		oid, err := s.repo.InsertOrderItem(ctx, tx, item)
		if err != nil {
			return nil, err
		}
		used = append(used, UsedItem{OrderItemID: oid, Quantity: p.Quantity})
	}
	return used, nil
}

// insertExtrasWithoutsConfigs does bulk inserts for extras, withouts, configurations
func (s *OrderService) insertExtrasWithoutsConfigs(ctx context.Context, tx *sql.Tx, req *CreateOrderRequest, items []UsedItem) error {
	// Build maps from product iteration to order_item ids; we used ordering to match the order of products to items
	// Simpler approach: while inserting items we could have returned corresponding mapping; for now assume order preserved.
	extras := []ExtraInsert{}
	withouts := []WithoutInsert{}
	configs := []ConfigInsert{}

	itemIdx := 0
	for _, p := range req.Order.Products {
		if p.Quantity == 0 {
			continue
		}
		if itemIdx >= len(items) {
			return fmt.Errorf("internal mapping error: items length mismatch")
		}
		oid := items[itemIdx].OrderItemID
		// extras
		for _, e := range p.Extra {
			extras = append(extras, ExtraInsert{
				OrderID:     items[itemIdx].OrderItemID, // in DB extra has order_id and order_item_id; we'll provide both
				OrderItemID: oid,
				ComponentID: e.ComponentID,
				ProductID:   p.ProductID,
				MerchantID:  req.MerchantID,
				Price:       e.Price,
			})
		}
		// withouts
		for _, w := range p.Without {
			withouts = append(withouts, WithoutInsert{
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
					configs = append(configs, ConfigInsert{
						OrderItemID: oid,
						AttributeID: attr.ID,
						OptionID:    opt.ID,
						Quantity:    opt.Quantity,
					})
				}
			}
		}
		itemIdx++
	}

	if len(extras) > 0 {
		if err := s.repo.BulkInsertExtras(ctx, tx, extras); err != nil {
			return err
		}
	}
	if len(withouts) > 0 {
		if err := s.repo.BulkInsertWithouts(ctx, tx, withouts); err != nil {
			return err
		}
	}
	if len(configs) > 0 {
		if err := s.repo.BulkInsertConfigs(ctx, tx, configs); err != nil {
			return err
		}
	}
	return nil
}

// insertPayments inserts payments
func (s *OrderService) insertPayments(ctx context.Context, tx *sql.Tx, req *CreateOrderRequest, orderID int64) error {
	for _, p := range req.Order.Payments {
		pi := &PaymentInsert{
			MerchantID:     req.MerchantID,
			CashRegisterID: req.DeviceID,
			OrderID:        orderID,
			Amount:         p.Amount,
			MOP:            p.MOP,
			UserID:         req.Order.CreatedBy,
		}
		if err := s.repo.InsertPayment(ctx, tx, pi); err != nil {
			return err
		}
	}
	return nil
}

// ----------------- helpers -----------------
func joinPlaceholders(n int, start int) string {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = fmt.Sprintf("$%d", start+i)
	}
	return strings.Join(out, ", ")
}

func nullableInt64(i *int64) interface{} {
	if i == nil {
		return nil
	}
	return *i
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
