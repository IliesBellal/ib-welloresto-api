package weektemplates

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	auditpkg "welloresto-api/internal/modules/audit"
	employeespkg "welloresto-api/internal/modules/planning/employees"
	leavepkg "welloresto-api/internal/modules/planning/leave"
	schedulepkg "welloresto-api/internal/modules/planning/schedule"
	shared "welloresto-api/internal/modules/planning/shared"
	"welloresto-api/internal/utils/dbutils"
)

type EmployeeReader interface {
	GetEmployeeByID(ctx context.Context, merchantID, employeeID string) (*employeespkg.Employee, error)
	GetEmployeePositionByID(ctx context.Context, merchantID, id string) (*employeespkg.EmployeePosition, error)
	GetEmployeePositionByLabel(ctx context.Context, merchantID, label, excludeID string) (*employeespkg.EmployeePosition, error)
}

type WeekSourceReader interface {
	GetPlanningWeekByID(ctx context.Context, merchantID, weekID string) (*schedulepkg.PlanningWeek, error)
	GetPlanningWeekByStartDate(ctx context.Context, merchantID string, startDate time.Time, excludeWeekID string) (*schedulepkg.PlanningWeek, error)
	ListPlanningShifts(ctx context.Context, merchantID, weekID string) ([]schedulepkg.PlanningShift, error)
	CreatePlanningWeek(ctx context.Context, merchantID string, week schedulepkg.PlanningWeek) (*schedulepkg.PlanningWeek, error)
	CreatePlanningShift(ctx context.Context, merchantID string, shift schedulepkg.PlanningShift) (*schedulepkg.PlanningShift, error)
	SoftDeletePlanningShift(ctx context.Context, merchantID, shiftID string) error
}

type LeaveReader interface {
	ListApprovedLeavesOverlappingRange(ctx context.Context, merchantID string, employeeIDs []string, startDate, endDate time.Time) ([]leavepkg.PlanningLeaveRequest, error)
}

type Service struct {
	repo         *Repository
	employeeRepo EmployeeReader
	weekSource   WeekSourceReader
	leaveRepo    LeaveReader
	auditService auditpkg.AuditService
}

func NewService(repo *Repository, employeeRepo EmployeeReader, weekSource WeekSourceReader, leaveRepo LeaveReader, auditService auditpkg.AuditService) *Service {
	return &Service{repo: repo, employeeRepo: employeeRepo, weekSource: weekSource, leaveRepo: leaveRepo, auditService: auditService}
}

func (s *Service) ListWeekTemplates(ctx context.Context) ([]WeekTemplate, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	return s.repo.ListWeekTemplates(ctx, user.MerchantID)
}

func (s *Service) GetWeekTemplate(ctx context.Context, id string) (*WeekTemplate, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	tpl, err := s.repo.GetWeekTemplateByID(ctx, user.MerchantID, strings.TrimSpace(id))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, models.ErrPlanningWeekTemplateNotFound
		}
		return nil, err
	}
	shifts, err := s.repo.ListWeekTemplateShifts(ctx, user.MerchantID, tpl.ID)
	if err != nil {
		return nil, err
	}
	tpl.WeekTemplateShifts = shifts
	return tpl, nil
}

