package schedule

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"welloresto-api/internal/helpers"
	settingspkg "welloresto-api/internal/modules/planning/settings"
	planningcommpkg "welloresto-api/internal/modules/planningcomm"
)

const inactivePlanningSMSFallbackWindow = 7 * 24 * time.Hour

const (
	notificationModeAll         = "all"
	notificationModeChangesOnly = "changes_only"
	notificationModeNone        = "none"
)

type PlanningSettingsReader interface {
	GetOrCreateSettings(ctx context.Context, merchantID string) (*settingspkg.PlanningSettings, error)
}

type PlanningWeekPublisher interface {
	SendPublishedWeek(ctx context.Context, msg planningcommpkg.PublishedWeekMessage)
}

type ServiceOption func(*Service)

func WithSettingsReader(reader PlanningSettingsReader) ServiceOption {
	return func(s *Service) { s.settingsReader = reader }
}

func WithPlanningPublisher(publisher PlanningWeekPublisher) ServiceOption {
	return func(s *Service) { s.publisher = publisher }
}

type PublishPlanningWeekRequest struct {
	NotificationMode *string `json:"notification_mode,omitempty"`
}

type publishedShiftSnapshot struct {
	EmployeeID    string
	ShiftDate     time.Time
	StartTime     string
	EndTime       string
	PositionLabel string
}

type planningNotificationRecipient struct {
	EmployeeID       string
	FirstName        string
	LastName         string
	Email            *string
	Phone            *string
	LastLoginAt      *time.Time
	LastDeviceUsedAt *time.Time
}

func (r planningNotificationRecipient) DisplayName() string {
	fullName := strings.TrimSpace(strings.TrimSpace(r.FirstName) + " " + strings.TrimSpace(r.LastName))
	if fullName == "" {
		return strings.TrimSpace(r.EmployeeID)
	}
	return fullName
}

func (r planningNotificationRecipient) LastActivityAt() *time.Time {
	if r.LastLoginAt == nil {
		return r.LastDeviceUsedAt
	}
	if r.LastDeviceUsedAt == nil {
		return r.LastLoginAt
	}
	if r.LastDeviceUsedAt.After(*r.LastLoginAt) {
		return r.LastDeviceUsedAt
	}
	return r.LastLoginAt
}

func normalizeNotificationMode(mode string) string {
	return strings.ToLower(strings.TrimSpace(mode))
}

func resolveNotificationMode(requested *string, hasPreviousSnapshot bool) (string, error) {
	if requested == nil || strings.TrimSpace(*requested) == "" {
		if hasPreviousSnapshot {
			return notificationModeChangesOnly, nil
		}
		return notificationModeAll, nil
	}
	mode := normalizeNotificationMode(*requested)
	switch mode {
	case notificationModeAll, notificationModeChangesOnly, notificationModeNone:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid notification_mode")
	}
}

func canonicalShiftKeys(items []publishedShiftSnapshot) []string {
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, fmt.Sprintf("%s|%s|%s|%s", item.ShiftDate.UTC().Format("2006-01-02"), strings.TrimSpace(item.StartTime), strings.TrimSpace(item.EndTime), strings.TrimSpace(item.PositionLabel)))
	}
	sort.Strings(keys)
	return keys
}

func employeeShiftsChanged(previous, current []publishedShiftSnapshot) bool {
	prevKeys := canonicalShiftKeys(previous)
	currentKeys := canonicalShiftKeys(current)
	if len(prevKeys) != len(currentKeys) {
		return true
	}
	for index := range prevKeys {
		if prevKeys[index] != currentKeys[index] {
			return true
		}
	}
	return false
}

