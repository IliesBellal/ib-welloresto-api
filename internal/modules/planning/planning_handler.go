package planning

import (
	documentspkg "welloresto-api/internal/modules/planning/documents"
	employeespkg "welloresto-api/internal/modules/planning/employees"
	leavepkg "welloresto-api/internal/modules/planning/leave"
	refspkg "welloresto-api/internal/modules/planning/refs"
	schedulepkg "welloresto-api/internal/modules/planning/schedule"
	settingspkg "welloresto-api/internal/modules/planning/settings"
	shifttemplatespkg "welloresto-api/internal/modules/planning/shifttemplates"
	swapspkg "welloresto-api/internal/modules/planning/swaps"
	timeentriespkg "welloresto-api/internal/modules/planning/timeentries"
	weektemplatespkg "welloresto-api/internal/modules/planning/weektemplates"
)

type DocumentsHandler = documentspkg.Handler
type SettingsHandler = settingspkg.Handler
type RefsHandler = refspkg.Handler
type EmployeesHandler = employeespkg.Handler
type ScheduleHandler = schedulepkg.Handler
type ShiftTemplatesHandler = shifttemplatespkg.Handler
type WeekTemplatesHandler = weektemplatespkg.Handler
type TimeEntriesHandler = timeentriespkg.Handler
type LeaveRequestsHandler = leavepkg.Handler
type ShiftSwapsHandler = swapspkg.Handler

type PlanningHandler struct {
	svc *PlanningService
	*DocumentsHandler
	*SettingsHandler
	*RefsHandler
	*EmployeesHandler
	*ScheduleHandler
	*ShiftTemplatesHandler
	*WeekTemplatesHandler
	*TimeEntriesHandler
	*LeaveRequestsHandler
	*ShiftSwapsHandler
}

func NewHandler(svc *PlanningService) *PlanningHandler {
	return &PlanningHandler{
		svc:                   svc,
		DocumentsHandler:      documentspkg.NewHandler(svc.DocumentsService),
		SettingsHandler:       settingspkg.NewHandler(svc.SettingsService),
		RefsHandler:           refspkg.NewHandler(svc.RefsService),
		EmployeesHandler:      employeespkg.NewHandler(svc.EmployeesService),
		ScheduleHandler:       schedulepkg.NewHandler(svc.ScheduleService),
		ShiftTemplatesHandler: shifttemplatespkg.NewHandler(svc.ShiftTemplatesService),
		WeekTemplatesHandler:  weektemplatespkg.NewHandler(svc.WeekTemplatesService),
		TimeEntriesHandler:    timeentriespkg.NewHandler(svc.TimeEntriesService),
		LeaveRequestsHandler:  leavepkg.NewHandler(svc.LeaveRequestsService),
		ShiftSwapsHandler:     swapspkg.NewHandler(svc.ShiftSwapsService),
	}
}
