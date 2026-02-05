package stripe

import (
	"context"
	"database/sql"
)

type Repository interface {
	WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error

	// Transactional methods (accept *sql.Tx)
	InsertPayment(ctx context.Context, tx *sql.Tx, p Payment) (int64, error)
	InsertStripePayment(ctx context.Context, tx *sql.Tx, sp StripePayment) error
	UpdateOrderPaymentStatus(ctx context.Context, tx *sql.Tx, orderID string) error
	UpdateOrderDetails(ctx context.Context, tx *sql.Tx, checkoutSessionID, orderID string) error
	UpdateOrderItemsPaid(ctx context.Context, tx *sql.Tx, checkoutSessionID, orderID string) error
	GetOrder(ctx context.Context, tx *sql.Tx, orderID string) (*Order, error)
	GetMerchant(ctx context.Context, tx *sql.Tx, merchantID string) (*Merchant, error)
	GetAutoAcceptSettings(ctx context.Context, tx *sql.Tx, orderID, merchantID string) (string, *Merchant, error) // Returns orderType and settings

	// Customer management
	FindCustomer(ctx context.Context, tx *sql.Tx, email, merchantID string) (*Customer, error)
	CreateCustomer(ctx context.Context, tx *sql.Tx, c Customer, merchantID string) (int64, error)
	UpdateCustomer(ctx context.Context, tx *sql.Tx, c Customer) error
	UpdateOrderCustomer(ctx context.Context, tx *sql.Tx, orderID string, customerID int64) error

	// Fees & Intents
	GetAccountIDByPaymentIntent(ctx context.Context, tx *sql.Tx, paymentIntentID string) (string, error)
	UpdateFees(ctx context.Context, tx *sql.Tx, paymentIntentID string, wrFees, stripeFees, totalFee int64) error
	UpdatePaymentIntentStatus(ctx context.Context, tx *sql.Tx, paymentIntentID, status string) error
	DisablePayment(ctx context.Context, tx *sql.Tx, paymentIntentID string) error

	// Subscription (Simplified placeholders based on your PHP)
	CreateInvoice(ctx context.Context, tx *sql.Tx, merchantID, invoiceID string, amount int64, created int64, customerID string) error
	PayInvoice(ctx context.Context, tx *sql.Tx, invoiceID string, paidAt int64) error
}

type mysqlRepo struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) Repository {
	return &mysqlRepo{db: db}
}

