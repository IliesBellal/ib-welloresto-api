package stripe

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
)

type Repository interface {

	// Transactional methods (accept *sql.db)
	InsertPayment(cdb context.Context, p Payment) (int64, error)
	InsertStripePayment(cdb context.Context, sp StripePayment) error
	UpdateOrderPaymentStatus(cdb context.Context, orderID string) error
	UpdateOrderDetails(cdb context.Context, checkoutSessionID, orderID string) error
	UpdateOrderItemsPaid(cdb context.Context, checkoutSessionID, orderID string) error
	UpdateOrderCreationDate(cdb context.Context, orderID string) error
	// ConfirmKioskCardPayment fait transiter brand_status de PENDING_CARD_PAYMENT
	// vers PENDING pour une commande Kiosk dont le paiement Terminal a réussi.
	// Ne touche jamais merchant_approval (déjà ACCEPTED depuis la création côté
	// Kiosk, voir docs/KIOSK_DECISIONS.md). Guard WHERE brand_status =
	// 'PENDING_CARD_PAYMENT' : idempotent si le webhook est rejoué après une
	// transition déjà effectuée. Retourne true si une ligne a été modifiée.
	ConfirmKioskCardPayment(cdb context.Context, merchantID, orderID string) (bool, error)
	// LockOrderForUpdate verrouille (FOR UPDATE) la ligne orders le temps de
	// la transaction englobante — sérialise deux payment_intent.succeeded
	// concurrents pour la même commande (guard par PaymentIntent, voir
	// StripeWebhookService.handleTerminalPaymentSucceeded et
	// docs/KIOSK_DECISIONS.md).
	LockOrderForUpdate(cdb context.Context, merchantID, orderID string) error
	// GetCapturedPaymentIntentForOrder retourne le payment_intent_id le plus
	// récent marqué CAPTURED pour cette commande, s'il existe. Utilisé par le
	// guard par PaymentIntent : distingue un replay légitime (même PI) d'un
	// second PaymentIntent qui aurait déjà capturé la commande (scénario de
	// double-encaissement documenté, voir docs/KIOSK_DECISIONS.md).
	GetCapturedPaymentIntentForOrder(cdb context.Context, merchantID, orderID string) (paymentIntentID string, found bool, err error)
	// GetOrderMerchantKioskForPaymentIntent résout order_id/merchant_id/kiosk_id
	// (nullable) à partir d'un payment_intent_id — jointure stripe_payments/orders.
	// kiosk_id est posé au moment du dispatch reader (voir
	// stripeclient.TerminalPaymentStore.SetKioskIDForPaymentIntent) : c'est ce
	// qui permet au webhook de résoudre quelle borne notifier sans passer par
	// un lookup reader->kiosk (docs/TERMINAL_SERVER_DRIVEN_CONTRACT.md).
	GetOrderMerchantKioskForPaymentIntent(cdb context.Context, paymentIntentID string) (orderID, merchantID string, kioskID *string, found bool, err error)
	// GetReaderIDForKiosk résout le reader appairé d'un kiosk
	// (kiosks.stripe_reader_id) — nécessaire pour recalculer le statut via
	// stripeclient.TerminalService.GetPaymentStatus avant tout push d'échec
	// (voir StripeWebhookService.pushTerminalPaymentUpdateRecomputed).
	GetReaderIDForKiosk(cdb context.Context, kioskID string) (readerID string, found bool, err error)
	// SetTerminalCardDetails écrit les 5 colonnes carte (brand/last4/receipt)
	// sur la ligne stripe_payments du PaymentIntent, relues depuis
	// pi.LatestCharge (expand) à payment_intent.succeeded.
	SetTerminalCardDetails(cdb context.Context, paymentIntentID, brand, last4, applicationPreferredName, dedicatedFileName, authorizationCode string) error
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

	// Subscription (LOT B B2a-0 : remplace l'ancien CreateInvoice/PayInvoice,
	// qui écrivaient subscription_invoices — confirmé sans aucun lecteur
	// applicatif, voir docs/decisions.md — au profit du modèle LOT B
	// B1b/B1c, subscriptions.status/current_period_end)
	SetSubscriptionStatus(cdb context.Context, merchantID, status string) error
	UpdateSubscriptionBillingPeriod(cdb context.Context, merchantID string, periodEndUnix int64) error
	// GetMerchantIDByStripeSubscriptionID — LOT B F3 : resolves merchantID
	// from a Stripe Subscription id, empty string if none matches. See its
	// doc comment on the concrete implementation for why this exists.
	GetMerchantIDByStripeSubscriptionID(cdb context.Context, stripeSubscriptionID string) (string, error)

	GetMerchantByStripeAccountID(cdb context.Context, accountID string) (*PayoutMerchant, error)

	// Connect account status
	UpdateStripeAccountVerificationStatus(cdb context.Context, accountID, status string) error
	GetMerchantIDByStripeAccountID(cdb context.Context, accountID string) (string, error)
	SetScanNOrderActivated(cdb context.Context, merchantID string, activated bool) error

	// Idempotence webhook (stripe_webhook_events, voir
	// docs/KIOSK_DECISIONS.md) — StripeWebhookService.ProcessEvent est le
	// point d'entrée unique du endpoint /webhooks/stripe, gardé par ces deux
	// méthodes avant tout dispatch par type d'event.
	//
	// MarkEventProcessed insère event.id. alreadyProcessed=true si la ligne
	// existait déjà (event rejoué par Stripe) : ProcessEvent doit retourner
	// nil (200) sans retraiter.
	MarkEventProcessed(cdb context.Context, eventID, eventType string) (alreadyProcessed bool, err error)
	// DeleteProcessedEvent retire la marque posée par MarkEventProcessed —
	// appelée quand le traitement de l'event échoue ensuite, pour qu'un retry
	// Stripe légitime (delivery différente du même event) puisse reprocesser
	// plutôt que de rester bloqué derrière un faux "déjà traité".
	DeleteProcessedEvent(cdb context.Context, eventID string) error
}

