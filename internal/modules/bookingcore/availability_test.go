package bookingcore

import (
	"testing"
	"time"
)

func TestResolveDurationMinutes_UsesMatchingRule(t *testing.T) {
	settings := DefaultBookingSettings()
	rules := []DurationRule{
		{MinPartySize: 1, MaxPartySize: 4, DurationMinutes: 90, Enabled: true},
		{MinPartySize: 5, MaxPartySize: 8, DurationMinutes: 120, Enabled: true},
	}

	if got := ResolveDurationMinutes(6, settings, rules); got != 120 {
		t.Fatalf("expected 120, got %d", got)
	}
}

func TestComputeSlots_AppliesOverbookingAndRangeTimes(t *testing.T) {
	settings := DefaultBookingSettings()
	settings.OverbookingPercent = 10
	settings.SlotIntervalMinutes = 15

	first := "12:30:00"
	last := "13:30:00"
	slots := ComputeSlots(
		SlotParams{
			RequestedDate:   "2026-07-08",
			PartySize:       4,
			BookingSettings: settings,
			DurationRules:   []DurationRule{{MinPartySize: 1, MaxPartySize: 8, DurationMinutes: 90, Enabled: true}},
		},
		[]SlotRange{{ID: 1, HourFrom: "12:00:00", HourTo: "15:00:00", BookingCapacity: 40, FirstBookingTime: &first, LastBookingTime: &last}},
		map[string]int{},
		time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC),
	)

	if len(slots) == 0 {
		t.Fatal("expected slots")
	}
	if slots[0].DateFrom != "2026-07-08 12:30:00" {
		t.Fatalf("expected first slot at 12:30, got %s", slots[0].DateFrom)
	}
	if slots[0].Capacity != 44 {
		t.Fatalf("expected overbooked capacity 44, got %d", slots[0].Capacity)
	}
	if slots[len(slots)-1].DateFrom != "2026-07-08 13:30:00" {
		t.Fatalf("expected last slot at 13:30, got %s", slots[len(slots)-1].DateFrom)
	}
}

func TestComputeSlots_RejectsPartySizeOutsideBounds(t *testing.T) {
	settings := DefaultBookingSettings()
	settings.ReserveMinimumPartySize = 2
	settings.ReserveMaximumPartySize = 6

	slots := ComputeSlots(
		SlotParams{RequestedDate: "2026-07-08", PartySize: 1, BookingSettings: settings},
		[]SlotRange{{ID: 1, HourFrom: "12:00:00", HourTo: "15:00:00", BookingCapacity: 20}},
		map[string]int{},
		time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC),
	)

	if len(slots) != 0 {
		t.Fatalf("expected no slots, got %d", len(slots))
	}
}
