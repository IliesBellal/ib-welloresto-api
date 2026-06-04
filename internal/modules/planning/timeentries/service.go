package timeentries

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	auditpkg "welloresto-api/internal/modules/audit"
	employeespkg "welloresto-api/internal/modules/planning/employees"
	schedulepkg "welloresto-api/internal/modules/planning/schedule"
	settingspkg "welloresto-api/internal/modules/planning/settings"
	sharedpkg "welloresto-api/internal/modules/planning/shared"
)

type EmployeeReader interface {
	GetEmployeeByID(ctx context.Context, merchantID, employeeID string) (*employeespkg.Employee, error)
	GetEmployeeIDByMemberID(ctx context.Context, merchantID, memberID string) (string, error)
}

type ShiftReader interface {
	GetPlanningShiftByID(ctx context.Context, merchantID, shiftID string) (*schedulepkg.PlanningShift, error)
	GetPlanningWeekByID(ctx context.Context, merchantID, weekID string) (*schedulepkg.PlanningWeek, error)
	GetPlanningWeekByStartDate(ctx context.Context, merchantID string, startDate time.Time, excludeWeekID string) (*schedulepkg.PlanningWeek, error)
	ListPlanningShifts(ctx context.Context, merchantID, weekID string) ([]schedulepkg.PlanningShift, error)
}

type SettingsReader interface {
	GetOrCreateSettings(ctx context.Context, merchantID string) (*settingspkg.PlanningSettings, error)
}

type Service struct {
	repo         *Repository
	employeeRepo EmployeeReader
	shiftRepo    ShiftReader
	settingsRepo SettingsReader
	auditService auditpkg.AuditService
}

func NewService(repo *Repository, employeeRepo EmployeeReader, shiftRepo ShiftReader, settingsRepo SettingsReader, auditService auditpkg.AuditService) *Service {
	return &Service{repo: repo, employeeRepo: employeeRepo, shiftRepo: shiftRepo, settingsRepo: settingsRepo, auditService: auditService}
}

func (s *Service) ResolveCurrentEmployeeID(ctx context.Context) (string, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return "", models.ErrUnauthorized
	}
	employeeID, err := sharedpkg.ResolvePlanningEmployeeID(ctx, s.employeeRepo, user.MerchantID, "me", user.MerchantRightsID)
	if err != nil {
		return "", err
	}
	if _, err := s.employeeRepo.GetEmployeeByID(ctx, user.MerchantID, employeeID); err != nil {
		return "", models.ErrPlanningEmployeeNotFound
	}
	return employeeID, nil
}

func (s *Service) ListCurrentUserTeamWeekShifts(ctx context.Context, weekStartRaw, weekIDRaw string) (string, string, []schedulepkg.PlanningShift, error) {
	currentEmployeeID, err := s.ResolveCurrentEmployeeID(ctx)
	if err != nil {
		return "", "", nil, err
	}

	if s.shiftRepo == nil {
		return "", "", nil, models.ErrInternalServerError
	}

	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return "", "", nil, models.ErrUnauthorized
	}

	weekID := strings.TrimSpace(weekIDRaw)
	weekStart := strings.TrimSpace(weekStartRaw)

	var week *schedulepkg.PlanningWeek
	if weekID != "" {
		week, err = s.shiftRepo.GetPlanningWeekByID(ctx, user.MerchantID, weekID)
		if err == sql.ErrNoRows {
			return currentEmployeeID, "", []schedulepkg.PlanningShift{}, nil
		}
		if err != nil {
			return "", "", nil, err
		}
	} else {
		if weekStart == "" {
			return "", "", nil, models.ErrPlanningInvalidDate
		}
		startDate, parseErr := sharedpkg.ParsePlanningDate(weekStart)
		if parseErr != nil {
			return "", "", nil, parseErr
		}
		week, err = s.shiftRepo.GetPlanningWeekByStartDate(ctx, user.MerchantID, startDate, "")
		if err == sql.ErrNoRows {
			return currentEmployeeID, "", []schedulepkg.PlanningShift{}, nil
		}
		if err != nil {
			return "", "", nil, err
		}
	}

	if week == nil || strings.TrimSpace(week.ID) == "" {
		return currentEmployeeID, "", []schedulepkg.PlanningShift{}, nil
	}
	if !strings.EqualFold(strings.TrimSpace(week.Status), "published") {
		return currentEmployeeID, "", []schedulepkg.PlanningShift{}, nil
	}

	items, err := s.shiftRepo.ListPlanningShifts(ctx, user.MerchantID, week.ID)
	if err != nil {
		return "", "", nil, err
	}

	return currentEmployeeID, week.ID, items, nil
}