func (s *Service) publishWeekAndNotify(ctx context.Context, merchantID string, week *PlanningWeek, req PublishPlanningWeekRequest) (*PlanningWeek, error) {
	currentSnapshots, err := s.repo.ListPlanningShiftsForPublication(ctx, merchantID, week.ID)
	if err != nil {
		return nil, err
	}
	previousSnapshots, err := s.repo.ListPublishedShiftSnapshots(ctx, merchantID, week.ID)
	if err != nil {
		return nil, err
	}
	mode, err := resolveNotificationMode(req.NotificationMode, len(previousSnapshots) > 0)
	if err != nil {
		return nil, err
	}
	publishedAt := time.Now().UTC()
	publishedWeek, err := s.repo.PublishPlanningWeekWithSnapshots(ctx, merchantID, week.ID, publishedAt, currentSnapshots)
	if err != nil {
		return nil, err
	}
	if mode == notificationModeNone || s.publisher == nil || s.settingsReader == nil {
		return publishedWeek, nil
	}
	settings, err := s.settingsReader.GetOrCreateSettings(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	merchantName, err := s.repo.GetMerchantDisplayName(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	currentByEmployee := groupPublishedShiftsByEmployee(currentSnapshots)
	previousByEmployee := groupPublishedShiftsByEmployee(previousSnapshots)
	employeeIDs := unionEmployeeIDs(currentByEmployee, previousByEmployee, mode)
	if len(employeeIDs) == 0 {
		return publishedWeek, nil
	}
	recipients, err := s.repo.ListPlanningNotificationRecipients(ctx, merchantID, employeeIDs)
	if err != nil {
		return nil, err
	}
	recipientsByID := mapRecipientsByID(recipients)
	weekLabel := formatPlanningWeekLabel(publishedWeek)

	for _, employeeID := range employeeIDs {
		if mode == notificationModeChangesOnly && !employeeShiftsChanged(previousByEmployee[employeeID], currentByEmployee[employeeID]) {
			continue
		}
		recipient, ok := recipientsByID[employeeID]
		if !ok {
			continue
		}
		if recipient.Email == nil && recipient.Phone == nil {
			continue
		}
		lastActivity := recipient.LastActivityAt()
		s.publisher.SendPublishedWeek(ctx, planningcommpkg.PublishedWeekMessage{
			WeekID:        publishedWeek.ID,
			MerchantName:  merchantName,
			EmployeeID:    employeeID,
			EmployeeName:  recipient.DisplayName(),
			EmployeeEmail: derefString(recipient.Email),
			EmployeePhone: derefString(recipient.Phone),
			WeekLabel:     weekLabel,
			Shifts:        toPlanningCommShifts(currentByEmployee[employeeID]),
			AllowSMS:      settings.PlanningSMSNotificationsEnabled,
			SendInlineSMS: shouldSendInlinePlanningSMS(lastActivity, publishedAt),
		})
	}

	return publishedWeek, nil
}

func shouldSendInlinePlanningSMS(lastActivity *time.Time, publishedAt time.Time) bool {
	if lastActivity == nil {
		return true
	}
	return publishedAt.Sub(*lastActivity) > inactivePlanningSMSFallbackWindow
}

func groupPublishedShiftsByEmployee(items []publishedShiftSnapshot) map[string][]publishedShiftSnapshot {
	grouped := make(map[string][]publishedShiftSnapshot, len(items))
	for _, item := range items {
		employeeID := strings.TrimSpace(item.EmployeeID)
		if employeeID == "" {
			continue
		}
		grouped[employeeID] = append(grouped[employeeID], item)
	}
	return grouped
}

func unionEmployeeIDs(current, previous map[string][]publishedShiftSnapshot, mode string) []string {
	seen := map[string]bool{}
	ids := make([]string, 0, len(current)+len(previous))
	for employeeID := range current {
		if !seen[employeeID] {
			seen[employeeID] = true
			ids = append(ids, employeeID)
		}
	}
	if mode != notificationModeAll && mode != notificationModeChangesOnly {
		sort.Strings(ids)
		return ids
	}
	for employeeID := range previous {
		if !seen[employeeID] {
			seen[employeeID] = true
			ids = append(ids, employeeID)
		}
	}
	sort.Strings(ids)
	return ids
}

func mapRecipientsByID(items []planningNotificationRecipient) map[string]planningNotificationRecipient {
	mapped := make(map[string]planningNotificationRecipient, len(items))
	for _, item := range items {
		mapped[item.EmployeeID] = item
	}
	return mapped
}

func toPlanningCommShifts(items []publishedShiftSnapshot) []planningcommpkg.ShiftSummary {
	summaries := make([]planningcommpkg.ShiftSummary, 0, len(items))
	for _, item := range items {
		summaries = append(summaries, planningcommpkg.ShiftSummary{
			DayLabel:      frenchWeekdayLabel(item.ShiftDate),
			StartTime:     trimClockSeconds(item.StartTime),
			EndTime:       trimClockSeconds(item.EndTime),
			PositionLabel: item.PositionLabel,
		})
	}
	return summaries
}

func formatPlanningWeekLabel(week *PlanningWeek) string {
	if week == nil {
		return "semaine à venir"
	}
	return fmt.Sprintf("la semaine du %s au %s", week.StartDate.UTC().Format("02/01/2006"), week.EndDate.UTC().Format("02/01/2006"))
}

func trimClockSeconds(raw string) string {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) >= 2 {
		return parts[0] + ":" + parts[1]
	}
	return strings.TrimSpace(raw)
}

func frenchWeekdayLabel(value time.Time) string {
	switch value.UTC().Weekday() {
	case time.Monday:
		return "Lun"
	case time.Tuesday:
		return "Mar"
	case time.Wednesday:
		return "Mer"
	case time.Thursday:
		return "Jeu"
	case time.Friday:
		return "Ven"
	case time.Saturday:
		return "Sam"
	default:
		return "Dim"
	}
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func newPublishedShiftSnapshot(employeeID string, shiftDate time.Time, startTime, endTime, positionLabel string) publishedShiftSnapshot {
	return publishedShiftSnapshot{
		EmployeeID:    strings.TrimSpace(employeeID),
		ShiftDate:     shiftDate,
		StartTime:     strings.TrimSpace(startTime),
		EndTime:       strings.TrimSpace(endTime),
		PositionLabel: strings.TrimSpace(positionLabel),
	}
}

func snapshotID() string {
	return helpers.GeneratePrefixedID("plan-publish-snap")
}
