package planning

import (
	"welloresto-api/internal/infrastructure/r2"
	documentspkg "welloresto-api/internal/modules/planning/documents"
	employeespkg "welloresto-api/internal/modules/planning/employees"
	leavepkg "welloresto-api/internal/modules/planning/leave"
	refspkg "welloresto-api/internal/modules/planning/refs"
	schedulepkg "welloresto-api/internal/modules/planning/schedule"
	settingspkg "welloresto-api/internal/modules/planning/settings"
	swapspkg "welloresto-api/internal/modules/planning/swaps"
	timeentriespkg "welloresto-api/internal/modules/planning/timeentries"
)

type DocumentsService = documentspkg.Service
type SettingsService = settingspkg.Service
type RefsService = refspkg.Service
type EmployeesService = employeespkg.Service
type ScheduleService = schedulepkg.Service
type TimeEntriesService = timeentriespkg.Service
type LeaveRequestsService = leavepkg.Service
type ShiftSwapsService = swapspkg.Service

type PlanningService struct {
	repo      *PlanningRepository
	privateR2 *r2.Client
	*DocumentsService
	*SettingsService
	*RefsService
	*EmployeesService
	*ScheduleService
	*TimeEntriesService
	*LeaveRequestsService
	*ShiftSwapsService
}

func NewService(repo *PlanningRepository, privateR2 *r2.Client) *PlanningService {
	scheduleService := schedulepkg.NewService(repo.ScheduleRepository, repo.EmployeesRepository)
	return &PlanningService{
		repo:                 repo,
		privateR2:            privateR2,
		DocumentsService:     documentspkg.NewService(repo.DocumentsRepository, repo.EmployeesRepository, privateR2),
		SettingsService:      settingspkg.NewService(repo.SettingsRepository),
		RefsService:          refspkg.NewService(repo.RefsRepository),
		EmployeesService:     employeespkg.NewService(repo.EmployeesRepository),
		ScheduleService:      scheduleService,
		TimeEntriesService:   timeentriespkg.NewService(repo.TimeEntriesRepository, repo.EmployeesRepository, repo.ScheduleRepository, repo.RefsRepository),
		LeaveRequestsService: leavepkg.NewService(repo.LeaveRepository, repo.EmployeesRepository),
		ShiftSwapsService:    swapspkg.NewService(repo.ShiftSwapsRepository, repo.EmployeesRepository, repo.ScheduleRepository, scheduleService),
	}
}
