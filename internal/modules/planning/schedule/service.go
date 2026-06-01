package schedule

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	auditpkg "welloresto-api/internal/modules/audit"
	employeespkg "welloresto-api/internal/modules/planning/employees"
	sharedpkg "welloresto-api/internal/modules/planning/shared"
)

type EmployeeReader interface {
	GetEmployeeByID(ctx context.Context, merchantID, employeeID string) (*employeespkg.Employee, error)
}

type PositionReader interface {
	GetEmployeePositionByID(ctx context.Context, merchantID, positionID string) (*employeespkg.EmployeePosition, error)
	GetEmployeePositionByLabel(ctx context.Context, merchantID, label, excludeID string) (*employeespkg.EmployeePosition, error)
}

type Service struct {
	repo         *Repository
	employeeRepo EmployeeReader
	positionRepo PositionReader
	auditService auditpkg.AuditService
}

func NewService(repo *Repository, employeeRepo EmployeeReader, positionRepo PositionReader, auditService auditpkg.AuditService) *Service {
	return &Service{repo: repo, employeeRepo: employeeRepo, positionRepo: positionRepo, auditService: auditService}
}

func (s *Service) ListPlanningWeeks(ctx context.Context) ([]PlanningWeek, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	return s.repo.ListPlanningWeeks(ctx, user.MerchantID)
}

func (s *Service) CreatePlanningWeek(ctx context.Context, req PlanningWeekCreateRequest) (*PlanningWeek, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	startDate, endDate, err := sharedpkg.ParsePlanningDateRange(req.StartDate, req.EndDate)
	if err != nil {
		return nil, err
	}
	status := "draft"
	if req.Status != nil && strings.TrimSpace(*req.Status) != "" {
		status = strings.ToLower(strings.TrimSpace(*req.Status))
	}
	if !sharedpkg.IsValidPlanningWeekStatus(status) {
		return nil, models.ErrValidationError
	}
	if existing, existingErr := s.repo.GetPlanningWeekByStartDate(ctx, user.MerchantID, startDate, ""); existingErr == nil && existing != nil {
		return nil, models.ErrPlanningWeekAlreadyExists
	} else if existingErr != nil && existingErr != sql.ErrNoRows {
		return nil, existingErr
	}
	week := PlanningWeek{Label: req.Label, StartDate: startDate, EndDate: endDate, Status: status, Notes: req.Notes}
	return s.repo.CreatePlanningWeek(ctx, user.MerchantID, week)
}

func (s *Service) GetPlanningWeek(ctx context.Context, weekID string) (*PlanningWeek, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(weekID) == "" {
		return nil, models.ErrMissingResourceID
	}
	week, err := s.repo.GetPlanningWeekByID(ctx, user.MerchantID, weekID)
	if err == sql.ErrNoRows || week == nil {
		return nil, models.ErrPlanningWeekNotFound
	}
	return week, err
}

func (s *Service) UpdatePlanningWeek(ctx context.Context, weekID string, req PlanningWeekUpdateRequest) (*PlanningWeek, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(weekID) == "" {
		return nil, models.ErrMissingResourceID
	}
	current, err := s.repo.GetPlanningWeekByID(ctx, user.MerchantID, weekID)
	if err == sql.ErrNoRows || current == nil {
		return nil, models.ErrPlanningWeekNotFound
	} else if err != nil {
		return nil, err
	}
	updated := PlanningWeek{}
	if req.Label != nil {
		updated.Label = req.Label
	}
	if req.StartDate != nil {
		startDate, parseErr := sharedpkg.ParsePlanningDate(*req.StartDate)
		if parseErr != nil {
			return nil, parseErr
		}
		updated.StartDate = startDate
	}
	if req.EndDate != nil {
		endDate, parseErr := sharedpkg.ParsePlanningDate(*req.EndDate)
		if parseErr != nil {
			return nil, parseErr
		}
		updated.EndDate = endDate
	}
	if req.Status != nil && strings.TrimSpace(*req.Status) != "" {
		status := strings.ToLower(strings.TrimSpace(*req.Status))
		if !sharedpkg.IsValidPlanningWeekStatus(status) {
			return nil, models.ErrValidationError
		}
		updated.Status = status
	}
	if req.Notes != nil {
		updated.Notes = req.Notes
	}
	if !updated.StartDate.IsZero() || !updated.EndDate.IsZero() {
		start := current.StartDate
		end := current.EndDate
		if !updated.StartDate.IsZero() {
			start = updated.StartDate
		}
		if !updated.EndDate.IsZero() {
			end = updated.EndDate
		}
		if end.Before(start) {
			return nil, models.ErrPlanningInvalidDate
		}
	}
	startDate := current.StartDate
	if !updated.StartDate.IsZero() {
		startDate = updated.StartDate
	}
	if existing, existingErr := s.repo.GetPlanningWeekByStartDate(ctx, user.MerchantID, startDate, weekID); existingErr == nil && existing != nil {
		return nil, models.ErrPlanningWeekAlreadyExists
	} else if existingErr != nil && existingErr != sql.ErrNoRows {
		return nil, existingErr
	}
	return s.repo.UpdatePlanningWeek(ctx, user.MerchantID, weekID, updated)
}

