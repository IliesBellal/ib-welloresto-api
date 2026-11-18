package models

import (
	"errors"
	"strings"
)

// Compte client (docs/customer-accounts.md) : avance et ardoise, un solde par
// client et par établissement.

// AccountMOP est le moyen de paiement d'une commande réglée « sur le compte
// client » : le solde du client est débité (ou recrédité pour un
// remboursement), aucun argent ne change de main.
const AccountMOP = "ACCOUNT"

// Types de mouvement de compte (customer_account_operations.operation_type).
const (
	// AccountOperationDeposit : le client verse de l'argent (avance, ou
	// règlement de son ardoise). Monte le solde.
	AccountOperationDeposit = "DEPOSIT"
	// AccountOperationRefund : l'établissement rend au client tout ou partie
	// de son solde créditeur. Baisse le solde.
	AccountOperationRefund = "REFUND"
	// AccountOperationWriteOff : abandon de créance, l'établissement renonce à
	// tout ou partie de l'ardoise. Monte le solde, sans argent.
	AccountOperationWriteOff = "WRITE_OFF"
)

// AccountMoneyMOPs : moyens par lesquels l'argent d'un versement ou d'un
// remboursement de solde peut passer. Pas de titre-restaurant (il paie un
// repas, pas une avance).
var AccountMoneyMOPs = []string{"ES", "CB", "CHEQUE", "TRANSFER"}

// IsAccountMoneyMOP indique si mop fait partie de AccountMoneyMOPs.
func IsAccountMoneyMOP(mop string) bool {
	for _, m := range AccountMoneyMOPs {
		if m == mop {
			return true
		}
	}
	return false
}

// AccountCashDepositLimit : un particulier ne peut pas payer un professionnel
// plus de 1 000 € en espèces (code monétaire et financier, art. L112-6 et
// D112-3). Appliqué à chaque versement en espèces.
const AccountCashDepositLimit = 100000

// AccountRegisterMOP est le code d'une ligne de mouvement de compte dans le Z
// d'un registre (cash_registers_items.mop) : ACCOUNT_IN_<moyen> pour l'argent
// versé, ACCOUNT_OUT_<moyen> pour l'argent rendu (montant négatif).
func AccountRegisterMOP(mop string, amount int64) string {
	if amount < 0 {
		return "ACCOUNT_OUT_" + mop
	}
	return "ACCOUNT_IN_" + mop
}

// AccountRegisterLineMOP renvoie le moyen de paiement d'une ligne de
// mouvement de compte du Z (ACCOUNT_IN_ES -> ES), ok=false pour toute autre
// ligne.
func AccountRegisterLineMOP(registerMOP string) (string, bool) {
	for _, prefix := range []string{"ACCOUNT_IN_", "ACCOUNT_OUT_"} {
		if strings.HasPrefix(registerMOP, prefix) {
			return strings.TrimPrefix(registerMOP, prefix), true
		}
	}
	return "", false
}

var accountMOPLabels = map[string]string{
	"ES": "Espèces", "CB": "CB", "CHEQUE": "Chèque", "TRANSFER": "Virement", "STRIPE": "En ligne",
}

// AccountMOPLabel renvoie le libellé d'un moyen de versement (« Espèces »).
func AccountMOPLabel(mop string) string {
	if label := accountMOPLabels[mop]; label != "" {
		return label
	}
	return mop
}

// AccountRegisterLabel renvoie le libellé d'une ligne de compte client du Z
// (« Versement compte client · Espèces »), ou « Compte client » pour les
// commandes réglées sur le compte ; vide pour toute autre ligne. Repli quand le
// catalogue labels n'a pas l'entrée (migration 220 non jouée).
func AccountRegisterLabel(registerMOP string) string {
	if registerMOP == AccountMOP {
		return "Compte client"
	}
	mop, ok := AccountRegisterLineMOP(registerMOP)
	if !ok {
		return ""
	}
	label := accountMOPLabels[mop]
	if label == "" {
		label = mop
	}
	if strings.HasPrefix(registerMOP, "ACCOUNT_OUT_") {
		return "Remboursement compte client · " + label
	}
	return "Versement compte client · " + label
}

// CustomerAccountOperation est un mouvement du compte d'un client.
type CustomerAccountOperation struct {
	OperationID    int64   `json:"operation_id"`
	CustomerID     int64   `json:"customer_id"`
	OperationType  string  `json:"operation_type"`
	Amount         int64   `json:"amount"`
	MOP            *string `json:"mop"`
	Comment        *string `json:"comment"`
	UserID         string  `json:"user_id"`
	UserName       string  `json:"user_name,omitempty"`
	CashRegisterID *string `json:"cash_register_id"`
	OperationDate  int64   `json:"operation_date"`
	Hash           string  `json:"hash,omitempty"`
}

