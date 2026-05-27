package timeentries

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	employeespkg "welloresto-api/internal/modules/planning/employees"
	schedulepkg "welloresto-api/internal/modules/planning/schedule"
	sharedpkg "welloresto-api/internal/modules/planning/shared"
)

type EmployeeReader interface {
	GetEmployeeByID(ctx context.Context, merchantID, employeeID string) (*employeespkg.Employee, error)
}

type ShiftReader interface {
	GetPlanningShiftByID(ctx context.Context, merchantID, shiftID string) (*schedulepkg.PlanningShift, error)
}

type TimeTrackingModeReader interface {
	TimeTrackingModeExists(ctx context.Context, code string) (bool, error)
}

type Service struct {
	repo         *Repository
	employeeRepo EmployeeReader
	shiftRepo    ShiftReader
	refsRepo     TimeTrackingModeReader
}

func NewService(repo *Repository, employeeRepo EmployeeReader, shiftRepo ShiftReader, refsRepo TimeTrackingModeReader) *Service {
	return &Service{repo: repo, employeeRepo: employeeRepo, shiftRepo: shiftRepo, refsRepo: refsRepo}
}

func (s *Service) ListEmployeeTimeEntries(ctx context.Context, employeeID string) ([]PlanningTimeEntry, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(employeeID) == "" {
		return nil, models.ErrMissingResourceID
	}
	if _, err := s.employeeRepo.GetEmployeeByID(ctx, user.MerchantID, employeeID); err != nil {
		return nil, models.ErrPlanningEmployeeNotFound
	}
	return s.repo.ListEmployeeTimeEntries(ctx, user.MerchantID, employeeID)
}

func (s *Service) GetCurrentEmployeeTimeEntry(ctx context.Context, employeeID string) (*PlanningTimeEntry, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(employeeID) == "" {
		return nil, models.ErrMissingResourceID
	}
	if _, err := s.employeeRepo.GetEmployeeByID(ctx, user.MerchantID, employeeID); err != nil {
		return nil, models.ErrPlanningEmployeeNotFound
	}
	entry, err := s.repo.GetOpenPlanningTimeEntryForEmployee(ctx, user.MerchantID, employeeID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return entry, err
}

func (s *Service) StartEmployeeTimeEntry(ctx context.Context, employeeID string, req PlanningTimeEntryStartRequest) (*PlanningTimeEntry, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(employeeID) == "" {
		return nil, models.ErrMissingResourceID
	}
	employee, err := s.employeeRepo.GetEmployeeByID(ctx, user.MerchantID, employeeID)
	if err != nil {
		return nil, models.ErrPlanningEmployeeNotFound
	}
	if openEntry, openErr := s.repo.GetOpenPlanningTimeEntryForEmployee(ctx, user.MerchantID, employeeID); openErr == nil && openEntry != nil {
		return nil, models.ErrPlanningTimeEntryAlreadyOpen
	} else if openErr != nil && openErr != sql.ErrNoRows {
		return nil, openErr
	}
	entryModeCode := strings.TrimSpace(employee.TimeTrackingModeCode)
	if req.EntryModeCode != nil && strings.TrimSpace(*req.EntryModeCode) != "" {
		entryModeCode = strings.TrimSpace(*req.EntryModeCode)
	}
	entryModeExists, err := s.refsRepo.TimeTrackingModeExists(ctx, entryModeCode)
	if err != nil {
		return nil, err
	}
	if !entryModeExists {
		return nil, models.ErrPlanningTimeEntryModeInvalid
	}
	clockInAt := time.Now().UTC()
	if req.ClockInAt != nil && strings.TrimSpace(*req.ClockInAt) != "" {
		parsedClockInAt, parseErr := sharedpkg.ParsePlanningDateTime(*req.ClockInAt)
		if parseErr != nil {
			return nil, parseErr
		}
		clockInAt = parsedClockInAt
	}
	shiftID := sharedpkg.TrimOptionalString(req.ShiftID)
	if shiftID != nil {
		shift, shiftErr := s.shiftRepo.GetPlanningShiftByID(ctx, user.MerchantID, *shiftID)
		if shiftErr == sql.ErrNoRows {
			return nil, models.ErrPlanningShiftNotFound
		} else if shiftErr != nil {
			return nil, shiftErr
		}
		if shift.EmployeeID == nil || strings.TrimSpace(*shift.EmployeeID) != strings.TrimSpace(employeeID) {
			return nil, models.ErrPlanningTimeEntryShiftInvalid
		}
		if !sharedpkg.SamePlanningDay(shift.ShiftDate, clockInAt) {
			return nil, models.ErrPlanningTimeEntryShiftInvalid
		}
	}
	entry := PlanningTimeEntry{
		EmployeeID:    employeeID,
		ShiftID:       shiftID,
		EntryModeCode: entryModeCode,
		ClockInAt:     clockInAt,
		ClockInNote:   sharedpkg.TrimOptionalString(req.ClockInNote),
	}
	return s.repo.CreatePlanningTimeEntry(ctx, user.MerchantID, entry)
}

func (s *Service) StopEmployeeTimeEntry(ctx context.Context, employeeID string, req PlanningTimeEntryStopRequest) (*PlanningTimeEntry, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(employeeID) == "" {
		return nil, models.ErrMissingResourceID
	}
	if _, err := s.employeeRepo.GetEmployeeByID(ctx, user.MerchantID, employeeID); err != nil {
		return nil, models.ErrPlanningEmployeeNotFound
	}
	var entry *PlanningTimeEntry
	entryID := sharedpkg.TrimOptionalString(req.EntryID)
	if entryID != nil {
		entry, err = s.repo.GetPlanningTimeEntryByID(ctx, user.MerchantID, *entryID)
		if err == sql.ErrNoRows {
			return nil, models.ErrPlanningTimeEntryNotFound
		} else if err != nil {
			return nil, err
		}
		if strings.TrimSpace(entry.EmployeeID) != strings.TrimSpace(employeeID) {
			return nil, models.ErrPlanningTimeEntryNotFound
		}
		if entry.ClockOutAt != nil {
			return nil, models.ErrPlanningTimeEntryNotOpen
		}
	} else {
		entry, err = s.repo.GetOpenPlanningTimeEntryForEmployee(ctx, user.MerchantID, employeeID)
		if err == sql.ErrNoRows {
			return nil, models.ErrPlanningTimeEntryNotOpen
		} else if err != nil {
			return nil, err
		}
	}
	clockOutAt := time.Now().UTC()
	if req.ClockOutAt != nil && strings.TrimSpace(*req.ClockOutAt) != "" {
		parsedClockOutAt, parseErr := sharedpkg.ParsePlanningDateTime(*req.ClockOutAt)
		if parseErr != nil {
			return nil, parseErr
		}
		clockOutAt = parsedClockOutAt
	}
	if !clockOutAt.After(entry.ClockInAt) {
		return nil, models.ErrPlanningTimeEntryInvalidRange
	}
	return s.repo.ClosePlanningTimeEntry(ctx, user.MerchantID, entry.ID, clockOutAt, sharedpkg.TrimOptionalString(req.ClockOutNote))
}
