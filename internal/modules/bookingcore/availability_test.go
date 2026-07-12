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

func mustClockToUTC(t *testing.T, date string, clock string, loc *time.Location) time.Time {
	t.Helper()

	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", date+" "+clock, loc)
	if err != nil {
		t.Fatalf("failed to parse local clock %s %s: %v", date, clock, err)
	}

	return parsed.UTC()
}

func slotRangeForLocalDay(t *testing.T, date string, loc *time.Location, hourFrom string, hourTo string, capacity int) SlotRange {
	t.Helper()

	return SlotRange{
		ID:              1,
		StartUTC:        mustClockToUTC(t, date, hourFrom, loc),
		EndUTC:          mustClockToUTC(t, date, hourTo, loc),
		BookingCapacity: capacity,
	}
}

func firstImpactedSlot(slots []ComputedSlot) *ComputedSlot {
	for i := range slots {
		if slots[i].RemainingCapacity < slots[i].Capacity {
			return &slots[i]
		}
	}
	return nil
}

func timePart(dateTime string) string {
	if len(dateTime) >= 19 {
		return dateTime[11:19]
	}
	return dateTime
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

	first := mustClockToUTC(t, "2026-07-08", "12:30:00", time.UTC)
	last := mustClockToUTC(t, "2026-07-08", "13:30:00", time.UTC)
	slots := ComputeSlots(
		SlotParams{
			RequestedDate:   "2026-07-08",
			PartySize:       4,
			BookingSettings: settings,
			DurationRules:   []DurationRule{{MinPartySize: 1, MaxPartySize: 8, DurationMinutes: 90, Enabled: true}},
		},
		[]SlotRange{{ID: 1, StartUTC: mustClockToUTC(t, "2026-07-08", "12:00:00", time.UTC), EndUTC: mustClockToUTC(t, "2026-07-08", "15:00:00", time.UTC), BookingCapacity: 40, FirstBookingTimeUTC: &first, LastBookingTimeUTC: &last}},
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
		[]SlotRange{{ID: 1, StartUTC: mustClockToUTC(t, "2026-07-08", "12:00:00", time.UTC), EndUTC: mustClockToUTC(t, "2026-07-08", "15:00:00", time.UTC), BookingCapacity: 20}},
		map[string]int{},
		time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC),
	)

	if len(slots) != 0 {
		t.Fatalf("expected no slots, got %d", len(slots))
	}
}

func TestAvailabilityUTCInternal_ParisCESTFullDayFromMidnight(t *testing.T) {
	settings := DefaultBookingSettings()
	settings.SlotIntervalMinutes = 1
	settings.DefaultBookingDuration = 15
	settings.LastBookingOffsetMinutes = 1

	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("failed to load timezone: %v", err)
	}

	requestedDateLocal := "2026-07-15"
	utcSlots := ComputeSlots(
		SlotParams{
			RequestedDate:   requestedDateLocal,
			PartySize:       1,
			BookingSettings: settings,
		},
		[]SlotRange{slotRangeForLocalDay(t, requestedDateLocal, paris, "00:00:00", "23:59:00", 10)},
		map[string]int{},
		time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC),
	)

	parisSlots := ConvertComputedSlotsFromUTC(utcSlots, paris)
	if len(parisSlots) == 0 {
		t.Fatal("expected slots for full-day range")
	}
	if parisSlots[0].DateFrom != "2026-07-15 00:00:00" {
		t.Fatalf("expected first local slot at midnight, got %s", parisSlots[0].DateFrom)
	}
	last := parisSlots[len(parisSlots)-1]
	if last.DateTo != "2026-07-15 23:59:00" {
		t.Fatalf("expected slots to cover local day end, got last DateTo=%s", last.DateTo)
	}
}

