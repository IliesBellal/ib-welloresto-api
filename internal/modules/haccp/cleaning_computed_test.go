package haccp

import (
	"testing"
	"time"
)

func TestComputeCleaningComputed(t *testing.T) {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)

	dayBefore := now.AddDate(0, 0, -1)
	twoDaysBefore := now.AddDate(0, 0, -2)
	weekBefore := now.AddDate(0, 0, -7)

	tests := []struct {
		name         string
		last         *time.Time
		unit         string
		count        int
		wantDueToday bool
		wantOverdue  bool
	}{
		{name: "no execution yet", last: nil, unit: "day", count: 1, wantDueToday: true, wantOverdue: false},
		{name: "daily due today", last: &dayBefore, unit: "day", count: 1, wantDueToday: true, wantOverdue: false},
		{name: "daily overdue", last: &twoDaysBefore, unit: "day", count: 1, wantDueToday: false, wantOverdue: true},
		{name: "weekly due today", last: &weekBefore, unit: "week", count: 1, wantDueToday: true, wantOverdue: false},
		{name: "monthly not due", last: &dayBefore, unit: "month", count: 1, wantDueToday: false, wantOverdue: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDue, gotOver := computeCleaningComputed(now, tt.last, tt.unit, tt.count)
			if gotDue != tt.wantDueToday || gotOver != tt.wantOverdue {
				t.Fatalf("computeCleaningComputed() = (%v,%v), expected (%v,%v)", gotDue, gotOver, tt.wantDueToday, tt.wantOverdue)
			}
		})
	}
}
