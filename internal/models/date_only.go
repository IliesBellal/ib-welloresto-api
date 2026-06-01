package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const dateOnlyLayout = "2006-01-02"

type DateOnly struct {
	value time.Time
}

func NewDateOnly(value time.Time) DateOnly {
	year, month, day := value.Date()
	return DateOnly{value: time.Date(year, month, day, 0, 0, 0, 0, time.UTC)}
}

func ParseDateOnly(raw string) (DateOnly, error) {
	trimmed := strings.TrimSpace(raw)
	parsed, err := time.Parse(dateOnlyLayout, trimmed)
	if err != nil {
		return DateOnly{}, fmt.Errorf("invalid date format, expected YYYY-MM-DD: %w", err)
	}
	return NewDateOnly(parsed), nil
}

func (d DateOnly) String() string {
	if d.value.IsZero() {
		return ""
	}
	return d.value.Format(dateOnlyLayout)
}

func (d DateOnly) Time() time.Time {
	return d.value
}

func (d DateOnly) MarshalJSON() ([]byte, error) {
	if d.value.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(d.value.Format(dateOnlyLayout))
}

func (d *DateOnly) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	parsed, err := ParseDateOnly(value)
	if err != nil {
		return err
	}
	d.value = parsed.value
	return nil
}

// NullableDateOnlyPatchField distinguishes between an absent field,
// an explicit JSON null, and a concrete date value for PATCH payloads.
type NullableDateOnlyPatchField struct {
	Present bool
	Value   *DateOnly
}

func (f *NullableDateOnlyPatchField) UnmarshalJSON(data []byte) error {
	f.Present = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		f.Value = nil
		return nil
	}

	var value DateOnly
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	f.Value = &value
	return nil
}

func (f NullableDateOnlyPatchField) IsZero() bool {
	return !f.Present
}
