package schedule

import "time"

type PlanningWeek struct {
	ID         string     `json:"id"`
	MerchantID string     `json:"merchant_id"`
	Label      *string    `json:"label,omitempty"`
	StartDate  time.Time  `json:"start_date"`
	EndDate    time.Time  `json:"end_date"`
	Status     string     `json:"status"`
	Notes      *string    `json:"notes,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	DeletedAt  *time.Time `json:"deleted_at,omitempty"`
}

type PlanningWeekCreateRequest struct {
	Label     *string `json:"label,omitempty"`
	StartDate string  `json:"start_date"`
	EndDate   string  `json:"end_date"`
	Status    *string `json:"status,omitempty"`
	Notes     *string `json:"notes,omitempty"`
}

type PlanningWeekUpdateRequest struct {
	Label     *string `json:"label,omitempty"`
	StartDate *string `json:"start_date,omitempty"`
	EndDate   *string `json:"end_date,omitempty"`
	Status    *string `json:"status,omitempty"`
	Notes     *string `json:"notes,omitempty"`
}

type PlanningShift struct {
	ID           string     `json:"id"`
	MerchantID   string     `json:"merchant_id"`
	WeekID       string     `json:"week_id"`
	EmployeeID   *string    `json:"employee_id,omitempty"`
	Title        string     `json:"title"`
	ShiftDate    time.Time  `json:"shift_date"`
	StartTime    string     `json:"start_time"`
	EndTime      string     `json:"end_time"`
	BreakMinutes int        `json:"break_minutes"`
	Position     *string    `json:"position,omitempty"`
	Location     *string    `json:"location,omitempty"`
	Notes        *string    `json:"notes,omitempty"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
}

type PlanningShiftCreateRequest struct {
	EmployeeID   *string `json:"employee_id,omitempty"`
	Title        string  `json:"title"`
	ShiftDate    string  `json:"shift_date"`
	StartTime    string  `json:"start_time"`
	EndTime      string  `json:"end_time"`
	BreakMinutes *int    `json:"break_minutes,omitempty"`
	Position     *string `json:"position,omitempty"`
	Location     *string `json:"location,omitempty"`
	Notes        *string `json:"notes,omitempty"`
	Status       *string `json:"status,omitempty"`
}

type PlanningShiftUpdateRequest struct {
	EmployeeID   *string `json:"employee_id,omitempty"`
	Title        *string `json:"title,omitempty"`
	ShiftDate    *string `json:"shift_date,omitempty"`
	StartTime    *string `json:"start_time,omitempty"`
	EndTime      *string `json:"end_time,omitempty"`
	BreakMinutes *int    `json:"break_minutes,omitempty"`
	Position     *string `json:"position,omitempty"`
	Location     *string `json:"location,omitempty"`
	Notes        *string `json:"notes,omitempty"`
	Status       *string `json:"status,omitempty"`
}
