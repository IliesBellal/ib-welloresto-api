package customer_accounts

import (
	"context"
	"net/mail"
	"strings"
	"time"

	"welloresto-api/internal/customeraccount"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

// Relances des ardoises : email (et SMS si l'établissement l'a choisi), avec
// le lien de paiement en ligne quand l'établissement a un compte Stripe.
//
// Rythme automatique : première relance quand la plus ancienne commande non
// réglée a ReminderDays jours, puis tous les ReminderDays jours, au plus
// ReminderMax relances pour cette dette (compter repart quand l'ardoise est
// soldée). Envoi à 10 h, heure de l'établissement.

// Sources d'une relance.
const (
	ReminderAuto   = "AUTO"
	ReminderManual = "MANUAL"
)

// reminderHour : heure locale des relances automatiques.
const reminderHour = 10

// reminderDue décide si un client endetté doit être relancé maintenant.
func reminderDue(now, oldestUnpaid time.Time, sent int, last *time.Time, days, max int) bool {
	interval := time.Duration(days) * 24 * time.Hour
	if now.Sub(oldestUnpaid) < interval || sent >= max {
		return false
	}
	return last == nil || now.Sub(*last) >= interval-time.Hour
}

// reminderSMS : texte court du SMS de relance.
func reminderSMS(merchantName, amountDue, paymentURL string) string {
	msg := merchantName + " : votre ardoise de " + amountDue + " est à régler."
	if paymentURL != "" {
		msg += " Payer en ligne : " + paymentURL
	}
	return msg
}

// sendReminder envoie une relance et l'inscrit au journal. Renvoie les canaux
// utilisés (models.ErrAccountNoContact si aucun).
func (s *Service) sendReminder(ctx context.Context, merchantID string, customerID int64, balance int64, oldestUnpaid time.Time,
	settings models.CustomerAccountSettings, source, userID string) (string, error) {
	c, err := s.repo.Contact(ctx, merchantID, customerID)
	if err != nil {
		return "", err
	}
	m, err := s.repo.Merchant(ctx, merchantID)
	if err != nil {
		return "", err
	}
	amountDue := formatMoney(-balance)
	paymentURL := s.paymentURLIfAvailable(ctx, merchantID, customerID)

	var channels []string
	if addr, err := mail.ParseAddress(c.Email); err == nil && addr.Address == c.Email && s.deps.Mailer != nil {
		data := mailer.CustomerAccountEmailData{
			Brand: s.brand(ctx, merchantID, m.Name), MerchantName: m.Name, CustomerName: c.Name,
			BalanceLabel: balanceLabel(balance), AmountDue: amountDue, PaymentURL: paymentURL,
		}
		if !oldestUnpaid.IsZero() {
			data.SinceLabel = oldestUnpaid.In(m.Loc).Format("02/01/2006")
		}
		s.deps.Mailer.SendAsync(m.Name, mailer.InvoiceEmail, c.Email, "Votre ardoise chez "+m.Name, "customer_account_reminder.html", data)
		channels = append(channels, "EMAIL")
	}
	if settings.ReminderSMS && c.Phone != "" && s.deps.SMS != nil {
		if err := s.deps.SMS.SendTransactional(ctx, merchantID, c.Phone, reminderSMS(m.Name, amountDue, paymentURL)); err != nil {
			logger.FromContext(ctx).Warn("customer account reminder: SMS not sent",
				zap.String("merchant_id", merchantID), zap.Int64("customer_id", customerID), zap.Error(err))
		} else {
			channels = append(channels, "SMS")
		}
	}
	if len(channels) == 0 {
		return "", models.ErrAccountNoContact
	}
	joined := strings.Join(channels, "+")
	return joined, s.repo.RecordReminder(ctx, merchantID, customerID, source, joined, balance, userID)
}

// Remind relance un client depuis le back-office, quel que soit le rythme
// automatique.
func (s *Service) Remind(ctx context.Context, customerID int64) (*models.CustomerAccountReminder, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if err := s.requireCustomer(ctx, user.MerchantID, customerID); err != nil {
		return nil, err
	}
	db := s.repo.DB(ctx)
	settings, err := customeraccount.LoadMerchantSettings(ctx, db, user.MerchantID)
	if err != nil {
		return nil, err
	}
	ledger, err := customeraccount.LoadLedger(ctx, db, user.MerchantID, customerID)
	if err != nil {
		return nil, err
	}
	oldest, ok := customeraccount.OldestUnpaid(ledger)
	if !ok {
		return nil, models.ErrAccountNothingDue
	}
	balance := ledgerBalance(ledger)
	channels, err := s.sendReminder(ctx, user.MerchantID, customerID, balance, oldest, settings, ReminderManual, user.UserID)
	if err != nil {
		return nil, err
	}
	return &models.CustomerAccountReminder{SentAt: s.deps.Now().Unix(), Source: ReminderManual, Channels: channels, Balance: balance}, nil
}

func ledgerBalance(entries []customeraccount.LedgerEntry) int64 {
	var b int64
	for _, e := range entries {
		b += e.Delta
	}
	return b
}

// RunReminders envoie les relances automatiques dues (tâche horaire ; chaque
// établissement est traité à reminderHour, heure locale).
func (s *Service) RunReminders(ctx context.Context) {
	log := logger.FromContext(ctx)
	merchants, err := s.repo.ReminderMerchants(ctx)
	if err != nil {
		log.Error("customer account reminders: merchants", zap.Error(err))
		return
	}
	for _, merchantID := range merchants {
		if err := s.runMerchantReminders(ctx, merchantID); err != nil {
			log.Error("customer account reminders", zap.String("merchant_id", merchantID), zap.Error(err))
		}
	}
}

func (s *Service) runMerchantReminders(ctx context.Context, merchantID string) error {
	loc, err := s.repo.MerchantLocation(ctx, merchantID)
	if err != nil {
		return err
	}
	now := s.deps.Now()
	if now.In(loc).Hour() != reminderHour {
		return nil
	}
	db := s.repo.DB(ctx)
	settings, err := customeraccount.LoadMerchantSettings(ctx, db, merchantID)
	if err != nil || !settings.ReminderEnabled {
		return err
	}
	ledger, err := customeraccount.LoadLedger(ctx, db, merchantID, 0)
	if err != nil {
		return err
	}
	for _, aging := range customeraccount.AgeDebts(ledger, now) {
		customerID := aging.CustomerSummary.CustomerID
		oldest := time.Unix(aging.OldestUnpaid, 0)
		sent, last, err := s.repo.RemindersSince(ctx, merchantID, customerID, oldest)
		if err != nil {
			return err
		}
		if !reminderDue(now, oldest, sent, last, settings.ReminderDays, settings.ReminderMax) {
			continue
		}
		if _, err := s.sendReminder(ctx, merchantID, customerID, -aging.Debt, oldest, settings, ReminderAuto, ""); err != nil &&
			err != models.ErrAccountNoContact {
			logger.FromContext(ctx).Warn("customer account reminder not sent",
				zap.String("merchant_id", merchantID), zap.Int64("customer_id", customerID), zap.Error(err))
		}
	}
	return nil
}
