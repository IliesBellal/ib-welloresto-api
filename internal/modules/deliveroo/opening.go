package deliveroo

import (
	"fmt"
	"strings"
	"time"
)

// Horaires d'ouverture d'un site Deliveroo
// (GET /site/v1/brands/{brand}/sites/{site}/opening_hours) et calcul des
// réveils qui en découlent (docs/deliveroo-menu-sync.md, « Réveils »).
//
// Deliveroo remet « épuisé » en « disponible » quand le site ouvre ; la doc ne
// précise pas si c'est l'heure prévue ou le passage en ligne de la tablette.
// On contrôle donc peu après l'heure prévue, puis on attend que le statut du
// site soit OPEN avant d'aligner les disponibilités.

// OpeningPeriod : une plage d'ouverture, heures locales du site (« 12:00 »).
type OpeningPeriod struct {
	LocalStartTime string `json:"local_start_time"`
	LocalEndTime   string `json:"local_end_time"`
}

// OpeningHours : jour de la semaine en anglais minuscule (monday…sunday) -> plages.
type OpeningHours map[string][]OpeningPeriod

// OpeningHoursResponse : réponse du GET opening_hours.
type OpeningHoursResponse struct {
	OpeningHours OpeningHours `json:"opening_hours"`
}

// TimeZoneResponse : réponse du GET time_zone.
type TimeZoneResponse struct {
	TimeZone string `json:"time_zone"`
}

// SiteStatusResponse : réponse du GET status (OPEN, CLOSED, READY_TO_OPEN).
type SiteStatusResponse struct {
	Status string `json:"status"`
}

const (
	// SiteStatusOpen : le site reçoit des commandes.
	SiteStatusOpen = "OPEN"

	// openingWakeDelay : délai après l'heure d'ouverture prévue avant le contrôle.
	openingWakeDelay = 5 * time.Minute
)

var weekdayNames = [...]string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}

// NextOpeningWake renvoie le premier instant strictement postérieur à after qui
// tombe openingWakeDelay après le début d'une plage d'ouverture, en heure
// locale du site. Cherche sur les 8 prochains jours. Le second résultat est
// false quand les horaires ne donnent aucune ouverture exploitable.
func NextOpeningWake(after time.Time, loc *time.Location, hours OpeningHours) (time.Time, bool) {
	if loc == nil || len(hours) == 0 {
		return time.Time{}, false
	}
	local := after.In(loc)
	for d := 0; d <= 8; d++ {
		day := time.Date(local.Year(), local.Month(), local.Day()+d, 0, 0, 0, 0, loc)
		var best time.Time
		found := false
		for _, p := range hours[weekdayNames[day.Weekday()]] {
			h, m, ok := parseLocalClock(p.LocalStartTime)
			if !ok {
				continue
			}
			cand := time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, loc).Add(openingWakeDelay)
			if !cand.After(after) {
				continue
			}
			if !found || cand.Before(best) {
				best, found = cand, true
			}
		}
		if found {
			return best, true
		}
	}
	return time.Time{}, false
}

// parseLocalClock lit « 12:00 », « 12:00:00 » ou « 2020-11-24 12:00 » (le
// schéma de la doc mélange les formats).
func parseLocalClock(s string) (hour, minute int, ok bool) {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexAny(s, " T"); i >= 0 {
		s = s[i+1:]
	}
	var sec int
	n, _ := fmt.Sscanf(s, "%d:%d:%d", &hour, &minute, &sec)
	if n < 2 || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, false
	}
	return hour, minute, true
}