// CustomerAccountPayment est une commande réglée sur le compte (ou un
// remboursement de commande sur le compte, montant négatif).
type CustomerAccountPayment struct {
	PaymentID   int64  `json:"payment_id"`
	OrderID     int64  `json:"order_id"`
	OrderNum    string `json:"order_num"`
	Amount      int64  `json:"amount"`
	Enabled     bool   `json:"enabled"`
	PaymentDate int64  `json:"payment_date"`
	// ReceiptNumber : numéro du ticket de vente de la commande (vide tant
	// qu'elle n'est pas close).
	ReceiptNumber string `json:"receipt_number,omitempty"`
}

// CustomerAccountEntry est une ligne de l'historique d'un compte : un
// mouvement (Operation) ou une commande (Payment), avec son effet sur le
// solde (Delta) et le solde après elle.
type CustomerAccountEntry struct {
	Kind         string                    `json:"kind"` // "operation" ou "payment"
	Date         int64                     `json:"date"`
	Delta        int64                     `json:"delta"`
	BalanceAfter int64                     `json:"balance_after"`
	Operation    *CustomerAccountOperation `json:"operation,omitempty"`
	Payment      *CustomerAccountPayment   `json:"payment,omitempty"`
}

// CustomerAccount est l'état du compte d'un client.
type CustomerAccount struct {
	CustomerID int64 `json:"customer_id"`
	// Balance : solde en centimes ; > 0 avance, < 0 ardoise.
	Balance int64 `json:"balance"`
	// CreditLimit : dette maximale autorisée par l'établissement (0 = ardoise
	// désactivée).
	CreditLimit int64 `json:"credit_limit"`
	// Available : ce que le client peut encore régler sur son compte
	// (Balance + CreditLimit, jamais négatif).
	Available int64                  `json:"available"`
	Entries   []CustomerAccountEntry `json:"entries"`

	// Réglages : plafond de l'établissement, réglages propres au client,
	// mode des espèces, paiement en ligne possible.
	MerchantCreditLimit    int64                           `json:"merchant_credit_limit"`
	CustomerSettings       CustomerAccountCustomerSettings `json:"customer_settings"`
	CashMode               string                          `json:"cash_mode"`
	OnlinePaymentAvailable bool                            `json:"online_payment_available"`
	Reminders              []CustomerAccountReminder       `json:"reminders"`
}

