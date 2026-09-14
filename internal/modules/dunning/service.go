package dunning

import (
	"context"
	"database/sql"
	"time"

	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/infrastructure/sms"
	"welloresto-api/internal/modules/openinghours"
)

// stripeRetry is the slice of Stripe capability RetryNow needs — a seam
// mirroring billing.stripeBillingClient, for the same reason (no
// STRIPE_API_KEY in this dev environment).
type stripeRetry interface {
	RetryLatestOpenInvoice(customerID string) error
}

type Service struct {
	repo   *Repository
	db     *sql.DB // raw handle openinghours.FetchActiveSlots needs directly
	email  mailer.Service
	sms    sms.Service
	stripe stripeRetry
	// billingRepo resolves merchantID -> stripe_customer_id for RetryNow —
	// a direct cross-module read of platform_billing_customers (same
	// posture as every other cross-module read already in this codebase),
	// not an import of the billing package itself.
}

func NewService(repo *Repository, db *sql.DB, email mailer.Service, smsSvc sms.Service, stripeSvc stripeRetry) *Service {
	return &Service{repo: repo, db: db, email: email, sms: smsSvc, stripe: stripeSvc}
}

// HandlePaymentFailed implements the invoice.payment_failed side of §7.5's
// state machine — called once per Stripe event, escalating on the 1st vs
// 2nd failure, a no-op on any further one (documented assumption:
// subscriptions.status is already 'past_due' and the 14-day window already
// running by then — nothing more to escalate to before B2b-2's suspension
// itself, which the cron applies once the deadline passes).
func (s *Service) HandlePaymentFailed(ctx context.Context, merchantID string) error {
	existing, err := s.repo.GetState(ctx, merchantID)
	if err != nil {
		return err
	}

	if existing == nil {
		if err := s.repo.CreateFirstFailure(ctx, merchantID); err != nil {
			return err
		}
		if err := s.repo.SetSubscriptionStatus(ctx, merchantID, "past_due"); err != nil {
			return err
		}
		return s.sendFirstFailureNotice(ctx, merchantID)
	}

	if existing.SecondFailedAt == nil {
		deadline := time.Now().Add(suspensionWindow)
		if err := s.repo.RecordSecondFailure(ctx, merchantID, deadline); err != nil {
			return err
		}
		return s.sendSecondFailureNotice(ctx, merchantID, deadline)
	}

	return nil
}

// ClearDunning is called from invoice.paid's success path (in addition to
// that handler's own subscriptions.status='active' write) — "toutes les
// relances programmées annulées": since nothing is ever pre-scheduled (see
// subscription_dunning's table comment), clearing the row is the entire
// cancellation — the cron simply won't find this merchant next time it runs.
func (s *Service) ClearDunning(ctx context.Context, merchantID string) error {
	return s.repo.ClearDunning(ctx, merchantID)
}