func TestAvailabilityUTCInternal_ParisCESTWindow(t *testing.T) {
	settings := DefaultBookingSettings()
	settings.SlotIntervalMinutes = 15
	settings.DefaultBookingDuration = 90

	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("failed to load timezone: %v", err)
	}

	requestedDateLocal := "2026-07-16"
	bookingStartUTC := "2026-07-16T03:00:00Z"
	bookingEndUTC := "2026-07-16T04:30:00Z"

	occupation := BuildOccupationByInterval(
		[]IntervalBooking{{PartySize: 4, StartDate: bookingStartUTC, EndDate: &bookingEndUTC}},
		settings.SlotIntervalMinutes,
		settings,
		nil,
	)

	utcSlots := ComputeSlots(
		SlotParams{
			RequestedDate:   requestedDateLocal,
			PartySize:       1,
			BookingSettings: settings,
			DurationRules:   []DurationRule{{MinPartySize: 1, MaxPartySize: 10, DurationMinutes: 90, Enabled: true}},
		},
		[]SlotRange{slotRangeForLocalDay(t, requestedDateLocal, paris, "04:00:00", "08:00:00", 6)},
		occupation,
		time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC),
	)

	parisSlots := ConvertComputedSlotsFromUTC(utcSlots, paris)
	firstImpacted := firstImpactedSlot(parisSlots)
	if firstImpacted == nil {
		t.Fatal("expected at least one impacted slot")
	}
	if got := timePart(firstImpacted.DateFrom); got != "05:00:00" {
		t.Fatalf("expected first impacted slot at 05:00:00, got %s", got)
	}

	for _, slot := range parisSlots {
		if slot.RemainingCapacity < slot.Capacity {
			timeFrom := timePart(slot.DateFrom)
			if timeFrom < "05:00:00" || timeFrom >= "06:30:00" {
				t.Fatalf("unexpected impacted slot outside [05:00:00, 06:30:00): %s", slot.DateFrom)
			}
		}
	}

	slotAtFive := findSlotByDateFrom(parisSlots, "2026-07-16 05:00:00")
	if slotAtFive == nil {
		t.Fatal("expected slot at 05:00:00")
	}
	if slotAtFive.RemainingCapacity >= slotAtFive.Capacity {
		t.Fatalf("expected slot 05:00:00 to be impacted, got remaining=%d capacity=%d", slotAtFive.RemainingCapacity, slotAtFive.Capacity)
	}

	slotAtSixThirty := findSlotByDateFrom(parisSlots, "2026-07-16 06:30:00")
	if slotAtSixThirty == nil {
		t.Fatal("expected slot at 06:30:00")
	}
	if slotAtSixThirty.RemainingCapacity != slotAtSixThirty.Capacity {
		t.Fatalf("expected slot 06:30:00 to be unaffected, got remaining=%d capacity=%d", slotAtSixThirty.RemainingCapacity, slotAtSixThirty.Capacity)
	}
}

