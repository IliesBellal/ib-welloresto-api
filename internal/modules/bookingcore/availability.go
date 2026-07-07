package bookingcore

import "time"

type IntervalBooking struct {
	PartySize int
	StartDate string
	EndDate   *string
}

type SlotRange struct {
	ID              int
	HourFrom        string
	HourTo          string
	BookingCapacity int
}

type SlotParams struct {
	RequestedDate            string
	SlotIntervalMinutes      int
	DefaultDurationMinutes   int
	LastBookingOffsetMinutes int
}

type ComputedSlot struct {
	HourOfOperationID      int
	DateFrom               string
	DateTo                 string
	Available              bool
	Capacity               int
	RemainingCapacity      int
	DebugCapacity          int
	DebugMaxBookedInWindow int
	DebugRemainingCapacity int
}

func NormalizeRequestedDate(t time.Time) string {
	return t.Format("2006-01-02")
}

func BuildOccupationByInterval(bookings []IntervalBooking, interval int, fallbackDurationMin int) map[string]int {
	occ := make(map[string]int)

	for _, b := range bookings {
		start, _ := time.Parse("2006-01-02 15:04:05", b.StartDate)
		end := start

		if b.EndDate != nil {
			end, _ = time.Parse("2006-01-02 15:04:05", *b.EndDate)
		} else {
			end = start.Add(time.Duration(fallbackDurationMin) * time.Minute)
		}

		cur := start
		for cur.Before(end) {
			key := cur.Format("15:04:05")
			occ[key] += b.PartySize
			cur = cur.Add(time.Duration(interval) * time.Minute)
		}
	}

	return occ
}

func ComputeSlots(params SlotParams, ranges []SlotRange, occupation map[string]int, now time.Time) []ComputedSlot {
	slots := []ComputedSlot{}
	nowStr := now.Format("2006-01-02")
	nowTime := now.Format("15:04:05")

	for _, tr := range ranges {
		start, _ := time.Parse("2006-01-02 15:04:05", params.RequestedDate+" "+tr.HourFrom)
		endOfService, _ := time.Parse("2006-01-02 15:04:05", params.RequestedDate+" "+tr.HourTo)
		last := endOfService.Add(-time.Duration(params.LastBookingOffsetMinutes) * time.Minute)

		for !start.After(last) {
			available := true
			maxOcc := 0

			if params.RequestedDate == nowStr && start.Format("15:04:05") < nowTime {
				available = false
			}

			newStart := start
			newEnd := start.Add(time.Duration(params.DefaultDurationMinutes) * time.Minute)

			if newEnd.After(endOfService) {
				available = false
			}

			cur := newStart
			for cur.Before(newEnd) {
				occ := occupation[cur.Format("15:04:05")]
				if occ > maxOcc {
					maxOcc = occ
				}
				cur = cur.Add(time.Duration(params.SlotIntervalMinutes) * time.Minute)
			}

			remaining := tr.BookingCapacity - maxOcc

			slot := ComputedSlot{
				HourOfOperationID:      tr.ID,
				DateFrom:               start.Format("2006-01-02 15:04:05"),
				DateTo:                 start.Add(time.Duration(params.SlotIntervalMinutes) * time.Minute).Format("2006-01-02 15:04:05"),
				Available:              available,
				Capacity:               tr.BookingCapacity,
				RemainingCapacity:      remaining,
				DebugCapacity:          tr.BookingCapacity,
				DebugMaxBookedInWindow: maxOcc,
				DebugRemainingCapacity: remaining,
			}

			slots = append(slots, slot)
			start = start.Add(time.Duration(params.SlotIntervalMinutes) * time.Minute)
		}
	}

	return slots
}
