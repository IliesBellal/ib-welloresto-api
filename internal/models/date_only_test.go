package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDateOnlyJSONRoundTrip(t *testing.T) {
	var value DateOnly
	if err := json.Unmarshal([]byte(`"2026-06-01"`), &value); err != nil {
		t.Fatalf("unmarshal error = %v", err)
	}
	if value.String() != "2026-06-01" {
		t.Fatalf("string value = %q, want 2026-06-01", value.String())
	}

	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal error = %v", err)
	}
	if string(payload) != `"2026-06-01"` {
		t.Fatalf("marshal payload = %s, want \"2026-06-01\"", string(payload))
	}
}

func TestNullableDateOnlyPatchFieldAbsentAndNull(t *testing.T) {
	type req struct {
		ContractStartDate NullableDateOnlyPatchField `json:"contract_start_date,omitempty"`
	}

	var absent req
	if err := json.Unmarshal([]byte(`{}`), &absent); err != nil {
		t.Fatalf("unmarshal absent error = %v", err)
	}
	if absent.ContractStartDate.Present {
		t.Fatalf("expected absent field to be not present")
	}

	var nullValue req
	if err := json.Unmarshal([]byte(`{"contract_start_date":null}`), &nullValue); err != nil {
		t.Fatalf("unmarshal null error = %v", err)
	}
	if !nullValue.ContractStartDate.Present {
		t.Fatalf("expected null field to be present")
	}
	if nullValue.ContractStartDate.Value != nil {
		t.Fatalf("expected null field value to be nil")
	}

	var dateValue req
	if err := json.Unmarshal([]byte(`{"contract_start_date":"2026-06-01"}`), &dateValue); err != nil {
		t.Fatalf("unmarshal date error = %v", err)
	}
	if !dateValue.ContractStartDate.Present {
		t.Fatalf("expected date field to be present")
	}
	if dateValue.ContractStartDate.Value == nil || dateValue.ContractStartDate.Value.String() != "2026-06-01" {
		t.Fatalf("expected parsed date value 2026-06-01, got %#v", dateValue.ContractStartDate.Value)
	}
}

func TestNewDateOnlyNormalizesDayWithoutTimezoneShift(t *testing.T) {
	loc := time.FixedZone("UTC+2", 2*3600)
	input := time.Date(2026, 6, 1, 23, 45, 0, 0, loc)
	value := NewDateOnly(input)
	if value.String() != "2026-06-01" {
		t.Fatalf("normalized date = %s, want 2026-06-01", value.String())
	}
}
