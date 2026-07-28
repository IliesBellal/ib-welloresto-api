package planning

import (
	"welloresto-api/internal/infrastructure/r2"
	auditpkg "welloresto-api/internal/modules/audit"
	daycommentspkg "welloresto-api/internal/modules/planning/daycomments"
	documentspkg "welloresto-api/internal/modules/planning/documents"
	employeespkg "welloresto-api/internal/modules/planning/employees"
	leavepkg "welloresto-api/internal/modules/planning/leave"
	performancepkg "welloresto-api/internal/modules/planning/performance"
	refspkg "welloresto-api/internal/modules/planning/refs"
	revenueforecastpkg "welloresto-api/internal/modules/planning/revenueforecast"
	schedulepkg "welloresto-api/internal/modules/planning/schedule"
	settingspkg "welloresto-api/internal/modules/planning/settings"
	shifttemplatespkg "welloresto-api/internal/modules/planning/shifttemplates"
	swapspkg "welloresto-api/internal/modules/planning/swaps"
	timeentriespkg "welloresto-api/internal/modules/planning/timeentries"
	weektemplatespkg "welloresto-api/internal/modules/planning/weektemplates"
	planningcommpkg "welloresto-api/internal/modules/planningcomm"
)

type DayCommentsService = daycommentspkg.Service
type DocumentsService = documentspkg.Service
type SettingsService = settingspkg.Service
type RefsService = refspkg.Service
type EmployeesService = employeespkg.Service
type ScheduleService = schedulepkg.Service
type ShiftTemplatesService = shifttemplatespkg.Service
type WeekTemplatesService = weektemplatespkg.Service
type TimeEntriesService = timeentriespkg.Service
type LeaveRequestsService = leavepkg.Service
type ShiftSwapsService = swapspkg.Service
type PerformanceService = performancepkg.Service
type RevenueForecastService = revenueforecastpkg.Service

type PlanningService struct {
	repo      *PlanningRepository
	privateR2 *r2.Client
	*DayCommentsService
	*DocumentsService
	*SettingsService
	*RefsService
	*EmployeesService
	*ScheduleService
	*ShiftTemplatesService
	*WeekTemplatesService
	*TimeEntriesService
	*LeaveRequestsService
	*ShiftSwapsService
	*PerformanceService
	*RevenueForecastService
}

func NewService(repo *PlanningRepository, privateR2 *r2.Client, auditService auditpkg.AuditService, planningPublisher *planningcommpkg.Service) *PlanningService {
	scheduleService := schedulepkg.NewService(
		repo.ScheduleRepository,
		repo.EmployeesRepository,
		repo.EmployeesRepository,
		auditService,
		schedulepkg.WithSettingsReader(repo.SettingsRepository),
		schedulepkg.WithPlanningPublisher(planningPublisher),
	)
	return &PlanningService{
		repo:                   repo,
		privateR2:              privateR2,
		DayCommentsService:     daycommentspkg.NewService(repo.DayCommentsRepository, auditService),
		DocumentsService:       documentspkg.NewService(repo.DocumentsRepository, repo.EmployeesRepository, privateR2),
		SettingsService:        settingspkg.NewService(repo.SettingsRepository),
		RefsService:            refspkg.NewService(repo.RefsRepository),
		EmployeesService:       employeespkg.NewService(repo.EmployeesRepository),
		ScheduleService:        scheduleService,
		ShiftTemplatesService:  shifttemplatespkg.NewService(repo.ShiftTemplatesRepository, repo.EmployeesRepository),
		WeekTemplatesService:   weektemplatespkg.NewService(repo.WeekTemplatesRepository, repo.EmployeesRepository, repo.ScheduleRepository, repo.LeaveRepository, auditService),
		TimeEntriesService:     timeentriespkg.NewService(repo.TimeEntriesRepository, repo.EmployeesRepository, repo.ScheduleRepository, repo.SettingsRepository, auditService),
		LeaveRequestsService:   leavepkg.NewService(repo.LeaveRepository, repo.EmployeesRepository),
		ShiftSwapsService:      swapspkg.NewService(repo.ShiftSwapsRepository, repo.EmployeesRepository, repo.ScheduleRepository, scheduleService, repo.SettingsRepository),
		PerformanceService:     performancepkg.NewService(repo.PerformanceRepository),
		RevenueForecastService: revenueforecastpkg.NewService(repo.RevenueForecastRepository),
	}
}
