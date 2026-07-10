package bookingcore

import "time"

type IntervalBooking struct {
	PartySize       int
	StartDate       string
	EndDate         *string
	DurationMinutes *int
}

type BookingSettings struct {
	DefaultBookingDuration        int
	AutoAcceptReserveBookings     bool
	ReserveMaximumPartySize       int
	ReserveMinimumPartySize       int
	FirstBookingOffsetMinutes     int
	LastBookingOffsetMinutes      int
	CancelBookingLimitOffsetHours int
	SlotIntervalMinutes           int
	CancelableByCustomer          bool
	Enabled                       bool
	OverbookingPercent            int
	MaxBookingHorizonDays         int
	PendingExpirationHours        int
}

type DurationRule struct {
	MinPartySize    int
	MaxPartySize    int
	DurationMinutes int
	Enabled         bool
}

type SlotRange struct {
	ID               int
	HourFrom         string
	HourTo           string
	BookingCapacity  int
	FirstBookingTime *string
	LastBookingTime  *string
}

type SlotParams struct {
	RequestedDate   string
	PartySize       int
	BookingSettings BookingSettings
	DurationRules   []DurationRule
}

type ComputedSlot struct {
	HourOfOperationID      int
	DateFrom               string
	DateTo                 string
	DurationMinutes        int
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

func DefaultBookingSettings() BookingSettings {
	return BookingSettings{
		DefaultBookingDuration:        90,
		ReserveMaximumPartySize:       8,
		ReserveMinimumPartySize:       1,
		LastBookingOffsetMinutes:      60,
		CancelBookingLimitOffsetHours: 48,
		SlotIntervalMinutes:           15,
		CancelableByCustomer:          true,
		Enabled:                       true,
		OverbookingPercent:            0,
		MaxBookingHorizonDays:         90,
		PendingExpirationHours:        24,
	}
}

func NormalizeBookingSettings(settings BookingSettings) BookingSettings {
	defaults := DefaultBookingSettings()

	if settings.DefaultBookingDuration <= 0 {
		settings.DefaultBookingDuration = defaults.DefaultBookingDuration
	}
	if settings.ReserveMaximumPartySize <= 0 {
		settings.ReserveMaximumPartySize = defaults.ReserveMaximumPartySize
	}
	if settings.ReserveMinimumPartySize <= 0 {
		settings.ReserveMinimumPartySize = defaults.ReserveMinimumPartySize
	}
	if settings.LastBookingOffsetMinutes <= 0 {
		settings.LastBookingOffsetMinutes = defaults.LastBookingOffsetMinutes
	}
	if settings.CancelBookingLimitOffsetHours <= 0 {
		settings.CancelBookingLimitOffsetHours = defaults.CancelBookingLimitOffsetHours
	}
	if settings.SlotIntervalMinutes <= 0 {
		settings.SlotIntervalMinutes = defaults.SlotIntervalMinutes
	}
	if settings.MaxBookingHorizonDays <= 0 {
		settings.MaxBookingHorizonDays = defaults.MaxBookingHorizonDays
	}
	if settings.PendingExpirationHours <= 0 {
		settings.PendingExpirationHours = defaults.PendingExpirationHours
	}
	if settings.OverbookingPercent < 0 {
		settings.OverbookingPercent = defaults.OverbookingPercent
	}

	return settings
}

func ResolveDurationMinutes(partySize int, settings BookingSettings, rules []DurationRule) int {
	settings = NormalizeBookingSettings(settings)

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if partySize >= rule.MinPartySize && partySize <= rule.MaxPartySize && rule.DurationMinutes > 0 {
			return rule.DurationMinutes
		}
	}

	return settings.DefaultBookingDuration
}

