package stripe

import (
	"context"
	"database/sql"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

type Repository interface {

	// Transactional methods (accept *sql.db)
	InsertPayment(cdb context.Context, p Payment) (int64, error)
	InsertStripePayment(cdb context.Context, sp StripePayment) error
	UpdateOrderPaymentStatus(cdb context.Context, orderID string) error
	UpdateOrderDetails(cdb context.Context, checkoutSessionID, orderID string) error
	UpdateOrderItemsPaid(cdb context.Context, checkoutSessionID, orderID string) error
	UpdateOrderCreationDate(cdb context.Context, orderID string) error
	GetOrder(cdb context.Context, orderID string) (*Order, error)
	GetMerchant(cdb context.Context, merchantID string) (*Merchant, error)
	GetAutoAcceptSettings(cdb context.Context, orderID, merchantID string) (string, *Merchant, error) // Returns orderType and settings

	// Customer management
	FindCustomer(cdb context.Context, email, merchantID string) (*Customer, error)
	CreateCustomer(cdb context.Context, c Customer, merchantID string) (int64, error)
	UpdateCustomer(cdb context.Context, c Customer) error
	UpdateOrderCustomer(cdb context.Context, orderID string, customerID int64) error

	// Fees & Intents
	GetAccountIDByPaymentIntent(cdb context.Context, paymentIntentID string) (string, error)
	UpdateFees(cdb context.Context, paymentIntentID string, wrFees, stripeFees, totalFee int64) error
	UpdatePaymentIntentStatus(cdb context.Context, paymentIntentID, status string) error
	DisablePayment(cdb context.Context, paymentIntentID string) error

	// Subscription (Simplified placeholders based on your PHP)
	CreateInvoice(cdb context.Context, merchantID, invoiceID string, amount int64, created int64, customerID string) error
	PayInvoice(cdb context.Context, invoiceID string, paidAt int64) error

	GetMerchantByStripeAccountID(cdb context.Context, accountID string) (*PayoutMerchant, error)

	// Connect account status
	UpdateStripeAccountVerificationStatus(cdb context.Context, accountID, status string) error
}

type mysqlRepo struct {
	database *sql.DB
}

func NewRepository(db *sql.DB) Repository {
	return &mysqlRepo{database: db}
}

// Implémentations

func (r *mysqlRepo) GetMerchantByStripeAccountID(cdb context.Context, accountID string) (*PayoutMerchant, error) {
	db := dbutils.GetDB(cdb, r.database)
	log := logger.FromContext(cdb)

	var pm PayoutMerchant

	// Ta requête PHP traduite :
	query := `
        SELECT m.email, m.fullName
        FROM stripe_accounts sa
        INNER JOIN merchant m on sa.merchant_id = m.id
        WHERE sa.account_id = ?
        AND m.email IS NOT NULL
    `

	err := db.QueryRowContext(cdb, query, accountID).Scan(&pm.Email, &pm.BusinessName)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Pas d'erreur, juste pas de résultat
		}
		log.Error(err.Error())
		return nil, err
	}

	return &pm, nil
}

func (r *mysqlRepo) InsertPayment(cdb context.Context, p Payment) (int64, error) {
	db := dbutils.GetDB(cdb, r.database)
	log := logger.FromContext(cdb)

	query := `INSERT INTO payments(merchant_id, order_id, user_id, amount, mop, payment_date) 
	          VALUES(?, ?, ?, ?, ?, UTC_TIMESTAMP())`
	res, err := db.ExecContext(cdb, query, p.MerchantID, p.OrderID, models.StripeWebhookUserID, p.Amount, models.StripeMOP)
	if err != nil {
		log.Error(err.Error())
		return 0, err
	}
	return res.LastInsertId()
}

func (r *mysqlRepo) InsertStripePayment(cdb context.Context, sp StripePayment) error {
	db := dbutils.GetDB(cdb, r.database)

	query := `INSERT INTO stripe_payments(order_id, payment_id, payment_intent_id, checkout_session_id, customer_email, stripe_session_date) 
	          VALUES(?, ?, ?, ?, ?, UTC_TIMESTAMP())`
	_, err := db.ExecContext(cdb, query, sp.OrderID, sp.PaymentID, sp.PaymentIntentID, sp.CheckoutSessionID, sp.CustomerEmail)
	return err
}

func (r *mysqlRepo) UpdateOrderPaymentStatus(cdb context.Context, orderID string) error {
	db := dbutils.GetDB(cdb, r.database)

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
	_, err := db.ExecContext(cdb, query, orderID, orderID)
	return err
}

