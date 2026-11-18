// Package customeraccount porte le compte client (docs/customer-accounts.md) :
// solde, contrôle d'un paiement sur le compte et écriture scellée d'un
// mouvement. Partagé par la caisse (paiements ACCOUNT, order_life_cycle), le
// module des comptes (versements, remboursements, abandons) et le registre de
// caisse (lignes du Z).
//
// Le solde n'est stocké nulle part : il vaut la somme des mouvements
// (customer_account_operations.amount) moins celle des paiements actifs sur
// le compte (payments, mop ACCOUNT ; un remboursement de commande sur le compte
// est un paiement négatif, qui recrédite). Les deux sources sont scellées.
package customeraccount

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

// Lock sérialise, jusqu'à la fin de la transaction, ce qui débite ou écrit le
// compte d'un client : deux paiements simultanés ne peuvent pas dépasser à eux
// deux le solde disponible. Verrou consultatif dédié, que rien d'autre ne
// prend : il ne peut pas former de cycle avec les verrous de chaînes ou de
// lignes. À prendre avant la chaîne des paiements ou des mouvements.
func Lock(ctx context.Context, merchantID string, customerID int64) error {
	tx := dbutils.ExtractTx(ctx)
	if tx == nil {
		return fiscal.ErrNoTransaction
	}
	key := fmt.Sprintf("customer_account:%s:%d", merchantID, customerID)
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key); err != nil {
		return fmt.Errorf("customeraccount: lock %d: %w", customerID, err)
	}
	return nil
}

// balanceSQL : solde d'un client (centimes). Arguments : merchant, customer,
// merchant, customer.
const balanceSQL = `
	SELECT COALESCE((SELECT SUM(amount) FROM customer_account_operations
	                 WHERE merchant_id = ? AND customer_id = ?), 0)
	     - COALESCE((SELECT SUM(amount) FROM payments
	                 WHERE merchant_id = ? AND customer_id = ? AND mop = 'ACCOUNT' AND enabled = TRUE), 0)`

// Balance renvoie le solde du compte d'un client : > 0 avance, < 0 ardoise.
func Balance(ctx context.Context, db *dbx.DB, merchantID string, customerID int64) (int64, error) {
	var balance int64
	if err := db.QueryRowContext(ctx, balanceSQL, merchantID, customerID, merchantID, customerID).Scan(&balance); err != nil {
		return 0, fmt.Errorf("customeraccount: balance %d: %w", customerID, err)
	}
	return balance, nil
}

// CreditLimit renvoie le plafond d'ardoise de l'établissement (0 = ardoise
// désactivée).
func CreditLimit(ctx context.Context, db *dbx.DB, merchantID string) (int64, error) {
	var limit sql.NullInt64
	err := db.QueryRowContext(ctx, `
		SELECT customer_account_credit_limit FROM merchant_parameters WHERE merchant_id = ?`, merchantID).Scan(&limit)
	if err != nil && err != sql.ErrNoRows {
		return 0, fmt.Errorf("customeraccount: credit limit: %w", err)
	}
	if !limit.Valid || limit.Int64 < 0 {
		return 0, nil
	}
	return limit.Int64, nil
}

// Available : ce qu'un client peut encore régler sur son compte.
func Available(balance, creditLimit int64) int64 {
	if a := balance + creditLimit; a > 0 {
		return a
	}
	return 0
}

// CheckDebit vérifie qu'un client peut régler amount (centimes, > 0) sur son
// compte : avance + plafond d'ardoise effectif (le sien, ou celui de
// l'établissement ; 0 si l'ardoise lui est interdite). L'appelant tient Lock.
func CheckDebit(ctx context.Context, db *dbx.DB, merchantID string, customerID, amount int64) error {
	balance, err := Balance(ctx, db, merchantID, customerID)
	if err != nil {
		return err
	}
	limit, cs, err := CustomerCreditLimit(ctx, db, merchantID, customerID)
	if err != nil {
		return err
	}
	if amount > Available(balance, limit) {
		if cs.CreditBlocked {
			return models.ErrAccountCreditBlocked
		}
		return models.ErrAccountInsufficientBalance
	}
	return nil
}

