package ubereats

import (
	"fmt"
	"sort"
	"strings"

	"welloresto-api/internal/modules/openinghours"
)

// Horaires du menu Uber Eats (service_availability) construits depuis les
// horaires d'ouverture de l'établissement (hours_of_operation) — pas depuis les
// horaires de livraison, réservés à ScanNOrder (docs/DELIVERY_HOURS.md).
//
// Uber Eats fixe les heures d'ouverture du restaurant à l'union des
// service_availability de ses menus : un menu « 24 h sur 24 » ouvrirait
// l'établissement en permanence.

var uberWeekdays = []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}

// ServiceAvailability : plages d'un jour de la semaine.
type ServiceAvailability struct {
	DayOfWeek   string       `json:"day_of_week"`
	TimePeriods []TimePeriod `json:"time_periods"`
}

// TimePeriod : plage « HH:MM » - « HH:MM » (fin au plus tard 23:59).
type TimePeriod struct {
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
}

const endOfDay = 24*60 - 1 // 23:59, borne maximale acceptée par Uber Eats

type minuteRange struct{ from, to int }

// ServiceAvailabilityFromSlots convertit les créneaux d'ouverture en plages par
// jour :
//   - un créneau couvre les jours DayOfWeekFrom à DayOfWeekTo avec les mêmes
//     heures (1 = lundi ... 7 = dimanche) ;
//   - un créneau qui passe minuit (fin avant le début, ou fin à 00:00) est coupé :
//     fin de journée jusqu'à 23:59, puis début du jour suivant jusqu'à l'heure de fin ;
//   - les plages qui se chevauchent ou se touchent sont fusionnées.
//
// Renvoie nil quand aucun créneau n'est exploitable.
func ServiceAvailabilityFromSlots(slots []openinghours.Slot) []ServiceAvailability {
	days := make([][]minuteRange, 7)

	for _, s := range slots {
		from, okFrom := parseHHMM(s.HourFrom)
		to, okTo := parseHHMM(s.HourTo)
		if !okFrom || !okTo || s.DayOfWeekFrom < 1 || s.DayOfWeekFrom > 7 || s.DayOfWeekTo < 1 || s.DayOfWeekTo > 7 {
			continue
		}

		// Jours couverts : de DayOfWeekFrom à DayOfWeekTo, en repassant par le
		// lundi si la plage est « à cheval » sur la fin de semaine (ex. 6 → 2).
		covered := []int{}
		for d, n := s.DayOfWeekFrom, 0; n < 7; n++ {
			covered = append(covered, d)
			if d == s.DayOfWeekTo {
				break
			}
			d = d%7 + 1
		}

		for _, d := range covered {
			idx := d - 1
			switch {
			case from == 0 && to == 0: // 00:00 - 00:00 : toute la journée
				days[idx] = append(days[idx], minuteRange{0, endOfDay})
			case to == 0: // fin à minuit : jusqu'à 23:59, rien le lendemain
				days[idx] = append(days[idx], minuteRange{from, endOfDay})
			case to > from:
				days[idx] = append(days[idx], minuteRange{from, to})
			case to < from: // passe minuit
				days[idx] = append(days[idx], minuteRange{from, endOfDay})
				next := d % 7
				days[next] = append(days[next], minuteRange{0, to})
			}
			// to == from (hors minuit) : créneau vide, ignoré.
		}
	}

	var out []ServiceAvailability
	for i, ranges := range days {
		merged := mergeRanges(ranges)
		if len(merged) == 0 {
			continue
		}
		sa := ServiceAvailability{DayOfWeek: uberWeekdays[i]}
		for _, r := range merged {
			sa.TimePeriods = append(sa.TimePeriods, TimePeriod{StartTime: formatHHMM(r.from), EndTime: formatHHMM(r.to)})
		}
		out = append(out, sa)
	}
	return out
}

func mergeRanges(in []minuteRange) []minuteRange {
	if len(in) == 0 {
		return nil
	}
	sort.Slice(in, func(i, j int) bool {
		if in[i].from != in[j].from {
			return in[i].from < in[j].from
		}
		return in[i].to < in[j].to
	})
	out := []minuteRange{in[0]}
	for _, r := range in[1:] {
		last := &out[len(out)-1]
		if r.from <= last.to+1 { // chevauche ou touche
			if r.to > last.to {
				last.to = r.to
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

// parseHHMM lit « HH:MM » ou « HH:MM:SS » en minutes depuis minuit.
func parseHHMM(s string) (int, bool) {
	s = strings.TrimSpace(s)
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
		return 0, false
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

func formatHHMM(minutes int) string {
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}