func (r *mysqlRepo) UpdateOrderDetails(cdb context.Context, checkoutSessionID, orderID string) error {
	db := dbutils.GetDB(cdb, r.database)

	query := `UPDATE orders o
              INNER JOIN stripe_payments sp ON o.order_id = sp.order_id
              SET o.brand_status = 'PENDING_APPROVAL',
                  o.merchant_approval = 'PENDING_APPROVAL',
                  o.last_update = UTC_TIMESTAMP()
              WHERE sp.checkout_session_id = ? AND o.order_id = ?`
	_, err := db.ExecContext(cdb, query, checkoutSessionID, orderID)
	return err
}

func (r *mysqlRepo) UpdateOrderItemsPaid(cdb context.Context, checkoutSessionID, orderID string) error {
	db := dbutils.GetDB(cdb, r.database)

	query := `UPDATE orderitems oi
              INNER JOIN orders o on o.order_id = oi.order_id
              INNER JOIN stripe_payments sp on o.order_id = sp.order_id
              SET oi.isPaid = true, oi.paid_quantity = oi.quantity
              WHERE sp.checkout_session_id = ? AND o.order_id = ?`
	_, err := db.ExecContext(cdb, query, checkoutSessionID, orderID)
	return err
}

func (r *mysqlRepo) UpdateOrderCreationDate(cdb context.Context, orderID string) error {
	db := dbutils.GetDB(cdb, r.database)

	query := `UPDATE orders SET creation_date = UTC_TIMESTAMP() WHERE order_id = ?`
	_, err := db.ExecContext(cdb, query, orderID)
	return err
}

func (r *mysqlRepo) GetOrder(cdb context.Context, orderID string) (*Order, error) {
	db := dbutils.GetDB(cdb, r.database)
	log := logger.FromContext(cdb)

	var o Order
	query := `SELECT order_id, price, creation_date, customer_id FROM orders WHERE order_id = ?`
	err := db.QueryRowContext(cdb, query, orderID).Scan(&o.OrderID, &o.Price, &o.CreationDate, &o.CustomerID)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	return &o, nil
}

func (r *mysqlRepo) GetMerchant(cdb context.Context, merchantID string) (*Merchant, error) {
	db := dbutils.GetDB(cdb, r.database)
	log := logger.FromContext(cdb)

	var m Merchant
	var logo sql.NullString
	query := `SELECT m.id, m.fullName, m.timezone, mp.currency, qr.code, m.logo_url
	          FROM merchant m
	          INNER JOIN merchant_parameters mp on mp.merchant_id = m.id
	          LEFT JOIN qrcodes qr on qr.merchant_id = m.id
	          WHERE m.id = ? LIMIT 1`
	err := db.QueryRowContext(cdb, query, merchantID).Scan(&m.ID, &m.BusinessName, &m.Timezone, &m.Currency, &m.Code, &logo)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	m.LogoURL = "http://storage.welloresto.fr/img/defaults/wr_logo_invoice.png"
	if logo.Valid && logo.String != "" {
		m.LogoURL = logo.String
	}
	return &m, nil
}

func (r *mysqlRepo) GetAutoAcceptSettings(cdb context.Context, orderID, merchantID string) (string, *Merchant, error) {
	db := dbutils.GetDB(cdb, r.database)

	var m Merchant
	var orderType string
	// Attention: MySQL retourne les booléens comme 1 ou 0 (int ou string selon driver)
	// On simplifie la query pour récupérer les flags
	query := `SELECT mp.auto_accept_sno_delivery_orders, mp.auto_accept_sno_take_away_orders, o.order_type
              FROM orders o
              INNER JOIN merchant_parameters mp on mp.merchant_id = o.merchant_id
              WHERE o.order_id = ? AND o.merchant_id = ?`

	err := db.QueryRowContext(cdb, query, orderID, merchantID).Scan(&m.AutoAcceptDelivery, &m.AutoAcceptTakeaway, &orderType)
	return orderType, &m, err
}

// --- Customer Logic ---
func (r *mysqlRepo) FindCustomer(cdb context.Context, email, merchantID string) (*Customer, error) {
	db := dbutils.GetDB(cdb, r.database)

	var c Customer
	query := `SELECT customer_id, customer_name, customer_email, customer_address FROM customer 
	          WHERE customer_email = ? AND merchant_id = ?`
	err := db.QueryRowContext(cdb, query, email, merchantID).Scan(&c.ID, &c.Name, &c.Email, &c.Address)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &c, err
}

func (r *mysqlRepo) CreateCustomer(cdb context.Context, c Customer, merchantID string) (int64, error) {
	db := dbutils.GetDB(cdb, r.database)
	log := logger.FromContext(cdb)

	query := `INSERT INTO customer (customer_name, customer_email, customer_address, merchant_id) VALUES (?, ?, ?, ?)`
	res, err := db.ExecContext(cdb, query, c.Name, c.Email, c.Address, merchantID)
	if err != nil {
		log.Error(err.Error())
		return 0, err
	}
	return res.LastInsertId()
}

