package planning

import (
	"database/sql"

	documentspkg "welloresto-api/internal/modules/planning/documents"
	employeespkg "welloresto-api/internal/modules/planning/employees"
	leavepkg "welloresto-api/internal/modules/planning/leave"
	refspkg "welloresto-api/internal/modules/planning/refs"
	schedulepkg "welloresto-api/internal/modules/planning/schedule"
	settingspkg "welloresto-api/internal/modules/planning/settings"
	swapspkg "welloresto-api/internal/modules/planning/swaps"
	timeentriespkg "welloresto-api/internal/modules/planning/timeentries"
)

type DocumentsRepository = documentspkg.Repository
type SettingsRepository = settingspkg.Repository
type RefsRepository = refspkg.Repository
type EmployeesRepository = employeespkg.Repository
type ScheduleRepository = schedulepkg.Repository
type TimeEntriesRepository = timeentriespkg.Repository
type LeaveRepository = leavepkg.Repository
type ShiftSwapsRepository = swapspkg.Repository

type PlanningRepository struct {
	db *sql.DB
	*DocumentsRepository
	*SettingsRepository
	*RefsRepository
	*EmployeesRepository
	*ScheduleRepository
	*TimeEntriesRepository
	*LeaveRepository
	*ShiftSwapsRepository
}

func NewRepository(db *sql.DB) *PlanningRepository {
	return &PlanningRepository{
		db:                    db,
		DocumentsRepository:   documentspkg.NewRepository(db),
		SettingsRepository:    settingspkg.NewRepository(db),
		RefsRepository:        refspkg.NewRepository(db),
		EmployeesRepository:   employeespkg.NewRepository(db),
		ScheduleRepository:    schedulepkg.NewRepository(db),
		TimeEntriesRepository: timeentriespkg.NewRepository(db),
		LeaveRepository:       leavepkg.NewRepository(db),
		ShiftSwapsRepository:  swapspkg.NewRepository(db),
	}
}

type scannable interface {
	Scan(dest ...any) error
}

type scannableRows interface {
	Scan(dest ...any) error
}
