package demorequest

import (
	"testing"
	"time"
)

func mustParisTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02 15:04", value, parisLocation())
	if err != nil {
		t.Fatalf("mustParisTime(%q): %v", value, err)
	}
	return parsed
}

func TestGenerateCandidateSlots_ExcludesSunday(t *testing.T) {
	// 2026-09-21 is a Monday — the window covers the following Sunday
	// (2026-09-27) within slotLookaheadDays.
	now := mustParisTime(t, "2026-09-21 08:00")
	for _, slot := range generateCandidateSlots(now) {
		if slot.Weekday() == time.Sunday {
			t.Fatalf("generateCandidateSlots returned a Sunday slot: %v", slot)
		}
	}
}

func TestGenerateCandidateSlots_RespectsLeadTime(t *testing.T) {
	// 09:30 on a Monday, mid-window — anything starting before 09:30+3h
	// (12:30) must not appear.
	now := mustParisTime(t, "2026-09-21 09:30")
	earliestAllowed := now.Add(slotLeadTime)
	for _, slot := range generateCandidateSlots(now) {
		if slot.Before(earliestAllowed) {
			t.Fatalf("slot %v starts before the lead time cutoff %v", slot, earliestAllowed)
		}
	}
}

func TestGenerateCandidateSlots_StaysWithinWindows(t *testing.T) {
	now := mustParisTime(t, "2026-09-21 08:00")
	for _, slot := range generateCandidateSlots(now) {
		inWindow := false
		for _, w := range slotWindows {
			start := time.Date(slot.Year(), slot.Month(), slot.Day(), w.StartHour, w.StartMinute, 0, 0, slot.Location())
			end := time.Date(slot.Year(), slot.Month(), slot.Day(), w.EndHour, w.EndMinute, 0, 0, slot.Location())
			if !slot.Before(start) && slot.Add(time.Duration(slotDurationMinutes)*time.Minute).Compare(end) <= 0 {
				inWindow = true
				break
			}
		}
		if !inWindow {
			t.Fatalf("slot %v falls outside every configured window", slot)
		}
	}
}

func TestIsCandidateSlot(t *testing.T) {
	now := mustParisTime(t, "2026-09-21 08:00")
	candidates := generateCandidateSlots(now)
	if len(candidates) == 0 {
		t.Fatal("expected at least one candidate slot to test against")
	}

	if !isCandidateSlot(candidates[0], now) {
		t.Fatalf("expected %v to be a valid candidate slot", candidates[0])
	}

	arbitrary := mustParisTime(t, "2026-09-21 03:00") // outside any window
	if isCandidateSlot(arbitrary, now) {
		t.Fatalf("expected %v (outside any window) to be rejected", arbitrary)
	}
}
