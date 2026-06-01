package shared

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
	"time"

	"welloresto-api/internal/models"
)

var planningHexColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type PlanningMemberEmployeeIDResolver interface {
	GetEmployeeIDByMemberID(ctx context.Context, merchantID string, memberID int64) (string, error)
}

func ParsePlanningDateRange(startDateRaw, endDateRaw string) (time.Time, time.Time, error) {
	startDate, err := ParsePlanningDate(startDateRaw)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	endDate, err := ParsePlanningDate(endDateRaw)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if endDate.Before(startDate) {
		return time.Time{}, time.Time{}, models.ErrPlanningInvalidDate
	}
	return startDate, endDate, nil
}

func ParsePlanningDate(raw string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, models.ErrPlanningInvalidDate
	}
	return parsed.UTC(), nil
}

func ParsePlanningTimeRange(startRaw, endRaw string) (string, string, error) {
	startTime, err := ParsePlanningTime(startRaw)
	if err != nil {
		return "", "", err
	}
	endTime, err := ParsePlanningTime(endRaw)
	if err != nil {
		return "", "", err
	}
	if !endTime.After(startTime) {
		return "", "", models.ErrPlanningShiftInvalidRange
	}
	return startTime.Format("15:04:05"), endTime.Format("15:04:05"), nil
}

func ParsePlanningTime(raw string) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, models.ErrPlanningInvalidHours
	}
	if parsed, err := time.Parse("15:04:05", trimmed); err == nil {
		return parsed, nil
	}
	parsed, err := time.Parse("15:04", trimmed)
	if err != nil {
		return time.Time{}, models.ErrPlanningInvalidHours
	}
	return parsed, nil
}

func ParsePlanningDateTime(raw string) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, models.ErrPlanningInvalidDate
	}
	formats := []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"}
	for _, format := range formats {
		if parsed, err := time.Parse(format, trimmed); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, models.ErrPlanningInvalidDate
}

func TrimOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func NormalizePlanningHexColor(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func IsValidPlanningHexColor(value string) bool {
	return planningHexColorPattern.MatchString(value)
}

func SamePlanningDay(left time.Time, right time.Time) bool {
	leftUTC := left.UTC()
	rightUTC := right.UTC()
	return leftUTC.Year() == rightUTC.Year() && leftUTC.Month() == rightUTC.Month() && leftUTC.Day() == rightUTC.Day()
}

func DerefString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func IsValidPlanningWeekStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "draft", "published", "locked":
		return true
	default:
		return false
	}
}

func IsValidPlanningShiftStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "planned", "confirmed", "done", "cancelled":
		return true
	default:
		return false
	}
}

func IsValidPlanningLeaveType(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "paid", "unpaid", "sick", "other":
		return true
	default:
		return false
	}
}

func IsValidPlanningLeaveStatus(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "pending", "approved", "rejected", "cancelled":
		return true
	default:
		return false
	}
}

func IsValidPlanningShiftSwapStatus(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "pending", "approved", "rejected", "cancelled":
		return true
	default:
		return false
	}
}

func IsCurrentPlanningEmployeeReference(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "me")
}

func ResolvePlanningEmployeeID(ctx context.Context, resolver PlanningMemberEmployeeIDResolver, merchantID, requestedEmployeeID string, currentMemberID int64) (string, error) {
	employeeID := strings.TrimSpace(requestedEmployeeID)
	if !IsCurrentPlanningEmployeeReference(employeeID) {
		return employeeID, nil
	}
	if currentMemberID == 0 {
		return "", models.ErrPlanningEmployeeNotFound
	}
	resolvedEmployeeID, err := resolver.GetEmployeeIDByMemberID(ctx, merchantID, currentMemberID)
	if err == sql.ErrNoRows || strings.TrimSpace(resolvedEmployeeID) == "" {
		return "", models.ErrPlanningEmployeeNotFound
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resolvedEmployeeID), nil
}