type mysqlRepo struct {
	database *sql.DB
}

func NewRepository(db *sql.DB) Repository {
	return &mysqlRepo{database: db}
}

// Implémentations

func (r *mysqlRepo) GetMerchantByStripeAccountID(cdb context.Context, accountID string) (*PayoutMerchant, error) {
	db := dbx.GetDB(cdb, r.database)
	log := logger.FromContext(cdb)

	var pm PayoutMerchant

	// merchant.id is an integer identity while stripe_accounts.merchant_id is
	// varchar (merchant_id is carried as a string everywhere else in the Go
	// code, see 12-merchant-id-unification.md) — MySQL implicitly casts
	// across the join, Postgres requires an explicit one, and CAST syntax
	// itself differs per dialect (CHAR vs TEXT).
	joinCast := "CAST(m.id AS CHAR)"
	if dbx.ActiveDialect() == dbx.Postgres {
		joinCast = "CAST(m.id AS TEXT)"
	}
	query := fmt.Sprintf(`
        SELECT m.email, m.fullName
        FROM stripe_accounts sa
        INNER JOIN merchant m on sa.merchant_id = %s
        WHERE sa.account_id = ?
        AND m.email IS NOT NULL
    `, joinCast)

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

// Decom
func (r *mysqlRepo) InsertPayment(cdb context.Context, p Payment) (int64, error) {
	db := dbx.GetDB(cdb, r.database)
	log := logger.FromContext(cdb)

	query := fmt.Sprintf(`INSERT INTO payments(merchant_id, order_id, user_id, amount, mop, payment_date)
	          VALUES(?, ?, ?, ?, ?, %s)`, dbx.UTCNow())
	id, err := db.InsertReturningID(cdb, query, "payment_id", p.MerchantID, p.OrderID, models.StripeWebhookUserID, p.Amount, models.StripeMOP)
	if err != nil {
		log.Error(err.Error())
		return 0, err
	}
	return id, nil
}

func (r *mysqlRepo) InsertStripePayment(cdb context.Context, sp StripePayment) error {
	db := dbx.GetDB(cdb, r.database)

	query := fmt.Sprintf(`INSERT INTO stripe_payments(order_id, payment_id, payment_intent_id, checkout_session_id, customer_email, stripe_session_date)
	          VALUES(?, ?, ?, ?, ?, %s)`, dbx.UTCNow())
	_, err := db.ExecContext(cdb, query, sp.OrderID, sp.PaymentID, sp.PaymentIntentID, sp.CheckoutSessionID, sp.CustomerEmail)
	return err
}

func (r *mysqlRepo) UpdateOrderPaymentStatus(cdb context.Context, orderID string) error {
	db := dbx.GetDB(cdb, r.database)

	// Logique complexe de SUM convertie. Réécrite en sous-requête scalaire
	// corrélée (portable MySQL/Postgres) plutôt qu'en UPDATE...JOIN — MySQL
	// n'a pas d'équivalent direct au UPDATE...FROM de Postgres, mais cette
	// forme évite le problème : elle est valide telle quelle sur les deux
	// dialectes.
	// Postgres does not allow qualifying SET target columns with the table
	// alias (unlike MySQL) — the alias remains usable in the subquery/WHERE.
	query := fmt.Sprintf(`UPDATE orders o
              SET isPaid = (o.price <= COALESCE((
                  SELECT SUM(p.amount) FROM payments p WHERE p.enabled = true AND p.order_id = o.order_id
              ), 0)),
                  creation_date = %s
              WHERE o.order_id = ?`, dbx.UTCNow())
	_, err := db.ExecContext(cdb, query, orderID)
	return err
}

func (r *mysqlRepo) UpdateOrderDetails(cdb context.Context, checkoutSessionID, orderID string) error {
	db := dbx.GetDB(cdb, r.database)

	// UPDATE...JOIN rewritten as EXISTS (portable MySQL/Postgres). Postgres
	// does not allow qualifying SET target columns with the table alias.
	query := fmt.Sprintf(`UPDATE orders o
              SET brand_status = 'PENDING_APPROVAL',
                  merchant_approval = 'PENDING_APPROVAL',
                  last_update = %s
              WHERE o.order_id = ?
                AND EXISTS (SELECT 1 FROM stripe_payments sp WHERE sp.order_id = o.order_id AND sp.checkout_session_id = ?)`, dbx.UTCNow())
	_, err := db.ExecContext(cdb, query, orderID, checkoutSessionID)
	return err
}

func (r *mysqlRepo) UpdateOrderItemsPaid(cdb context.Context, checkoutSessionID, orderID string) error {
	db := dbx.GetDB(cdb, r.database)

	// UPDATE...JOIN rewritten as EXISTS (portable MySQL/Postgres).
	query := `UPDATE orderitems oi
              SET isPaid = true, paid_quantity = oi.quantity
              WHERE oi.order_id = ?
                AND EXISTS (SELECT 1 FROM stripe_payments sp WHERE sp.order_id = oi.order_id AND sp.checkout_session_id = ?)`
	_, err := db.ExecContext(cdb, query, orderID, checkoutSessionID)
	return err
}

func (r *mysqlRepo) UpdateOrderCreationDate(cdb context.Context, orderID string) error {
	db := dbx.GetDB(cdb, r.database)

	query := fmt.Sprintf(`UPDATE orders SET creation_date = %s WHERE order_id = ?`, dbx.UTCNow())
	_, err := db.ExecContext(cdb, query, orderID)
	return err
}

func (r *mysqlRepo) ConfirmKioskCardPayment(cdb context.Context, merchantID, orderID string) (bool, error) {
	db := dbx.GetDB(cdb, r.database)

	query := fmt.Sprintf(`UPDATE orders SET brand_status = 'PENDING', last_update = %s
              WHERE order_id = ? AND merchant_id = ? AND brand_status = 'PENDING_CARD_PAYMENT'`, dbx.UTCNow())
	res, err := db.ExecContext(cdb, query, orderID, merchantID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (r *mysqlRepo) LockOrderForUpdate(cdb context.Context, merchantID, orderID string) error {
	db := dbx.GetDB(cdb, r.database)

	var discard string
	err := db.QueryRowContext(cdb, `SELECT order_id FROM orders WHERE order_id = ? AND merchant_id = ? FOR UPDATE`, orderID, merchantID).Scan(&discard)
	if errors.Is(err, sql.ErrNoRows) {
		// Commande introuvable pour ce merchant : rien à verrouiller — les
		// étapes suivantes de la transaction (ConfirmKioskCardPayment, etc.)
		// échoueront proprement d'elles-mêmes (0 ligne affectée).
		return nil
	}
	return err
}

func (r *mysqlRepo) GetCapturedPaymentIntentForOrder(cdb context.Context, merchantID, orderID string) (string, bool, error) {
	db := dbx.GetDB(cdb, r.database)
	const q = `
		SELECT sp.payment_intent_id
		FROM stripe_payments sp
		INNER JOIN orders o ON o.order_id = sp.order_id
		WHERE sp.order_id = ? AND o.merchant_id = ?
		  AND sp.payment_intent_status = 'CAPTURED'
		ORDER BY sp.id DESC
		LIMIT 1`
	var piID string
	err := db.QueryRowContext(cdb, q, orderID, merchantID).Scan(&piID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return piID, true, nil
}

func (r *mysqlRepo) GetOrderMerchantKioskForPaymentIntent(cdb context.Context, paymentIntentID string) (string, string, *string, bool, error) {
	db := dbx.GetDB(cdb, r.database)
	const q = `
		SELECT sp.order_id, o.merchant_id, sp.kiosk_id
		FROM stripe_payments sp
		INNER JOIN orders o ON o.order_id = sp.order_id
		WHERE sp.payment_intent_id = ?
		ORDER BY sp.id DESC
		LIMIT 1`
	var orderID, merchantID string
	var kioskID sql.NullString
	err := db.QueryRowContext(cdb, q, paymentIntentID).Scan(&orderID, &merchantID, &kioskID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil, false, nil
	}
	if err != nil {
		return "", "", nil, false, err
	}
	if kioskID.Valid {
		return orderID, merchantID, &kioskID.String, true, nil
	}
	return orderID, merchantID, nil, true, nil
}

func (r *mysqlRepo) GetReaderIDForKiosk(cdb context.Context, kioskID string) (string, bool, error) {
	db := dbx.GetDB(cdb, r.database)
	var readerID sql.NullString
	err := db.QueryRowContext(cdb, `SELECT stripe_reader_id FROM kiosks WHERE id = ?`, kioskID).Scan(&readerID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !readerID.Valid || readerID.String == "" {
		return "", false, nil
	}
	return readerID.String, true, nil
}

func (r *mysqlRepo) SetTerminalCardDetails(cdb context.Context, paymentIntentID, brand, last4, applicationPreferredName, dedicatedFileName, authorizationCode string) error {
	db := dbx.GetDB(cdb, r.database)
	_, err := db.ExecContext(cdb,
		`UPDATE stripe_payments SET card_brand = ?, card_last4 = ?, card_application_preferred_name = ?, card_dedicated_file_name = ?, card_authorization_code = ? WHERE payment_intent_id = ?`,
		brand, last4, applicationPreferredName, dedicatedFileName, authorizationCode, paymentIntentID)
	return err
}

func (r *mysqlRepo) GetOrder(cdb context.Context, orderID string) (*Order, error) {
	db := dbx.GetDB(cdb, r.database)
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
	db := dbx.GetDB(cdb, r.database)
	log := logger.FromContext(cdb)

	var m Merchant
	var logo sql.NullString
	// merchant.id is an integer identity while merchant_parameters.merchant_id
	// / qrcodes.merchant_id are varchar — same cross-type join issue as
	// GetMerchantByStripeAccountID above.
	joinCast := "CAST(m.id AS CHAR)"
	if dbx.ActiveDialect() == dbx.Postgres {
		joinCast = "CAST(m.id AS TEXT)"
	}
	query := fmt.Sprintf(`SELECT m.id, m.fullName, m.timezone, mp.currency, qr.code, m.logo_url
	          FROM merchant m
	          INNER JOIN merchant_parameters mp on mp.merchant_id = %[1]s
	          LEFT JOIN qrcodes qr on qr.merchant_id = %[1]s
	          WHERE m.id = ? LIMIT 1`, joinCast)
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
	db := dbx.GetDB(cdb, r.database)

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
	db := dbx.GetDB(cdb, r.database)

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
	db := dbx.GetDB(cdb, r.database)
	log := logger.FromContext(cdb)

	query := `INSERT INTO customer (customer_name, customer_email, customer_address, merchant_id) VALUES (?, ?, ?, ?)`
	id, err := db.InsertReturningID(cdb, query, "customer_id", c.Name, c.Email, c.Address, merchantID)
	if err != nil {
		log.Error(err.Error())
		return 0, err
	}
	return id, nil
}

func (r *mysqlRepo) UpdateCustomer(cdb context.Context, c Customer) error {
	db := dbx.GetDB(cdb, r.database)

	query := `UPDATE customer SET customer_email = ?, customer_name = COALESCE(customer_name, ?), customer_address = COALESCE(?, customer_address) WHERE customer_id = ?`
	_, err := db.ExecContext(cdb, query, c.Email, c.Name, c.Address, c.ID)
	return err
}

func (r *mysqlRepo) UpdateOrderCustomer(cdb context.Context, orderID string, customerID int64) error {
	db := dbx.GetDB(cdb, r.database)

	_, err := db.ExecContext(cdb, "UPDATE orders SET customer_id = ? WHERE order_id = ?", customerID, orderID)
	return err
}

// --- Fees & Intents ---

func (r *mysqlRepo) GetAccountIDByPaymentIntent(cdb context.Context, paymentIntentID string) (string, error) {
	db := dbx.GetDB(cdb, r.database)

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
	db := dbx.GetDB(cdb, r.database)
	log := logger.FromContext(cdb)

	_, err := db.ExecContext(cdb, `UPDATE stripe_payments SET wello_resto_total_fees = ?, stripe_total_fees = ? WHERE payment_intent_id = ?`, wrFees, stripeFees, paymentIntentID)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	// net_amount = amount - fee : montant réellement encaissé par le merchant une
	// fois les frais Stripe (+ commission plateforme) déduits. Renseigne les
	// paiements en ligne (Checkout) comme Terminal (card_present), qui passent
	// tous deux par ce même mécanisme charge.captured -> UpdateFees.
	// UPDATE...JOIN rewritten as EXISTS (portable MySQL/Postgres).
	_, err = db.ExecContext(cdb, `UPDATE payments p SET fee = ?, net_amount = p.amount - ?
	          WHERE EXISTS (SELECT 1 FROM stripe_payments sp WHERE sp.payment_id = p.payment_id AND sp.payment_intent_id = ?)`,
		totalFee, totalFee, paymentIntentID)
	return err
}

func (r *mysqlRepo) UpdatePaymentIntentStatus(cdb context.Context, paymentIntentID, status string) error {
	db := dbx.GetDB(cdb, r.database)

	_, err := db.ExecContext(cdb, `UPDATE stripe_payments SET payment_intent_status = ? WHERE payment_intent_id = ?`, status, paymentIntentID)
	return err
}

func (r *mysqlRepo) DisablePayment(cdb context.Context, paymentIntentID string) error {
	db := dbx.GetDB(cdb, r.database)

	// UPDATE...JOIN rewritten as EXISTS (portable MySQL/Postgres).
	query := `UPDATE payments p
	          SET enabled = false
	          WHERE EXISTS (SELECT 1 FROM stripe_payments sp WHERE sp.payment_id = p.payment_id AND sp.payment_intent_id = ?)`
	_, err := db.ExecContext(cdb, query, paymentIntentID)
	return err
}

// --- Subscription (LOT B B2a-0) ---

// SetSubscriptionStatus writes subscriptions.status for merchantID —
// invoice.paid's confirmation that the current billing period was actually
// collected.
func (r *mysqlRepo) SetSubscriptionStatus(cdb context.Context, merchantID, status string) error {
	db := dbx.GetDB(cdb, r.database)
	_, err := db.ExecContext(cdb, `UPDATE subscriptions SET status = ? WHERE merchant_id = ?`, status, merchantID)
	return err
}

// UpdateSubscriptionBillingPeriod writes subscriptions.current_period_end
// for merchantID from a Stripe Invoice's period_end (Unix epoch). Postgres
// only (to_timestamp) — the sole live dialect, see CLAUDE.md — unlike the
// FROM_UNIXTIME/to_timestamp split this replaces.
func (r *mysqlRepo) UpdateSubscriptionBillingPeriod(cdb context.Context, merchantID string, periodEndUnix int64) error {
	db := dbx.GetDB(cdb, r.database)
	_, err := db.ExecContext(cdb, `UPDATE subscriptions SET current_period_end = to_timestamp(?) WHERE merchant_id = ?`, periodEndUnix, merchantID)
	return err
}

// GetMerchantIDByStripeSubscriptionID — LOT B F3 (docs/decisions.md) : found
// necessary by running a real invoice.created/invoice.paid webhook cycle
// end-to-end for the first time (stripe listen was never available before —
// only direct API reads had been done, in B2c-0). invoice.Metadata is
// EMPTY on every real Invoice Stripe generates from a Subscription —
// Stripe does not copy a Subscription's metadata onto its invoices, contrary
// to what HandleInvoiceCreated/HandleInvoicePaid/HandleInvoicePaymentFailed
// assumed. invoice.Subscription.ID (a bare id reference, always present even
// unexpanded — same shape as the PaymentMethod finding in B2b-0) is the
// correlation key that actually works: subscriptions.stripe_subscription_id
// already stores it (B2c-0), unambiguously even for a merchant sharing a
// mutualized platform_billing_customers.stripe_customer_id with another
// merchant (invoice.Customer.ID would NOT be unambiguous in that case — each
// merchant still gets its own distinct Stripe Subscription regardless of
// which Customer bills it).
func (r *mysqlRepo) GetMerchantIDByStripeSubscriptionID(cdb context.Context, stripeSubscriptionID string) (string, error) {
	if stripeSubscriptionID == "" {
		return "", nil
	}
	db := dbx.GetDB(cdb, r.database)
	var merchantID string
	err := db.QueryRowContext(cdb, `SELECT merchant_id FROM subscriptions WHERE stripe_subscription_id = ?`, stripeSubscriptionID).Scan(&merchantID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return merchantID, err
}

// UpdateStripeAccountVerificationStatus caches the Connect account status after an account.updated webhook.
func (r *mysqlRepo) UpdateStripeAccountVerificationStatus(cdb context.Context, accountID, status string) error {
	db := dbx.GetDB(cdb, r.database)

	_, err := db.ExecContext(cdb,
		`UPDATE stripe_accounts SET verification_status = ? WHERE account_id = ?`,
		status, accountID,
	)
	return err
}

func (r *mysqlRepo) GetMerchantIDByStripeAccountID(cdb context.Context, accountID string) (string, error) {
	db := dbx.GetDB(cdb, r.database)

	var merchantID string
	err := db.QueryRowContext(cdb,
		`SELECT merchant_id FROM stripe_accounts WHERE account_id = ? LIMIT 1`,
		accountID,
	).Scan(&merchantID)
	if err != nil {
		return "", err
	}

	return merchantID, nil
}

func (r *mysqlRepo) SetScanNOrderActivated(cdb context.Context, merchantID string, activated bool) error {
	db := dbx.GetDB(cdb, r.database)

	_, err := db.ExecContext(cdb,
		`UPDATE scannorder_settings SET activated = ? WHERE merchant_id = ?`,
		activated, merchantID,
	)
	return err
}

// --- Idempotence webhook (stripe_webhook_events) ---

// MarkEventProcessed — ON CONFLICT DO NOTHING : syntaxe Postgres, seul
// dialecte vivant (voir CLAUDE.md). alreadyProcessed=true (0 ligne insérée)
// signifie que ce event.id a déjà une marque, posée par une delivery Stripe
// antérieure du même event (replay).
func (r *mysqlRepo) MarkEventProcessed(cdb context.Context, eventID, eventType string) (bool, error) {
	db := dbx.GetDB(cdb, r.database)

	res, err := db.ExecContext(cdb,
		`INSERT INTO stripe_webhook_events(event_id, event_type) VALUES (?, ?) ON CONFLICT (event_id) DO NOTHING`,
		eventID, eventType)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 0, nil
}

func (r *mysqlRepo) DeleteProcessedEvent(cdb context.Context, eventID string) error {
	db := dbx.GetDB(cdb, r.database)

	_, err := db.ExecContext(cdb, `DELETE FROM stripe_webhook_events WHERE event_id = ?`, eventID)
	return err
}