func (s *Service) CreateWeekTemplate(ctx context.Context, req WeekTemplateCreateRequest) (*WeekTemplate, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	label := strings.TrimSpace(req.Label)
	if label == "" {
		return nil, models.ErrPlanningWeekTemplateLabelRequired
	}
	if !req.Shifts.Present || req.Shifts.Null {
		return nil, models.ErrInvalidData
	}

	notes := shared.TrimOptionalString(req.Notes)
	active := true
	if req.Active != nil {
		active = *req.Active
	}

	shifts, err := s.validateShiftInputs(ctx, user.MerchantID, req.Shifts.Value)
	if err != nil {
		return nil, err
	}

	now := nowUTC()
	tpl := WeekTemplate{
		ID:         helpers.GeneratePrefixedID(helpers.PlanningWeekTemplateIDPrefix),
		MerchantID: user.MerchantID,
		Label:      label,
		Notes:      notes,
		Active:     active,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	for i := range shifts {
		shifts[i].WeekTemplateID = tpl.ID
		shifts[i].CreatedAt = now
		shifts[i].UpdatedAt = now
		if shifts[i].ID == "" {
			shifts[i].ID = helpers.GeneratePrefixedID(helpers.PlanningWeekTemplateShiftIDPrefix)
		}
	}

	if err := s.repo.CreateWeekTemplateWithShifts(ctx, tpl, shifts); err != nil {
		return nil, err
	}

	tpl.ShiftCount = len(shifts)
	tpl.WeekTemplateShifts = shifts
	return &tpl, nil
}

func (s *Service) UpdateWeekTemplate(ctx context.Context, id string, req WeekTemplateUpdateRequest) (*WeekTemplate, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	id = strings.TrimSpace(id)
	if id == "" {
		return nil, models.ErrPlanningWeekTemplateNotFound
	}
	if req.Label == nil && !req.Notes.Present && req.Active == nil && !req.Shifts.Present {
		return nil, models.ErrInvalidData
	}

	existing, err := s.repo.GetWeekTemplateByID(ctx, user.MerchantID, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, models.ErrPlanningWeekTemplateNotFound
		}
		return nil, err
	}

	if req.Label != nil {
		label := strings.TrimSpace(*req.Label)
		if label == "" {
			return nil, models.ErrPlanningWeekTemplateLabelRequired
		}
		existing.Label = label
	}
	if req.Notes.Present {
		existing.Notes = shared.TrimOptionalString(req.Notes.Value)
	}
	if req.Active != nil {
		existing.Active = *req.Active
	}

	replaceShifts := false
	newShifts := make([]WeekTemplateShift, 0)
	if req.Shifts.Present {
		if req.Shifts.Null {
			return nil, models.ErrInvalidData
		}
		replaceShifts = true
		validated, err := s.validateShiftInputs(ctx, user.MerchantID, req.Shifts.Value)
		if err != nil {
			return nil, err
		}
		now := nowUTC()
		for i := range validated {
			validated[i].ID = helpers.GeneratePrefixedID(helpers.PlanningWeekTemplateShiftIDPrefix)
			validated[i].WeekTemplateID = existing.ID
			validated[i].CreatedAt = now
			validated[i].UpdatedAt = now
		}
		newShifts = validated
	}

	if err := s.repo.UpdateWeekTemplateWithOptionalShifts(ctx, user.MerchantID, existing.ID, *existing, replaceShifts, newShifts); err != nil {
		if err == sql.ErrNoRows {
			return nil, models.ErrPlanningWeekTemplateNotFound
		}
		return nil, err
	}

	updated, err := s.repo.GetWeekTemplateByID(ctx, user.MerchantID, existing.ID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, models.ErrPlanningWeekTemplateNotFound
		}
		return nil, err
	}
	shifts, err := s.repo.ListWeekTemplateShifts(ctx, user.MerchantID, existing.ID)
	if err != nil {
		return nil, err
	}
	updated.WeekTemplateShifts = shifts
	return updated, nil
}

func (s *Service) DeleteWeekTemplate(ctx context.Context, id string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return models.ErrUnauthorized
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return models.ErrPlanningWeekTemplateNotFound
	}
	if err := s.repo.SoftDeleteWeekTemplate(ctx, user.MerchantID, id); err != nil {
		if err == sql.ErrNoRows {
			return models.ErrPlanningWeekTemplateNotFound
		}
		return err
	}
	return nil
}