// RunCascade is the cron entry point (internal/tasks) — re-evaluates every
// currently past_due merchant's real state on each call, exactly as the
// brief requires ("déclencheur réévalué à l'envoi, jamais un envoi planifié
// à l'avance"). One merchant's error doesn't stop the rest.
func (s *Service) RunCascade(ctx context.Context) []error {
	states, err := s.repo.ListPastDue(ctx)
	if err != nil {
		return []error{err}
	}

	var errs []error
	for _, st := range states {
		if err := s.evaluateOne(ctx, st); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func (s *Service) evaluateOne(ctx context.Context, st State) error {
	open, err := s.isCurrentlyOpen(ctx, st.MerchantID)
	if err != nil {
		return err
	}
	if open {
		// "Aucun envoi pendant les services" — try again next cron tick.
		return nil
	}

	now := time.Now()

	if st.SuspensionDeadline != nil && !now.Before(*st.SuspensionDeadline) {
		return s.repo.SetSubscriptionStatus(ctx, st.MerchantID, "suspended")
	}

	// Once inside the final 48h window, that phase owns every subsequent
	// cron tick until suspension — never fall through to the weekly cadence
	// below (a bug caught by test: without this early return, the run right
	// after the final notice was sent found FinalNoticeSentAt already set,
	// skipped this branch, and incorrectly fired a weekly reminder too).
	if st.SuspensionDeadline != nil && !now.Before(st.SuspensionDeadline.Add(-finalNoticeLeadTime)) {
		if st.FinalNoticeSentAt != nil {
			return nil
		}
		if err := s.sendFinalNotice(ctx, st.MerchantID, *st.SuspensionDeadline); err != nil {
			return err
		}
		return s.repo.MarkFinalNoticeSent(ctx, st.MerchantID)
	}

	if st.LastReminderSentAt == nil || now.Sub(*st.LastReminderSentAt) >= weeklyReminderInterval {
		if err := s.sendWeeklyReminder(ctx, st.MerchantID, st.ReminderCount+1, st.SuspensionDeadline); err != nil {
			return err
		}
		return s.repo.IncrementReminder(ctx, st.MerchantID)
	}

	return nil
}

// isCurrentlyOpen evaluates "pendant les services" in the merchant's own
// timezone (openinghours.ComputePOSStatus, the same computation POS status
// already relies on) — reused rather than reimplemented.
func (s *Service) isCurrentlyOpen(ctx context.Context, merchantID string) (bool, error) {
	tzName, err := s.repo.GetMerchantTimezone(ctx, merchantID)
	if err != nil {
		return false, err
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)

	slots, err := openinghours.FetchActiveSlots(ctx, s.db, merchantID, now)
	if err != nil {
		return false, err
	}
	return openinghours.ComputePOSStatus(now, slots).IsOpen, nil
}

// --- Notices ----------------------------------------------------------------
//
// Template names below are new Brevo templates this chantier introduces —
// they must be created in Brevo before these sends produce real content (see
// docs/decisions.md); the code path itself is complete and correct
// regardless.

func (s *Service) sendFirstFailureNotice(ctx context.Context, merchantID string) error {
	contact, err := s.repo.GetMerchantOwnerContact(ctx, merchantID)
	if err != nil {
		return err
	}
	// "SANS réclamation — dit quoi vérifier côté banque... pas 'vous devez'"
	// — informational tone lives in the Brevo template content itself, not
	// in this call site; only the template NAME and data are Go's concern.
	s.email.SendAsync("Wello Resto", mailer.InvoiceEmail, contact.Email, "Un point sur votre moyen de paiement", "dunning_first_notice.html", map[string]interface{}{
		"MerchantName": contact.Name,
	})
	return nil
}

func (s *Service) sendSecondFailureNotice(ctx context.Context, merchantID string, deadline time.Time) error {
	contact, err := s.repo.GetMerchantOwnerContact(ctx, merchantID)
	if err != nil {
		return err
	}
	data := map[string]interface{}{
		"MerchantName":       contact.Name,
		"SuspensionDeadline": deadline.Format("02/01/2006"),
	}
	s.email.SendAsync("Wello Resto", mailer.InvoiceEmail, contact.Email, "Action requise sur votre abonnement", "dunning_second_notice.html", data)
	if contact.Tel != "" {
		s.sms.SendSMSAsync("Wello Resto", contact.Tel, "Un second prélèvement a échoué. Régularisez avant le "+deadline.Format("02/01/2006")+" pour éviter la suspension de votre compte.")
	}
	return nil
}

func (s *Service) sendWeeklyReminder(ctx context.Context, merchantID string, reminderNumber int, deadline *time.Time) error {
	contact, err := s.repo.GetMerchantOwnerContact(ctx, merchantID)
	if err != nil {
		return err
	}
	data := map[string]interface{}{
		"MerchantName": contact.Name,
	}
	// "à partir de la 2e relance, annonce la date de suspension" — only
	// meaningful once the 14-day window is actually open (deadline != nil);
	// before that, a weekly reminder stays purely informational regardless
	// of its number.
	if reminderNumber >= 2 && deadline != nil {
		data["SuspensionDeadline"] = deadline.Format("02/01/2006")
	}
	s.email.SendAsync("Wello Resto", mailer.InvoiceEmail, contact.Email, "Rappel : votre moyen de paiement", "dunning_weekly_reminder.html", data)
	return nil
}

func (s *Service) sendFinalNotice(ctx context.Context, merchantID string, deadline time.Time) error {
	contact, err := s.repo.GetMerchantOwnerContact(ctx, merchantID)
	if err != nil {
		return err
	}
	data := map[string]interface{}{
		"MerchantName":       contact.Name,
		"SuspensionDeadline": deadline.Format("02/01/2006"),
	}
	s.email.SendAsync("Wello Resto", mailer.InvoiceEmail, contact.Email, "Dernier rappel avant suspension", "dunning_final_notice.html", data)
	if contact.Tel != "" {
		s.sms.SendSMSAsync("Wello Resto", contact.Tel, "Dernier rappel : votre compte sera suspendu le "+deadline.Format("02/01/2006")+" sans régularisation.")
	}
	return nil
}

// RetryNow implements the back-office "réessayer maintenant" button —
// retries collection on the merchant's existing mandate (its Stripe
// Customer's latest open invoice), never asking for a new IBAN. Assumes a
// Stripe Invoice/Subscription already exists upstream of this cascade (this
// chantier builds the cascade reacting to invoice.*/setup_intent.* events,
// not the recurring invoice creation itself — out of scope here, see
// docs/decisions.md).
func (s *Service) RetryNow(ctx context.Context, merchantID string) error {
	customerID, err := s.repo.GetStripeCustomerID(ctx, merchantID)
	if err != nil {
		return err
	}
	return s.stripe.RetryLatestOpenInvoice(customerID)
}