// Helper pour gérer les transactions
func (r *mysqlRepo) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (r *mysqlRepo) InsertPayment(ctx context.Context, tx *sql.Tx, p Payment) (int64, error) {
	query := `INSERT INTO payments(merchant_id, order_id, user_id, amount, mop, payment_date) 
	          VALUES(?, ?, '0', ?, 'STRIPE', UTC_TIMESTAMP())`
	res, err := tx.ExecContext(ctx, query, p.MerchantID, p.OrderID, p.Amount)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (r *mysqlRepo) InsertStripePayment(ctx context.Context, tx *sql.Tx, sp StripePayment) error {
	query := `INSERT INTO stripe_payments(order_id, payment_id, payment_intent_id, checkout_session_id, customer_email, stripe_session_date) 
	          VALUES(?, ?, ?, ?, ?, UTC_TIMESTAMP())`
	_, err := tx.ExecContext(ctx, query, sp.OrderID, sp.PaymentID, sp.PaymentIntentID, sp.CheckoutSessionID, sp.CustomerEmail)
	return err
}

func (r *mysqlRepo) UpdateOrderPaymentStatus(ctx context.Context, tx *sql.Tx, orderID string) error {
	// Logique complexe de SUM convertie
	query := `UPDATE orders o
              LEFT JOIN (
                  SELECT p.order_id, IFNULL(SUM(p.amount), 0) AS total_paid
                  FROM payments p
                  WHERE p.enabled = 1 and p.order_id = ?
                  GROUP BY p.order_id
              ) AS p_sum ON o.order_id = p_sum.order_id
              SET o.isPaid = (o.price <= p_sum.total_paid),
                  o.creation_date = UTC_TIMESTAMP()
              WHERE o.order_id = ?`
	_, err := tx.ExecContext(ctx, query, orderID, orderID)
	return err
}

func (r *mysqlRepo) UpdateOrderDetails(ctx context.Context, tx *sql.Tx, checkoutSessionID, orderID string) error {
	query := `UPDATE orders o
              INNER JOIN stripe_payments sp ON o.order_id = sp.order_id
              SET o.brand_status = 'PENDING_APPROVAL',
                  o.merchant_approval = 'PENDING_APPROVAL',
                  o.last_update = UTC_TIMESTAMP()
              WHERE sp.checkout_session_id = ? AND o.order_id = ?`
	_, err := tx.ExecContext(ctx, query, checkoutSessionID, orderID)
	return err
}

func (r *mysqlRepo) UpdateOrderItemsPaid(ctx context.Context, tx *sql.Tx, checkoutSessionID, orderID string) error {
	query := `UPDATE orderitems oi
              INNER JOIN orders o on o.order_id = oi.order_id
              INNER JOIN stripe_payments sp on o.order_id = sp.order_id
              SET oi.isPaid = true, oi.paid_quantity = oi.quantity
              WHERE sp.checkout_session_id = ? AND o.order_id = ?`
	_, err := tx.ExecContext(ctx, query, checkoutSessionID, orderID)
	return err
}

func (r *mysqlRepo) GetOrder(ctx context.Context, tx *sql.Tx, orderID string) (*Order, error) {
	var o Order
	query := `SELECT order_id, price, creation_date, customer_id FROM orders WHERE order_id = ?`
	err := tx.QueryRowContext(ctx, query, orderID).Scan(&o.OrderID, &o.Price, &o.CreationDate, &o.CustomerID)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *mysqlRepo) GetMerchant(ctx context.Context, tx *sql.Tx, merchantID string) (*Merchant, error) {
	var m Merchant
	var logo sql.NullString
	query := `SELECT m.id, m.fullName, m.timezone, mp.currency, IFNULL(qr.code, ''), m.logo_url
	          FROM merchant m
	          INNER JOIN merchant_parameters mp on mp.merchant_id = m.id
	          LEFT JOIN qrcodes qr on qr.merchant_id = m.id
	          WHERE m.id = ? LIMIT 1`
	err := tx.QueryRowContext(ctx, query, merchantID).Scan(&m.ID, &m.BusinessName, &m.Timezone, &m.Currency, &m.Code, &logo)
	if err != nil {
		return nil, err
	}
	m.LogoURL = "http://storage.welloresto.fr/img/defaults/wr_logo_invoice.png"
	if logo.Valid && logo.String != "" {
		m.LogoURL = logo.String
	}
	return &m, nil
}

func (r *mysqlRepo) GetAutoAcceptSettings(ctx context.Context, tx *sql.Tx, orderID, merchantID string) (string, *Merchant, error) {
	var m Merchant
	var orderType string
	// Attention: MySQL retourne les booléens comme 1 ou 0 (int ou string selon driver)
	// On simplifie la query pour récupérer les flags
	query := `SELECT mp.auto_accept_sno_delivery_orders, mp.auto_accept_sno_take_away_orders, o.order_type
              FROM orders o
              INNER JOIN merchant_parameters mp on mp.merchant_id = o.merchant_id
              WHERE o.order_id = ? AND o.merchant_id = ?`

	err := tx.QueryRowContext(ctx, query, orderID, merchantID).Scan(&m.AutoAcceptDelivery, &m.AutoAcceptTakeaway, &orderType)
	return orderType, &m, err
}

// --- Customer Logic ---
func (r *mysqlRepo) FindCustomer(ctx context.Context, tx *sql.Tx, email, merchantID string) (*Customer, error) {
	var c Customer
	query := `SELECT customer_id, customer_name, customer_email, customer_address FROM customer 
	          WHERE customer_email = ? AND merchant_id = ?`
	err := tx.QueryRowContext(ctx, query, email, merchantID).Scan(&c.ID, &c.Name, &c.Email, &c.Address)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &c, err
}

func (r *mysqlRepo) CreateCustomer(ctx context.Context, tx *sql.Tx, c Customer, merchantID string) (int64, error) {
	query := `INSERT INTO customer (customer_name, customer_email, customer_address, merchant_id) VALUES (?, ?, ?, ?)`
	res, err := tx.ExecContext(ctx, query, c.Name, c.Email, c.Address, merchantID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (r *mysqlRepo) UpdateCustomer(ctx context.Context, tx *sql.Tx, c Customer) error {
	query := `UPDATE customer SET customer_email = ?, customer_name = COALESCE(customer_name, ?), customer_address = COALESCE(?, customer_address) WHERE customer_id = ?`
	_, err := tx.ExecContext(ctx, query, c.Email, c.Name, c.Address, c.ID)
	return err
}

func (r *mysqlRepo) UpdateOrderCustomer(ctx context.Context, tx *sql.Tx, orderID string, customerID int64) error {
	_, err := tx.ExecContext(ctx, "UPDATE orders SET customer_id = ? WHERE order_id = ?", customerID, orderID)
	return err
}

// --- Fees & Intents ---

func (r *mysqlRepo) GetAccountIDByPaymentIntent(ctx context.Context, tx *sql.Tx, paymentIntentID string) (string, error) {
	var accountID string
	query := `SELECT sa.account_id
	          FROM stripe_payments sp
	          INNER JOIN payments p on sp.payment_id = p.payment_id
	          INNER JOIN stripe_accounts sa on sa.merchant_id = p.merchant_id
	          WHERE sp.payment_intent_id = ?`
	err := tx.QueryRowContext(ctx, query, paymentIntentID).Scan(&accountID)
	return accountID, err
}

func (r *mysqlRepo) UpdateFees(ctx context.Context, tx *sql.Tx, paymentIntentID string, wrFees, stripeFees, totalFee int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE stripe_payments SET wello_resto_total_fees = ?, stripe_total_fees = ? WHERE payment_intent_id = ?`, wrFees, stripeFees, paymentIntentID)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `UPDATE payments p INNER JOIN stripe_payments sp on sp.payment_id = p.payment_id SET p.fee = ? WHERE sp.payment_intent_id = ?`, totalFee, paymentIntentID)
	return err
}

func (r *mysqlRepo) UpdatePaymentIntentStatus(ctx context.Context, tx *sql.Tx, paymentIntentID, status string) error {
	_, err := tx.ExecContext(ctx, `UPDATE stripe_payments SET payment_intent_status = ? WHERE payment_intent_id = ?`, status, paymentIntentID)
	return err
}

func (r *mysqlRepo) DisablePayment(ctx context.Context, tx *sql.Tx, paymentIntentID string) error {
	query := `UPDATE payments p 
	          INNER JOIN stripe_payments sp on sp.payment_id = p.payment_id
	          SET p.enabled = '0' 
	          WHERE sp.payment_intent_id = ?`
	_, err := tx.ExecContext(ctx, query, paymentIntentID)
	return err
}

// --- Subscription ---
func (r *mysqlRepo) CreateInvoice(ctx context.Context, tx *sql.Tx, merchantID, invoiceID string, amount int64, created int64, customerID string) error {
	// Note: Conversion of `created` timestamp to string date handled here or in SQL
	query := `INSERT INTO subscription_invoices(merchant_id, invoice_id, invoice_date, amount)
			  SELECT ?, ?, FROM_UNIXTIME(?), ?
			  FROM welloresto_stripe_customers WHERE stripe_customer_id = ?`
	_, err := tx.ExecContext(ctx, query, merchantID, invoiceID, created, amount, customerID)
	return err
}

func (r *mysqlRepo) PayInvoice(ctx context.Context, tx *sql.Tx, invoiceID string, paidAt int64) error {
	query := `UPDATE subscription_invoices SET status = '1', payment_date = FROM_UNIXTIME(?) WHERE invoice_id = ?`
	_, err := tx.ExecContext(ctx, query, paidAt, invoiceID)
	return err
}