func (s *Service) DeletePlanningWeek(ctx context.Context, weekID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return models.ErrUnauthorized
	}
	if strings.TrimSpace(weekID) == "" {
		return models.ErrMissingResourceID
	}
	if err := s.repo.SoftDeletePlanningWeek(ctx, user.MerchantID, weekID); err == sql.ErrNoRows {
		return models.ErrPlanningWeekNotFound
	} else {
		return err
	}
}

func (s *Service) ListPlanningShifts(ctx context.Context, weekID string) ([]PlanningShift, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(weekID) == "" {
		return nil, models.ErrMissingResourceID
	}
	if _, err := s.repo.GetPlanningWeekByID(ctx, user.MerchantID, weekID); err == sql.ErrNoRows {
		return nil, models.ErrPlanningWeekNotFound
	} else if err != nil {
		return nil, err
	}
	return s.repo.ListPlanningShifts(ctx, user.MerchantID, weekID)
}

func (s *Service) GetPlanningShift(ctx context.Context, shiftID string) (*PlanningShift, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(shiftID) == "" {
		return nil, models.ErrMissingResourceID
	}
	shift, err := s.repo.GetPlanningShiftByID(ctx, user.MerchantID, shiftID)
	if err == sql.ErrNoRows || shift == nil {
		return nil, models.ErrPlanningShiftNotFound
	}
	return shift, err
}

func (s *Service) CreatePlanningShift(ctx context.Context, weekID string, req PlanningShiftCreateRequest) (*PlanningShift, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(weekID) == "" {
		return nil, models.ErrMissingResourceID
	}
	week, err := s.repo.GetPlanningWeekByID(ctx, user.MerchantID, weekID)
	if err == sql.ErrNoRows || week == nil {
		return nil, models.ErrPlanningWeekNotFound
	} else if err != nil {
		return nil, err
	}
	// Title filter is too restrictive and shouldn't be required
	if strings.TrimSpace(req.Title) == "" {
		//return nil, models.ErrValidationError
	}
	shiftDate, err := sharedpkg.ParsePlanningDate(req.ShiftDate)
	if err != nil {
		return nil, err
	}
	if shiftDate.Before(week.StartDate) || shiftDate.After(week.EndDate) {
		return nil, models.ErrPlanningInvalidDate
	}
	startTime, endTime, err := sharedpkg.ParsePlanningTimeRange(req.StartTime, req.EndTime)
	if err != nil {
		return nil, err
	}
	normalizedEmployeeID := normalizeShiftEmployeeID(req.EmployeeID)
	if normalizedEmployeeID != nil {
		if _, err := s.employeeRepo.GetEmployeeByID(ctx, user.MerchantID, *normalizedEmployeeID); err != nil {
			return nil, models.ErrPlanningEmployeeNotFound
		}
		if conflictErr := s.EnsureShiftHasNoConflicts(ctx, user.MerchantID, nil, *normalizedEmployeeID, shiftDate, startTime, endTime); conflictErr != nil {
			return nil, conflictErr
		}
	}
	resolvedPositionID, resolvedPositionLabel, err := s.resolveShiftPosition(ctx, user.MerchantID, req.PositionID, req.Position)
	if err != nil {
		return nil, err
	}
	status := "planned"
	if req.Status != nil && strings.TrimSpace(*req.Status) != "" {
		status = strings.ToLower(strings.TrimSpace(*req.Status))
	}
	if !sharedpkg.IsValidPlanningShiftStatus(status) {
		return nil, models.ErrValidationError
	}
	breakMinutes := 0
	if req.BreakMinutes != nil {
		breakMinutes = *req.BreakMinutes
	}
	shift := PlanningShift{
		WeekID:       weekID,
		EmployeeID:   normalizedEmployeeID,
		PositionID:   resolvedPositionID,
		Title:        strings.TrimSpace(req.Title),
		ShiftDate:    shiftDate,
		StartTime:    startTime,
		EndTime:      endTime,
		BreakMinutes: breakMinutes,
		Position:     resolvedPositionLabel,
		Location:     req.Location,
		Notes:        req.Notes,
		Status:       status,
	}
	createdShift, err := s.repo.CreatePlanningShift(ctx, user.MerchantID, shift)
	if err != nil {
		return nil, err
	}
	s.logShiftAssignmentChange(ctx, user.MerchantID, user.UserID, createdShift.ID, nil, createdShift.EmployeeID)
	return createdShift, nil
}

