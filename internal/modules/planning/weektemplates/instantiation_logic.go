package weektemplates

import (
	"fmt"
	"sort"
	"strings"
	"time"

	employeespkg "welloresto-api/internal/modules/planning/employees"
	leavepkg "welloresto-api/internal/modules/planning/leave"
	schedulepkg "welloresto-api/internal/modules/planning/schedule"
)

type ProjectedTemplateShift struct {
	TargetWeekStart time.Time
	ShiftDate       time.Time
	TemplateShift   WeekTemplateShift
}

type ConflictClassification struct {
	Reason          *ConflictReason
	ExistingShiftID *string
	Idempotent      bool
	ForceUnassigned bool
}

func projectTemplateShiftToDate(templateShift WeekTemplateShift, weekStart time.Time) (time.Time, error) {
	if templateShift.DayOfWeek < 0 || templateShift.DayOfWeek > 6 {
		return time.Time{}, fmt.Errorf("day_of_week out of range: %d", templateShift.DayOfWeek)
	}
	weekStartDate := canonicalDate(weekStart)
	if weekStartDate.Weekday() != time.Monday {
		return time.Time{}, fmt.Errorf("target_week_start must be monday")
	}
	offset := templateShift.DayOfWeek - 1
	if templateShift.DayOfWeek == 0 {
		offset = 6
	}
	return weekStartDate.AddDate(0, 0, offset), nil
}

func classifyConflict(projectedShift ProjectedTemplateShift, existingShifts []schedulepkg.PlanningShift, leaves []leavepkg.PlanningLeaveRequest, employee *employeespkg.Employee) ConflictClassification {
	templateEmployeeID := projectedShift.TemplateShift.EmployeeID
	if templateEmployeeID == nil || strings.TrimSpace(*templateEmployeeID) == "" {
		return ConflictClassification{}
	}

	templateStart := normalizeClock(projectedShift.TemplateShift.StartTime)
	templateEnd := normalizeClock(projectedShift.TemplateShift.EndTime)
	if templateStart == "" || templateEnd == "" {
		return ConflictClassification{}
	}

	for _, existing := range existingShifts {
		if !sameEmployee(existing.EmployeeID, *templateEmployeeID) {
			continue
		}
		if !sameDate(existing.ShiftDate, projectedShift.ShiftDate) {
			continue
		}
		existingStart := normalizeClock(existing.StartTime)
		existingEnd := normalizeClock(existing.EndTime)
		if existingStart == templateStart && existingEnd == templateEnd && sameStringPtr(existing.PositionID, projectedShift.TemplateShift.PositionID) {
			return ConflictClassification{Idempotent: true}
		}
	}

	for _, leave := range leaves {
		if leave.Status != "approved" || leave.EmployeeID != *templateEmployeeID {
			continue
		}
		if dateInRange(projectedShift.ShiftDate, leave.StartDate, leave.EndDate) {
			reason := ConflictReasonOnLeave
			return ConflictClassification{Reason: &reason, ForceUnassigned: true}
		}
	}

	if employee != nil && employee.ContractEndDate != nil {
		contractEnd := canonicalDate(*employee.ContractEndDate)
		if contractEnd.Before(canonicalDate(projectedShift.ShiftDate)) {
			reason := ConflictReasonContractEnded
			return ConflictClassification{Reason: &reason, ForceUnassigned: true}
		}
	}

	for _, existing := range existingShifts {
		if !sameEmployee(existing.EmployeeID, *templateEmployeeID) {
			continue
		}
		if !sameDate(existing.ShiftDate, projectedShift.ShiftDate) {
			continue
		}
		if timesOverlap(templateStart, templateEnd, normalizeClock(existing.StartTime), normalizeClock(existing.EndTime)) {
			reason := ConflictReasonOverlap
			existingID := existing.ID
			return ConflictClassification{Reason: &reason, ExistingShiftID: &existingID}
		}
	}

	return ConflictClassification{}
}

