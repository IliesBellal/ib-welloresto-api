package customer_accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/models"
)

// Repository lit les comptes clients. Les écritures passent par
// internal/customeraccount (scellement, verrous).
type Repository struct {
	database *sql.DB
}

func NewRepository(database *sql.DB) *Repository {
	return &Repository{database: database}
}

// DB renvoie la base, ou la transaction portée par ctx.
func (r *Repository) DB(ctx context.Context) *dbx.DB {
	return dbx.GetDB(ctx, r.database)
}

// CustomerBelongsToMerchant : le client existe, est actif et appartient à
// l'établissement.
func (r *Repository) CustomerBelongsToMerchant(ctx context.Context, merchantID string, customerID int64) (bool, error) {
	var ok bool
	err := r.DB(ctx).QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM customer WHERE customer_id = ? AND merchant_id = ? AND enabled = TRUE)`,
		customerID, merchantID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("customer_accounts: customer: %w", err)
	}
	return ok, nil
}

// RegisterIsOpen : le registre existe et n'est pas fermé. Verrou partagé de la
// ligne jusqu'au commit : la fermeture du registre (qui la verrouille en
// exclusif avant de calculer son Z) attend la fin de l'opération.
func (r *Repository) RegisterIsOpen(ctx context.Context, cashRegisterID string) (bool, error) {
	var closed bool
	err := r.DB(ctx).QueryRowContext(ctx, `
		SELECT closed FROM cash_registers WHERE cash_register_id = ? FOR SHARE`, cashRegisterID).Scan(&closed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("customer_accounts: register: %w", err)
	}
	return !closed, nil
}

// Entries renvoie l'historique d'un compte, du plus récent au plus ancien,
// avec le solde après chaque ligne : mouvements et commandes réglées sur le
// compte (paiements actifs).
func (r *Repository) Entries(ctx context.Context, merchantID string, customerID int64) ([]models.CustomerAccountEntry, error) {
	db := r.DB(ctx)
	var entries []models.CustomerAccountEntry

	rows, err := db.QueryContext(ctx, `
		SELECT o.operation_id, o.operation_type, o.amount, o.mop, o.comment, o.user_id,
		       COALESCE(NULLIF(TRIM(CONCAT_WS(' ', u.first_name, u.last_name)), ''), ''),
		       o.cash_register_id, o.operation_date, COALESCE(o.hash, '')
		FROM customer_account_operations o
		LEFT JOIN users u ON u.user_id = o.user_id
		WHERE o.merchant_id = ? AND o.customer_id = ?`, merchantID, customerID)
	if err != nil {
		return nil, fmt.Errorf("customer_accounts: operations: %w", err)
	}
	for rows.Next() {
		op := models.CustomerAccountOperation{CustomerID: customerID}
		var mop, comment, register sql.NullString
		var at time.Time
		if err := rows.Scan(&op.OperationID, &op.OperationType, &op.Amount, &mop, &comment, &op.UserID, &op.UserName,
			&register, &at, &op.Hash); err != nil {
			rows.Close()
			return nil, fmt.Errorf("customer_accounts: operations: %w", err)
		}
		op.MOP = nullable(mop)
		op.Comment = nullable(comment)
		op.CashRegisterID = nullable(register)
		op.OperationDate = at.Unix()
		o := op
		entries = append(entries, models.CustomerAccountEntry{Kind: "operation", Date: o.OperationDate, Delta: o.Amount, Operation: &o})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Numéro du ticket de vente de la commande (relevé de compte).
	rows, err = db.QueryContext(ctx, `
		SELECT p.payment_id, p.order_id, COALESCE(o.order_num::text, ''), p.amount, p.payment_date,
		       COALESCE((SELECT r.receipt_number FROM receipts r
		                 WHERE r.order_id = p.order_id AND r.total_ttc >= 0
		                 ORDER BY r.created_at LIMIT 1), '')
		FROM payments p
		LEFT JOIN orders o ON o.order_id = p.order_id
		WHERE p.merchant_id = ? AND p.customer_id = ? AND p.mop = 'ACCOUNT' AND p.enabled = TRUE`, merchantID, customerID)
	if err != nil {
		return nil, fmt.Errorf("customer_accounts: payments: %w", err)
	}
	for rows.Next() {
		p := models.CustomerAccountPayment{Enabled: true}
		var at time.Time
		if err := rows.Scan(&p.PaymentID, &p.OrderID, &p.OrderNum, &p.Amount, &at, &p.ReceiptNumber); err != nil {
			rows.Close()
			return nil, fmt.Errorf("customer_accounts: payments: %w", err)
		}
		p.PaymentDate = at.Unix()
		pay := p
		// Un paiement débite le compte ; un remboursement (montant négatif) le recrédite.
		entries = append(entries, models.CustomerAccountEntry{Kind: "payment", Date: pay.PaymentDate, Delta: -pay.Amount, Payment: &pay})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return withBalances(entries), nil
}

// withBalances trie l'historique par date, calcule le solde après chaque
// ligne, puis le renvoie du plus récent au plus ancien.
func withBalances(entries []models.CustomerAccountEntry) []models.CustomerAccountEntry {
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Date < entries[j].Date })
	var balance int64
	for i := range entries {
		balance += entries[i].Delta
		entries[i].BalanceAfter = balance
	}
	out := make([]models.CustomerAccountEntry, len(entries))
	for i := range entries {
		out[len(entries)-1-i] = entries[i]
	}
	return out
}

// NonZeroAccounts liste les comptes non soldés de l'établissement, dettes en
// premier.
func (r *Repository) NonZeroAccounts(ctx context.Context, merchantID string) ([]models.CustomerAccountSummary, error) {
	rows, err := r.DB(ctx).QueryContext(ctx, `
		WITH ops AS (
			SELECT customer_id, SUM(amount) AS amount, MAX(operation_date) AS last_at
			FROM customer_account_operations WHERE merchant_id = ? GROUP BY customer_id
		), pays AS (
			SELECT customer_id, SUM(amount) AS amount, MAX(payment_date) AS last_at
			FROM payments
			WHERE merchant_id = ? AND mop = 'ACCOUNT' AND enabled = TRUE AND customer_id IS NOT NULL
			GROUP BY customer_id
		), balances AS (
			SELECT COALESCE(ops.customer_id, pays.customer_id) AS customer_id,
			       COALESCE(ops.amount, 0) - COALESCE(pays.amount, 0) AS balance,
			       GREATEST(ops.last_at, pays.last_at) AS last_at
			FROM ops FULL JOIN pays ON pays.customer_id = ops.customer_id
		)
		SELECT b.customer_id, COALESCE(c.customer_first_name, ''), COALESCE(c.customer_last_name, ''),
		       COALESCE(c.customer_business_name, ''), COALESCE(c.customer_tel, ''), b.balance, b.last_at
		FROM balances b
		JOIN customer c ON c.customer_id = b.customer_id
		WHERE b.balance <> 0
		ORDER BY b.balance ASC, b.last_at DESC`, merchantID, merchantID)
	if err != nil {
		return nil, fmt.Errorf("customer_accounts: list: %w", err)
	}
	defer rows.Close()
	out := []models.CustomerAccountSummary{}
	for rows.Next() {
		var s models.CustomerAccountSummary
		var at sql.NullTime
		if err := rows.Scan(&s.CustomerID, &s.FirstName, &s.LastName, &s.BusinessName, &s.Phone, &s.Balance, &at); err != nil {
			return nil, fmt.Errorf("customer_accounts: list: %w", err)
		}
		if at.Valid {
			s.LastMovement = at.Time.Unix()
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Reminders renvoie les relances d'un client, plus récentes d'abord.
func (r *Repository) Reminders(ctx context.Context, merchantID string, customerID int64) ([]models.CustomerAccountReminder, error) {
	rows, err := r.DB(ctx).QueryContext(ctx, `
		SELECT sent_at, source, channels, balance FROM customer_account_reminders
		WHERE merchant_id = ? AND customer_id = ? ORDER BY sent_at DESC LIMIT 50`, merchantID, customerID)
	if err != nil {
		return nil, fmt.Errorf("customer_accounts: reminders: %w", err)
	}
	defer rows.Close()
	out := []models.CustomerAccountReminder{}
	for rows.Next() {
		var rm models.CustomerAccountReminder
		var at time.Time
		if err := rows.Scan(&at, &rm.Source, &rm.Channels, &rm.Balance); err != nil {
			return nil, fmt.Errorf("customer_accounts: reminders: %w", err)
		}
		rm.SentAt = at.Unix()
		out = append(out, rm)
	}
	return out, rows.Err()
}

// RemindersSince compte les relances d'un client depuis une date, et donne la
// dernière.
func (r *Repository) RemindersSince(ctx context.Context, merchantID string, customerID int64, since time.Time) (int, *time.Time, error) {
	var count int
	var last sql.NullTime
	err := r.DB(ctx).QueryRowContext(ctx, `
		SELECT COUNT(*), MAX(sent_at) FROM customer_account_reminders
		WHERE merchant_id = ? AND customer_id = ? AND sent_at >= ?`, merchantID, customerID, since.UTC()).Scan(&count, &last)
	if err != nil {
		return 0, nil, fmt.Errorf("customer_accounts: reminders since: %w", err)
	}
	if !last.Valid {
		return count, nil, nil
	}
	return count, &last.Time, nil
}

// RecordReminder inscrit une relance envoyée.
func (r *Repository) RecordReminder(ctx context.Context, merchantID string, customerID int64, source, channels string, balance int64, userID string) error {
	_, err := r.DB(ctx).ExecContext(ctx, `
		INSERT INTO customer_account_reminders (merchant_id, customer_id, source, channels, balance, user_id)
		VALUES (?, ?, ?, ?, ?, NULLIF(?, ''))`, merchantID, customerID, source, channels, balance, userID)
	if err != nil {
		return fmt.Errorf("customer_accounts: record reminder: %w", err)
	}
	return nil
}

// CustomerSummaries renvoie nom et téléphone de plusieurs clients.
func (r *Repository) CustomerSummaries(ctx context.Context, merchantID string, ids []int64) (map[int64]models.CustomerAccountSummary, error) {
	out := map[int64]models.CustomerAccountSummary{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.DB(ctx).QueryContext(ctx, `
		SELECT customer_id, COALESCE(customer_first_name, ''), COALESCE(customer_last_name, ''),
		       COALESCE(customer_business_name, ''), COALESCE(customer_tel, '')
		FROM customer WHERE merchant_id = ? AND customer_id = ANY(?)`, merchantID, ids)
	if err != nil {
		return nil, fmt.Errorf("customer_accounts: summaries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c models.CustomerAccountSummary
		if err := rows.Scan(&c.CustomerID, &c.FirstName, &c.LastName, &c.BusinessName, &c.Phone); err != nil {
			return nil, fmt.Errorf("customer_accounts: summaries: %w", err)
		}
		out[c.CustomerID] = c
	}
	return out, rows.Err()
}

// CustomerContact : de quoi écrire à un client.
type CustomerContact struct {
	Name  string
	Email string
	Phone string
}

// Contact renvoie le nom, l'email et le téléphone d'un client.
func (r *Repository) Contact(ctx context.Context, merchantID string, customerID int64) (CustomerContact, error) {
	var c CustomerContact
	var first, last, business string
	err := r.DB(ctx).QueryRowContext(ctx, `
		SELECT COALESCE(customer_first_name, ''), COALESCE(customer_last_name, ''), COALESCE(customer_business_name, ''),
		       COALESCE(customer_email, ''), COALESCE(customer_tel, '')
		FROM customer WHERE customer_id = ? AND merchant_id = ?`, customerID, merchantID).
		Scan(&first, &last, &business, &c.Email, &c.Phone)
	if errors.Is(err, sql.ErrNoRows) {
		return c, models.ErrNotFound
	}
	if err != nil {
		return c, fmt.Errorf("customer_accounts: contact: %w", err)
	}
	c.Name = strings.TrimSpace(first + " " + last)
	if c.Name == "" {
		c.Name = strings.TrimSpace(business)
	}
	c.Email = strings.TrimSpace(c.Email)
	c.Phone = strings.TrimSpace(c.Phone)
	return c, nil
}

// MerchantInfo : identité de l'établissement imprimée sur le relevé.
type MerchantInfo struct {
	Name    string
	Address string
	SIRET   string
	Phone   string
	Loc     *time.Location
}

// Merchant renvoie l'identité et le fuseau de l'établissement.
func (r *Repository) Merchant(ctx context.Context, merchantID string) (MerchantInfo, error) {
	var m MerchantInfo
	var tz string
	err := r.DB(ctx).QueryRowContext(ctx, `
		SELECT COALESCE(fullname, ''), COALESCE(address, ''), COALESCE(siret, ''), COALESCE(merchanttel, ''),
		       COALESCE(timezone, '')
		FROM merchant WHERE id::text = ?`, merchantID).Scan(&m.Name, &m.Address, &m.SIRET, &m.Phone, &tz)
	if err != nil {
		return m, fmt.Errorf("customer_accounts: merchant: %w", err)
	}
	m.Loc = loadLocation(tz)
	return m, nil
}

// MerchantLocation renvoie le fuseau de l'établissement (Europe/Paris par défaut).
func (r *Repository) MerchantLocation(ctx context.Context, merchantID string) (*time.Location, error) {
	m, err := r.Merchant(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	return m.Loc, nil
}

func loadLocation(tz string) *time.Location {
	if loc, err := time.LoadLocation(strings.TrimSpace(tz)); err == nil && strings.TrimSpace(tz) != "" {
		return loc
	}
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		return time.UTC
	}
	return loc
}

// StripeAccount : compte Stripe Connect de l'établissement et frais
// ScanNOrder (appliqués aussi au paiement en ligne d'un compte client).
type StripeAccount struct {
	AccountID    string
	VariableFees float64
	FixedFees    int64
}

// Stripe renvoie le compte Stripe de l'établissement (ok=false sans compte).
func (r *Repository) Stripe(ctx context.Context, merchantID string) (StripeAccount, bool, error) {
	var a StripeAccount
	var variable sql.NullFloat64
	var fixed sql.NullInt64
	err := r.DB(ctx).QueryRowContext(ctx, `
		SELECT COALESCE(sa.account_id, ''), snos.variable_fees, snos.fixed_fees
		FROM stripe_accounts sa
		LEFT JOIN scannorder_settings snos ON snos.merchant_id = sa.merchant_id
		WHERE sa.merchant_id = ? AND COALESCE(sa.account_id, '') <> ''
		LIMIT 1`, merchantID).Scan(&a.AccountID, &variable, &fixed)
	if errors.Is(err, sql.ErrNoRows) {
		return a, false, nil
	}
	if err != nil {
		return a, false, fmt.Errorf("customer_accounts: stripe account: %w", err)
	}
	a.VariableFees, a.FixedFees = variable.Float64, fixed.Int64
	return a, true, nil
}

// PaymentLinkToken renvoie le jeton encore valide d'un client, ou en crée un.
func (r *Repository) PaymentLinkToken(ctx context.Context, merchantID string, customerID int64, newToken string, ttl time.Duration) (string, error) {
	db := r.DB(ctx)
	var token string
	err := db.QueryRowContext(ctx, `
		SELECT token FROM customer_account_payment_links
		WHERE merchant_id = ? AND customer_id = ? AND expires_at > now() + interval '7 days'
		ORDER BY created_at DESC LIMIT 1`, merchantID, customerID).Scan(&token)
	if err == nil {
		return token, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("customer_accounts: payment link: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO customer_account_payment_links (token, merchant_id, customer_id, expires_at)
		VALUES (?, ?, ?, ?)`, newToken, merchantID, customerID, time.Now().Add(ttl).UTC()); err != nil {
		return "", fmt.Errorf("customer_accounts: payment link: %w", err)
	}
	return newToken, nil
}

