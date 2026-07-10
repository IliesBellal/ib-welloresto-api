package bookingcore

import (
	"testing"
	"time"
)

func findSlotByDateFrom(slots []ComputedSlot, dateFrom string) *ComputedSlot {
	for i := range slots {
		if slots[i].DateFrom == dateFrom {
			return &slots[i]
		}
	}
	return nil
}

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

func TestConvertComputedSlotsFromUTC_ParisScenario(t *testing.T) {
	settings := DefaultBookingSettings()
	settings.SlotIntervalMinutes = 15

	start := "2026-07-16T03:00:00Z"
	end := "2026-07-16T04:30:00Z"

	occupation := BuildOccupationByInterval(
		[]IntervalBooking{{PartySize: 4, StartDate: start, EndDate: &end}},
		settings.SlotIntervalMinutes,
		settings,
		nil,
	)

	utcSlots := ComputeSlots(
		SlotParams{
			RequestedDate:   "2026-07-16",
			PartySize:       1,
			BookingSettings: settings,
			DurationRules:   []DurationRule{{MinPartySize: 1, MaxPartySize: 10, DurationMinutes: 90, Enabled: true}},
		},
		[]SlotRange{{ID: 1, HourFrom: "01:45:00", HourTo: "08:00:00", BookingCapacity: 6}},
		occupation,
		time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC),
	)

	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("failed to load timezone: %v", err)
	}

	parisSlots := ConvertComputedSlotsFromUTC(utcSlots, paris)

	for _, slot := range parisSlots {
		if slot.RemainingCapacity < slot.Capacity {
			if slot.DateFrom < "2026-07-16 03:45:00" || slot.DateFrom > "2026-07-16 06:15:00" {
				t.Fatalf("unexpected impacted slot outside expected Paris range: %s", slot.DateFrom)
			}
		}
	}

	impactedAtFive := findSlotByDateFrom(parisSlots, "2026-07-16 05:00:00")
	if impactedAtFive == nil {
		t.Fatal("expected slot at 2026-07-16 05:00:00")
	}
	if impactedAtFive.RemainingCapacity >= impactedAtFive.Capacity {
		t.Fatalf("expected slot 05:00 to be impacted, got remaining=%d capacity=%d", impactedAtFive.RemainingCapacity, impactedAtFive.Capacity)
	}

	notImpactedAtSixThirty := findSlotByDateFrom(parisSlots, "2026-07-16 06:30:00")
	if notImpactedAtSixThirty == nil {
		t.Fatal("expected slot at 2026-07-16 06:30:00")
	}
	if notImpactedAtSixThirty.RemainingCapacity != notImpactedAtSixThirty.Capacity {
		t.Fatalf("expected slot 06:30 to be unaffected, got remaining=%d capacity=%d", notImpactedAtSixThirty.RemainingCapacity, notImpactedAtSixThirty.Capacity)
	}
}
