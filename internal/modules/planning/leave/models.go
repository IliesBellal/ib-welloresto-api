package leave

import (
	"encoding/json"
	"time"

	modelspkg "welloresto-api/internal/models"
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

type PlanningLeaveConflictingShift struct {
	ID         string             `json:"id"`
	WeekID     string             `json:"week_id"`
	ShiftDate  modelspkg.DateOnly `json:"shift_date"`
	StartTime  string             `json:"start_time"`
	EndTime    string             `json:"end_time"`
	PositionID *string            `json:"position_id"`
	Position   *string            `json:"position"`
}

type PlanningLeaveRequestCreateRequest struct {
	EmployeeID string  `json:"employee_id"`
	LeaveType  string  `json:"leave_type"`
	StartDate  string  `json:"start_date"`
	EndDate    string  `json:"end_date"`
	Reason     *string `json:"reason,omitempty"`
}

// PlanningLeaveRequestSelfCreateRequest is the employee-facing payload for
// /planning/me/leave-requests. employee_id and status are intentionally absent
// because both are controlled by the backend.
type PlanningLeaveRequestSelfCreateRequest struct {
	LeaveType string  `json:"leave_type"`
	StartDate string  `json:"start_date"`
	EndDate   string  `json:"end_date"`
	Reason    *string `json:"reason,omitempty"`
}

type PlanningLeaveRequestUpdateRequest struct {
	LeaveType   *string `json:"leave_type,omitempty"`
	StartDate   *string `json:"start_date,omitempty"`
	EndDate     *string `json:"end_date,omitempty"`
	Status      *string `json:"status,omitempty"`
	Reason      *string `json:"reason,omitempty"`
	ManagerNote *string `json:"manager_note,omitempty"`
}

// PlanningLeaveRequestSelfView is the employee-facing DTO for /planning/me/leave-requests.
// It deliberately omits internal user IDs (requested_by_user_id, processed_by_user_id)
// and exposes manager_note only when the request has been processed (approved/rejected).
type PlanningLeaveRequestSelfView struct {
	ID          string     `json:"id"`
	EmployeeID  string     `json:"employee_id"`
	LeaveType   string     `json:"leave_type"`
	StartDate   time.Time  `json:"-"`
	EndDate     time.Time  `json:"-"`
	Status      string     `json:"status"`
	Reason      *string    `json:"reason,omitempty"`
	ManagerNote *string    `json:"-"` // conditionally exposed via MarshalJSON
	ProcessedAt *time.Time `json:"processed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (v PlanningLeaveRequestSelfView) MarshalJSON() ([]byte, error) {
	type selfViewJSON struct {
		ID          string     `json:"id"`
		EmployeeID  string     `json:"employee_id"`
		LeaveType   string     `json:"leave_type"`
		StartDate   string     `json:"start_date"`
		EndDate     string     `json:"end_date"`
		Status      string     `json:"status"`
		Reason      *string    `json:"reason,omitempty"`
		ManagerNote *string    `json:"manager_note,omitempty"`
		ProcessedAt *time.Time `json:"processed_at,omitempty"`
		CreatedAt   time.Time  `json:"created_at"`
	}

	// Expose manager_note only when the request has been processed.
	var managerNote *string
	if v.Status == "approved" || v.Status == "rejected" {
		managerNote = v.ManagerNote
	}

	return json.Marshal(selfViewJSON{
		ID:          v.ID,
		EmployeeID:  v.EmployeeID,
		LeaveType:   v.LeaveType,
		StartDate:   formatPlanningLeaveDateOnly(v.StartDate),
		EndDate:     formatPlanningLeaveDateOnly(v.EndDate),
		Status:      v.Status,
		Reason:      v.Reason,
		ManagerNote: managerNote,
		ProcessedAt: v.ProcessedAt,
		CreatedAt:   v.CreatedAt,
	})
}
