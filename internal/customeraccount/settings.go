package customeraccount

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/models"
)

// Réglages du compte client : ceux de l'établissement (merchant_parameters)
// et ceux d'un client (customer_account_settings, migration 221).

// Bornes des réglages de relance.
const (
	minReminderDays = 1
	maxReminderDays = 365
	maxReminderMax  = 20
)

// LoadMerchantSettings lit les réglages de l'établissement. Paiement en ligne
// possible : un compte Stripe Connect enregistré (stripe_accounts).
func LoadMerchantSettings(ctx context.Context, db *dbx.DB, merchantID string) (models.CustomerAccountSettings, error) {
	s := models.CustomerAccountSettings{CashMode: models.AccountCashModeBlock, ReminderDays: 30, ReminderMax: 3}
	var cashMode sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT mp.customer_account_credit_limit, mp.customer_account_cash_mode,
		       mp.customer_account_reminder_enabled, mp.customer_account_reminder_days,
		       mp.customer_account_reminder_max, mp.customer_account_reminder_sms,
		       EXISTS (SELECT 1 FROM stripe_accounts sa
		               WHERE sa.merchant_id = mp.merchant_id AND COALESCE(sa.account_id, '') <> '')
		FROM merchant_parameters mp WHERE mp.merchant_id = ?`, merchantID).Scan(
		&s.CreditLimit, &cashMode, &s.ReminderEnabled, &s.ReminderDays, &s.ReminderMax, &s.ReminderSMS,
		&s.OnlinePaymentAvailable)
	if err == sql.ErrNoRows {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("customeraccount: merchant settings: %w", err)
	}
	if s.CreditLimit < 0 {
		s.CreditLimit = 0
	}
	if strings.ToUpper(cashMode.String) == models.AccountCashModeWarn {
		s.CashMode = models.AccountCashModeWarn
	}
	return s, nil
}

// ValidateMerchantSettings contrôle les valeurs modifiables.
func ValidateMerchantSettings(s models.CustomerAccountSettings, maxCreditLimit int64) error {
	if s.CreditLimit < 0 || s.CreditLimit > maxCreditLimit {
		return models.ErrAccountInvalidAmount
	}
	if s.CashMode != models.AccountCashModeBlock && s.CashMode != models.AccountCashModeWarn {
		return fmt.Errorf("%w: cash_mode", models.ErrInvalidInput)
	}
	if s.ReminderDays < minReminderDays || s.ReminderDays > maxReminderDays ||
		s.ReminderMax < 1 || s.ReminderMax > maxReminderMax {
		return fmt.Errorf("%w: reminder", models.ErrInvalidInput)
	}
	return nil
}

// SaveMerchantSettings écrit les réglages de l'établissement (validés).
func SaveMerchantSettings(ctx context.Context, db *dbx.DB, merchantID string, s models.CustomerAccountSettings) error {
	res, err := db.ExecContext(ctx, `
		UPDATE merchant_parameters SET
			customer_account_credit_limit = ?, customer_account_cash_mode = ?,
			customer_account_reminder_enabled = ?, customer_account_reminder_days = ?,
			customer_account_reminder_max = ?, customer_account_reminder_sms = ?
		WHERE merchant_id = ?`,
		s.CreditLimit, s.CashMode, s.ReminderEnabled, s.ReminderDays, s.ReminderMax, s.ReminderSMS, merchantID)
	if err != nil {
		return fmt.Errorf("customeraccount: save merchant settings: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return models.ErrNotFound
	}
	return nil
}

// LoadCustomerSettings lit les réglages propres à un client (défaut : plafond
// de l'établissement, ardoise autorisée).
func LoadCustomerSettings(ctx context.Context, db *dbx.DB, merchantID string, customerID int64) (models.CustomerAccountCustomerSettings, error) {
	var s models.CustomerAccountCustomerSettings
	var limit sql.NullInt64
	err := db.QueryRowContext(ctx, `
		SELECT credit_limit, credit_blocked, reminders_disabled FROM customer_account_settings
		WHERE merchant_id = ? AND customer_id = ?`, merchantID, customerID).Scan(&limit, &s.CreditBlocked, &s.RemindersDisabled)
	if err == sql.ErrNoRows {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("customeraccount: customer settings: %w", err)
	}
	if limit.Valid {
		v := limit.Int64
		s.CreditLimit = &v
	}
	return s, nil
}

// SaveCustomerSettings écrit les réglages propres à un client.
func SaveCustomerSettings(ctx context.Context, db *dbx.DB, merchantID string, customerID int64, s models.CustomerAccountCustomerSettings, userID string) error {
	if s.CreditLimit != nil && *s.CreditLimit < 0 {
		return models.ErrAccountInvalidAmount
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO customer_account_settings (merchant_id, customer_id, credit_limit, credit_blocked, reminders_disabled, updated_at, updated_by)
		VALUES (?, ?, ?, ?, ?, now(), ?)
		ON CONFLICT (merchant_id, customer_id) DO UPDATE
		SET credit_limit = EXCLUDED.credit_limit, credit_blocked = EXCLUDED.credit_blocked,
		    reminders_disabled = EXCLUDED.reminders_disabled,
		    updated_at = now(), updated_by = EXCLUDED.updated_by`,
		merchantID, customerID, s.CreditLimit, s.CreditBlocked, s.RemindersDisabled, userID)
	if err != nil {
		return fmt.Errorf("customeraccount: save customer settings: %w", err)
	}
	return nil
}

