package models

import (
	"bytes"
	"database/sql/driver"
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

func (d DateOnly) Before(other time.Time) bool {
	return d.value.Before(NewDateOnly(other).value)
}

func (d DateOnly) After(other time.Time) bool {
	return d.value.After(NewDateOnly(other).value)
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

func (d DateOnly) Value() (driver.Value, error) {
	if d.value.IsZero() {
		return nil, nil
	}
	return d.value.Format(dateOnlyLayout), nil
}

func (d *DateOnly) Scan(value any) error {
	switch typed := value.(type) {
	case nil:
		d.value = time.Time{}
		return nil
	case time.Time:
		d.value = NewDateOnly(typed).value
		return nil
	case []byte:
		parsed, err := ParseDateOnly(string(typed))
		if err != nil {
			return err
		}
		d.value = parsed.value
		return nil
	case string:
		parsed, err := ParseDateOnly(typed)
		if err != nil {
			return err
		}
		d.value = parsed.value
		return nil
	default:
		return fmt.Errorf("cannot scan %T into DateOnly", value)
	}
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
