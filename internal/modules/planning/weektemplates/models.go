package weektemplates

import (
	"encoding/json"
	"time"
)

type ConflictMode string

const (
	ConflictModeKeepExisting         ConflictMode = "keep_existing"
	ConflictModeReplace              ConflictMode = "replace"
	ConflictModeTemplateToUnassigned ConflictMode = "template_to_unassigned"
)

type ConflictReason string

const (
	ConflictReasonOverlap       ConflictReason = "overlap"
	ConflictReasonOnLeave       ConflictReason = "on_leave"
	ConflictReasonContractEnded ConflictReason = "contract_ended"
)

const MaxPreviewTargetWeeks = 26

type WeekTemplate struct {
	ID                 string              `json:"id"`
	MerchantID         string              `json:"merchant_id"`
	Label              string              `json:"label"`
	Notes              *string             `json:"notes"`
	Active             bool                `json:"active"`
	ShiftCount         int                 `json:"shift_count"`
	WeekTemplateShifts []WeekTemplateShift `json:"-"`
	CreatedAt          time.Time           `json:"created_at"`
	UpdatedAt          time.Time           `json:"updated_at"`
}

type WeekTemplateShift struct {
	ID             string    `json:"id"`
	WeekTemplateID string    `json:"week_template_id"`
	DayOfWeek      int       `json:"day_of_week"`
	EmployeeID     *string   `json:"employee_id"`
	PositionID     *string   `json:"position_id"`
	Title          *string   `json:"title"`
	StartTime      string    `json:"start_time"`
	EndTime        string    `json:"end_time"`
	BreakMinutes   int       `json:"break_minutes"`
	Location       *string   `json:"location"`
	Notes          *string   `json:"notes"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type WeekTemplateCreateRequest struct {
	Label  string                       `json:"label"`
	Notes  *string                      `json:"notes"`
	Active *bool                        `json:"active,omitempty"`
	Shifts WeekTemplateShiftInputsField `json:"shifts"`
}

type WeekTemplateUpdateRequest struct {
	Label  *string                      `json:"label,omitempty"`
	Notes  NullableStringPatchField     `json:"notes,omitempty"`
	Active *bool                        `json:"active,omitempty"`
	Shifts WeekTemplateShiftInputsField `json:"shifts,omitempty"`
}

type WeekTemplateFromWeekRequest struct {
	WeekID string  `json:"week_id"`
	Label  string  `json:"label"`
	Notes  *string `json:"notes"`
}

type WeekTemplatePreviewRequest struct {
	TargetWeekStarts []string `json:"target_week_starts"`
}

type WeekTemplateInstantiateRequest struct {
	TargetWeekStarts []string     `json:"target_week_starts"`
	ConflictMode     ConflictMode `json:"conflict_mode"`
}

type InstantiationShiftRef struct {
	DayOfWeek  int     `json:"day_of_week"`
	StartTime  string  `json:"start_time"`
	EndTime    string  `json:"end_time"`
	PositionID *string `json:"position_id"`
}

type InstantiationConflict struct {
	TargetWeekStart string                `json:"target_week_start"`
	Day             string                `json:"day"`
	TemplateShift   InstantiationShiftRef `json:"template_shift"`
	ExistingShiftID *string               `json:"existing_shift_id"`
	EmployeeID      string                `json:"employee_id"`
	EmployeeName    string                `json:"employee_name"`
	Reason          ConflictReason        `json:"reason"`
}

type InstantiationPreview struct {
	TargetWeekStarts       []string                `json:"target_week_starts"`
	ToCreateCount          int                     `json:"to_create_count"`
	Conflicts              []InstantiationConflict `json:"conflicts"`
	ImpactedEmployeeCount  int                     `json:"impacted_employee_count"`
	AutoUnassignedCount    int                     `json:"auto_unassigned_count"`
	IdempotentSkippedCount int                     `json:"idempotent_skipped_count"`
}

type InstantiationPerWeekResult struct {
	TargetWeekStart string `json:"target_week_start"`
	WeekID          string `json:"week_id"`
	CreatedCount    int    `json:"created_count"`
	AssignedCount   int    `json:"assigned_count"`
	UnassignedCount int    `json:"unassigned_count"`
	ReplacedCount   int    `json:"replaced_count"`
	SkippedCount    int    `json:"skipped_count"`
}

type InstantiationResult struct {
	CreatedCount    int                          `json:"created_count"`
	AssignedCount   int                          `json:"assigned_count"`
	UnassignedCount int                          `json:"unassigned_count"`
	ReplacedCount   int                          `json:"replaced_count"`
	SkippedCount    int                          `json:"skipped_count"`
	PerWeek         []InstantiationPerWeekResult `json:"per_week"`
}

type WeekTemplateShiftInput struct {
	DayOfWeek    *int    `json:"day_of_week"`
	EmployeeID   *string `json:"employee_id"`
	PositionID   *string `json:"position_id"`
	Title        *string `json:"title"`
	StartTime    *string `json:"start_time"`
	EndTime      *string `json:"end_time"`
	BreakMinutes *int    `json:"break_minutes"`
	Location     *string `json:"location"`
	Notes        *string `json:"notes"`
}

type WeekTemplateShiftInputsField struct {
	Present bool
	Null    bool
	Value   []WeekTemplateShiftInput
}

func (f *WeekTemplateShiftInputsField) UnmarshalJSON(data []byte) error {
	f.Present = true
	if string(data) == "null" {
		f.Null = true
		f.Value = nil
		return nil
	}

	f.Null = false
	var value []WeekTemplateShiftInput
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	f.Value = value
	return nil
}

func (f WeekTemplateShiftInputsField) IsZero() bool {
	return !f.Present
}

type NullableStringPatchField struct {
	Present bool
	Value   *string
}

func (m ConflictMode) IsValid() bool {
	switch m {
	case ConflictModeKeepExisting, ConflictModeReplace, ConflictModeTemplateToUnassigned:
		return true
	default:
		return false
	}
}

func (f *NullableStringPatchField) UnmarshalJSON(data []byte) error {
	f.Present = true
	if string(data) == "null" {
		f.Value = nil
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	f.Value = &value
	return nil
}

func (f NullableStringPatchField) IsZero() bool {
	return !f.Present
}
