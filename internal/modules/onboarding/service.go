package onboarding

import (
	"context"
	"fmt"
	"strings"

	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// GetOnboarding returns merchantID's onboarding tasks, scoped to the
// caller's own merchant — every authenticated request in this API is scoped
// to the merchant carried by its token (CLAUDE.md), and this route is no
// exception: {id} in the URL must match the caller's own merchant.
func (s *Service) GetOnboarding(ctx context.Context, merchantID string) ([]Task, error) {
	currentUser, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return nil, models.ErrMissingResourceID
	}
	if currentUser.MerchantID != merchantID {
		return nil, models.ErrForbidden
	}
	return s.repo.ListByMerchant(ctx, merchantID)
}

// RecomputeOnboarding is LOT A Semaine 3, Chantier 13's automatic-completion
// entry point: called from every write path whose success can complete an
// onboarding task (menu: after a product gains a price + VAT rates; device:
// after a kiosk enrollment; team: after a staff member is added; logo: after
// merchant.logo_url is set) rather than from a SQL trigger, so the reasoning
// stays in Go and is directly testable per task.
//
// "payment" is deliberately absent — it is LOT B's SEPA-mandate concern; see
// MarkPaymentMandateAccepted for the hook this chantier leaves ready for it.
//
// Idempotent: each condition check is re-evaluated from current data (not
// from a delta), and SetTaskDoneIfNotDone no-ops once a task is already
// 'done', so calling this twice for the same or unrelated events is always
// safe — callers are never expected to reason about whether it "needs" to
// run, only to call it after any write that could plausibly affect one of
// these four conditions.
func (s *Service) RecomputeOnboarding(ctx context.Context, merchantID string) error {
	checks := []struct {
		taskKey string
		met     func(context.Context, string) (bool, error)
	}{
		{"menu", s.repo.MenuConditionMet},
		{"device", s.repo.DeviceConditionMet},
		{"team", s.repo.TeamConditionMet},
		{"logo", s.repo.LogoConditionMet},
	}
	for _, c := range checks {
		met, err := c.met(ctx, merchantID)
		if err != nil {
			return fmt.Errorf("RecomputeOnboarding(%s): check %s: %w", merchantID, c.taskKey, err)
		}
		if !met {
			continue
		}
		if err := s.repo.SetTaskDoneIfNotDone(ctx, merchantID, c.taskKey); err != nil {
			return fmt.Errorf("RecomputeOnboarding(%s): mark %s done: %w", merchantID, c.taskKey, err)
		}
	}
	return nil
}

// RecomputeOnboardingBestEffort is the call site variant for the four write
// paths above: onboarding tracking must never fail the business operation
// that just succeeded (a product save, a kiosk enrollment, a staff-member
// creation, a logo upload), so a recompute error is logged, not returned.
func (s *Service) RecomputeOnboardingBestEffort(ctx context.Context, merchantID string) {
	if err := s.RecomputeOnboarding(ctx, merchantID); err != nil {
		logger.FromContext(ctx).Warn("onboarding: RecomputeOnboarding failed", zap.String("merchant_id", merchantID), zap.Error(err))
	}
}

// MarkPaymentMandateAccepted is the "payment" task's hook, per the chantier's
// explicit instruction to place it without implementing the SEPA mandate
// flow itself (LOT B). Not called anywhere yet — LOT B's webhook/handler for
// an accepted SEPA mandate is the intended, not-yet-built caller.
func (s *Service) MarkPaymentMandateAccepted(ctx context.Context, merchantID string) error {
	return s.repo.SetTaskDoneIfNotDone(ctx, merchantID, "payment")
}

// SkipTask handles POST /v1/merchants/{id}/onboarding/{code}/skip — owner-only
// (Rights.Admin, the same flag signup freezes on the account created at
// merchant creation — see signup.Service.createOwnerAndMerchant), restricted
// to SkippableTaskKeys, with a mandatory reason.
func (s *Service) SkipTask(ctx context.Context, merchantID, taskKey, reason string) error {
	currentUser, err := middleware.UserFromContext(ctx)
	if err != nil {
		return models.ErrUnauthorized
	}
	merchantID = strings.TrimSpace(merchantID)
	if currentUser.MerchantID != merchantID {
		return models.ErrForbidden
	}
	if !currentUser.Rights.Admin {
		return models.ErrForbidden
	}
	taskKey = strings.TrimSpace(taskKey)
	if !SkippableTaskKeys[taskKey] {
		return models.ErrOnboardingTaskNotSkippable
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return models.ErrOnboardingSkipReasonRequired
	}

	changed, err := s.repo.SetTaskSkipped(ctx, merchantID, taskKey, reason)
	if err != nil {
		return err
	}
	if !changed {
		return models.ErrOnboardingTaskAlreadyDone
	}
	return nil
}
