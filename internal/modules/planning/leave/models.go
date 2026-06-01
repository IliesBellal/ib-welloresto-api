package leave

import (
	"encoding/json"
	"time"
)

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

func (r PlanningLeaveRequest) MarshalJSON() ([]byte, error) {
	type planningLeaveRequestJSON struct {
		ID                string     `json:"id"`
		MerchantID        string     `json:"merchant_id"`
		EmployeeID        string     `json:"employee_id"`
		LeaveType         string     `json:"leave_type"`
		StartDate         string     `json:"start_date"`
		EndDate           string     `json:"end_date"`
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

	return json.Marshal(planningLeaveRequestJSON{
		ID:                r.ID,
		MerchantID:        r.MerchantID,
		EmployeeID:        r.EmployeeID,
		LeaveType:         r.LeaveType,
		StartDate:         formatPlanningLeaveDateOnly(r.StartDate),
		EndDate:           formatPlanningLeaveDateOnly(r.EndDate),
		Status:            r.Status,
		Reason:            r.Reason,
		ManagerNote:       r.ManagerNote,
		RequestedByUserID: r.RequestedByUserID,
		ProcessedByUserID: r.ProcessedByUserID,
		ProcessedAt:       r.ProcessedAt,
		CreatedAt:         r.CreatedAt,
		UpdatedAt:         r.UpdatedAt,
		DeletedAt:         r.DeletedAt,
	})
}

func formatPlanningLeaveDateOnly(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format("2006-01-02")
}

type PlanningLeaveRequestListFilters struct {
	EmployeeID string
	Status     string
	Page       int
	PageSize   int
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
