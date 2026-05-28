package timeentries

import "time"

type PlanningTimeEntry struct {
	ID               string     `json:"id"`
	MerchantID       string     `json:"merchant_id"`
	EmployeeID       string     `json:"employee_id"`
	ShiftID          *string    `json:"shift_id,omitempty"`
	AttendanceSource string     `json:"attendance_source"`
	ClockInAt        time.Time  `json:"clock_in_at"`
	ClockOutAt       *time.Time `json:"clock_out_at,omitempty"`
	ClockInNote      *string    `json:"clock_in_note,omitempty"`
	ClockOutNote     *string    `json:"clock_out_note,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	DeletedAt        *time.Time `json:"deleted_at,omitempty"`
}

type PlanningTimeEntryStartRequest struct {
	ShiftID     *string `json:"shift_id,omitempty"`
	ClockInAt   *string `json:"clock_in_at,omitempty"`
	ClockInNote *string `json:"clock_in_note,omitempty"`
}

type PlanningTimeEntryStopRequest struct {
	EntryID      *string `json:"entry_id,omitempty"`
	ClockOutAt   *string `json:"clock_out_at,omitempty"`
	ClockOutNote *string `json:"clock_out_note,omitempty"`
}

type PlanningTimeEntryListFilters struct {
	Page     int
	PageSize int
}
