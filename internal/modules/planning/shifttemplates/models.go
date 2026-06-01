package shifttemplates

import (
	"bytes"
	"encoding/json"
	"time"
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

type ShiftTemplate struct {
	ID           string    `json:"id"`
	Label        string    `json:"label"`
	StartTime    string    `json:"start_time"`
	EndTime      string    `json:"end_time"`
	BreakMinutes int       `json:"break_minutes"`
	PositionID   *string   `json:"position_id"`
	Color        string    `json:"color"`
	SortOrder    int       `json:"sort_order"`
	Active       bool      `json:"active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type ShiftTemplateCreateRequest struct {
	Label        string  `json:"label"`
	StartTime    string  `json:"start_time"`
	EndTime      string  `json:"end_time"`
	BreakMinutes *int    `json:"break_minutes"`
	PositionID   *string `json:"position_id"`
	Color        string  `json:"color"`
	SortOrder    *int    `json:"sort_order,omitempty"`
	Active       *bool   `json:"active,omitempty"`
}

type ShiftTemplateUpdateRequest struct {
	Label        *string                  `json:"label,omitempty"`
	StartTime    *string                  `json:"start_time,omitempty"`
	EndTime      *string                  `json:"end_time,omitempty"`
	BreakMinutes *int                     `json:"break_minutes,omitempty"`
	PositionID   NullableStringPatchField `json:"position_id,omitempty"`
	Color        *string                  `json:"color,omitempty"`
	SortOrder    *int                     `json:"sort_order,omitempty"`
	Active       *bool                    `json:"active,omitempty"`
}