func buildPreview(templateShifts []WeekTemplateShift, targetWeekStarts []time.Time, existingByWeek map[string][]schedulepkg.PlanningShift, leaves []leavepkg.PlanningLeaveRequest, employees map[string]*employeespkg.Employee) (InstantiationPreview, error) {
	normalizedWeekStarts := normalizeTargetWeekStarts(targetWeekStarts)
	preview := InstantiationPreview{
		TargetWeekStarts: make([]string, 0, len(normalizedWeekStarts)),
		Conflicts:        make([]InstantiationConflict, 0),
	}

	impactedEmployees := map[string]struct{}{}
	for _, weekStart := range normalizedWeekStarts {
		weekStartISO := dateISO(weekStart)
		preview.TargetWeekStarts = append(preview.TargetWeekStarts, weekStartISO)
		existingForWeek := existingByWeek[weekStartISO]

		for _, templateShift := range templateShifts {
			projectedDate, err := projectTemplateShiftToDate(templateShift, weekStart)
			if err != nil {
				return InstantiationPreview{}, err
			}
			projected := ProjectedTemplateShift{TargetWeekStart: weekStart, ShiftDate: projectedDate, TemplateShift: templateShift}

			if templateShift.EmployeeID == nil || strings.TrimSpace(*templateShift.EmployeeID) == "" {
				preview.ToCreateCount++
				continue
			}

			empID := strings.TrimSpace(*templateShift.EmployeeID)
			classification := classifyConflict(projected, existingForWeek, leaves, employees[empID])
			if classification.Idempotent {
				preview.IdempotentSkippedCount++
				continue
			}
			if classification.Reason == nil {
				preview.ToCreateCount++
				continue
			}

			conflict := InstantiationConflict{
				TargetWeekStart: weekStartISO,
				Day:             dateISO(projectedDate),
				TemplateShift: InstantiationShiftRef{
					DayOfWeek:  templateShift.DayOfWeek,
					StartTime:  templateShift.StartTime,
					EndTime:    templateShift.EndTime,
					PositionID: templateShift.PositionID,
				},
				ExistingShiftID: classification.ExistingShiftID,
				EmployeeID:      empID,
				EmployeeName:    employeeDisplayName(employees[empID], empID),
				Reason:          *classification.Reason,
			}
			preview.Conflicts = append(preview.Conflicts, conflict)
			impactedEmployees[empID] = struct{}{}

			if classification.ForceUnassigned {
				preview.ToCreateCount++
				preview.AutoUnassignedCount++
			}
		}
	}

	preview.ImpactedEmployeeCount = len(impactedEmployees)
	return preview, nil
}

func normalizeTargetWeekStarts(values []time.Time) []time.Time {
	seen := map[string]time.Time{}
	for _, value := range values {
		normalized := canonicalDate(value)
		seen[dateISO(normalized)] = normalized
	}
	items := make([]time.Time, 0, len(seen))
	for _, value := range seen {
		items = append(items, value)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Before(items[j]) })
	return items
}

func timesOverlap(startA, endA, startB, endB string) bool {
	if startA == "" || endA == "" || startB == "" || endB == "" {
		return false
	}
	return !(endA <= startB || startA >= endB)
}

func normalizeClock(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) >= 5 {
		return trimmed[:5]
	}
	return ""
}

func dateISO(value time.Time) string {
	return canonicalDate(value).Format("2006-01-02")
}

func canonicalDate(value time.Time) time.Time {
	y, m, d := value.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func sameDate(left, right time.Time) bool {
	return dateISO(left) == dateISO(right)
}

func sameEmployee(existingEmployeeID *string, expected string) bool {
	if existingEmployeeID == nil {
		return false
	}
	return strings.TrimSpace(*existingEmployeeID) == expected
}

func sameStringPtr(left, right *string) bool {
	leftTrimmed := strings.TrimSpace(deref(left))
	rightTrimmed := strings.TrimSpace(deref(right))
	return leftTrimmed == rightTrimmed
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func dateInRange(day, start, end time.Time) bool {
	dayDate := canonicalDate(day)
	startDate := canonicalDate(start)
	endDate := canonicalDate(end)
	return (dayDate.Equal(startDate) || dayDate.After(startDate)) && (dayDate.Equal(endDate) || dayDate.Before(endDate))
}

func employeeDisplayName(employee *employeespkg.Employee, fallbackID string) string {
	if employee == nil {
		return fallbackID
	}
	fullName := strings.TrimSpace(strings.TrimSpace(employee.FirstName) + " " + strings.TrimSpace(employee.LastName))
	if fullName == "" {
		return fallbackID
	}
	return fullName
}
