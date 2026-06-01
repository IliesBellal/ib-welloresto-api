package timeentries

import "time"

type PlanningTimeEntry struct {
	ID                 string     `json:"id"`
	MerchantID         string     `json:"merchant_id"`
	EmployeeID         string     `json:"employee_id"`
	ShiftID            *string    `json:"shift_id"`
	AttendanceSource   string     `json:"attendance_source"`
	ClockInAt          time.Time  `json:"clock_in_at"`
	ClockOutAt         *time.Time `json:"clock_out_at"`
	ClockInNote        *string    `json:"clock_in_note"`
	ClockOutNote       *string    `json:"clock_out_note"`
	ModifiedBy         *string    `json:"modified_by"`
	ModifiedAt         *time.Time `json:"modified_at"`
	ModificationReason *string    `json:"modification_reason"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	DeletedAt          *time.Time `json:"deleted_at,omitempty"`
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

type PlanningTimeEntryManualCreateRequest struct {
	ShiftID            *string `json:"shift_id,omitempty"`
	ClockInAt          string  `json:"clock_in_at"`
	ClockOutAt         string  `json:"clock_out_at"`
	ClockInNote        *string `json:"clock_in_note,omitempty"`
	ClockOutNote       *string `json:"clock_out_note,omitempty"`
	ModificationReason string  `json:"modification_reason"`
}

type PlanningTimeEntryCorrectionRequest struct {
	ClockInAt          *string `json:"clock_in_at,omitempty"`
	ClockOutAt         *string `json:"clock_out_at,omitempty"`
	ClockInNote        *string `json:"clock_in_note,omitempty"`
	ClockOutNote       *string `json:"clock_out_note,omitempty"`
	ModificationReason string  `json:"modification_reason"`
}

type PlanningTimeEntryDeleteRequest struct {
	ModificationReason string `json:"modification_reason"`
}

type PlanningTimeEntryListFilters struct {
	From       string
	To         string
	EmployeeID string
	Page       int
	PageSize   int
}
