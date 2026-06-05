package planning

import (
	"database/sql"

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
	statspkg "welloresto-api/internal/modules/stats"
)

type DocumentsRepository = documentspkg.Repository
type SettingsRepository = settingspkg.Repository
type RefsRepository = refspkg.Repository
type EmployeesRepository = employeespkg.Repository
type ScheduleRepository = schedulepkg.Repository
type ShiftTemplatesRepository = shifttemplatespkg.Repository
type WeekTemplatesRepository = weektemplatespkg.Repository
type TimeEntriesRepository = timeentriespkg.Repository
type LeaveRepository = leavepkg.Repository
type ShiftSwapsRepository = swapspkg.Repository
type PerformanceRepository = performancepkg.Repository
type RevenueForecastRepository = revenueforecastpkg.Repository

type PlanningRepository struct {
	db *sql.DB
	*DocumentsRepository
	*SettingsRepository
	*RefsRepository
	*EmployeesRepository
	*ScheduleRepository
	*ShiftTemplatesRepository
	*WeekTemplatesRepository
	*TimeEntriesRepository
	*LeaveRepository
	*ShiftSwapsRepository
	*PerformanceRepository
	*RevenueForecastRepository
}

func NewRepository(db *sql.DB) *PlanningRepository {
	statsRepository := statspkg.NewStatsRepository(db)
	return &PlanningRepository{
		db:                        db,
		DocumentsRepository:       documentspkg.NewRepository(db),
		SettingsRepository:        settingspkg.NewRepository(db),
		RefsRepository:            refspkg.NewRepository(db),
		EmployeesRepository:       employeespkg.NewRepository(db),
		ScheduleRepository:        schedulepkg.NewRepository(db),
		ShiftTemplatesRepository:  shifttemplatespkg.NewRepository(db),
		WeekTemplatesRepository:   weektemplatespkg.NewRepository(db),
		TimeEntriesRepository:     timeentriespkg.NewRepository(db),
		LeaveRepository:           leavepkg.NewRepository(db),
		ShiftSwapsRepository:      swapspkg.NewRepository(db),
		PerformanceRepository:     performancepkg.NewRepository(db, statsRepository),
		RevenueForecastRepository: revenueforecastpkg.NewRepository(db),
	}
}

type scannable interface {
	Scan(dest ...any) error
}

type scannableRows interface {
	Scan(dest ...any) error
}
