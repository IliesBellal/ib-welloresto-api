package settings

import (
	"database/sql"
	"fmt"
	"testing"
	"time"
)

func TestScanPlanningHolidayRowAcceptsByteDate(t *testing.T) {
	row := stubPlanningHolidayRow{values: []any{
		nil,
		[]byte("2026-06-01"),
		"Fete test",
		true,
		true,
		nil,
	}}

	item, err := scanPlanningHolidayRow(row)
	if err != nil {
		t.Fatalf("scanPlanningHolidayRow() error = %v", err)
	}

	if got, want := item.Date, time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("scanPlanningHolidayRow() date = %v, want %v", got, want)
	}
	if item.Label == nil || *item.Label != "Fete test" {
		t.Fatalf("scanPlanningHolidayRow() label = %v, want Fete test", item.Label)
	}
	if !item.IsLegalHoliday {
		t.Fatal("scanPlanningHolidayRow() IsLegalHoliday = false, want true")
	}
	if !item.CountAsHoliday {
		t.Fatal("scanPlanningHolidayRow() CountAsHoliday = false, want true")
	}
}

func TestScanPlanningHolidayOverrideRowAcceptsByteDate(t *testing.T) {
	createdAt := time.Date(2026, time.June, 1, 10, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, time.June, 1, 11, 0, 0, 0, time.UTC)
	row := stubPlanningHolidayRow{values: []any{
		"override_123",
		"merchant_123",
		[]byte("2026-06-01"),
		"Ouvert exceptionnellement",
		true,
		false,
		createdAt,
		updatedAt,
		nil,
	}}

	item, err := scanPlanningHolidayOverrideRow(row)
	if err != nil {
		t.Fatalf("scanPlanningHolidayOverrideRow() error = %v", err)
	}

	if got, want := item.HolidayDate, time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("scanPlanningHolidayOverrideRow() holiday_date = %v, want %v", got, want)
	}
	if item.Label == nil || *item.Label != "Ouvert exceptionnellement" {
		t.Fatalf("scanPlanningHolidayOverrideRow() label = %v, want Ouvert exceptionnellement", item.Label)
	}
	if item.IsOpen == nil || !*item.IsOpen {
		t.Fatalf("scanPlanningHolidayOverrideRow() is_open = %v, want true", item.IsOpen)
	}
	if item.CountAsHoliday == nil || *item.CountAsHoliday {
		t.Fatalf("scanPlanningHolidayOverrideRow() count_as_holiday = %v, want false", item.CountAsHoliday)
	}
	if !item.CreatedAt.Equal(createdAt) {
		t.Fatalf("scanPlanningHolidayOverrideRow() created_at = %v, want %v", item.CreatedAt, createdAt)
	}
	if !item.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("scanPlanningHolidayOverrideRow() updated_at = %v, want %v", item.UpdatedAt, updatedAt)
	}
}

type stubPlanningHolidayRow struct {
	values []any
	err    error
}

func (s stubPlanningHolidayRow) Scan(dest ...any) error {
	if s.err != nil {
		return s.err
	}
	if len(dest) != len(s.values) {
		return fmt.Errorf("scan destination count = %d, want %d", len(dest), len(s.values))
	}
	for i := range dest {
		if err := assignStubScanValue(dest[i], s.values[i]); err != nil {
			return fmt.Errorf("scan destination %d: %w", i, err)
		}
	}
	return nil
}

func assignStubScanValue(dest any, value any) error {
	switch d := dest.(type) {
	case *any:
		*d = value
		return nil
	case *string:
		switch v := value.(type) {
		case string:
			*d = v
			return nil
		case []byte:
			*d = string(v)
			return nil
		default:
			return fmt.Errorf("unsupported string source %T", value)
		}
	case *bool:
		v, ok := value.(bool)
		if !ok {
			return fmt.Errorf("unsupported bool source %T", value)
		}
		*d = v
		return nil
	case *time.Time:
		v, ok := value.(time.Time)
		if !ok {
			return fmt.Errorf("unsupported time source %T", value)
		}
		*d = v
		return nil
	case *sql.NullString:
		if value == nil {
			*d = sql.NullString{}
			return nil
		}
		switch v := value.(type) {
		case string:
			*d = sql.NullString{String: v, Valid: true}
			return nil
		case []byte:
			*d = sql.NullString{String: string(v), Valid: true}
			return nil
		default:
			return fmt.Errorf("unsupported NullString source %T", value)
		}
	case *sql.NullBool:
		if value == nil {
			*d = sql.NullBool{}
			return nil
		}
		v, ok := value.(bool)
		if !ok {
			return fmt.Errorf("unsupported NullBool source %T", value)
		}
		*d = sql.NullBool{Bool: v, Valid: true}
		return nil
	case *sql.NullTime:
		if value == nil {
			*d = sql.NullTime{}
			return nil
		}
		v, ok := value.(time.Time)
		if !ok {
			return fmt.Errorf("unsupported NullTime source %T", value)
		}
		*d = sql.NullTime{Time: v, Valid: true}
		return nil
	default:
		return fmt.Errorf("unsupported destination %T", dest)
	}
}
