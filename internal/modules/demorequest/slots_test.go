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

func TestGenerateGridSlots_IncludesUnbookableSlots(t *testing.T) {
	// La grille (2026-09-25) doit conserver les créneaux structurellement
	// non réservables (trop proches, ou marqués isSimulatedBusy) — c'est
	// generateCandidateSlots qui filtre, pas generateGridSlots : le tableau
	// du site vitrine a besoin de les voir pour les griser.
	now := mustParisTime(t, "2026-09-21 09:30")
	grid := generateGridSlots(now)
	candidates := generateCandidateSlots(now)
	if len(grid) <= len(candidates) {
		t.Fatalf("expected generateGridSlots (%d) to include more slots than generateCandidateSlots (%d)", len(grid), len(candidates))
	}

	earliest := now.Add(slotLeadTime)
	foundTooSoon := false
	for _, slot := range grid {
		if slot.Before(earliest) {
			foundTooSoon = true
			break
		}
	}
	if !foundTooSoon {
		t.Fatal("expected generateGridSlots to include at least one slot within the lead-time cutoff")
	}
}

func TestIsSlotBookable_MatchesGenerateCandidateSlots(t *testing.T) {
	now := mustParisTime(t, "2026-09-21 08:00")
	candidateSet := make(map[int64]bool)
	for _, slot := range generateCandidateSlots(now) {
		candidateSet[slot.Unix()] = true
	}

	for _, slot := range generateGridSlots(now) {
		if isSlotBookable(slot, now) != candidateSet[slot.Unix()] {
			t.Fatalf("isSlotBookable(%v) disagrees with generateCandidateSlots membership", slot)
		}
	}
}

func TestGenerateCandidateSlots_IncludesSunday(t *testing.T) {
	// 2026-09-24 (fondateur) : tous les jours sont ouverts, dimanche compris
	// — régression à surveiller si quelqu'un réintroduit l'exclusion.
	now := mustParisTime(t, "2026-09-21 08:00") // Monday
	found := false
	for _, slot := range generateCandidateSlots(now) {
		if slot.Weekday() == time.Sunday {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected generateCandidateSlots to include at least one Sunday slot")
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

func TestIsSimulatedBusy_Deterministic(t *testing.T) {
	slot := mustParisTime(t, "2026-09-21 09:00")
	first := isSimulatedBusy(slot)
	for i := 0; i < 5; i++ {
		if got := isSimulatedBusy(slot); got != first {
			t.Fatalf("isSimulatedBusy(%v) is not deterministic: got %v then %v", slot, first, got)
		}
	}
}

func TestIsSimulatedBusy_RoughlyMatchesConfiguredRatio(t *testing.T) {
	// Pas un test de qualité statistique du hash — juste un garde-fou pour
	// détecter une régression grossière (ex: ratio toujours 0% ou 100%) si
	// quelqu'un modifie isSimulatedBusy sans y penser.
	loc := parisLocation()
	start := time.Date(2026, time.September, 21, 8, 0, 0, 0, loc)
	total, busy := 0, 0
	for i := 0; i < 2000; i++ {
		slot := start.Add(time.Duration(i) * time.Duration(slotDurationMinutes) * time.Minute)
		total++
		if isSimulatedBusy(slot) {
			busy++
		}
	}
	ratio := float64(busy) / float64(total)
	if ratio < simulatedBusyRatio-0.1 || ratio > simulatedBusyRatio+0.1 {
		t.Fatalf("observed busy ratio %.2f is too far from configured simulatedBusyRatio %.2f", ratio, simulatedBusyRatio)
	}
}

func TestGenerateCandidateSlots_NeverIncludesSimulatedBusySlots(t *testing.T) {
	now := mustParisTime(t, "2026-09-21 08:00")
	for _, slot := range generateCandidateSlots(now) {
		if isSimulatedBusy(slot) {
			t.Fatalf("generateCandidateSlots returned %v, which isSimulatedBusy marks as busy", slot)
		}
	}
}