// Balances renvoie le solde de plusieurs clients (absents : solde nul).
func Balances(ctx context.Context, db *dbx.DB, merchantID string, customerIDs []int64) (map[int64]int64, error) {
	out := make(map[int64]int64, len(customerIDs))
	if len(customerIDs) == 0 {
		return out, nil
	}
	rows, err := db.QueryContext(ctx, `
		SELECT customer_id, SUM(amount) FROM (
			SELECT customer_id, amount FROM customer_account_operations
			WHERE merchant_id = ? AND customer_id = ANY(?)
			UNION ALL
			SELECT customer_id, -amount FROM payments
			WHERE merchant_id = ? AND mop = 'ACCOUNT' AND enabled = TRUE AND customer_id = ANY(?)
		) t GROUP BY customer_id`, merchantID, customerIDs, merchantID, customerIDs)
	if err != nil {
		return nil, fmt.Errorf("customeraccount: balances: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, balance int64
		if err := rows.Scan(&id, &balance); err != nil {
			return nil, fmt.Errorf("customeraccount: balances: %w", err)
		}
		out[id] = balance
	}
	return out, rows.Err()
}

// BalancesSQL : sous-requête (customer_id, balance) des clients ayant un
// mouvement, pour filtrer une liste de clients. Arguments : merchant, merchant.
const BalancesSQL = `
	SELECT customer_id, SUM(amount) AS balance FROM (
		SELECT customer_id, amount FROM customer_account_operations WHERE merchant_id = ?
		UNION ALL
		SELECT customer_id, -amount FROM payments
		WHERE merchant_id = ? AND mop = 'ACCOUNT' AND enabled = TRUE AND customer_id IS NOT NULL
	) t GROUP BY customer_id`

// OrderCustomer renvoie le client de l'établissement associé à une commande,
// models.ErrAccountCustomerRequired s'il n'y en a pas (commande anonyme,
// client d'un autre établissement ou désactivé).
func OrderCustomer(ctx context.Context, db *dbx.DB, merchantID, orderID string) (int64, error) {
	var customerID sql.NullInt64
	err := db.QueryRowContext(ctx, `
		SELECT c.customer_id
		FROM orders o
		JOIN customer c ON c.customer_id = o.customer_id
		WHERE o.order_id = ? AND o.merchant_id = ? AND c.merchant_id = ? AND c.enabled = TRUE`,
		orderID, merchantID, merchantID).Scan(&customerID)
	if err == sql.ErrNoRows || (err == nil && !customerID.Valid) {
		return 0, models.ErrAccountCustomerRequired
	}
	if err != nil {
		return 0, fmt.Errorf("customeraccount: order customer: %w", err)
	}
	return customerID.Int64, nil
}

// AttachOrderCustomer rattache à une commande sans client le client choisi à la
// caisse pour la régler sur son compte (dans la transaction du paiement).
// Sans effet si la commande est déjà à ce client ; models.ErrAccountOrderHasOtherCustomer
// si elle en a un autre, models.ErrAccountCustomerRequired si le client n'est pas
// un client actif de l'établissement ou si la commande vient d'une plateforme.
func AttachOrderCustomer(ctx context.Context, db *dbx.DB, merchantID, orderID string, customerID int64) error {
	var current sql.NullInt64
	var brand sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT customer_id, brand FROM orders
		WHERE order_id = ? AND merchant_id = ?
		FOR UPDATE`, orderID, merchantID).Scan(&current, &brand)
	if err == sql.ErrNoRows {
		return models.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("customeraccount: read order customer: %w", err)
	}
	if brand.Valid && brand.String != models.BrandWelloResto {
		return models.ErrAccountCustomerRequired
	}
	if current.Valid && current.Int64 > 0 {
		if current.Int64 == customerID {
			return nil
		}
		return models.ErrAccountOrderHasOtherCustomer
	}
	var ok int
	err = db.QueryRowContext(ctx, `
		SELECT 1 FROM customer WHERE customer_id = ? AND merchant_id = ? AND enabled = TRUE`,
		customerID, merchantID).Scan(&ok)
	if err == sql.ErrNoRows {
		return models.ErrAccountCustomerRequired
	}
	if err != nil {
		return fmt.Errorf("customeraccount: check customer: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE orders SET customer_id = ? WHERE order_id = ? AND merchant_id = ?`,
		customerID, orderID, merchantID); err != nil {
		return fmt.Errorf("customeraccount: attach order customer: %w", err)
	}
	return nil
}

// Operation est un mouvement à écrire.
type Operation struct {
	MerchantID     string
	CustomerID     int64
	Type           string // models.AccountOperation*
	Amount         int64  // effet sur le solde (signé)
	MOP            *string
	Comment        *string
	UserID         string
	CashRegisterID *string
}