// CustomerAccountSummary est un compte non soldé, pour la liste des comptes.
type CustomerAccountSummary struct {
	CustomerID   int64  `json:"customer_id"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	BusinessName string `json:"business_name"`
	Phone        string `json:"phone"`
	Balance      int64  `json:"balance"`
	LastMovement int64  `json:"last_movement"`
}

// CustomerAccountOperationRequest : POST /customer-accounts/{customer_id}/deposit,
// /refund ou /write-off.
type CustomerAccountOperationRequest struct {
	DeviceID string `json:"device_id"`
	// Amount : montant de l'opération, en centimes, toujours positif.
	Amount int64 `json:"amount"`
	// MOP : moyen de paiement de l'argent (versement, remboursement).
	MOP     string `json:"mop"`
	Comment string `json:"comment"`
	// NonResidentConfirmed : la caisse confirme un client non-résident pour
	// un versement en espèces au-delà de 1 000 € (mode « avertir »).
	NonResidentConfirmed bool `json:"non_resident_confirmed"`
}

// Réglage des versements en espèces au-delà de AccountCashDepositLimit.
const (
	// AccountCashModeBlock : refusés (défaut).
	AccountCashModeBlock = "BLOCK"
	// AccountCashModeWarn : acceptés jusqu'à AccountCashNonResidentLimit si la
	// caisse confirme un client non-résident (CMF D112-3).
	AccountCashModeWarn = "WARN"
)

// AccountCashNonResidentLimit : plafond des espèces pour un particulier
// non-résident (15 000 €).
const AccountCashNonResidentLimit = 1500000

// AccountOnlineMOP : versement payé en ligne (lien de paiement Stripe), sans
// registre de caisse.
const AccountOnlineMOP = "STRIPE"

// CustomerAccountSettings : réglages du compte client de l'établissement.
type CustomerAccountSettings struct {
	// CreditLimit : plafond d'ardoise par défaut (centimes, 0 = désactivée).
	CreditLimit int64 `json:"credit_limit"`
	// CashMode : AccountCashModeBlock ou AccountCashModeWarn.
	CashMode string `json:"cash_mode"`
	// Relances automatiques des ardoises : activées, première relance et
	// intervalle (jours), nombre maximal, SMS en plus de l'email.
	ReminderEnabled bool `json:"reminder_enabled"`
	ReminderDays    int  `json:"reminder_days"`
	ReminderMax     int  `json:"reminder_max"`
	ReminderSMS     bool `json:"reminder_sms"`
	// OnlinePaymentAvailable : l'établissement a un compte Stripe actif (lien
	// de paiement possible). Lecture seule.
	OnlinePaymentAvailable bool `json:"online_payment_available"`
}

// CustomerAccountCustomerSettings : réglages propres à un client.
type CustomerAccountCustomerSettings struct {
	// CreditLimit : plafond propre (centimes) ; nil = celui de l'établissement.
	CreditLimit *int64 `json:"credit_limit"`
	// CreditBlocked : ardoise interdite (son avance reste utilisable).
	CreditBlocked bool `json:"credit_blocked"`
}

// CustomerAccountAging : ancienneté de l'ardoise d'un client (centimes).
// Chaque versement solde d'abord les commandes les plus anciennes.
type CustomerAccountAging struct {
	CustomerSummary CustomerAccountSummary `json:"customer"`
	Debt            int64                  `json:"debt"`
	Days0To30       int64                  `json:"days_0_30"`
	Days31To60      int64                  `json:"days_31_60"`
	Days61Plus      int64                  `json:"days_61_plus"`
	// OldestUnpaid : date (Unix) de la plus ancienne commande non soldée.
	OldestUnpaid int64 `json:"oldest_unpaid"`
}

// CustomerAccountMonth : mouvements des comptes clients d'un mois (centimes).
type CustomerAccountMonth struct {
	Month     string `json:"month"` // AAAA-MM
	Deposits  int64  `json:"deposits"`
	Refunds   int64  `json:"refunds"`    // argent rendu (positif)
	WriteOffs int64  `json:"write_offs"` // dettes abandonnées
	Consumed  int64  `json:"consumed"`   // commandes réglées sur compte, nettes des remboursements sur compte
	NetChange int64  `json:"net_change"` // variation du total des soldes
	EndCredit int64  `json:"end_credit"` // avances en fin de mois (4191)
	EndDebt   int64  `json:"end_debt"`   // ardoises en fin de mois (411), positif
}

// CustomerAccountReminder : relance envoyée.
type CustomerAccountReminder struct {
	SentAt   int64  `json:"sent_at"`
	Source   string `json:"source"`
	Channels string `json:"channels"`
	Balance  int64  `json:"balance"`
}

var (
	// ErrAccountCustomerRequired : paiement sur le compte d'une commande sans
	// client identifié de l'établissement.
	ErrAccountCustomerRequired = errors.New("account_customer_required")
	// ErrAccountInsufficientBalance : le paiement dépasse le solde disponible
	// (avance + plafond d'ardoise).
	ErrAccountInsufficientBalance = errors.New("account_insufficient_balance")
	// ErrAccountRefundExceedsBalance : remboursement supérieur au solde
	// créditeur.
	ErrAccountRefundExceedsBalance = errors.New("account_refund_exceeds_balance")
	// ErrAccountWriteOffExceedsDebt : abandon supérieur à la dette.
	ErrAccountWriteOffExceedsDebt = errors.New("account_write_off_exceeds_debt")
	// ErrAccountInvalidMOP : moyen de paiement non accepté pour un versement
	// ou un remboursement de solde.
	ErrAccountInvalidMOP = errors.New("account_invalid_mop")
	// ErrAccountInvalidAmount : montant nul, négatif ou hors limites.
	ErrAccountInvalidAmount = errors.New("account_invalid_amount")
	// ErrAccountCashLimit : versement en espèces au-delà de 1 000 €.
	ErrAccountCashLimit = errors.New("account_cash_limit")
	// ErrAccountOrderHasOtherCustomer : la commande est déjà rattachée à un autre
	// client que celui choisi pour le paiement sur compte.
	ErrAccountOrderHasOtherCustomer = errors.New("account_order_has_other_customer")
	// ErrAccountReasonRequired : remboursement ou abandon sans motif.
	ErrAccountReasonRequired = errors.New("account_reason_required")
	// ErrAccountCashConfirmationRequired : espèces au-delà de 1 000 € en mode
	// « avertir », sans confirmation d'un client non-résident.
	ErrAccountCashConfirmationRequired = errors.New("account_cash_confirmation_required")
	// ErrAccountCashNonResidentLimit : espèces au-delà de 15 000 €.
	ErrAccountCashNonResidentLimit = errors.New("account_cash_non_resident_limit")
	// ErrAccountCreditBlocked : ardoise interdite pour ce client.
	ErrAccountCreditBlocked = errors.New("account_credit_blocked")
	// ErrAccountNothingDue : aucune dette (lien de paiement, relance).
	ErrAccountNothingDue = errors.New("account_nothing_due")
	// ErrAccountOnlinePaymentUnavailable : pas de compte Stripe actif.
	ErrAccountOnlinePaymentUnavailable = errors.New("account_online_payment_unavailable")
	// ErrAccountNoContact : ni email ni téléphone utilisable pour une relance.
	ErrAccountNoContact = errors.New("account_no_contact")
)