func (s *Service) UpdatePlanningShift(ctx context.Context, shiftID string, req PlanningShiftUpdateRequest) (*PlanningShift, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(shiftID) == "" {
		return nil, models.ErrMissingResourceID
	}
	current, err := s.repo.GetPlanningShiftByID(ctx, user.MerchantID, shiftID)
	if err == sql.ErrNoRows || current == nil {
		return nil, models.ErrPlanningShiftNotFound
	} else if err != nil {
		return nil, err
	}
	previousEmployeeID := normalizeShiftEmployeeID(current.EmployeeID)
	if req.Title != nil {
		if strings.TrimSpace(*req.Title) == "" {
			return nil, models.ErrValidationError
		}
		current.Title = strings.TrimSpace(*req.Title)
	}
	if req.EmployeeID.Present {
		normalizedEmployeeID := normalizeShiftEmployeeID(req.EmployeeID.Value)
		if normalizedEmployeeID == nil {
			current.EmployeeID = nil
		} else {
			if _, err := s.employeeRepo.GetEmployeeByID(ctx, user.MerchantID, *normalizedEmployeeID); err != nil {
				return nil, models.ErrPlanningEmployeeNotFound
			}
			current.EmployeeID = normalizedEmployeeID
		}
	}
	if req.PositionID.Present {
		resolvedPositionID, resolvedPositionLabel, resolveErr := s.resolveShiftPosition(ctx, user.MerchantID, req.PositionID.Value, nil)
		if resolveErr != nil {
			return nil, resolveErr
		}
		current.PositionID = resolvedPositionID
		current.Position = resolvedPositionLabel
	} else if req.Position != nil {
		resolvedPositionID, resolvedPositionLabel, resolveErr := s.resolveShiftPosition(ctx, user.MerchantID, nil, req.Position)
		if resolveErr != nil {
			return nil, resolveErr
		}
		current.PositionID = resolvedPositionID
		current.Position = resolvedPositionLabel
	}
	if req.ShiftDate != nil {
		shiftDate, parseErr := sharedpkg.ParsePlanningDate(*req.ShiftDate)
		if parseErr != nil {
			return nil, parseErr
		}
		current.ShiftDate = shiftDate
	}
	if req.StartTime != nil {
		current.StartTime = strings.TrimSpace(*req.StartTime)
	}
	if req.EndTime != nil {
		current.EndTime = strings.TrimSpace(*req.EndTime)
	}
	if req.BreakMinutes != nil {
		current.BreakMinutes = *req.BreakMinutes
	}
	if req.Location != nil {
		current.Location = req.Location
	}
	if req.Notes != nil {
		current.Notes = req.Notes
	}
	if req.Status != nil && strings.TrimSpace(*req.Status) != "" {
		status := strings.ToLower(strings.TrimSpace(*req.Status))
		if !sharedpkg.IsValidPlanningShiftStatus(status) {
			return nil, models.ErrValidationError
		}
		current.Status = status
	}
	startTime, endTime, parseErr := sharedpkg.ParsePlanningTimeRange(current.StartTime, current.EndTime)
	if parseErr != nil {
		return nil, parseErr
	}
	current.StartTime = startTime
	current.EndTime = endTime
	week, err := s.repo.GetPlanningWeekByID(ctx, user.MerchantID, current.WeekID)
	if err == sql.ErrNoRows || week == nil {
		return nil, models.ErrPlanningWeekNotFound
	} else if err != nil {
		return nil, err
	}
	if current.ShiftDate.Before(week.StartDate) || current.ShiftDate.After(week.EndDate) {
		return nil, models.ErrPlanningInvalidDate
	}
	if normalizedCurrentEmployeeID := normalizeShiftEmployeeID(current.EmployeeID); normalizedCurrentEmployeeID != nil {
		if conflictErr := s.EnsureShiftHasNoConflicts(ctx, user.MerchantID, &shiftID, *normalizedCurrentEmployeeID, current.ShiftDate, current.StartTime, current.EndTime); conflictErr != nil {
			return nil, conflictErr
		}
	}
	updatedShift, err := s.repo.UpdatePlanningShift(ctx, user.MerchantID, shiftID, *current)
	if err != nil {
		return nil, err
	}
	s.logShiftAssignmentChange(ctx, user.MerchantID, user.UserID, shiftID, previousEmployeeID, updatedShift.EmployeeID)
	return updatedShift, nil
}

