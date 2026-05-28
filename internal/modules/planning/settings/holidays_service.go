package settings

import (
	"context"
	"database/sql"
	"strings"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	sharedpkg "welloresto-api/internal/modules/planning/shared"
)

func (s *Service) ListPlanningHolidays(ctx context.Context, filters PlanningHolidayListFilters) ([]PlanningHoliday, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	startDate, endDate, err := sharedpkg.ParsePlanningDateRange(filters.StartDate, filters.EndDate)
	if err != nil {
		return nil, models.ErrPlanningInvalidDate
	}
	return s.repo.ListPlanningHolidays(ctx, user.MerchantID, startDate, endDate)
}

func (s *Service) PatchPlanningHolidayOverride(ctx context.Context, holidayDateRaw string, req PlanningHolidayOverridePatchRequest) (*PlanningHoliday, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(holidayDateRaw) == "" {
		return nil, models.ErrMissingResourceID
	}
	holidayDate, err := sharedpkg.ParsePlanningDate(holidayDateRaw)
	if err != nil {
		return nil, models.ErrPlanningInvalidDate
	}
	if !hasPlanningHolidayPatchChanges(req) {
		return nil, models.ErrValidationError
	}

	current, err := s.repo.GetPlanningHolidayOverrideByDate(ctx, user.MerchantID, holidayDate)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	next := &planningHolidayOverrideRecord{HolidayDate: holidayDate}
	if current != nil {
		*next = *current
	}
	if req.ClearLabel != nil && *req.ClearLabel {
		next.Label = nil
	}
	if req.Label != nil {
		next.Label = sharedpkg.TrimOptionalString(req.Label)
	}
	if req.ClearIsOpen != nil && *req.ClearIsOpen {
		next.IsOpen = nil
	}
	if req.IsOpen != nil {
		next.IsOpen = req.IsOpen
	}
	if req.ClearCountAsHoliday != nil && *req.ClearCountAsHoliday {
		next.CountAsHoliday = nil
	}
	if req.CountAsHoliday != nil {
		next.CountAsHoliday = req.CountAsHoliday
	}

	if next.Label == nil && next.IsOpen == nil && next.CountAsHoliday == nil {
		if current == nil {
			return nil, models.ErrValidationError
		}
		if err := s.repo.SoftDeletePlanningHolidayOverride(ctx, user.MerchantID, holidayDate); err != nil {
			return nil, err
		}
		return s.repo.ResolvePlanningHoliday(ctx, user.MerchantID, holidayDate)
	}

	if current == nil {
		if _, err := s.repo.CreatePlanningHolidayOverride(ctx, user.MerchantID, *next); err != nil {
			return nil, err
		}
	} else {
		if _, err := s.repo.UpdatePlanningHolidayOverride(ctx, user.MerchantID, *next); err != nil {
			return nil, err
		}
	}

	return s.repo.ResolvePlanningHoliday(ctx, user.MerchantID, holidayDate)
}

func (s *Service) DeletePlanningHolidayOverride(ctx context.Context, holidayDateRaw string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return models.ErrUnauthorized
	}
	if strings.TrimSpace(holidayDateRaw) == "" {
		return models.ErrMissingResourceID
	}
	holidayDate, err := sharedpkg.ParsePlanningDate(holidayDateRaw)
	if err != nil {
		return models.ErrPlanningInvalidDate
	}
	if err := s.repo.SoftDeletePlanningHolidayOverride(ctx, user.MerchantID, holidayDate); err == sql.ErrNoRows {
		return models.ErrPlanningHolidayOverrideNotFound
	} else {
		return err
	}
}

func hasPlanningHolidayPatchChanges(req PlanningHolidayOverridePatchRequest) bool {
	return req.Label != nil || req.IsOpen != nil || req.CountAsHoliday != nil || (req.ClearLabel != nil && *req.ClearLabel) || (req.ClearIsOpen != nil && *req.ClearIsOpen) || (req.ClearCountAsHoliday != nil && *req.ClearCountAsHoliday)
}
