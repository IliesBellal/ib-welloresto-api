package swaps

import (
	"encoding/json"
	"time"

	"welloresto-api/internal/models"
)

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

type PlanningShiftSwapRequestSelfShiftView struct {
	ID            string           `json:"id"`
	EmployeeID    *string          `json:"employee_id,omitempty"`
	PositionID    *string          `json:"position_id,omitempty"`
	Position      *string          `json:"position,omitempty"`
	PositionColor *string          `json:"position_color,omitempty"`
	Title         *string          `json:"title,omitempty"`
	ShiftDate     *models.DateOnly `json:"shift_date,omitempty"`
	StartTime     *string          `json:"start_time,omitempty"`
	EndTime       *string          `json:"end_time,omitempty"`
}

type PlanningShiftSwapRequestSelfView struct {
	ID                    string                                `json:"id"`
	RequesterEmployeeID   string                                `json:"requester_employee_id"`
	RequesterEmployeeName *string                               `json:"requester_employee_name,omitempty"`
	RequesterShiftID      string                                `json:"requester_shift_id"`
	RequesterShift        PlanningShiftSwapRequestSelfShiftView `json:"requester_shift"`
	TargetEmployeeID      string                                `json:"target_employee_id"`
	TargetEmployeeName    *string                               `json:"target_employee_name,omitempty"`
	TargetShiftID         string                                `json:"target_shift_id"`
	TargetShift           PlanningShiftSwapRequestSelfShiftView `json:"target_shift"`
	Status                string                                `json:"status"`
	Reason                *string                               `json:"reason,omitempty"`
	ManagerNote           *string                               `json:"-"`
	ProcessedAt           *time.Time                            `json:"processed_at,omitempty"`
	CreatedAt             time.Time                             `json:"created_at"`
}

func (v PlanningShiftSwapRequestSelfView) MarshalJSON() ([]byte, error) {
	type selfViewJSON struct {
		ID                    string                                `json:"id"`
		RequesterEmployeeID   string                                `json:"requester_employee_id"`
		RequesterEmployeeName *string                               `json:"requester_employee_name,omitempty"`
		RequesterShiftID      string                                `json:"requester_shift_id"`
		RequesterShift        PlanningShiftSwapRequestSelfShiftView `json:"requester_shift"`
		TargetEmployeeID      string                                `json:"target_employee_id"`
		TargetEmployeeName    *string                               `json:"target_employee_name,omitempty"`
		TargetShiftID         string                                `json:"target_shift_id"`
		TargetShift           PlanningShiftSwapRequestSelfShiftView `json:"target_shift"`
		Status                string                                `json:"status"`
		Reason                *string                               `json:"reason,omitempty"`
		ManagerNote           *string                               `json:"manager_note,omitempty"`
		ProcessedAt           *time.Time                            `json:"processed_at,omitempty"`
		CreatedAt             time.Time                             `json:"created_at"`
	}

	var managerNote *string
	if v.Status == "approved" || v.Status == "rejected" {
		managerNote = v.ManagerNote
	}

	return json.Marshal(selfViewJSON{
		ID:                    v.ID,
		RequesterEmployeeID:   v.RequesterEmployeeID,
		RequesterEmployeeName: v.RequesterEmployeeName,
		RequesterShiftID:      v.RequesterShiftID,
		RequesterShift:        v.RequesterShift,
		TargetEmployeeID:      v.TargetEmployeeID,
		TargetEmployeeName:    v.TargetEmployeeName,
		TargetShiftID:         v.TargetShiftID,
		TargetShift:           v.TargetShift,
		Status:                v.Status,
		Reason:                v.Reason,
		ManagerNote:           managerNote,
		ProcessedAt:           v.ProcessedAt,
		CreatedAt:             v.CreatedAt,
	})
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