func (s *Service) DeletePlanningShift(ctx context.Context, shiftID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return models.ErrUnauthorized
	}
	if strings.TrimSpace(shiftID) == "" {
		return models.ErrMissingResourceID
	}
	if err := s.repo.SoftDeletePlanningShift(ctx, user.MerchantID, shiftID); err == sql.ErrNoRows {
		return models.ErrPlanningShiftNotFound
	} else {
		return err
	}
}

func (s *Service) EnsureShiftHasNoConflicts(ctx context.Context, merchantID string, excludeShiftID *string, employeeID string, shiftDate time.Time, startTime string, endTime string) error {
	if strings.TrimSpace(employeeID) == "" {
		return nil
	}
	existing, err := s.repo.ListEmployeeShiftsByDate(ctx, merchantID, employeeID, shiftDate, sharedpkg.DerefString(excludeShiftID))
	if err != nil {
		return err
	}
	newStart, err := time.Parse("15:04:05", startTime)
	if err != nil {
		return models.ErrPlanningInvalidHours
	}
	newEnd, err := time.Parse("15:04:05", endTime)
	if err != nil {
		return models.ErrPlanningInvalidHours
	}
	if !newEnd.After(newStart) {
		return models.ErrPlanningShiftInvalidRange
	}
	for _, item := range existing {
		currentStart, err := time.Parse("15:04:05", item.StartTime)
		if err != nil {
			return models.ErrPlanningInvalidHours
		}
		currentEnd, err := time.Parse("15:04:05", item.EndTime)
		if err != nil {
			return models.ErrPlanningInvalidHours
		}
		if newStart.Before(currentEnd) && currentStart.Before(newEnd) {
			return models.ErrPlanningShiftConflict
		}
	}
	return nil
}

func normalizeShiftEmployeeID(value *string) *string {
	return normalizeShiftOptionalString(value)
}

func normalizeShiftOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func (s *Service) resolveShiftPosition(ctx context.Context, merchantID string, positionIDValue, legacyPositionValue *string) (*string, *string, error) {
	normalizedPositionID := normalizeShiftOptionalString(positionIDValue)
	if normalizedPositionID != nil {
		if s.positionRepo == nil {
			return nil, nil, models.ErrPlanningPositionNotFound
		}
		position, err := s.positionRepo.GetEmployeePositionByID(ctx, merchantID, *normalizedPositionID)
		if err == sql.ErrNoRows || position == nil {
			return nil, nil, models.ErrPlanningPositionNotFound
		} else if err != nil {
			return nil, nil, err
		}
		resolvedPositionID := position.ID
		resolvedPositionLabel := position.Label
		return &resolvedPositionID, &resolvedPositionLabel, nil
	}

	normalizedLegacyPosition := normalizeShiftOptionalString(legacyPositionValue)
	if normalizedLegacyPosition != nil {
		if s.positionRepo == nil {
			return nil, nil, models.ErrPlanningPositionNotFound
		}
		position, err := s.positionRepo.GetEmployeePositionByLabel(ctx, merchantID, *normalizedLegacyPosition, "")
		if err == sql.ErrNoRows || position == nil {
			return nil, nil, models.ErrPlanningPositionNotFound
		} else if err != nil {
			return nil, nil, err
		}
		resolvedPositionID := position.ID
		resolvedPositionLabel := position.Label
		return &resolvedPositionID, &resolvedPositionLabel, nil
	}

	return nil, nil, nil
}

func (s *Service) logShiftAssignmentChange(ctx context.Context, merchantID, userID, shiftID string, previousEmployeeID, currentEmployeeID *string) {
	if s.auditService == nil {
		return
	}
	if sameNormalizedEmployeeID(previousEmployeeID, currentEmployeeID) {
		return
	}
	_ = s.auditService.LogChange(ctx, merchantID, userID, "update", "planning_shift", shiftID,
		map[string]any{"employee_id": normalizeShiftEmployeeID(previousEmployeeID)},
		map[string]any{"employee_id": normalizeShiftEmployeeID(currentEmployeeID)},
	)
}

func sameNormalizedEmployeeID(left, right *string) bool {
	normalizedLeft := normalizeShiftEmployeeID(left)
	normalizedRight := normalizeShiftEmployeeID(right)
	switch {
	case normalizedLeft == nil && normalizedRight == nil:
		return true
	case normalizedLeft == nil || normalizedRight == nil:
		return false
	default:
		return *normalizedLeft == *normalizedRight
	}
}