func TestAvailabilityUTCInternal_NewYorkESTWindow(t *testing.T) {
	settings := DefaultBookingSettings()
	settings.SlotIntervalMinutes = 15
	settings.DefaultBookingDuration = 90

	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("failed to load timezone: %v", err)
	}

	requestedDateLocal := "2026-01-16"
	bookingStartUTC := "2026-01-16T10:00:00Z"
	bookingEndUTC := "2026-01-16T11:30:00Z"

	occupation := BuildOccupationByInterval(
		[]IntervalBooking{{PartySize: 4, StartDate: bookingStartUTC, EndDate: &bookingEndUTC}},
		settings.SlotIntervalMinutes,
		settings,
		nil,
	)

	utcSlots := ComputeSlots(
		SlotParams{
			RequestedDate:   requestedDateLocal,
			PartySize:       1,
			BookingSettings: settings,
			DurationRules:   []DurationRule{{MinPartySize: 1, MaxPartySize: 10, DurationMinutes: 90, Enabled: true}},
		},
		[]SlotRange{slotRangeForLocalDay(t, requestedDateLocal, newYork, "04:00:00", "08:00:00", 6)},
		occupation,
		time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC),
	)

	nySlots := ConvertComputedSlotsFromUTC(utcSlots, newYork)
	firstImpacted := firstImpactedSlot(nySlots)
	if firstImpacted == nil {
		t.Fatal("expected at least one impacted slot")
	}
	if got := timePart(firstImpacted.DateFrom); got != "05:00:00" {
		t.Fatalf("expected first impacted slot at 05:00:00, got %s", got)
	}

	for _, slot := range nySlots {
		if slot.RemainingCapacity < slot.Capacity {
			timeFrom := timePart(slot.DateFrom)
			if timeFrom < "05:00:00" || timeFrom >= "06:30:00" {
				t.Fatalf("unexpected impacted slot outside [05:00:00, 06:30:00): %s", slot.DateFrom)
			}
		}
	}

	slotAtFive := findSlotByDateFrom(nySlots, "2026-01-16 05:00:00")
	if slotAtFive == nil {
		t.Fatal("expected slot at 05:00:00")
	}
	if slotAtFive.RemainingCapacity >= slotAtFive.Capacity {
		t.Fatalf("expected slot 05:00:00 to be impacted, got remaining=%d capacity=%d", slotAtFive.RemainingCapacity, slotAtFive.Capacity)
	}

	slotAtSixThirty := findSlotByDateFrom(nySlots, "2026-01-16 06:30:00")
	if slotAtSixThirty == nil {
		t.Fatal("expected slot at 06:30:00")
	}
	if slotAtSixThirty.RemainingCapacity != slotAtSixThirty.Capacity {
		t.Fatalf("expected slot 06:30:00 to be unaffected, got remaining=%d capacity=%d", slotAtSixThirty.RemainingCapacity, slotAtSixThirty.Capacity)
	}
}

func TestAvailabilityUTCInternal_NewYorkESTBusinessHours(t *testing.T) {
	settings := DefaultBookingSettings()
	settings.SlotIntervalMinutes = 15
	settings.DefaultBookingDuration = 90

	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("failed to load timezone: %v", err)
	}

	requestedDateLocal := "2026-01-16"
	utcSlots := ComputeSlots(
		SlotParams{RequestedDate: requestedDateLocal, PartySize: 2, BookingSettings: settings},
		[]SlotRange{slotRangeForLocalDay(t, requestedDateLocal, newYork, "09:00:00", "22:00:00", 12)},
		map[string]int{},
		time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC),
	)

	localSlots := ConvertComputedSlotsFromUTC(utcSlots, newYork)
	if len(localSlots) == 0 {
		t.Fatal("expected slots for New York business hours")
	}
	if localSlots[0].DateFrom != "2026-01-16 09:00:00" {
		t.Fatalf("expected first slot at 09:00 local, got %s", localSlots[0].DateFrom)
	}
}

func TestAvailabilityUTCInternal_SydneyEveningHours(t *testing.T) {
	settings := DefaultBookingSettings()
	settings.SlotIntervalMinutes = 15
	settings.DefaultBookingDuration = 60

	sydney, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		t.Fatalf("failed to load timezone: %v", err)
	}

	requestedDateLocal := "2026-07-15"
	utcSlots := ComputeSlots(
		SlotParams{RequestedDate: requestedDateLocal, PartySize: 2, BookingSettings: settings},
		[]SlotRange{slotRangeForLocalDay(t, requestedDateLocal, sydney, "18:00:00", "23:59:00", 10)},
		map[string]int{},
		time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC),
	)

	localSlots := ConvertComputedSlotsFromUTC(utcSlots, sydney)
	if len(localSlots) == 0 {
		t.Fatal("expected slots for Sydney evening range")
	}
	if localSlots[0].DateFrom != "2026-07-15 18:00:00" {
		t.Fatalf("expected first slot at 18:00 local, got %s", localSlots[0].DateFrom)
	}
}
