package schedule

import (
	"bytes"
	"encoding/json"
	"time"

	"welloresto-api/internal/models"
)

// NullableStringPatchField distinguishes between an absent field, an explicit
// JSON null, and a concrete string value for PATCH payloads.
type NullableStringPatchField struct {
	Present bool
	Value   *string
}

func (f *NullableStringPatchField) UnmarshalJSON(data []byte) error {
	f.Present = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
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

type PlanningWeek struct {
	ID          string     `json:"id"`
	MerchantID  string     `json:"merchant_id"`
	Label       *string    `json:"label,omitempty"`
	StartDate   time.Time  `json:"start_date"`
	EndDate     time.Time  `json:"end_date"`
	Status      string     `json:"status"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	Notes       *string    `json:"notes,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
}

func (w PlanningWeek) MarshalJSON() ([]byte, error) {
	type planningWeekJSON struct {
		ID          string     `json:"id"`
		MerchantID  string     `json:"merchant_id"`
		Label       *string    `json:"label,omitempty"`
		StartDate   string     `json:"start_date"`
		EndDate     string     `json:"end_date"`
		Status      string     `json:"status"`
		PublishedAt *time.Time `json:"published_at,omitempty"`
		Notes       *string    `json:"notes,omitempty"`
		CreatedAt   time.Time  `json:"created_at"`
		UpdatedAt   time.Time  `json:"updated_at"`
		DeletedAt   *time.Time `json:"deleted_at,omitempty"`
	}

	return json.Marshal(planningWeekJSON{
		ID:          w.ID,
		MerchantID:  w.MerchantID,
		Label:       w.Label,
		StartDate:   formatPlanningWeekDateOnly(w.StartDate),
		EndDate:     formatPlanningWeekDateOnly(w.EndDate),
		Status:      w.Status,
		PublishedAt: w.PublishedAt,
		Notes:       w.Notes,
		CreatedAt:   w.CreatedAt,
		UpdatedAt:   w.UpdatedAt,
		DeletedAt:   w.DeletedAt,
	})
}

func formatPlanningWeekDateOnly(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format("2006-01-02")
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
	ID           string          `json:"id"`
	MerchantID   string          `json:"merchant_id"`
	WeekID       string          `json:"week_id"`
	EmployeeID   *string         `json:"employee_id"`
	PositionID   *string         `json:"position_id"`
	Title        string          `json:"title"`
	ShiftDate    models.DateOnly `json:"shift_date"`
	StartTime    string          `json:"start_time"`
	EndTime      string          `json:"end_time"`
	BreakMinutes int             `json:"break_minutes"`
	Position     *string         `json:"position"`
	Location     *string         `json:"location"`
	Notes        *string         `json:"notes"`
	Status       string          `json:"status"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	DeletedAt    *time.Time      `json:"deleted_at,omitempty"`
}

// PlanningShiftTeamWeekView is used by employee self team-week reads.
// It preserves base shift fields and adds lightweight display metadata.
type PlanningShiftTeamWeekView struct {
	ID            string          `json:"id"`
	MerchantID    string          `json:"merchant_id"`
	WeekID        string          `json:"week_id"`
	EmployeeID    *string         `json:"employee_id"`
	EmployeeName  *string         `json:"employee_name"`
	PositionID    *string         `json:"position_id"`
	Position      *string         `json:"position"`
	PositionColor *string         `json:"position_color"`
	Title         string          `json:"title"`
	ShiftDate     models.DateOnly `json:"shift_date"`
	StartTime     string          `json:"start_time"`
	EndTime       string          `json:"end_time"`
	BreakMinutes  int             `json:"break_minutes"`
	Location      *string         `json:"location"`
	Notes         *string         `json:"notes"`
	Status        string          `json:"status"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	DeletedAt     *time.Time      `json:"deleted_at,omitempty"`
}

type PlanningShiftCreateRequest struct {
	EmployeeID   *string `json:"employee_id,omitempty"`
	PositionID   *string `json:"position_id,omitempty"`
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
	EmployeeID   NullableStringPatchField `json:"employee_id,omitempty"`
	PositionID   NullableStringPatchField `json:"position_id,omitempty"`
	Title        *string                  `json:"title,omitempty"`
	ShiftDate    *string                  `json:"shift_date,omitempty"`
	StartTime    *string                  `json:"start_time,omitempty"`
	EndTime      *string                  `json:"end_time,omitempty"`
	BreakMinutes *int                     `json:"break_minutes,omitempty"`
	Position     *string                  `json:"position,omitempty"`
	Location     *string                  `json:"location,omitempty"`
	Notes        *string                  `json:"notes,omitempty"`
	Status       *string                  `json:"status,omitempty"`
}
