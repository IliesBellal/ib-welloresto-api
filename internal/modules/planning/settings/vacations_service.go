package settings

import (
	"context"
	"database/sql"
	"strings"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	sharedpkg "welloresto-api/internal/modules/planning/shared"
)

func (s *Service) ListPlanningVacationPeriods(ctx context.Context) ([]PlanningVacationPeriod, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	return s.repo.ListPlanningVacationPeriods(ctx, user.MerchantID)
}

func (s *Service) CreatePlanningVacationPeriod(ctx context.Context, req PlanningVacationPeriodCreateRequest) (*PlanningVacationPeriod, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	startAt, endAt, err := normalizePlanningVacationRange(req.StartAt, req.EndAt)
	if err != nil {
		return nil, err
	}

	item := PlanningVacationPeriod{
		Label:   sharedpkg.TrimOptionalString(req.Label),
		StartAt: startAt,
		EndAt:   endAt,
	}
	return s.repo.CreatePlanningVacationPeriod(ctx, user.MerchantID, item)
}

func (s *Service) UpdatePlanningVacationPeriod(ctx context.Context, id string, req PlanningVacationPeriodUpdateRequest) (*PlanningVacationPeriod, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(id) == "" {
		return nil, models.ErrMissingResourceID
	}

	current, err := s.repo.GetPlanningVacationPeriod(ctx, user.MerchantID, id)
	if err == sql.ErrNoRows {
		return nil, models.ErrPlanningVacationPeriodNotFound
	}
	if err != nil {
		return nil, err
	}

	next := *current
	next.ID = id
	if req.Label != nil {
		next.Label = sharedpkg.TrimOptionalString(req.Label)
	}
	if req.StartAt != nil {
		startAt, err := normalizePlanningDateTime(*req.StartAt)
		if err != nil {
			return nil, err
		}
		next.StartAt = startAt
	}
	if req.EndAt != nil {
		endAt, err := normalizePlanningDateTime(*req.EndAt)
		if err != nil {
			return nil, err
		}
		next.EndAt = endAt
	}
	if next.EndAt <= next.StartAt {
		return nil, models.ErrPlanningVacationPeriodInvalidRange
	}

	return s.repo.UpdatePlanningVacationPeriod(ctx, user.MerchantID, next)
}

func (s *Service) DeletePlanningVacationPeriod(ctx context.Context, id string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return models.ErrUnauthorized
	}
	if strings.TrimSpace(id) == "" {
		return models.ErrMissingResourceID
	}
	if err := s.repo.SoftDeletePlanningVacationPeriod(ctx, user.MerchantID, id); err == sql.ErrNoRows {
		return models.ErrPlanningVacationPeriodNotFound
	} else {
		return err
	}
}

// normalizePlanningDateTime valide une date/heure locale marchand saisie par
// le client et la ramene au format canonique "YYYY-MM-DD HH:MM:SS" stocke en
// base (meme convention que POSHoursOfOperation.ValidFrom/ValidTo — voir le
// commentaire sur PlanningVacationPeriod). sharedpkg.ParsePlanningDateTime
// sert uniquement de validateur/normaliseur de format ici : le time.Time
// intermediaire n'est jamais compare ni persiste, seul le format numerique
// des composants saisis est reflete tel quel dans la chaine reformattee.
func normalizePlanningDateTime(raw string) (string, error) {
	parsed, err := sharedpkg.ParsePlanningDateTime(raw)
	if err != nil {
		return "", err
	}
	return parsed.Format("2006-01-02 15:04:05"), nil
}

func normalizePlanningVacationRange(startRaw, endRaw string) (string, string, error) {
	startAt, err := normalizePlanningDateTime(startRaw)
	if err != nil {
		return "", "", err
	}
	endAt, err := normalizePlanningDateTime(endRaw)
	if err != nil {
		return "", "", err
	}
	if endAt <= startAt {
		return "", "", models.ErrPlanningVacationPeriodInvalidRange
	}
	return startAt, endAt, nil
}