func (r *mysqlRepo) UpdateCustomer(cdb context.Context, c Customer) error {
	db := dbutils.GetDB(cdb, r.database)

	query := `UPDATE customer SET customer_email = ?, customer_name = COALESCE(customer_name, ?), customer_address = COALESCE(?, customer_address) WHERE customer_id = ?`
	_, err := db.ExecContext(cdb, query, c.Email, c.Name, c.Address, c.ID)
	return err
}

func (r *mysqlRepo) UpdateOrderCustomer(cdb context.Context, orderID string, customerID int64) error {
	db := dbutils.GetDB(cdb, r.database)

	_, err := db.ExecContext(cdb, "UPDATE orders SET customer_id = ? WHERE order_id = ?", customerID, orderID)
	return err
}

// --- Fees & Intents ---

func (r *mysqlRepo) GetAccountIDByPaymentIntent(cdb context.Context, paymentIntentID string) (string, error) {
	db := dbutils.GetDB(cdb, r.database)

	var accountID string
	query := `SELECT sa.account_id
	          FROM stripe_payments sp
	          INNER JOIN payments p on sp.payment_id = p.payment_id
	          INNER JOIN stripe_accounts sa on sa.merchant_id = p.merchant_id
	          WHERE sp.payment_intent_id = ?`
	err := db.QueryRowContext(cdb, query, paymentIntentID).Scan(&accountID)
	return accountID, err
}

func (r *mysqlRepo) UpdateFees(cdb context.Context, paymentIntentID string, wrFees, stripeFees, totalFee int64) error {
	db := dbutils.GetDB(cdb, r.database)
	log := logger.FromContext(cdb)

	_, err := db.ExecContext(cdb, `UPDATE stripe_payments SET wello_resto_total_fees = ?, stripe_total_fees = ? WHERE payment_intent_id = ?`, wrFees, stripeFees, paymentIntentID)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	_, err = db.ExecContext(cdb, `UPDATE payments p INNER JOIN stripe_payments sp on sp.payment_id = p.payment_id SET p.fee = ? WHERE sp.payment_intent_id = ?`, totalFee, paymentIntentID)
	return err
}

func (r *mysqlRepo) UpdatePaymentIntentStatus(cdb context.Context, paymentIntentID, status string) error {
	db := dbutils.GetDB(cdb, r.database)

	_, err := db.ExecContext(cdb, `UPDATE stripe_payments SET payment_intent_status = ? WHERE payment_intent_id = ?`, status, paymentIntentID)
	return err
}

func (r *mysqlRepo) DisablePayment(cdb context.Context, paymentIntentID string) error {
	db := dbutils.GetDB(cdb, r.database)

	query := `UPDATE payments p 
	          INNER JOIN stripe_payments sp on sp.payment_id = p.payment_id
	          SET p.enabled = '0' 
	          WHERE sp.payment_intent_id = ?`
	_, err := db.ExecContext(cdb, query, paymentIntentID)
	return err
}

// --- Subscription ---
func (r *mysqlRepo) CreateInvoice(cdb context.Context, merchantID, invoiceID string, amount int64, created int64, customerID string) error {
	db := dbutils.GetDB(cdb, r.database)

	// Note: Conversion of `created` timestamp to string date handled here or in SQL
	query := `INSERT INTO subscription_invoices(merchant_id, invoice_id, invoice_date, amount)
			  SELECT ?, ?, FROM_UNIXTIME(?), ?
			  FROM welloresto_stripe_customers WHERE stripe_customer_id = ?`
	_, err := db.ExecContext(cdb, query, merchantID, invoiceID, created, amount, customerID)
	return err
}

func (r *mysqlRepo) PayInvoice(cdb context.Context, invoiceID string, paidAt int64) error {
	db := dbutils.GetDB(cdb, r.database)

	query := `UPDATE subscription_invoices SET status = '1', payment_date = FROM_UNIXTIME(?) WHERE invoice_id = ?`
	_, err := db.ExecContext(cdb, query, paidAt, invoiceID)
	return err
}

// UpdateStripeAccountVerificationStatus caches the Connect account status after an account.updated webhook.
func (r *mysqlRepo) UpdateStripeAccountVerificationStatus(cdb context.Context, accountID, status string) error {
	db := dbutils.GetDB(cdb, r.database)

	_, err := db.ExecContext(cdb,
		`UPDATE stripe_accounts SET verification_status = ? WHERE account_id = ?`,
		status, accountID,
	)
	return err
}
