package schedule

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	employeespkg "welloresto-api/internal/modules/planning/employees"
	sharedpkg "welloresto-api/internal/modules/planning/shared"
)

type EmployeeReader interface {
	GetEmployeeByID(ctx context.Context, merchantID, employeeID string) (*employeespkg.Employee, error)
}

type Service struct {
	repo         *Repository
	employeeRepo EmployeeReader
}

func NewService(repo *Repository, employeeRepo EmployeeReader) *Service {
	return &Service{repo: repo, employeeRepo: employeeRepo}
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
	if strings.TrimSpace(req.Title) == "" {
		return nil, models.ErrValidationError
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
	if req.EmployeeID != nil && strings.TrimSpace(*req.EmployeeID) != "" {
		if _, err := s.employeeRepo.GetEmployeeByID(ctx, user.MerchantID, strings.TrimSpace(*req.EmployeeID)); err != nil {
			return nil, models.ErrPlanningEmployeeNotFound
		}
		if conflictErr := s.EnsureShiftHasNoConflicts(ctx, user.MerchantID, nil, strings.TrimSpace(*req.EmployeeID), shiftDate, startTime, endTime); conflictErr != nil {
			return nil, conflictErr
		}
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
		EmployeeID:   req.EmployeeID,
		Title:        strings.TrimSpace(req.Title),
		ShiftDate:    shiftDate,
		StartTime:    startTime,
		EndTime:      endTime,
		BreakMinutes: breakMinutes,
		Position:     req.Position,
		Location:     req.Location,
		Notes:        req.Notes,
		Status:       status,
	}
	return s.repo.CreatePlanningShift(ctx, user.MerchantID, shift)
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
	if req.Title != nil {
		if strings.TrimSpace(*req.Title) == "" {
			return nil, models.ErrValidationError
		}
		current.Title = strings.TrimSpace(*req.Title)
	}
	if req.EmployeeID != nil {
		trimmedEmployeeID := strings.TrimSpace(*req.EmployeeID)
		if trimmedEmployeeID == "" {
			current.EmployeeID = nil
		} else {
			if _, err := s.employeeRepo.GetEmployeeByID(ctx, user.MerchantID, trimmedEmployeeID); err != nil {
				return nil, models.ErrPlanningEmployeeNotFound
			}
			current.EmployeeID = &trimmedEmployeeID
		}
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
	if req.Position != nil {
		current.Position = req.Position
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
	if current.EmployeeID != nil && strings.TrimSpace(*current.EmployeeID) != "" {
		if conflictErr := s.EnsureShiftHasNoConflicts(ctx, user.MerchantID, &shiftID, strings.TrimSpace(*current.EmployeeID), current.ShiftDate, current.StartTime, current.EndTime); conflictErr != nil {
			return nil, conflictErr
		}
	}
	return s.repo.UpdatePlanningShift(ctx, user.MerchantID, shiftID, *current)
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
