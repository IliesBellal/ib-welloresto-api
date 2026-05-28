package settings

import (
	"context"
	"strings"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetSettings(ctx context.Context) (*PlanningSettings, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	return s.repo.GetOrCreateSettings(ctx, user.MerchantID)
}

func (s *Service) UpdateSettings(ctx context.Context, req PlanningSettingsUpdateRequest) (*PlanningSettings, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if req.LaborCountryCode != nil && strings.TrimSpace(*req.LaborCountryCode) == "" {
		return nil, models.ErrPlanningInvalidCountryCode
	}
	if req.MinDailyRestHours != nil && *req.MinDailyRestHours < 0 {
		return nil, models.ErrPlanningInvalidHours
	}
	if req.MinBreakMinutes != nil && *req.MinBreakMinutes < 0 {
		return nil, models.ErrPlanningInvalidHours
	}
	if req.AttendanceSource != nil && !IsValidAttendanceSource(*req.AttendanceSource) {
		return nil, models.ErrPlanningAttendanceSourceInvalid
	}
	if req.ShiftSwapApprovalMode != nil && !IsValidShiftSwapApprovalMode(*req.ShiftSwapApprovalMode) {
		return nil, models.ErrPlanningShiftSwapApprovalModeInvalid
	}
	return s.repo.UpdateSettings(ctx, user.MerchantID, req)
}
