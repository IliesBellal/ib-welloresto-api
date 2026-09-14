package subscriptions

import (
	"context"
	"strconv"
	"time"

	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

// CreateOverride validates and applies one commercial dérogation (LOT B B1d).
// The audit row (subscription_overrides) and the side-effect write
// (subscriptions.*_enabled / override_price_cents / max_kiosks, depending on
// kind) happen inside one transaction — a dérogation that's recorded but
// never actually took effect (or vice versa) would be worse than either
// failing cleanly.
// trialEndsAt is B2c-1's addition: for kind='price', a non-nil value grants
// the merchant immediate LIVE access (GrantTrialActivation) — exactly what
// "accès gratuit" is for — alongside the price override itself. NULL
// behaves exactly as before B2c-1 (§7.3, no LIVE side effect). Other kinds
// may also carry a trial_ends_at (reminders/bandeau apply generically, see
// RunTrialExpiryCheck) but only kind='price' grants/reverts activation —
// documented as a scope choice in docs/decisions.md, not spelled out
// explicitly for every kind by the brief.
func (s *Service) CreateOverride(ctx context.Context, merchantID, kind, target, reason string, note *string, createdBy string, expiresAt, trialEndsAt *time.Time) (Override, error) {
	if !ValidOverrideKinds[kind] {
		return Override{}, models.ErrInvalidOverrideKind
	}
	if !ValidOverrideReasons[reason] {
		return Override{}, models.ErrInvalidOverrideReason
	}

	var apply func(ctx context.Context) error

	switch kind {
	case OverrideKindModule:
		if !ValidCodes[target] {
			return Override{}, models.ErrOverrideTargetUnsupported
		}
		// P3 (LOT B PRÉALABLE) : kiosk/sms have no real price in
		// pricing_catalog yet — granting free access to them via dérogation
		// is blocked for the same reason AddItem refuses to bill them.
		if target == CodeKiosk || target == CodeSMS {
			return Override{}, models.ErrSubscriptionItemPriceUnavailable
		}
		column, ok := moduleOverrideColumns[target]
		if !ok {
			return Override{}, models.ErrOverrideTargetUnsupported
		}
		apply = func(ctx context.Context) error {
			return s.repo.SetModuleEnabled(ctx, merchantID, column)
		}

	case OverrideKindPrice:
		cents, err := parseNonNegativeInt(target)
		if err != nil {
			return Override{}, models.ErrOverrideTargetUnsupported
		}
		apply = func(ctx context.Context) error {
			if err := s.repo.SetOverridePriceCents(ctx, merchantID, cents); err != nil {
				return err
			}
			if trialEndsAt == nil {
				return nil
			}
			return s.repo.GrantTrialActivation(ctx, merchantID)
		}

	case OverrideKindKioskQuota:
		quota, err := parseNonNegativeInt(target)
		if err != nil {
			return Override{}, models.ErrOverrideTargetUnsupported
		}
		apply = func(ctx context.Context) error {
			return s.repo.SetMaxKiosks(ctx, merchantID, quota)
		}
	}

	var created Override
	err := dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		var err error
		created, err = s.repo.CreateOverrideRow(txCtx, merchantID, kind, target, reason, note, createdBy, expiresAt, trialEndsAt)
		if err != nil {
			return err
		}
		return apply(txCtx)
	})
	if err != nil {
		return Override{}, err
	}
	return created, nil
}

// ListActiveOverrides — thin pass-through, kept on Service (rather than
// callers using Repository directly) so the handler only ever depends on
// Service, matching this repo's Handler -> Service -> Repository layering.
func (s *Service) ListActiveOverrides(ctx context.Context) ([]Override, error) {
	return s.repo.ListActiveOverrides(ctx)
}

// RevokeOverride — see Repository.RevokeOverride's doc comment. Deliberately
// does not attempt to undo the side-effect write CreateOverride applied
// (flip the *_enabled column back off, clear override_price_cents, restore
// the previous max_kiosks): no prior value is recorded anywhere to restore
// to, and guessing one (e.g. "off") could be wrong if the module was already
// enabled independently of this dérogation. Reverting the underlying state
// is left as a manual staff action alongside the revocation — flagged as an
// open question in this chantier's report, not a silently-applied choice.
func (s *Service) RevokeOverride(ctx context.Context, id string) error {
	return s.repo.RevokeOverride(ctx, id)
}

