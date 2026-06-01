package schedule

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPlanningWeekMarshalJSONDateOnly(t *testing.T) {
	week := PlanningWeek{
		ID:         "wk-1",
		MerchantID: "m-1",
		StartDate:  time.Date(2026, 6, 1, 15, 30, 0, 0, time.FixedZone("UTC+2", 2*3600)),
		EndDate:    time.Date(2026, 6, 7, 22, 0, 0, 0, time.FixedZone("UTC+2", 2*3600)),
		Status:     "draft",
		CreatedAt:  time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC),
	}

	payload, err := json.Marshal(week)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	body := string(payload)
	if !strings.Contains(body, `"start_date":"2026-06-01"`) {
		t.Fatalf("expected start_date in YYYY-MM-DD, got %s", body)
	}
	if !strings.Contains(body, `"end_date":"2026-06-07"`) {
		t.Fatalf("expected end_date in YYYY-MM-DD, got %s", body)
	}
	if strings.Contains(body, "T15:30:00") || strings.Contains(body, "T22:00:00") {
		t.Fatalf("expected no ISO datetime for week dates, got %s", body)
	}
}
