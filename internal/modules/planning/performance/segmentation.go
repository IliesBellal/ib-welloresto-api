package performance

import (
	"math"
	"time"

	sharedpkg "welloresto-api/internal/modules/planning/shared"
)

// PremiumSegments buckets a duration of worked/planned time by premium
// classification. Night x Sunday are mutually exclusive with Normal/Night/
// Sunday (Normal+Night+Sunday+NightSunday always sums to the gross duration
// segmented). HolidaySeconds is an independent, possibly-overlapping
// marginal counter: holiday combination with night/Sunday is deliberately
// not modeled yet (see "Reporté — Jours fériés HCR" in
// docs/PLANNING_DECISIONS.md).
type PremiumSegments struct {
	NormalSeconds      int64 `json:"normal_seconds"`
	NightSeconds       int64 `json:"night_seconds"`
	SundaySeconds      int64 `json:"sunday_seconds"`
	NightSundaySeconds int64 `json:"night_sunday_seconds"`
	HolidaySeconds     int64 `json:"holiday_seconds"`
}

func (s *PremiumSegments) add(other PremiumSegments) {
	s.NormalSeconds += other.NormalSeconds
	s.NightSeconds += other.NightSeconds
	s.SundaySeconds += other.SundaySeconds
	s.NightSundaySeconds += other.NightSundaySeconds
	s.HolidaySeconds += other.HolidaySeconds
}

// applyBreakProration reduces every bucket proportionally to its share of
// the gross (Normal+Night+Sunday+NightSunday) duration. Shifts only store a
// flat break_minutes with no timestamp, so there is no way to know *when*
// in the shift the break happened — proportional distribution is the
// neutral choice (validated with Ilies, 2026-07-30): it neither favors nor
// penalizes the employee based on an assumption we can't verify.
func (s PremiumSegments) applyBreakProration(breakSeconds int64) PremiumSegments {
	grossTotal := s.NormalSeconds + s.NightSeconds + s.SundaySeconds + s.NightSundaySeconds
	if breakSeconds <= 0 || grossTotal <= 0 {
		return s
	}
	if breakSeconds > grossTotal {
		breakSeconds = grossTotal
	}

	prorate := func(bucket int64) int64 {
		reduced := bucket - int64(math.Round(float64(bucket)/float64(grossTotal)*float64(breakSeconds)))
		if reduced < 0 {
			return 0
		}
		return reduced
	}

	out := s
	out.NormalSeconds = prorate(s.NormalSeconds)
	out.NightSeconds = prorate(s.NightSeconds)
	out.SundaySeconds = prorate(s.SundaySeconds)
	out.NightSundaySeconds = prorate(s.NightSundaySeconds)

	if s.HolidaySeconds > 0 {
		// Marginal/overlapping bucket: reduced by the same proportional
		// share, independently of the four buckets above.
		reduction := int64(math.Round(float64(s.HolidaySeconds) / float64(grossTotal) * float64(breakSeconds)))
		out.HolidaySeconds = s.HolidaySeconds - reduction
		if out.HolidaySeconds < 0 {
			out.HolidaySeconds = 0
		}
	}

	return out
}

// nightWindow holds the merchant's night-shift boundary as offsets since
// local midnight. start > end is the normal case (the window spans
// midnight, e.g. 22:00-06:00).
type nightWindow struct {
	start time.Duration
	end   time.Duration
}

func parseNightWindow(startRaw, endRaw string) (nightWindow, error) {
	start, err := sharedpkg.ParsePlanningTime(startRaw)
	if err != nil {
		return nightWindow{}, err
	}
	end, err := sharedpkg.ParsePlanningTime(endRaw)
	if err != nil {
		return nightWindow{}, err
	}
	return nightWindow{start: clockOffset(start), end: clockOffset(end)}, nil
}

func clockOffset(t time.Time) time.Duration {
	return time.Duration(t.Hour())*time.Hour +
		time.Duration(t.Minute())*time.Minute +
		time.Duration(t.Second())*time.Second
}

// segmentInterval classifies [start, end) into premium buckets. start/end
// must be expressed as local wall-clock instants in the same consistent
// frame (either both the naive "shift_date + time" frame used for planned
// shifts, or both converted to the merchant's real IANA location for
// worked time entries — segmentInterval itself only reads
// Year/Month/Day/Hour/Minute/Second/Weekday, so it doesn't care which).
// holidayByDate maps local "2006-01-02" dates to whether that date counts
// as a holiday for this merchant.
func segmentInterval(start, end time.Time, night nightWindow, holidayByDate map[string]bool) PremiumSegments {
	var out PremiumSegments
	if !end.After(start) {
		return out
	}

	cur := start
	for cur.Before(end) {
		dayStart := time.Date(cur.Year(), cur.Month(), cur.Day(), 0, 0, 0, 0, cur.Location())
		dayEnd := dayStart.Add(24 * time.Hour)
		segEnd := end
		if dayEnd.Before(segEnd) {
			segEnd = dayEnd
		}

		total := segEnd.Sub(cur)
		nightDur := nightDurationWithinDay(dayStart, cur, segEnd, night)
		dayDur := total - nightDur

		if holidayByDate[dayStart.Format("2006-01-02")] {
			out.HolidaySeconds += int64(total.Seconds())
		}

		if cur.Weekday() == time.Sunday {
			out.SundaySeconds += int64(dayDur.Seconds())
			out.NightSundaySeconds += int64(nightDur.Seconds())
		} else {
			out.NormalSeconds += int64(dayDur.Seconds())
			out.NightSeconds += int64(nightDur.Seconds())
		}

		cur = segEnd
	}

	return out
}

// nightDurationWithinDay returns how much of [segStart, segEnd) — both
// already known to fall within the single calendar day starting at
// dayStart — lies inside the night window.
func nightDurationWithinDay(dayStart, segStart, segEnd time.Time, night nightWindow) time.Duration {
	if night.start <= night.end {
		// Configured without crossing midnight (unusual, but possible).
		return overlap(segStart, segEnd, dayStart.Add(night.start), dayStart.Add(night.end))
	}
	// Normal case: window spans midnight (e.g. 22:00-06:00), so it applies
	// as [00:00, end) and [start, 24:00) of THIS calendar day.
	before := overlap(segStart, segEnd, dayStart, dayStart.Add(night.end))
	after := overlap(segStart, segEnd, dayStart.Add(night.start), dayStart.Add(24*time.Hour))
	return before + after
}

func overlap(aStart, aEnd, bStart, bEnd time.Time) time.Duration {
	start := aStart
	if bStart.After(start) {
		start = bStart
	}
	end := aEnd
	if bEnd.Before(end) {
		end = bEnd
	}
	if end.Before(start) {
		return 0
	}
	return end.Sub(start)
}
