package openinghours

import (
	"sort"
	"time"
)

// Range est une plage d'ouverture datée [Start, End), en UTC.
type Range struct {
	Start time.Time
	End   time.Time
}

// OpenRanges déroule les créneaux hebdomadaires de hours_of_operation en
// plages datées couvrant [from, to), pour la réglette horaire du plan de salle
// (GET /locations?range_from&range_to).
//
// Même lecture des créneaux que reservation.GetOperationRanges : une ligne
// s'applique à chaque jour de [day_of_week_from, day_of_week_to] (avec
// bouclage dimanche → lundi), de hour_from à hour_to ce jour-là. Un créneau
// dont hour_to <= hour_from passe minuit et se termine le lendemain.
//
// Les heures sont évaluées dans loc (fuseau du marchand) ; les plages rendues
// sont rognées à [from, to), fusionnées quand elles se chevauchent ou se
// touchent, triées et exprimées en UTC.
func OpenRanges(slots []Slot, from, to time.Time, loc *time.Location) []Range {
	if loc == nil {
		loc = time.UTC
	}
	if !from.Before(to) {
		return nil
	}

	localFrom := from.In(loc)
	localTo := to.In(loc)
	// On part de la veille : un créneau ouvert la veille et passant minuit
	// (ex. 19:00-01:00) déborde sur le début de la période.
	day := time.Date(localFrom.Year(), localFrom.Month(), localFrom.Day()-1, 0, 0, 0, 0, loc)
	lastDay := time.Date(localTo.Year(), localTo.Month(), localTo.Day(), 0, 0, 0, 0, loc)

	var out []Range
	for !day.After(lastDay) {
		dow := isoWeekday(day)
		for _, s := range slots {
			if !slotCoversDay(s, dow) {
				continue
			}
			fromSec, okFrom := parseClock(s.HourFrom)
			toSec, okTo := parseClock(s.HourTo)
			if !okFrom || !okTo {
				continue
			}

			start := *atClock(day, 0, fromSec)
			end := *atClock(day, 0, toSec)
			if !end.After(start) {
				end = *atClock(day, 1, toSec)
			}
			if start.Before(from) {
				start = from
			}
			if end.After(to) {
				end = to
			}
			if start.Before(end) {
				out = append(out, Range{Start: start.UTC(), End: end.UTC()})
			}
		}
		day = time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, loc)
	}

	return mergeRanges(out)
}

// slotCoversDay : day (1 = lundi ... 7 = dimanche) est-il dans
// [DayOfWeekFrom, DayOfWeekTo], avec bouclage quand from > to (ex. 6 → 1).
func slotCoversDay(s Slot, day int) bool {
	if s.DayOfWeekFrom <= s.DayOfWeekTo {
		return s.DayOfWeekFrom <= day && day <= s.DayOfWeekTo
	}
	return day >= s.DayOfWeekFrom || day <= s.DayOfWeekTo
}

func mergeRanges(ranges []Range) []Range {
	if len(ranges) == 0 {
		return []Range{}
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].Start.Before(ranges[j].Start) })

	merged := []Range{ranges[0]}
	for _, r := range ranges[1:] {
		last := &merged[len(merged)-1]
		if !r.Start.After(last.End) {
			if r.End.After(last.End) {
				last.End = r.End
			}
			continue
		}
		merged = append(merged, r)
	}
	return merged
}
