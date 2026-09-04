// Package timeutil holds the one definition of "business day" — midnight to
// midnight in an establishment's own timezone — shared by every module that
// buckets orders by local day (stats, analytics). Extracted from
// internal/modules/stats/service.go, which computed the same
// time.Date(...) pattern independently at four call sites
// (GetRevenue/GetOrderCount/GetAverageBasket/GetHourlyData); this package is
// that pattern's single home so a fifth caller (internal/modules/analytics)
// does not duplicate it a fifth time.
package timeutil

import (
	"fmt"
	"time"
)

// LocalDayBounds returns the [start, end) UTC bounds of the local calendar
// day containing refTime, in tz. end is exclusive and computed as the next
// local midnight — NOT start.Add(24*time.Hour): time.Time.Add operates on
// the absolute instant, so on a DST-transition day (23 or 25 real hours
// between one local midnight and the next, e.g. Europe/Paris on
// 2026-03-29/2026-10-25) that shortcut lands an hour off local midnight.
// time.Date, by contrast, is asked for a wall-clock time and resolves the
// correct absolute instant for it under tz's DST rules, so day+1 at 00:00
// is always the true next local midnight regardless of the transition.
// Never replace this with a "<= 23:59:59" bound either — that silently
// drops the last second of the day.
func LocalDayBounds(refTime time.Time, tz *time.Location) (startUTC, endUTC time.Time) {
	inTz := refTime.In(tz)
	start := time.Date(inTz.Year(), inTz.Month(), inTz.Day(), 0, 0, 0, 0, tz)
	end := time.Date(inTz.Year(), inTz.Month(), inTz.Day()+1, 0, 0, 0, 0, tz)
	return start.UTC(), end.UTC()
}

// LocalDayRangeBounds returns the [start, end) UTC bounds spanning every
// local calendar day from fromDate through toDate (both inclusive, in tz).
// Use for a "from/to" period request where both dates are local calendar
// days and the caller wants every order created on toDate included. Same
// next-local-midnight computation as LocalDayBounds, for the same DST reason.
func LocalDayRangeBounds(fromDate, toDate time.Time, tz *time.Location) (startUTC, endUTC time.Time) {
	fromInTz := fromDate.In(tz)
	toInTz := toDate.In(tz)
	start := time.Date(fromInTz.Year(), fromInTz.Month(), fromInTz.Day(), 0, 0, 0, 0, tz)
	end := time.Date(toInTz.Year(), toInTz.Month(), toInTz.Day()+1, 0, 0, 0, 0, tz)
	return start.UTC(), end.UTC()
}

// TZOffset converts a *time.Location to a "+HH:MM"/"-HH:MM" UTC offset
// string at t — the format Postgres's `AT TIME ZONE (?::interval)` and
// MySQL's CONVERT_TZ both expect as a parameter. Same implementation as the
// former stats.GetTZOffset (kept there as a thin alias — see
// stats/repository.go) so analytics does not need to import stats for it.
func TZOffset(tz *time.Location, t time.Time) string {
	_, offset := t.In(tz).Zone()
	hours := offset / 3600
	minutes := (offset % 3600) / 60

	if offset >= 0 {
		return fmt.Sprintf("+%02d:%02d", hours, minutes)
	}
	return fmt.Sprintf("%03d:%02d", hours, minutes)
}
