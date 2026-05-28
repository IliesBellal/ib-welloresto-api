package swaps

import "time"

type PlanningShiftSwapRequest struct {
	ID                  string     `json:"id"`
	MerchantID          string     `json:"merchant_id"`
	RequesterEmployeeID string     `json:"requester_employee_id"`
	RequesterShiftID    string     `json:"requester_shift_id"`
	TargetEmployeeID    string     `json:"target_employee_id"`
	TargetShiftID       string     `json:"target_shift_id"`
	Status              string     `json:"status"`
	Reason              *string    `json:"reason,omitempty"`
	ManagerNote         *string    `json:"manager_note,omitempty"`
	RequestedByUserID   *string    `json:"requested_by_user_id,omitempty"`
	ProcessedByUserID   *string    `json:"processed_by_user_id,omitempty"`
	ProcessedAt         *time.Time `json:"processed_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	DeletedAt           *time.Time `json:"deleted_at,omitempty"`
}

type PlanningShiftSwapRequestListFilters struct {
	RequesterEmployeeID string
	TargetEmployeeID    string
	Status              string
	Page                int
	PageSize            int
}

type PlanningShiftSwapRequestCreateRequest struct {
	RequesterEmployeeID string  `json:"requester_employee_id"`
	RequesterShiftID    string  `json:"requester_shift_id"`
	TargetEmployeeID    string  `json:"target_employee_id"`
	TargetShiftID       string  `json:"target_shift_id"`
	Reason              *string `json:"reason,omitempty"`
}

type PlanningShiftSwapRequestUpdateRequest struct {
	Status      *string `json:"status,omitempty"`
	Reason      *string `json:"reason,omitempty"`
	ManagerNote *string `json:"manager_note,omitempty"`
}
