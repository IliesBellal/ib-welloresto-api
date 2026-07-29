package haccp

import (
	"testing"
	"time"
)

func TestNormalizeTemperatureReadingsDate(t *testing.T) {
	// now = 2026-05-25 14:30:00 UTC  (= 16:30 Paris UTC+2)
	now := time.Date(2026, 5, 25, 14, 30, 0, 0, time.UTC)

	tests := []struct {
		name      string
		raw       string
		tz        string
		expected  string
		wantError bool
	}{
		// UTC fallback (empty tz) — behaviour unchanged
		{name: "empty date UTC", raw: "", tz: "", expected: "2026-05-25 00:00:00"},
		{name: "date only UTC", raw: "2026-05-24", tz: "", expected: "2026-05-24 00:00:00"},
		{name: "datetime input UTC", raw: "2026-05-24 09:12:00", tz: "", expected: "2026-05-24 00:00:00"},
		{name: "rfc3339 input UTC", raw: "2026-05-24T09:12:00Z", tz: "", expected: "2026-05-24 00:00:00"},
		{name: "invalid", raw: "24/05/2026", tz: "", wantError: true},

		// Europe/Paris (UTC+2 in summer)
		// midnight Paris 2026-05-25 = 2026-05-24 22:00:00 UTC
		{name: "date only Paris", raw: "2026-05-25", tz: "Europe/Paris", expected: "2026-05-24 22:00:00"},
		// now (14:30 UTC) = 16:30 Paris → today in Paris is still 2026-05-25 → same result
		{name: "empty date Paris defaults to local today", raw: "", tz: "Europe/Paris", expected: "2026-05-24 22:00:00"},
		// RFC3339 with offset: date extracted in Paris tz
		{name: "rfc3339 Paris offset", raw: "2026-05-25T01:32:00+02:00", tz: "Europe/Paris", expected: "2026-05-24 22:00:00"},

		// Invalid timezone → silently falls back to UTC
		{name: "invalid tz falls back to UTC", raw: "2026-05-24", tz: "Not/ATimezone", expected: "2026-05-24 00:00:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeTemperatureReadingsDate(tt.raw, now, tt.tz)
			if tt.wantError {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Fatalf("normalizeTemperatureReadingsDate(%q, %q) = %s, want %s", tt.raw, tt.tz, got, tt.expected)
			}
		})
	}
}

func TestNormalizeActivitiesDateRange(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	t.Run("from to range in merchant timezone", func(t *testing.T) {
		startAt, endAt, responseDate, responseFrom, responseTo, err := normalizeActivitiesDateRange(ActivitiesListParams{
			From: "2026-06-30",
			To:   "2026-07-29",
		}, now, "Europe/Paris")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedStart := time.Date(2026, 6, 29, 22, 0, 0, 0, time.UTC)
		expectedEnd := time.Date(2026, 7, 29, 22, 0, 0, 0, time.UTC)

		if !startAt.Equal(expectedStart) {
			t.Fatalf("startAt = %v, want %v", startAt, expectedStart)
		}
		if !endAt.Equal(expectedEnd) {
			t.Fatalf("endAt = %v, want %v", endAt, expectedEnd)
		}
		if responseDate != "" {
			t.Fatalf("responseDate = %q, want empty", responseDate)
		}
		if responseFrom != "2026-06-30" || responseTo != "2026-07-29" {
			t.Fatalf("range response = (%q, %q), want (2026-06-30, 2026-07-29)", responseFrom, responseTo)
		}
	})

	t.Run("only from means one day range", func(t *testing.T) {
		startAt, endAt, _, responseFrom, responseTo, err := normalizeActivitiesDateRange(ActivitiesListParams{
			From: "2026-07-10",
		}, now, "UTC")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedStart := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
		expectedEnd := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)

		if !startAt.Equal(expectedStart) {
			t.Fatalf("startAt = %v, want %v", startAt, expectedStart)
		}
		if !endAt.Equal(expectedEnd) {
			t.Fatalf("endAt = %v, want %v", endAt, expectedEnd)
		}
		if responseFrom != "2026-07-10" || responseTo != "2026-07-10" {
			t.Fatalf("range response = (%q, %q), want (2026-07-10, 2026-07-10)", responseFrom, responseTo)
		}
	})

	t.Run("date fallback remains supported", func(t *testing.T) {
		startAt, endAt, responseDate, responseFrom, responseTo, err := normalizeActivitiesDateRange(ActivitiesListParams{
			Date: "2026-07-12",
		}, now, "UTC")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedStart := time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)
		expectedEnd := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)

		if !startAt.Equal(expectedStart) {
			t.Fatalf("startAt = %v, want %v", startAt, expectedStart)
		}
		if !endAt.Equal(expectedEnd) {
			t.Fatalf("endAt = %v, want %v", endAt, expectedEnd)
		}
		if responseDate != "2026-07-12" {
			t.Fatalf("responseDate = %q, want 2026-07-12", responseDate)
		}
		if responseFrom != "" || responseTo != "" {
			t.Fatalf("expected empty from/to, got (%q, %q)", responseFrom, responseTo)
		}
	})

	t.Run("invalid range order", func(t *testing.T) {
		_, _, _, _, _, err := normalizeActivitiesDateRange(ActivitiesListParams{
			From: "2026-07-29",
			To:   "2026-06-30",
		}, now, "UTC")
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
	})
}