func (s *Service) CreateWeekTemplateFromWeek(ctx context.Context, req WeekTemplateFromWeekRequest) (*WeekTemplate, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if s.weekSource == nil {
		return nil, models.ErrPlanningWeekNotFound
	}

	weekID := strings.TrimSpace(req.WeekID)
	label := strings.TrimSpace(req.Label)
	if weekID == "" {
		return nil, models.ErrPlanningWeekNotFound
	}
	if label == "" {
		return nil, models.ErrPlanningWeekTemplateLabelRequired
	}

	if _, err := s.weekSource.GetPlanningWeekByID(ctx, user.MerchantID, weekID); err != nil {
		if err == sql.ErrNoRows {
			return nil, models.ErrPlanningWeekNotFound
		}
		return nil, err
	}

	planningShifts, err := s.weekSource.ListPlanningShifts(ctx, user.MerchantID, weekID)
	if err != nil {
		return nil, err
	}

	now := nowUTC()
	tpl := WeekTemplate{
		ID:         helpers.GeneratePrefixedID(helpers.PlanningWeekTemplateIDPrefix),
		MerchantID: user.MerchantID,
		Label:      label,
		Notes:      shared.TrimOptionalString(req.Notes),
		Active:     true,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	shifts := make([]WeekTemplateShift, 0, len(planningShifts))
	for _, sourceShift := range planningShifts {
		positionID := sourceShift.PositionID
		if positionID == nil {
			resolvedPositionID, err := s.resolvePositionIDFromLegacyLabel(ctx, user.MerchantID, sourceShift.Position)
			if err != nil {
				return nil, err
			}
			positionID = resolvedPositionID
		}

		startTime, err := normalizeClockHHMM(sourceShift.StartTime)
		if err != nil {
			return nil, models.ErrPlanningShiftTemplateInvalidRange
		}
		endTime, err := normalizeClockHHMM(sourceShift.EndTime)
		if err != nil {
			return nil, models.ErrPlanningShiftTemplateInvalidRange
		}

		shift := WeekTemplateShift{
			ID:             helpers.GeneratePrefixedID(helpers.PlanningWeekTemplateShiftIDPrefix),
			WeekTemplateID: tpl.ID,
			DayOfWeek:      planningShiftDateToDayOfWeek(sourceShift.ShiftDate),
			EmployeeID:     sourceShift.EmployeeID,
			PositionID:     positionID,
			Title:          trimOrNil(sourceShift.Title),
			StartTime:      startTime,
			EndTime:        endTime,
			BreakMinutes:   sourceShift.BreakMinutes,
			Location:       shared.TrimOptionalString(sourceShift.Location),
			Notes:          shared.TrimOptionalString(sourceShift.Notes),
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		shifts = append(shifts, shift)
	}

	if err := s.repo.CreateWeekTemplateWithShifts(ctx, tpl, shifts); err != nil {
		return nil, err
	}

	tpl.ShiftCount = len(shifts)
	tpl.WeekTemplateShifts = shifts
	return &tpl, nil
}

func (s *Service) PreviewWeekTemplateInstantiation(ctx context.Context, templateID string, req WeekTemplatePreviewRequest) (*InstantiationPreview, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if s.weekSource == nil {
		return nil, models.ErrPlanningWeekNotFound
	}

	templateID = strings.TrimSpace(templateID)
	if templateID == "" {
		return nil, models.ErrPlanningWeekTemplateNotFound
	}

	targetWeekStarts, err := parseTargetWeekStarts(req.TargetWeekStarts)
	if err != nil {
		return nil, err
	}
	if len(targetWeekStarts) > MaxPreviewTargetWeeks {
		return nil, models.ErrPlanningWeekTemplatePreviewRangeTooLarge
	}

	tpl, err := s.repo.GetWeekTemplateByID(ctx, user.MerchantID, templateID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, models.ErrPlanningWeekTemplateNotFound
		}
		return nil, err
	}
	templateShifts, err := s.repo.ListWeekTemplateShifts(ctx, user.MerchantID, tpl.ID)
	if err != nil {
		return nil, err
	}

	existingByWeek := make(map[string][]schedulepkg.PlanningShift, len(targetWeekStarts))
	for _, weekStart := range targetWeekStarts {
		weekKey := dateISO(weekStart)
		week, weekErr := s.weekSource.GetPlanningWeekByStartDate(ctx, user.MerchantID, weekStart, "")
		if weekErr == sql.ErrNoRows {
			existingByWeek[weekKey] = []schedulepkg.PlanningShift{}
			continue
		}
		if weekErr != nil {
			return nil, weekErr
		}
		if week == nil {
			existingByWeek[weekKey] = []schedulepkg.PlanningShift{}
			continue
		}
		shifts, shiftsErr := s.weekSource.ListPlanningShifts(ctx, user.MerchantID, week.ID)
		if shiftsErr != nil {
			return nil, shiftsErr
		}
		existingByWeek[weekKey] = shifts
	}

	employeeIDs := collectTemplateEmployeeIDs(templateShifts)
	employeesByID := make(map[string]*employeespkg.Employee, len(employeeIDs))
	for _, employeeID := range employeeIDs {
		employee, employeeErr := s.employeeRepo.GetEmployeeByID(ctx, user.MerchantID, employeeID)
		if employeeErr == sql.ErrNoRows {
			continue
		}
		if employeeErr != nil {
			return nil, employeeErr
		}
		employeesByID[employeeID] = employee
	}

	leaves := make([]leavepkg.PlanningLeaveRequest, 0)
	if s.leaveRepo != nil && len(employeeIDs) > 0 && len(targetWeekStarts) > 0 {
		rangeStart := targetWeekStarts[0]
		rangeEnd := targetWeekStarts[len(targetWeekStarts)-1].AddDate(0, 0, 6)
		items, leavesErr := s.leaveRepo.ListApprovedLeavesOverlappingRange(ctx, user.MerchantID, employeeIDs, rangeStart, rangeEnd)
		if leavesErr != nil {
			return nil, leavesErr
		}
		leaves = items
	}

	preview, err := buildPreview(templateShifts, targetWeekStarts, existingByWeek, leaves, employeesByID)
	if err != nil {
		return nil, err
	}
	return &preview, nil
}

func (s *Service) InstantiateWeekTemplate(ctx context.Context, templateID string, req WeekTemplateInstantiateRequest) (*InstantiationResult, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if s.weekSource == nil {
		return nil, models.ErrPlanningWeekNotFound
	}

	templateID = strings.TrimSpace(templateID)
	if templateID == "" {
		return nil, models.ErrPlanningWeekTemplateNotFound
	}

	targetWeekStarts, err := parseTargetWeekStarts(req.TargetWeekStarts)
	if err != nil {
		return nil, err
	}
	if len(targetWeekStarts) > MaxPreviewTargetWeeks {
		return nil, models.ErrPlanningWeekTemplatePreviewRangeTooLarge
	}

	conflictMode := ConflictMode(strings.TrimSpace(string(req.ConflictMode)))
	if !conflictMode.IsValid() {
		return nil, models.ErrPlanningWeekTemplateInvalidConflictMode
	}

	tpl, err := s.repo.GetWeekTemplateByID(ctx, user.MerchantID, templateID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, models.ErrPlanningWeekTemplateNotFound
		}
		return nil, err
	}
	templateShifts, err := s.repo.ListWeekTemplateShifts(ctx, user.MerchantID, tpl.ID)
	if err != nil {
		return nil, err
	}

	weeksByStart := make(map[string]*schedulepkg.PlanningWeek, len(targetWeekStarts))
	existingByWeek := make(map[string][]schedulepkg.PlanningShift, len(targetWeekStarts))
	for _, weekStart := range targetWeekStarts {
		weekKey := dateISO(weekStart)
		week, weekErr := s.weekSource.GetPlanningWeekByStartDate(ctx, user.MerchantID, weekStart, "")
		if weekErr == sql.ErrNoRows {
			weeksByStart[weekKey] = nil
			existingByWeek[weekKey] = []schedulepkg.PlanningShift{}
			continue
		}
		if weekErr != nil {
			return nil, weekErr
		}
		weeksByStart[weekKey] = week
		if week == nil {
			existingByWeek[weekKey] = []schedulepkg.PlanningShift{}
			continue
		}
		shifts, shiftsErr := s.weekSource.ListPlanningShifts(ctx, user.MerchantID, week.ID)
		if shiftsErr != nil {
			return nil, shiftsErr
		}
		existingByWeek[weekKey] = shifts
	}

	employeeIDs := collectTemplateEmployeeIDs(templateShifts)
	employeesByID := make(map[string]*employeespkg.Employee, len(employeeIDs))
	for _, employeeID := range employeeIDs {
		employee, employeeErr := s.employeeRepo.GetEmployeeByID(ctx, user.MerchantID, employeeID)
		if employeeErr == sql.ErrNoRows {
			continue
		}
		if employeeErr != nil {
			return nil, employeeErr
		}
		employeesByID[employeeID] = employee
	}

	leaves := make([]leavepkg.PlanningLeaveRequest, 0)
	if s.leaveRepo != nil && len(employeeIDs) > 0 && len(targetWeekStarts) > 0 {
		rangeStart := targetWeekStarts[0]
		rangeEnd := targetWeekStarts[len(targetWeekStarts)-1].AddDate(0, 0, 6)
		items, leavesErr := s.leaveRepo.ListApprovedLeavesOverlappingRange(ctx, user.MerchantID, employeeIDs, rangeStart, rangeEnd)
		if leavesErr != nil {
			return nil, leavesErr
		}
		leaves = items
	}

	result := InstantiationResult{PerWeek: make([]InstantiationPerWeekResult, 0, len(targetWeekStarts))}

	err = dbutils.RunInTx(ctx, s.repo.db, func(txCtx context.Context) error {
		for _, weekStart := range targetWeekStarts {
			weekKey := dateISO(weekStart)
			week := weeksByStart[weekKey]
			if week == nil {
				createdWeek, createWeekErr := s.weekSource.CreatePlanningWeek(txCtx, user.MerchantID, schedulepkg.PlanningWeek{
					Label:     stringPtr(defaultWeekLabel(weekStart)),
					StartDate: weekStart,
					EndDate:   weekStart.AddDate(0, 0, 6),
					Status:    "draft",
				})
				if createWeekErr != nil {
					return createWeekErr
				}
				week = createdWeek
				weeksByStart[weekKey] = createdWeek
				existingByWeek[weekKey] = []schedulepkg.PlanningShift{}
			}

			perWeek := InstantiationPerWeekResult{TargetWeekStart: weekKey, WeekID: week.ID}
			existingForWeek := append([]schedulepkg.PlanningShift(nil), existingByWeek[weekKey]...)

			for _, templateShift := range templateShifts {
				projectedDate, projectionErr := projectTemplateShiftToDate(templateShift, weekStart)
				if projectionErr != nil {
					return fmt.Errorf("project template shift: %w", projectionErr)
				}
				projected := ProjectedTemplateShift{TargetWeekStart: weekStart, ShiftDate: projectedDate, TemplateShift: templateShift}

				if templateShift.EmployeeID == nil || strings.TrimSpace(*templateShift.EmployeeID) == "" {
					createdShift, createShiftErr := s.createInstantiatedShift(txCtx, user.MerchantID, week.ID, projectedDate, templateShift, nil)
					if createShiftErr != nil {
						return createShiftErr
					}
					existingForWeek = append(existingForWeek, *createdShift)
					incrementCreatedCounters(&result, &perWeek, false)
					continue
				}

				empID := strings.TrimSpace(*templateShift.EmployeeID)
				classification := classifyConflict(projected, existingForWeek, leaves, employeesByID[empID])

				if classification.Idempotent {
					result.SkippedCount++
					perWeek.SkippedCount++
					continue
				}

				if classification.Reason == nil {
					createdShift, createShiftErr := s.createInstantiatedShift(txCtx, user.MerchantID, week.ID, projectedDate, templateShift, templateShift.EmployeeID)
					if createShiftErr != nil {
						return createShiftErr
					}
					existingForWeek = append(existingForWeek, *createdShift)
					incrementCreatedCounters(&result, &perWeek, true)
					continue
				}

				switch *classification.Reason {
				case ConflictReasonOnLeave, ConflictReasonContractEnded:
					createdShift, createShiftErr := s.createInstantiatedShift(txCtx, user.MerchantID, week.ID, projectedDate, templateShift, nil)
					if createShiftErr != nil {
						return createShiftErr
					}
					existingForWeek = append(existingForWeek, *createdShift)
					incrementCreatedCounters(&result, &perWeek, false)

				case ConflictReasonOverlap:
					switch conflictMode {
					case ConflictModeKeepExisting:
						result.SkippedCount++
						perWeek.SkippedCount++

					case ConflictModeTemplateToUnassigned:
						createdShift, createShiftErr := s.createInstantiatedShift(txCtx, user.MerchantID, week.ID, projectedDate, templateShift, nil)
						if createShiftErr != nil {
							return createShiftErr
						}
						existingForWeek = append(existingForWeek, *createdShift)
						incrementCreatedCounters(&result, &perWeek, false)

					case ConflictModeReplace:
						if classification.ExistingShiftID == nil || strings.TrimSpace(*classification.ExistingShiftID) == "" {
							result.SkippedCount++
							perWeek.SkippedCount++
							continue
						}
						if deleteErr := s.weekSource.SoftDeletePlanningShift(txCtx, user.MerchantID, *classification.ExistingShiftID); deleteErr != nil {
							return deleteErr
						}
						result.ReplacedCount++
						perWeek.ReplacedCount++
						existingForWeek = removeShiftByID(existingForWeek, *classification.ExistingShiftID)

						createdShift, createShiftErr := s.createInstantiatedShift(txCtx, user.MerchantID, week.ID, projectedDate, templateShift, templateShift.EmployeeID)
						if createShiftErr != nil {
							return createShiftErr
						}
						existingForWeek = append(existingForWeek, *createdShift)
						incrementCreatedCounters(&result, &perWeek, true)
					}
				}
			}

			existingByWeek[weekKey] = existingForWeek
			result.PerWeek = append(result.PerWeek, perWeek)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	s.logInstantiationAudit(ctx, user.MerchantID, user.UserID, templateID, req, result)

	return &result, nil
}

func incrementCreatedCounters(total *InstantiationResult, perWeek *InstantiationPerWeekResult, assigned bool) {
	total.CreatedCount++
	perWeek.CreatedCount++
	if assigned {
		total.AssignedCount++
		perWeek.AssignedCount++
		return
	}
	total.UnassignedCount++
	perWeek.UnassignedCount++
}

func removeShiftByID(shifts []schedulepkg.PlanningShift, shiftID string) []schedulepkg.PlanningShift {
	filtered := make([]schedulepkg.PlanningShift, 0, len(shifts))
	for _, shift := range shifts {
		if shift.ID == shiftID {
			continue
		}
		filtered = append(filtered, shift)
	}
	return filtered
}

func (s *Service) createInstantiatedShift(ctx context.Context, merchantID, weekID string, shiftDate time.Time, templateShift WeekTemplateShift, employeeID *string) (*schedulepkg.PlanningShift, error) {
	title := "Shift"
	if templateShift.Title != nil {
		trimmedTitle := strings.TrimSpace(*templateShift.Title)
		if trimmedTitle != "" {
			title = trimmedTitle
		}
	}

	shift := schedulepkg.PlanningShift{
		WeekID:       weekID,
		EmployeeID:   employeeID,
		PositionID:   templateShift.PositionID,
		Title:        title,
		ShiftDate:    shiftDate,
		StartTime:    templateShift.StartTime,
		EndTime:      templateShift.EndTime,
		BreakMinutes: templateShift.BreakMinutes,
		Location:     templateShift.Location,
		Notes:        templateShift.Notes,
		Status:       "planned",
	}

	createdShift, err := s.weekSource.CreatePlanningShift(ctx, merchantID, shift)
	if err != nil {
		return nil, err
	}
	return createdShift, nil
}

func (s *Service) logInstantiationAudit(ctx context.Context, merchantID, userID, templateID string, req WeekTemplateInstantiateRequest, result InstantiationResult) {
	if s.auditService == nil {
		return
	}
	_ = s.auditService.LogChange(
		ctx,
		merchantID,
		userID,
		"create",
		"planning_week_template_instantiation",
		templateID,
		nil,
		map[string]any{
			"target_week_starts": req.TargetWeekStarts,
			"conflict_mode":      req.ConflictMode,
			"result":             result,
		},
	)
}

func defaultWeekLabel(weekStart time.Time) string {
	return "Semaine du " + canonicalDate(weekStart).Format("2006-01-02")
}

func stringPtr(value string) *string {
	return &value
}

func parseTargetWeekStarts(values []string) ([]time.Time, error) {
	if len(values) == 0 {
		return nil, models.ErrInvalidData
	}
	parsed := make([]time.Time, 0, len(values))
	for _, raw := range values {
		weekStart, err := shared.ParsePlanningDate(raw)
		if err != nil {
			return nil, models.ErrPlanningInvalidDate
		}
		if canonicalDate(weekStart).Weekday() != time.Monday {
			return nil, models.ErrPlanningInvalidDate
		}
		parsed = append(parsed, canonicalDate(weekStart))
	}
	return normalizeTargetWeekStarts(parsed), nil
}

func collectTemplateEmployeeIDs(shifts []WeekTemplateShift) []string {
	seen := map[string]struct{}{}
	ids := make([]string, 0)
	for _, shift := range shifts {
		if shift.EmployeeID == nil {
			continue
		}
		employeeID := strings.TrimSpace(*shift.EmployeeID)
		if employeeID == "" {
			continue
		}
		if _, exists := seen[employeeID]; exists {
			continue
		}
		seen[employeeID] = struct{}{}
		ids = append(ids, employeeID)
	}
	return ids
}

func (s *Service) resolvePositionIDFromLegacyLabel(ctx context.Context, merchantID string, positionLabel *string) (*string, error) {
	trimmed := shared.TrimOptionalString(positionLabel)
	if trimmed == nil {
		return nil, nil
	}
	if s.employeeRepo == nil {
		return nil, nil
	}
	position, err := s.employeeRepo.GetEmployeePositionByLabel(ctx, merchantID, *trimmed, "")
	if err == sql.ErrNoRows || position == nil || !position.Active {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	resolved := position.ID
	return &resolved, nil
}

func normalizeClockHHMM(raw string) (string, error) {
	parsed, err := shared.ParsePlanningTime(raw)
	if err != nil {
		return "", err
	}
	return parsed.Format("15:04"), nil
}

func trimOrNil(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func planningShiftDateToDayOfWeek(shiftDate time.Time) int {
	// INTENTIONAL: Go weekday numbering (Sunday=0..Saturday=6) matches the repo convention exactly.
	// planning_shifts.shift_date is a DATE column; we keep only Y-M-D and anchor at noon UTC to avoid
	// accidental timezone midnight boundary shifts when computing weekday.
	y, m, d := shiftDate.Date()
	canonicalDate := time.Date(y, m, d, 12, 0, 0, 0, time.UTC)
	return int(canonicalDate.Weekday())
}

func (s *Service) validateShiftInputs(ctx context.Context, merchantID string, inputs []WeekTemplateShiftInput) ([]WeekTemplateShift, error) {
	shifts := make([]WeekTemplateShift, 0, len(inputs))

	for _, input := range inputs {
		if input.DayOfWeek == nil {
			return nil, models.ErrPlanningWeekTemplateInvalidDayOfWeek
		}
		if *input.DayOfWeek < 0 || *input.DayOfWeek > 6 {
			return nil, models.ErrPlanningWeekTemplateInvalidDayOfWeek
		}

		if input.StartTime == nil || input.EndTime == nil {
			return nil, models.ErrInvalidData
		}
		startParsed, err := shared.ParsePlanningTime(*input.StartTime)
		if err != nil {
			return nil, models.ErrPlanningShiftTemplateInvalidRange
		}
		endParsed, err := shared.ParsePlanningTime(*input.EndTime)
		if err != nil {
			return nil, models.ErrPlanningShiftTemplateInvalidRange
		}

		breakMinutes := 0
		if input.BreakMinutes != nil {
			breakMinutes = *input.BreakMinutes
		}
		if breakMinutes < 0 {
			return nil, models.ErrInvalidData
		}

		employeeID := shared.TrimOptionalString(input.EmployeeID)
		if employeeID != nil {
			if _, err := s.employeeRepo.GetEmployeeByID(ctx, merchantID, *employeeID); err != nil {
				if err == sql.ErrNoRows {
					return nil, models.ErrPlanningEmployeeNotFound
				}
				return nil, err
			}
		}

		positionID := shared.TrimOptionalString(input.PositionID)
		if positionID != nil {
			if _, err := s.employeeRepo.GetEmployeePositionByID(ctx, merchantID, *positionID); err != nil {
				if err == sql.ErrNoRows {
					return nil, models.ErrPlanningPositionNotFound
				}
				return nil, err
			}
		}

		shift := WeekTemplateShift{
			DayOfWeek:    *input.DayOfWeek,
			EmployeeID:   employeeID,
			PositionID:   positionID,
			Title:        shared.TrimOptionalString(input.Title),
			StartTime:    startParsed.Format("15:04"),
			EndTime:      endParsed.Format("15:04"),
			BreakMinutes: breakMinutes,
			Location:     shared.TrimOptionalString(input.Location),
			Notes:        shared.TrimOptionalString(input.Notes),
		}
		shifts = append(shifts, shift)
	}

	return shifts, nil
}