// ResolvePaymentLink renvoie l'établissement et le client d'un jeton valide.
func (r *Repository) ResolvePaymentLink(ctx context.Context, token string) (string, int64, error) {
	var merchantID string
	var customerID int64
	err := r.DB(ctx).QueryRowContext(ctx, `
		SELECT merchant_id, customer_id FROM customer_account_payment_links
		WHERE token = ? AND expires_at > now()`, token).Scan(&merchantID, &customerID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, models.ErrNotFound
	}
	if err != nil {
		return "", 0, fmt.Errorf("customer_accounts: resolve payment link: %w", err)
	}
	return merchantID, customerID, nil
}

// ClaimOnlinePayment réserve une session Stripe (false : déjà traitée).
// paymentIntentID relie le paiement à ses remboursements éventuels.
func (r *Repository) ClaimOnlinePayment(ctx context.Context, sessionID, paymentIntentID, merchantID string, customerID, amount int64) (bool, error) {
	res, err := r.DB(ctx).ExecContext(ctx, `
		INSERT INTO customer_account_online_payments (checkout_session_id, payment_intent_id, merchant_id, customer_id, amount, operation_id)
		VALUES (?, NULLIF(?, ''), ?, ?, ?, 0)
		ON CONFLICT (checkout_session_id) DO NOTHING`, sessionID, paymentIntentID, merchantID, customerID, amount)
	if err != nil {
		return false, fmt.Errorf("customer_accounts: claim online payment: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// SetOnlinePaymentOperation relie la session au mouvement écrit.
func (r *Repository) SetOnlinePaymentOperation(ctx context.Context, sessionID string, operationID int64) error {
	_, err := r.DB(ctx).ExecContext(ctx, `
		UPDATE customer_account_online_payments SET operation_id = ? WHERE checkout_session_id = ?`, operationID, sessionID)
	return err
}

// OnlinePayment : un paiement en ligne reçu, avec la part déjà remboursée.
type OnlinePayment struct {
	SessionID  string
	MerchantID string
	CustomerID int64
	Amount     int64
	Refunded   int64
}

// LockOnlinePayment relit (et verrouille) le paiement en ligne d'un
// PaymentIntent ; ok=false s'il n'est pas connu.
func (r *Repository) LockOnlinePayment(ctx context.Context, paymentIntentID string) (OnlinePayment, bool, error) {
	var p OnlinePayment
	err := r.DB(ctx).QueryRowContext(ctx, `
		SELECT checkout_session_id, merchant_id, customer_id, amount, refunded_amount
		FROM customer_account_online_payments
		WHERE payment_intent_id = ?
		FOR UPDATE`, paymentIntentID).Scan(&p.SessionID, &p.MerchantID, &p.CustomerID, &p.Amount, &p.Refunded)
	if errors.Is(err, sql.ErrNoRows) {
		return OnlinePayment{}, false, nil
	}
	if err != nil {
		return OnlinePayment{}, false, fmt.Errorf("customer_accounts: lock online payment: %w", err)
	}
	return p, true, nil
}

// SetOnlinePaymentRefunded enregistre le cumul remboursé répercuté sur le compte.
func (r *Repository) SetOnlinePaymentRefunded(ctx context.Context, sessionID string, refunded int64) error {
	_, err := r.DB(ctx).ExecContext(ctx, `
		UPDATE customer_account_online_payments SET refunded_amount = ? WHERE checkout_session_id = ?`, refunded, sessionID)
	return err
}

// ReminderMerchants liste les établissements dont les relances sont activées.
func (r *Repository) ReminderMerchants(ctx context.Context) ([]string, error) {
	rows, err := r.DB(ctx).QueryContext(ctx, `
		SELECT merchant_id FROM merchant_parameters WHERE customer_account_reminder_enabled = TRUE`)
	if err != nil {
		return nil, fmt.Errorf("customer_accounts: reminder merchants: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func nullable(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	return &s.String
}