func BuildOccupationByInterval(bookings []IntervalBooking, interval int, settings BookingSettings, rules []DurationRule) map[string]int {
	settings = NormalizeBookingSettings(settings)
	occ := make(map[string]int)

	for _, b := range bookings {
		start, _ := time.Parse("2006-01-02T15:04:05Z", b.StartDate)
		end := start

		if b.EndDate != nil {
			end, _ = time.Parse("2006-01-02T15:04:05Z", *b.EndDate)
		} else if b.DurationMinutes != nil && *b.DurationMinutes > 0 {
			end = start.Add(time.Duration(*b.DurationMinutes) * time.Minute)
		} else {
			end = start.Add(time.Duration(ResolveDurationMinutes(b.PartySize, settings, rules)) * time.Minute)
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
	settings := NormalizeBookingSettings(params.BookingSettings)
	if params.PartySize < settings.ReserveMinimumPartySize || params.PartySize > settings.ReserveMaximumPartySize {
		return nil
	}

	durationMinutes := ResolveDurationMinutes(params.PartySize, settings, params.DurationRules)
	requestedDate, err := time.ParseInLocation("2006-01-02", params.RequestedDate, now.Location())
	if err != nil {
		return nil
	}

	requestedDayStart := time.Date(requestedDate.Year(), requestedDate.Month(), requestedDate.Day(), 0, 0, 0, 0, now.Location())
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if requestedDayStart.After(todayStart.AddDate(0, 0, settings.MaxBookingHorizonDays)) {
		return nil
	}

	minBookingNoticeBoundary := now.Add(60 * time.Minute)
	capacityMultiplier := 100 + settings.OverbookingPercent
	slots := []ComputedSlot{}
	nowStr := now.Format("2006-01-02")

	for _, tr := range ranges {
		start, _ := time.ParseInLocation("2006-01-02 15:04:05", params.RequestedDate+" "+tr.HourFrom, now.Location())
		endOfService, _ := time.ParseInLocation("2006-01-02 15:04:05", params.RequestedDate+" "+tr.HourTo, now.Location())

		if tr.FirstBookingTime != nil && *tr.FirstBookingTime != "" {
			firstBookingTime, err := time.ParseInLocation("2006-01-02 15:04:05", params.RequestedDate+" "+*tr.FirstBookingTime, now.Location())
			if err == nil && firstBookingTime.After(start) {
				start = firstBookingTime
			}
		}

		last := endOfService.Add(-time.Duration(settings.LastBookingOffsetMinutes) * time.Minute)
		if tr.LastBookingTime != nil && *tr.LastBookingTime != "" {
			lastBookingTime, err := time.ParseInLocation("2006-01-02 15:04:05", params.RequestedDate+" "+*tr.LastBookingTime, now.Location())
			if err == nil && lastBookingTime.Before(last) {
				last = lastBookingTime
			}
		}

		lastByDuration := endOfService.Add(-time.Duration(durationMinutes) * time.Minute)
		if lastByDuration.Before(last) {
			last = lastByDuration
		}

		for !start.After(last) {
			available := true
			maxOcc := 0

			if params.RequestedDate == nowStr && start.Before(minBookingNoticeBoundary) {
				available = false
			}

			newStart := start
			newEnd := start.Add(time.Duration(durationMinutes) * time.Minute)

			if newEnd.After(endOfService) {
				available = false
			}

			cur := newStart
			for cur.Before(newEnd) {
				occ := occupation[cur.Format("15:04:05")]
				if occ > maxOcc {
					maxOcc = occ
				}
				cur = cur.Add(time.Duration(settings.SlotIntervalMinutes) * time.Minute)
			}

			capacity := (tr.BookingCapacity * capacityMultiplier) / 100
			remaining := capacity - maxOcc
			if remaining < params.PartySize {
				available = false
			}

			slot := ComputedSlot{
				HourOfOperationID:      tr.ID,
				DateFrom:               start.Format("2006-01-02 15:04:05"),
				DateTo:                 newEnd.Format("2006-01-02 15:04:05"),
				DurationMinutes:        durationMinutes,
				Available:              available,
				Capacity:               capacity,
				RemainingCapacity:      remaining,
				DebugCapacity:          capacity,
				DebugMaxBookedInWindow: maxOcc,
				DebugRemainingCapacity: remaining,
			}

			slots = append(slots, slot)
			start = start.Add(time.Duration(settings.SlotIntervalMinutes) * time.Minute)
		}
	}

	return slots
}

func ConvertComputedSlotsFromUTC(slots []ComputedSlot, loc *time.Location) []ComputedSlot {
	if loc == nil {
		loc = time.UTC
	}

	converted := make([]ComputedSlot, 0, len(slots))
	for _, slot := range slots {
		updated := slot

		if from, err := time.ParseInLocation("2006-01-02 15:04:05", slot.DateFrom, time.UTC); err == nil {
			updated.DateFrom = from.In(loc).Format("2006-01-02 15:04:05")
		}

		if to, err := time.ParseInLocation("2006-01-02 15:04:05", slot.DateTo, time.UTC); err == nil {
			updated.DateTo = to.In(loc).Format("2006-01-02 15:04:05")
		}

		converted = append(converted, updated)
	}

	return converted
}