// GetNearestActiveTrial — thin pass-through, B2c-1's bandeau data source
// (consumed by billing.Service.GetActivationStatus).
func (s *Service) GetNearestActiveTrial(ctx context.Context, merchantID string) (*Override, error) {
	return s.repo.GetNearestActiveTrial(ctx, merchantID)
}

// trialReminderWindow — how close to trial_ends_at each reminder fires.
// "à J-7 et J-1" (§ B2c-1) : compared by calendar day, not exact hour — see
// RunTrialExpiryCheck.
const (
	trialReminder7dWindow = 7 * 24 * time.Hour
	trialReminder1dWindow = 1 * 24 * time.Hour
)

// RunTrialExpiryCheck implements B2c-1's cron-driven trial lifecycle —
// called from the same @hourly slot dunning's cascade already uses (see
// internal/tasks/dunning.go), not a second cron registration. Re-evaluates
// every active trial override fresh on each call: never a pre-scheduled
// future send, same discipline as the dunning cascade (B2b-1).
func (s *Service) RunTrialExpiryCheck(ctx context.Context) []error {
	overrides, err := s.repo.ListActiveTrialOverrides(ctx)
	if err != nil {
		return []error{err}
	}

	var errs []error
	now := time.Now()
	for _, o := range overrides {
		if err := s.evaluateTrial(ctx, o, now); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func (s *Service) evaluateTrial(ctx context.Context, o Override, now time.Time) error {
	deadline := *o.TrialEndsAt

	if !now.Before(deadline) {
		// Échéance dépassée.
		hasMandate, err := s.repo.HasSepaMandate(ctx, o.MerchantID)
		if err != nil {
			return err
		}
		if err := s.repo.RevokeOverride(ctx, o.ID); err != nil {
			return err
		}
		if hasMandate {
			// "devient simplement caduque en douceur" — l'abonnement
			// récurrent (B2c-0) a pris le relais, rien d'autre à faire.
			return nil
		}
		if o.Kind == OverrideKindPrice {
			return s.repo.RevertTrialActivation(ctx, o.MerchantID)
		}
		return nil
	}

	// Échéance à venir : rappels J-7 / J-1, jamais renvoyés (guard sur les
	// colonnes *_sent_at).
	if o.TrialReminder1dSentAt == nil && deadline.Sub(now) <= trialReminder1dWindow {
		if err := s.sendTrialReminder(ctx, o, deadline, 1); err != nil {
			return err
		}
		return s.repo.MarkTrialReminder1dSent(ctx, o.ID)
	}
	if o.TrialReminder7dSentAt == nil && deadline.Sub(now) <= trialReminder7dWindow {
		if err := s.sendTrialReminder(ctx, o, deadline, 7); err != nil {
			return err
		}
		return s.repo.MarkTrialReminder7dSent(ctx, o.ID)
	}
	return nil
}

// sendTrialReminder — template name is new, must be created in Brevo before
// it produces real content (same caveat as dunning's templates, see
// docs/decisions.md). mailer is nil-safe: a Service built without
// SetMailer (most tests) simply skips sending rather than panicking.
func (s *Service) sendTrialReminder(ctx context.Context, o Override, deadline time.Time, daysRemaining int) error {
	if s.mailer == nil {
		return nil
	}
	contact, err := s.repo.GetMerchantOwnerContact(ctx, o.MerchantID)
	if err != nil {
		return err
	}
	s.mailer.SendAsync("Wello Resto", mailer.InvoiceEmail, contact.Email, "Votre accès gratuit se termine bientôt", "trial_expiry_reminder.html", map[string]interface{}{
		"MerchantName":  contact.Name,
		"DaysRemaining": daysRemaining,
		"TrialEndsAt":   deadline.Format("02/01/2006"),
	})
	return nil
}

func parseNonNegativeInt(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, models.ErrOverrideTargetUnsupported
	}
	return n, nil
}
