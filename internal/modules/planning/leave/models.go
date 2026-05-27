package leave

import "time"

type PlanningLeaveRequest struct {
	ID                string     `json:"id"`
	MerchantID        string     `json:"merchant_id"`
	EmployeeID        string     `json:"employee_id"`
	LeaveType         string     `json:"leave_type"`
	StartDate         time.Time  `json:"start_date"`
	EndDate           time.Time  `json:"end_date"`
	Status            string     `json:"status"`
	Reason            *string    `json:"reason,omitempty"`
	ManagerNote       *string    `json:"manager_note,omitempty"`
	RequestedByUserID *string    `json:"requested_by_user_id,omitempty"`
	ProcessedByUserID *string    `json:"processed_by_user_id,omitempty"`
	ProcessedAt       *time.Time `json:"processed_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	DeletedAt         *time.Time `json:"deleted_at,omitempty"`
}

type PlanningLeaveRequestListFilters struct {
	EmployeeID string
	Status     string
}

type PlanningLeaveRequestCreateRequest struct {
	EmployeeID string  `json:"employee_id"`
	LeaveType  string  `json:"leave_type"`
	StartDate  string  `json:"start_date"`
	EndDate    string  `json:"end_date"`
	Reason     *string `json:"reason,omitempty"`
}

type PlanningLeaveRequestUpdateRequest struct {
	LeaveType   *string `json:"leave_type,omitempty"`
	StartDate   *string `json:"start_date,omitempty"`
	EndDate     *string `json:"end_date,omitempty"`
	Status      *string `json:"status,omitempty"`
	Reason      *string `json:"reason,omitempty"`
	ManagerNote *string `json:"manager_note,omitempty"`
}