// EffectiveCreditLimit : plafond d'ardoise d'un client — 0 si l'ardoise lui
// est interdite, son plafond propre s'il en a un, sinon celui de
// l'établissement.
func EffectiveCreditLimit(merchantLimit int64, c models.CustomerAccountCustomerSettings) int64 {
	if c.CreditBlocked {
		return 0
	}
	if c.CreditLimit != nil {
		return *c.CreditLimit
	}
	return merchantLimit
}

// CustomerCreditLimit renvoie le plafond d'ardoise effectif d'un client.
func CustomerCreditLimit(ctx context.Context, db *dbx.DB, merchantID string, customerID int64) (int64, models.CustomerAccountCustomerSettings, error) {
	merchantLimit, err := CreditLimit(ctx, db, merchantID)
	if err != nil {
		return 0, models.CustomerAccountCustomerSettings{}, err
	}
	cs, err := LoadCustomerSettings(ctx, db, merchantID, customerID)
	if err != nil {
		return 0, cs, err
	}
	return EffectiveCreditLimit(merchantLimit, cs), cs, nil
}

// CheckCashDeposit applique la limite des espèces à un versement : refus
// au-delà de 1 000 € en mode BLOCK ; en mode WARN, jusqu'à 15 000 € si la caisse
// a confirmé un client non-résident, mention ajoutée au motif (scellé).
func CheckCashDeposit(op *Operation, cashMode string, nonResidentConfirmed bool) error {
	if op.Type != models.AccountOperationDeposit || op.MOP == nil || *op.MOP != "ES" ||
		op.Amount <= models.AccountCashDepositLimit {
		return nil
	}
	if cashMode != models.AccountCashModeWarn {
		return models.ErrAccountCashLimit
	}
	if !nonResidentConfirmed {
		return models.ErrAccountCashConfirmationRequired
	}
	if op.Amount > models.AccountCashNonResidentLimit {
		return models.ErrAccountCashNonResidentLimit
	}
	note := "Client non-résident (déclaré à la caisse)"
	if op.Comment != nil && strings.TrimSpace(*op.Comment) != "" {
		note = strings.TrimSpace(*op.Comment) + " · " + note
	}
	if len(note) > 250 {
		note = note[:250]
	}
	op.Comment = &note
	return nil
}