func (s *Service) ListPlanningTimeEntries(ctx context.Context, filters PlanningTimeEntryListFilters) ([]PlanningTimeEntry, models.PaginationMetadata, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.PaginationMetadata{}, models.ErrUnauthorized
	}
	fromDate, toDate, err := sharedpkg.ParsePlanningDateRange(filters.From, filters.To)
	if err != nil {
		return nil, models.PaginationMetadata{}, models.ErrPlanningInvalidDate
	}
	filters.EmployeeID = strings.TrimSpace(filters.EmployeeID)
	if filters.EmployeeID != "" {
		filters.EmployeeID, err = sharedpkg.ResolvePlanningEmployeeID(ctx, s.employeeRepo, user.MerchantID, filters.EmployeeID, user.MerchantRightsID)
		if err != nil {
			return nil, models.PaginationMetadata{}, err
		}
		if _, err := s.employeeRepo.GetEmployeeByID(ctx, user.MerchantID, filters.EmployeeID); err != nil {
			return nil, models.PaginationMetadata{}, models.ErrPlanningEmployeeNotFound
		}
	}
	pagination := sharedpkg.NormalizePlanningPagination(filters.Page, filters.PageSize)
	filters.Page = pagination.Page
	filters.PageSize = pagination.PageSize
	items, totalItems, err := s.repo.ListPlanningTimeEntries(ctx, user.MerchantID, fromDate, toDate.AddDate(0, 0, 1), filters)
	if err != nil {
		return nil, models.PaginationMetadata{}, err
	}
	return items, sharedpkg.BuildPaginationMetadata(totalItems, pagination), nil
}

func (s *Service) ListEmployeeTimeEntries(ctx context.Context, employeeID string, filters PlanningTimeEntryListFilters) ([]PlanningTimeEntry, models.PaginationMetadata, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.PaginationMetadata{}, models.ErrUnauthorized
	}
	if strings.TrimSpace(employeeID) == "" {
		return nil, models.PaginationMetadata{}, models.ErrMissingResourceID
	}
	employeeID, err = sharedpkg.ResolvePlanningEmployeeID(ctx, s.employeeRepo, user.MerchantID, employeeID, user.MerchantRightsID)
	if err != nil {
		return nil, models.PaginationMetadata{}, err
	}
	if _, err := s.employeeRepo.GetEmployeeByID(ctx, user.MerchantID, employeeID); err != nil {
		return nil, models.PaginationMetadata{}, models.ErrPlanningEmployeeNotFound
	}
	pagination := sharedpkg.NormalizePlanningPagination(filters.Page, filters.PageSize)
	filters.Page = pagination.Page
	filters.PageSize = pagination.PageSize
	items, totalItems, err := s.repo.ListEmployeeTimeEntries(ctx, user.MerchantID, employeeID, filters)
	if err != nil {
		return nil, models.PaginationMetadata{}, err
	}
	return items, sharedpkg.BuildPaginationMetadata(totalItems, pagination), nil
}

