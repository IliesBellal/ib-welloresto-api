package leave

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPlanningLeaveRequestMarshalJSONDateOnly(t *testing.T) {
	item := PlanningLeaveRequest{
		ID:         "lr-1",
		MerchantID: "m-1",
		EmployeeID: "emp-1",
		LeaveType:  "paid",
		StartDate:  time.Date(2026, 6, 5, 9, 30, 0, 0, time.FixedZone("UTC+2", 2*3600)),
		EndDate:    time.Date(2026, 6, 10, 18, 0, 0, 0, time.FixedZone("UTC+2", 2*3600)),
		Status:     "approved",
		CreatedAt:  time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC),
	}

	payload, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	body := string(payload)
	if !strings.Contains(body, `"start_date":"2026-06-05"`) {
		t.Fatalf("expected start_date in YYYY-MM-DD, got %s", body)
	}
	if !strings.Contains(body, `"end_date":"2026-06-10"`) {
		t.Fatalf("expected end_date in YYYY-MM-DD, got %s", body)
	}
	if strings.Contains(body, "T09:30:00") || strings.Contains(body, "T18:00:00") {
		t.Fatalf("expected no ISO datetime for leave dates, got %s", body)
	}
}