// ValidateOperation contrôle la cohérence d'un mouvement (type, signe, moyen),
// avant tout accès à la base.
func ValidateOperation(op Operation) error {
	switch op.Type {
	case models.AccountOperationDeposit, models.AccountOperationWriteOff:
		if op.Amount <= 0 {
			return models.ErrAccountInvalidAmount
		}
	case models.AccountOperationRefund:
		if op.Amount >= 0 {
			return models.ErrAccountInvalidAmount
		}
	default:
		return fmt.Errorf("%w: operation type %q", models.ErrInvalidInput, op.Type)
	}
	if op.Type == models.AccountOperationWriteOff {
		if op.MOP != nil {
			return models.ErrAccountInvalidMOP
		}
	} else if op.MOP == nil {
		return models.ErrAccountInvalidMOP
	} else if !models.IsAccountMoneyMOP(*op.MOP) &&
		// Versement payé en ligne (lien de paiement) et son remboursement,
		// écrits par le webhook.
		!(op.Type != models.AccountOperationWriteOff && *op.MOP == models.AccountOnlineMOP) {
		return models.ErrAccountInvalidMOP
	}
	// La limite des espèces dépend du réglage de l'établissement :
	// CheckCashDeposit.
	if op.Type != models.AccountOperationDeposit && (op.Comment == nil || strings.TrimSpace(*op.Comment) == "") {
		return models.ErrAccountReasonRequired
	}
	return nil
}

// Insert scelle et écrit un mouvement, chaîné sur le dernier de
// l'établissement. Dans une transaction (verrou de la chaîne). L'appelant a
// validé le mouvement et contrôlé le solde sous Lock.
func Insert(ctx context.Context, db *dbx.DB, op Operation) (int64, time.Time, error) {
	if err := fiscal.LockChain(ctx, fiscal.ChainCustomerAccounts, op.MerchantID); err != nil {
		return 0, time.Time{}, err
	}
	var prevHash sql.NullString
	if err := db.QueryRowContext(ctx, `
		SELECT hash FROM customer_account_operations
		WHERE merchant_id = ? AND hash IS NOT NULL
		ORDER BY operation_date DESC, operation_id DESC LIMIT 1`, op.MerchantID).Scan(&prevHash); err != nil && err != sql.ErrNoRows {
		return 0, time.Time{}, fmt.Errorf("customeraccount: read previous hash: %w", err)
	}
	prev := fiscal.PrevOrGenesis(prevHash.String)
	now := fiscal.Now()
	payload := fiscal.AccountOperationPayload{
		MerchantID: op.MerchantID, CustomerID: op.CustomerID, OperationType: op.Type, Amount: op.Amount,
		MOP: op.MOP, Comment: op.Comment, UserID: op.UserID, CashRegisterID: op.CashRegisterID,
		OperationDate: fiscal.FormatTime(now),
	}
	hash, signature, err := fiscal.Seal(fiscal.ChainCustomerAccounts, prev, payload)
	if err != nil {
		return 0, time.Time{}, err
	}
	id, err := db.InsertReturningID(ctx, `
		INSERT INTO customer_account_operations
		(merchant_id, customer_id, operation_type, amount, mop, comment, user_id, cash_register_id, operation_date,
		 previous_hash, hash, signature, hash_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "operation_id",
		op.MerchantID, op.CustomerID, op.Type, op.Amount, op.MOP, op.Comment, op.UserID, op.CashRegisterID, now,
		prev, hash, signature, fiscal.HashVersion)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("customeraccount: insert operation: %w", err)
	}
	return id, now, nil
}

// RegisterLines renvoie les lignes du Z d'un registre pour les mouvements de
// compte passés dessus : une par moyen et par sens (ACCOUNT_IN_ES pour
// l'argent versé, ACCOUNT_OUT_ES, négative, pour l'argent rendu).
func RegisterLines(ctx context.Context, db *dbx.DB, cashRegisterID string) ([]models.MOPLine, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT mop, amount > 0 AS incoming, SUM(amount)
		FROM customer_account_operations
		WHERE cash_register_id = ? AND mop IS NOT NULL
		GROUP BY mop, amount > 0
		ORDER BY mop, amount > 0 DESC`, cashRegisterID)
	if err != nil {
		return nil, fmt.Errorf("customeraccount: register lines: %w", err)
	}
	defer rows.Close()
	var lines []models.MOPLine
	for rows.Next() {
		var mop string
		var incoming bool
		var amount int64
		if err := rows.Scan(&mop, &incoming, &amount); err != nil {
			return nil, fmt.Errorf("customeraccount: register lines: %w", err)
		}
		code := models.AccountRegisterMOP(mop, amount)
		lines = append(lines, models.MOPLine{MOP: code, Amount: int(amount), Label: models.AccountRegisterLabel(code)})
	}
	return lines, rows.Err()
}

// CashDelta : effet des lignes de compte du Z sur le tiroir-caisse (espèces
// versées moins espèces rendues).
func CashDelta(lines []models.MOPLine) int {
	total := 0
	for _, l := range lines {
		if mop, ok := models.AccountRegisterLineMOP(l.MOP); ok && mop == "ES" {
			total += l.Amount
		}
	}
	return total
}