func (s *Service) GetCurrentEmployeeTimeEntry(ctx context.Context, employeeID string) (*PlanningTimeEntry, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(employeeID) == "" {
		return nil, models.ErrMissingResourceID
	}
	employeeID, err = sharedpkg.ResolvePlanningEmployeeID(ctx, s.employeeRepo, user.MerchantID, employeeID, user.MerchantRightsID)
	if err != nil {
		return nil, err
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
	employeeID, err = sharedpkg.ResolvePlanningEmployeeID(ctx, s.employeeRepo, user.MerchantID, employeeID, user.MerchantRightsID)
	if err != nil {
		return nil, err
	}
	if _, err := s.employeeRepo.GetEmployeeByID(ctx, user.MerchantID, employeeID); err != nil {
		return nil, models.ErrPlanningEmployeeNotFound
	}
	if openEntry, openErr := s.repo.GetOpenPlanningTimeEntryForEmployee(ctx, user.MerchantID, employeeID); openErr == nil && openEntry != nil {
		return nil, models.ErrPlanningTimeEntryAlreadyOpen
	} else if openErr != nil && openErr != sql.ErrNoRows {
		return nil, openErr
	}
	planningSettings, err := s.settingsRepo.GetOrCreateSettings(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}
	if planningSettings.AttendanceSource != settingspkg.AttendanceSourcePointage {
		return nil, models.ErrPlanningTimeEntrySourceDisabled
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
		if !sharedpkg.SamePlanningDay(shift.ShiftDate.Time(), clockInAt) {
			return nil, models.ErrPlanningTimeEntryShiftInvalid
		}
	}
	entry := PlanningTimeEntry{
		EmployeeID:       employeeID,
		ShiftID:          shiftID,
		AttendanceSource: planningSettings.AttendanceSource,
		ClockInAt:        clockInAt,
		ClockInNote:      sharedpkg.TrimOptionalString(req.ClockInNote),
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
	employeeID, err = sharedpkg.ResolvePlanningEmployeeID(ctx, s.employeeRepo, user.MerchantID, employeeID, user.MerchantRightsID)
	if err != nil {
		return nil, err
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

func (s *Service) CreateEmployeeTimeEntry(ctx context.Context, employeeID string, req PlanningTimeEntryManualCreateRequest) (*PlanningTimeEntry, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(employeeID) == "" {
		return nil, models.ErrMissingResourceID
	}
	modificationReason, err := normalizeTimeEntryModificationReason(req.ModificationReason)
	if err != nil {
		return nil, err
	}
	employeeID, err = sharedpkg.ResolvePlanningEmployeeID(ctx, s.employeeRepo, user.MerchantID, employeeID, user.MerchantRightsID)
	if err != nil {
		return nil, err
	}
	if _, err := s.employeeRepo.GetEmployeeByID(ctx, user.MerchantID, employeeID); err != nil {
		return nil, models.ErrPlanningEmployeeNotFound
	}
	attendanceSource, err := s.ensureManualTimeEntryMutationsEnabled(ctx, user.MerchantID, nil)
	if err != nil {
		return nil, err
	}
	clockInAt, err := sharedpkg.ParsePlanningDateTime(req.ClockInAt)
	if err != nil {
		return nil, err
	}
	clockOutAt, err := sharedpkg.ParsePlanningDateTime(req.ClockOutAt)
	if err != nil {
		return nil, err
	}
	if !clockOutAt.After(clockInAt) {
		return nil, models.ErrPlanningTimeEntryInvalidRange
	}
	shiftID := sharedpkg.TrimOptionalString(req.ShiftID)
	if err := s.validateTimeEntryShift(ctx, user.MerchantID, employeeID, shiftID, clockInAt); err != nil {
		return nil, err
	}
	modifiedBy, err := timeEntryModifierEmail(user.Email)
	if err != nil {
		return nil, err
	}
	modifiedAt := time.Now().UTC()
	entry := PlanningTimeEntry{
		EmployeeID:         employeeID,
		ShiftID:            shiftID,
		AttendanceSource:   attendanceSource,
		ClockInAt:          clockInAt,
		ClockOutAt:         &clockOutAt,
		ClockInNote:        sharedpkg.TrimOptionalString(req.ClockInNote),
		ClockOutNote:       sharedpkg.TrimOptionalString(req.ClockOutNote),
		ModifiedBy:         &modifiedBy,
		ModifiedAt:         &modifiedAt,
		ModificationReason: &modificationReason,
	}
	// Manual corrections are created already closed, so the single-open-entry guard does not apply.
	createdEntry, err := s.repo.CreatePlanningTimeEntry(ctx, user.MerchantID, entry)
	if err != nil {
		return nil, err
	}
	s.logTimeEntryChange(ctx, user.MerchantID, user.UserID, "manual_create", createdEntry.ID, nil, createdEntry)
	return createdEntry, nil
}

func (s *Service) UpdateEmployeeTimeEntry(ctx context.Context, employeeID, entryID string, req PlanningTimeEntryCorrectionRequest) (*PlanningTimeEntry, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(employeeID) == "" || strings.TrimSpace(entryID) == "" {
		return nil, models.ErrMissingResourceID
	}
	if req.ClockInAt == nil && req.ClockOutAt == nil && req.ClockInNote == nil && req.ClockOutNote == nil {
		return nil, models.ErrInvalidData
	}
	modificationReason, err := normalizeTimeEntryModificationReason(req.ModificationReason)
	if err != nil {
		return nil, err
	}
	employeeID, err = sharedpkg.ResolvePlanningEmployeeID(ctx, s.employeeRepo, user.MerchantID, employeeID, user.MerchantRightsID)
	if err != nil {
		return nil, err
	}
	if _, err := s.employeeRepo.GetEmployeeByID(ctx, user.MerchantID, employeeID); err != nil {
		return nil, models.ErrPlanningEmployeeNotFound
	}
	current, err := s.repo.GetPlanningTimeEntryByID(ctx, user.MerchantID, strings.TrimSpace(entryID))
	if err == sql.ErrNoRows || current == nil {
		return nil, models.ErrPlanningTimeEntryNotFound
	} else if err != nil {
		return nil, err
	}
	if strings.TrimSpace(current.EmployeeID) != strings.TrimSpace(employeeID) {
		return nil, models.ErrPlanningTimeEntryNotFound
	}
	if _, err := s.ensureManualTimeEntryMutationsEnabled(ctx, user.MerchantID, &current.AttendanceSource); err != nil {
		return nil, err
	}
	previous := *current
	if req.ClockInAt != nil {
		trimmedClockInAt := strings.TrimSpace(*req.ClockInAt)
		if trimmedClockInAt == "" {
			return nil, models.ErrInvalidData
		}
		parsedClockInAt, parseErr := sharedpkg.ParsePlanningDateTime(trimmedClockInAt)
		if parseErr != nil {
			return nil, parseErr
		}
		current.ClockInAt = parsedClockInAt
	}
	if req.ClockOutAt != nil {
		trimmedClockOutAt := strings.TrimSpace(*req.ClockOutAt)
		if trimmedClockOutAt == "" {
			return nil, models.ErrInvalidData
		}
		parsedClockOutAt, parseErr := sharedpkg.ParsePlanningDateTime(trimmedClockOutAt)
		if parseErr != nil {
			return nil, parseErr
		}
		current.ClockOutAt = &parsedClockOutAt
	}
	if req.ClockInNote != nil {
		current.ClockInNote = sharedpkg.TrimOptionalString(req.ClockInNote)
	}
	if req.ClockOutNote != nil {
		current.ClockOutNote = sharedpkg.TrimOptionalString(req.ClockOutNote)
	}
	if current.ClockOutAt != nil && !current.ClockOutAt.After(current.ClockInAt) {
		return nil, models.ErrPlanningTimeEntryInvalidRange
	}
	modifiedBy, err := timeEntryModifierEmail(user.Email)
	if err != nil {
		return nil, err
	}
	modifiedAt := time.Now().UTC()
	current.ModifiedBy = &modifiedBy
	current.ModifiedAt = &modifiedAt
	current.ModificationReason = &modificationReason
	updatedEntry, err := s.repo.UpdatePlanningTimeEntry(ctx, user.MerchantID, current.ID, *current)
	if err != nil {
		return nil, err
	}
	s.logTimeEntryChange(ctx, user.MerchantID, user.UserID, "manual_update", updatedEntry.ID, previous, updatedEntry)
	return updatedEntry, nil
}

func (s *Service) DeleteEmployeeTimeEntry(ctx context.Context, employeeID, entryID string, req PlanningTimeEntryDeleteRequest) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return models.ErrUnauthorized
	}
	if strings.TrimSpace(employeeID) == "" || strings.TrimSpace(entryID) == "" {
		return models.ErrMissingResourceID
	}
	modificationReason, err := normalizeTimeEntryModificationReason(req.ModificationReason)
	if err != nil {
		return err
	}
	employeeID, err = sharedpkg.ResolvePlanningEmployeeID(ctx, s.employeeRepo, user.MerchantID, employeeID, user.MerchantRightsID)
	if err != nil {
		return err
	}
	if _, err := s.employeeRepo.GetEmployeeByID(ctx, user.MerchantID, employeeID); err != nil {
		return models.ErrPlanningEmployeeNotFound
	}
	current, err := s.repo.GetPlanningTimeEntryByID(ctx, user.MerchantID, strings.TrimSpace(entryID))
	if err == sql.ErrNoRows || current == nil {
		return models.ErrPlanningTimeEntryNotFound
	} else if err != nil {
		return err
	}
	if strings.TrimSpace(current.EmployeeID) != strings.TrimSpace(employeeID) {
		return models.ErrPlanningTimeEntryNotFound
	}
	if _, err := s.ensureManualTimeEntryMutationsEnabled(ctx, user.MerchantID, &current.AttendanceSource); err != nil {
		return err
	}
	previous := *current
	modifiedBy, err := timeEntryModifierEmail(user.Email)
	if err != nil {
		return err
	}
	modifiedAt := time.Now().UTC()
	err = s.repo.SoftDeletePlanningTimeEntry(ctx, user.MerchantID, current.ID, &modifiedBy, modifiedAt, &modificationReason)
	if err == sql.ErrNoRows {
		return models.ErrPlanningTimeEntryNotFound
	} else if err != nil {
		return err
	}
	current.ModifiedBy = &modifiedBy
	current.ModifiedAt = &modifiedAt
	current.ModificationReason = &modificationReason
	current.UpdatedAt = modifiedAt
	current.DeletedAt = &modifiedAt
	s.logTimeEntryChange(ctx, user.MerchantID, user.UserID, "manual_delete", current.ID, previous, map[string]interface{}{
		"deleted_at":          current.DeletedAt,
		"modified_by":         current.ModifiedBy,
		"modified_at":         current.ModifiedAt,
		"modification_reason": current.ModificationReason,
		"deleted":             true,
	})
	return nil
}

func (s *Service) ensureManualTimeEntryMutationsEnabled(ctx context.Context, merchantID string, entryAttendanceSource *string) (string, error) {
	planningSettings, err := s.settingsRepo.GetOrCreateSettings(ctx, merchantID)
	if err != nil {
		return "", err
	}
	if planningSettings.AttendanceSource != settingspkg.AttendanceSourcePointage {
		return "", models.ErrPlanningTimeEntrySourceDisabled
	}
	if entryAttendanceSource != nil && strings.TrimSpace(*entryAttendanceSource) != settingspkg.AttendanceSourcePointage {
		return "", models.ErrPlanningTimeEntrySourceDisabled
	}
	return planningSettings.AttendanceSource, nil
}

func (s *Service) validateTimeEntryShift(ctx context.Context, merchantID, employeeID string, shiftID *string, clockInAt time.Time) error {
	if shiftID == nil {
		return nil
	}
	shift, shiftErr := s.shiftRepo.GetPlanningShiftByID(ctx, merchantID, *shiftID)
	if shiftErr == sql.ErrNoRows {
		return models.ErrPlanningShiftNotFound
	} else if shiftErr != nil {
		return shiftErr
	}
	if shift.EmployeeID == nil || strings.TrimSpace(*shift.EmployeeID) != strings.TrimSpace(employeeID) {
		return models.ErrPlanningTimeEntryShiftInvalid
	}
	if !sharedpkg.SamePlanningDay(shift.ShiftDate.Time(), clockInAt) {
		return models.ErrPlanningTimeEntryShiftInvalid
	}
	return nil
}

func normalizeTimeEntryModificationReason(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", models.ErrInvalidData
	}
	return trimmed, nil
}

func timeEntryModifierEmail(email string) (string, error) {
	trimmed := strings.TrimSpace(email)
	if trimmed == "" {
		return "", models.ErrUnauthorized
	}
	return trimmed, nil
}

func (s *Service) logTimeEntryChange(ctx context.Context, merchantID, userID, action, entryID string, oldState, newState interface{}) {
	if s.auditService == nil {
		return
	}
	_ = s.auditService.LogChange(ctx, merchantID, userID, action, "planning_time_entry", entryID, oldState, newState)
}
